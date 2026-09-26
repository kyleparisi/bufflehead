//go:build !mas

package updater

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// In-place install (macOS only for now). Prepare does every step that can fail
// while the app is still running — locate the installed bundle, mount the DMG,
// verify the new app's signature, notarization, identity and version, copy it
// beside the installed one. Launch then starts a detached helper that waits for
// this process to exit, swaps the bundles with two renames (keeping a backup)
// and reopens the app. The relaunched app calls FinishInstall to drop the backup.

const (
	bundleID = "com.kyleparisi.bufflehead"
	teamID   = "63GMD6U4J2" // Developer ID the release DMG is signed with
	appName  = "Bufflehead.app"
)

// codeRequirement accepts only Developer ID code signed by our team.
const codeRequirement = `anchor apple generic and certificate 1[field.1.2.840.113635.100.6.2.6] and certificate leaf[field.1.2.840.113635.100.6.1.13] and certificate leaf[subject.OU] = "` + teamID + `"`

// InstallSupported reports whether this platform installs in place. Elsewhere
// the UI hands the verified download to the user.
func InstallSupported() bool { return runtime.GOOS == "darwin" }

// command runs an external tool; tests replace it.
var command = func(ctx context.Context, name string, args ...string) ([]byte, error) {
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("%s: %w: %s", name, err, strings.TrimSpace(string(out)))
	}
	return out, nil
}

// Pending is a prepared install waiting for the app to exit.
type Pending struct {
	App    string // installed bundle, replaced in place
	New    string // verified copy of the new bundle, beside App
	Backup string // where App is moved during the swap
	Log    string
}

func siblings(app string) (newApp, backup string) {
	dir := filepath.Dir(app)
	return filepath.Join(dir, ".Bufflehead-update.app"), filepath.Join(dir, ".Bufflehead-backup.app")
}

// installedBundle maps the running executable to its .app bundle and rejects
// locations that can't be replaced. Errors are user-facing.
func installedBundle(exe string) (string, error) {
	app := filepath.Dir(filepath.Dir(filepath.Dir(exe)))
	if filepath.Base(filepath.Dir(exe)) != "MacOS" || filepath.Ext(app) != ".app" {
		return "", errNotInstalled
	}
	if strings.Contains(app, "/AppTranslocation/") || strings.HasPrefix(app, "/Volumes/") {
		return "", errors.New("Bufflehead is running from a disk image or a quarantined download, so it can't replace itself. Move it to Applications, reopen it, then update.")
	}
	return app, nil
}

var errNotInstalled = errors.New("This copy of Bufflehead isn't an installed app (for example a development build), so it can't replace itself.")

func runningBundle() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		return "", err
	}
	return installedBundle(exe)
}

// InstallBlocker explains why this copy can't install in place, or returns ""
// when it can. It is cheap (no DMG access), so the UI can offer the right
// action before the user commits to installing.
func InstallBlocker(ctx context.Context) string {
	if !InstallSupported() {
		return "unsupported"
	}
	app, err := runningBundle()
	if err == nil {
		err = checkInstallable(ctx, app)
	}
	if err != nil {
		return err.Error()
	}
	return ""
}

// checkInstallable verifies app really is Bufflehead (never, say, the Godot
// editor running a dev build) and that its folder is writable.
func checkInstallable(ctx context.Context, app string) error {
	if id, err := plistValue(ctx, app, "CFBundleIdentifier"); err != nil || id != bundleID {
		return errNotInstalled
	}
	probe, err := os.CreateTemp(filepath.Dir(app), ".bufflehead-write-*")
	if err != nil {
		return fmt.Errorf("Bufflehead can't write to %s, so it can't replace itself. Open the installer and drag Bufflehead into place yourself.", filepath.Dir(app))
	}
	probe.Close()
	os.Remove(probe.Name())
	return nil
}

func plistValue(ctx context.Context, app, key string) (string, error) {
	out, err := command(ctx, "plutil", "-extract", key, "raw", "-o", "-", filepath.Join(app, "Contents", "Info.plist"))
	return strings.TrimSpace(string(out)), err
}

