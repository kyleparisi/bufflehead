//go:build !mas

package updater

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Phase is where the update flow currently stands.
type Phase int

const (
	Idle        Phase = iota
	Checking          // release lookup in flight
	UpToDate          // latest stable release is not newer
	Available         // newer release found, not downloaded
	Downloading       // asset download + verification in flight
	Ready             // verified asset staged on disk
	Installing        // verifying + copying the new app, helper not yet launched
	Restarting        // helper launched; the app must quit now
	Failed            // check, download or install failed (Err holds why)
)

func (p Phase) String() string {
	return [...]string{"idle", "checking", "up-to-date", "available", "downloading", "ready", "installing", "restarting", "failed"}[p]
}

// State is the update flow's single source of truth. The UI renders it; events
// change it only through the methods below, which are pure and never touch I/O.
type State struct {
	Phase  Phase
	Manual bool    // the user asked for this check, so "up to date" and errors are shown
	Open   bool    // whether the update modal is visible
	Update *Update // the newer release, once found
	Staged string  // verified download path, once Ready
	// Blocker explains why this copy can't install in place ("" = it can);
	// the UI then offers the installer instead. Set with the download result.
	Blocker string
	Err     string
}

// StartCheck reports whether a release lookup should be started. A manual
// request while an update is already known just reopens the modal.
func (s *State) StartCheck(manual bool) bool {
	switch s.Phase {
	case Checking, Downloading:
		if manual {
			s.Manual, s.Open = true, true
		}
		return false
	case Available, Ready, Installing, Restarting:
		if manual {
			s.Open = true
		}
		return false
	}
	*s = State{Phase: Checking, Manual: manual, Open: manual}
	return true
}

// CheckDone records a lookup result. Automatic checks stay silent unless an
// update is available.
func (s *State) CheckDone(u *Update, err error) {
	if s.Phase != Checking {
		return
	}
	switch {
	case err != nil:
		s.Phase, s.Err = Failed, err.Error()
	case u == nil:
		s.Phase = UpToDate
	default:
		s.Phase, s.Update = Available, u
	}
	s.Open = s.Manual || s.Phase == Available
}

// StartDownload reports whether a download should be started. It is allowed
// from Available, or from Failed as a retry when nothing is staged yet.
func (s *State) StartDownload() bool {
	if s.Update == nil || s.Staged != "" || (s.Phase != Available && s.Phase != Failed) {
		return false
	}
	s.Phase, s.Err = Downloading, ""
	return true
}

// DownloadDone records a download result, and whether (blocker == "") this
// copy can install it in place. It surfaces the result even if the modal was
// dismissed while the download ran.
func (s *State) DownloadDone(path, blocker string, err error) {
	if s.Phase != Downloading {
		return
	}
	if err != nil {
		s.Phase, s.Err = Failed, err.Error()
	} else {
		s.Phase, s.Staged, s.Blocker = Ready, path, blocker
	}
	s.Open = true
}

// StartInstall reports whether an in-place install should be started. Only
// from Ready, and only when nothing blocks it: install errors don't go away on
// retry, so a failed install falls back to the installer.
func (s *State) StartInstall() bool {
	if s.Phase != Ready || s.Staged == "" || s.Blocker != "" {
		return false
	}
	s.Phase, s.Err, s.Open = Installing, "", true
	return true
}

// InstallDone records whether the helper was launched; on success the app
// must quit so the helper can swap bundles.
func (s *State) InstallDone(err error) {
	if s.Phase != Installing {
		return
	}
	if err != nil {
		s.Phase, s.Err = Failed, err.Error()
	} else {
		s.Phase = Restarting
	}
	s.Open = true
}

// Dismiss hides the modal without cancelling in-flight work. Installing and
// Restarting can't be dismissed: the app is about to quit.
func (s *State) Dismiss() {
	if s.Phase != Installing && s.Phase != Restarting {
		s.Open = false
	}
}

// StageDir is the per-user directory updates are downloaded into.
func StageDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "Bufflehead", "updates"), nil
}

// StageNamed downloads and verifies a into dir under the asset's own name, so
// the OS recognises it (.dmg, .exe, .AppImage). An existing file is replaced.
func (c Client) StageNamed(ctx context.Context, a Asset, dir string) (string, error) {
	if a.Name == "" || filepath.Base(a.Name) != a.Name || strings.HasPrefix(a.Name, ".") {
		return "", errors.New("unsafe asset name")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	tmp, err := c.Stage(ctx, a, dir)
	if err != nil {
		return "", err
	}
	dest := filepath.Join(dir, a.Name)
	mode := os.FileMode(0o600)
	if runtime.GOOS == "linux" {
		mode = 0o700 // AppImages run in place
	}
	if err := os.Chmod(tmp, mode); err != nil {
		os.Remove(tmp)
		return "", err
	}
	os.Remove(dest)
	if err := os.Rename(tmp, dest); err != nil {
		os.Remove(tmp)
		return "", err
	}
	return dest, nil
}