// verifyApp checks the bundle's signature (deep, strict, our Team ID),
// Gatekeeper acceptance (notarized), identity and version.
func verifyApp(ctx context.Context, app, version string) error {
	if _, err := command(ctx, "codesign", "--verify", "--deep", "--strict", "-R="+codeRequirement, app); err != nil {
		return fmt.Errorf("signature check failed: %w", err)
	}
	if _, err := command(ctx, "spctl", "--assess", "--type", "execute", app); err != nil {
		return fmt.Errorf("Gatekeeper rejected the update: %w", err)
	}
	if id, err := plistValue(ctx, app, "CFBundleIdentifier"); err != nil || id != bundleID {
		return fmt.Errorf("update has bundle identifier %q, expected %q", id, bundleID)
	}
	if v, err := plistValue(ctx, app, "CFBundleShortVersionString"); err != nil || v != strings.TrimPrefix(version, "v") {
		return fmt.Errorf("update is version %q, expected %q", v, strings.TrimPrefix(version, "v"))
	}
	return nil
}

// PrepareInstall verifies the staged DMG for release tag and copies its app
// beside the installed one. Nothing installed is modified.
func PrepareInstall(ctx context.Context, dmg, tag string) (*Pending, error) {
	if !InstallSupported() {
		return nil, errors.ErrUnsupported
	}
	app, err := runningBundle()
	if err != nil {
		return nil, err
	}
	return prepareInstall(ctx, app, dmg, tag)
}

func prepareInstall(ctx context.Context, app, dmg, tag string) (*Pending, error) {
	if err := checkInstallable(ctx, app); err != nil {
		return nil, err
	}

	mnt, err := os.MkdirTemp("", "bufflehead-mnt-")
	if err != nil {
		return nil, err
	}
	defer os.Remove(mnt)
	if _, err := command(ctx, "hdiutil", "attach", "-nobrowse", "-readonly", "-noautoopen", "-mountpoint", mnt, dmg); err != nil {
		return nil, err
	}
	defer command(context.Background(), "hdiutil", "detach", "-force", mnt)

	// Verify the copy, not the DMG: it's what gets installed, and reading the
	// compressed image once (ditto) is much faster than verifying it in place.
	newApp, backup := siblings(app)
	if err := os.RemoveAll(newApp); err != nil {
		return nil, err
	}
	if _, err := command(ctx, "ditto", filepath.Join(mnt, appName), newApp); err != nil {
		os.RemoveAll(newApp)
		return nil, err
	}
	if err := verifyApp(ctx, newApp, tag); err != nil {
		os.RemoveAll(newApp)
		return nil, err
	}
	logPath := filepath.Join(filepath.Dir(dmg), "install.log")
	return &Pending{App: app, New: newApp, Backup: backup, Log: logPath}, nil
}

// swapScript waits for the app to exit, swaps bundles with a backup, and
// reopens whichever bundle ended up installed. Arguments: pid app new backup
// [opener] (tests pass a no-op opener).
const swapScript = `#!/bin/sh
pid=$1 app=$2 new=$3 bak=$4 open=${5:-open}
rm -f "$0" # sh keeps the open file; nothing to clean up later
echo "$(date) install: waiting for $pid"
i=0
while kill -0 "$pid" 2>/dev/null; do
  sleep 0.2; i=$((i+1))
  if [ $i -gt 300 ]; then echo "app did not exit; aborting"; rm -rf "$new"; exit 1; fi
done
rm -rf "$bak"
if ! mv "$app" "$bak"; then echo "backup failed"; rm -rf "$new"; $open "$app"; exit 1; fi
if mv "$new" "$app"; then
  echo "installed"
else
  echo "swap failed; restoring"; mv "$bak" "$app"
fi
$open "$app"
`

// Launch starts the detached swap helper for process pid. The caller must exit
// promptly afterwards; the helper gives up (and discards the copy) after 60s.
func (p *Pending) Launch(pid int) error {
	script, err := os.CreateTemp("", "bufflehead-install-*.sh")
	if err != nil {
		return err
	}
	if _, err := script.WriteString(swapScript); err != nil {
		script.Close()
		return err
	}
	script.Close()
	logf, err := os.OpenFile(p.Log, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer logf.Close()
	cmd := exec.Command("/bin/sh", script.Name(), strconv.Itoa(pid), p.App, p.New, p.Backup)
	cmd.Stdout, cmd.Stderr = logf, logf
	detach(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// FinishInstall runs at startup: if a previous install left a backup beside the
// running bundle, this launch is the new version, so drop the backup and the
// downloaded packages. It reports whether an install just completed.
func FinishInstall() bool {
	if !InstallSupported() {
		return false
	}
	app, err := runningBundle()
	if err != nil {
		return false
	}
	_, backup := siblings(app)
	if _, err := os.Stat(backup); err != nil {
		return false
	}
	os.RemoveAll(backup)
	if dir, err := StageDir(); err == nil {
		matches, _ := filepath.Glob(filepath.Join(dir, "Bufflehead-*"))
		for _, m := range matches {
			os.Remove(m)
		}
	}
	return true
}
