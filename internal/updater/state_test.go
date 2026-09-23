package updater

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestAutomaticCheckIsSilentUnlessAvailable(t *testing.T) {
	for _, tc := range []struct {
		name  string
		u     *Update
		err   error
		phase Phase
		open  bool
	}{
		{"up to date", nil, nil, UpToDate, false},
		{"error", nil, errors.New("offline"), Failed, false},
		{"available", &Update{}, nil, Available, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var s State
			if !s.StartCheck(false) || s.Open {
				t.Fatalf("start: %+v", s)
			}
			s.CheckDone(tc.u, tc.err)
			if s.Phase != tc.phase || s.Open != tc.open {
				t.Fatalf("got %v open=%v", s.Phase, s.Open)
			}
		})
	}
}

func TestManualCheckShowsEveryOutcome(t *testing.T) {
	var s State
	if !s.StartCheck(true) || !s.Open || s.Phase != Checking {
		t.Fatalf("start: %+v", s)
	}
	s.CheckDone(nil, nil)
	if s.Phase != UpToDate || !s.Open {
		t.Fatalf("got %+v", s)
	}
	s.Dismiss()
	if !s.StartCheck(true) || s.Phase != Checking {
		t.Fatalf("recheck after up to date: %+v", s)
	}
}

func TestManualDuringAutomaticCheckPromotes(t *testing.T) {
	var s State
	s.StartCheck(false)
	if s.StartCheck(true) {
		t.Fatal("started a second concurrent check")
	}
	s.CheckDone(nil, errors.New("offline"))
	if !s.Open || s.Err != "offline" {
		t.Fatalf("got %+v", s)
	}
}

func TestDownloadFlow(t *testing.T) {
	var s State
	if s.StartDownload() {
		t.Fatal("download without a release")
	}
	s.StartCheck(false)
	s.CheckDone(&Update{}, nil)
	if s.StartCheck(true) || !s.Open {
		t.Fatal("manual check should reopen a known update, not re-query")
	}
	if !s.StartDownload() || s.StartDownload() {
		t.Fatal("download should start exactly once")
	}
	s.Dismiss()
	s.DownloadDone("", "", errors.New("sha mismatch"))
	if s.Phase != Failed || !s.Open || s.Update == nil {
		t.Fatalf("failed download: %+v", s)
	}
	if !s.StartDownload() {
		t.Fatal("retry refused")
	}
	s.DownloadDone("/tmp/x.dmg", "", nil)
	if s.Phase != Ready || s.Staged != "/tmp/x.dmg" || !s.Open {
		t.Fatalf("ready: %+v", s)
	}
	s.CheckDone(nil, nil) // stale result must not clobber Ready
	if s.Phase != Ready {
		t.Fatalf("stale check changed phase to %v", s.Phase)
	}
}

func TestPresetVersion(t *testing.T) {
	cfg := []byte("[preset.1.options]\napplication/version=\"0.29.0\"\n  application/short_version=\"0.29.0\"\n")
	if v := PresetVersion(cfg); v != "0.29.0" {
		t.Fatalf("got %q", v)
	}
	if v := PresetVersion([]byte("nothing")); v != "" {
		t.Fatalf("got %q", v)
	}
	data, err := os.ReadFile("../../graphics/export_presets.cfg")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := version(PresetVersion(data)); err != nil {
		t.Fatalf("repo presets: %v", err)
	}
}

func TestStageNamed(t *testing.T) {
	a := asset("Bufflehead-0.30.0-macOS.dmg", "payload")
	dir := filepath.Join(t.TempDir(), "updates")
	os.MkdirAll(dir, 0o700)
	os.WriteFile(filepath.Join(dir, a.Name), []byte("old"), 0o600)
	path, err := client(200, "payload").StageNamed(context.Background(), a, dir)
	if err != nil || path != filepath.Join(dir, a.Name) {
		t.Fatalf("got %q, %v", path, err)
	}
	if data, _ := os.ReadFile(path); string(data) != "payload" {
		t.Fatalf("content %q", data)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Fatalf("left %d files", len(entries))
	}
	for _, name := range []string{"../evil.dmg", ".hidden", ""} {
		bad := a
		bad.Name = name
		if _, err := client(200, "payload").StageNamed(context.Background(), bad, dir); err == nil {
			t.Fatalf("accepted %q", name)
		}
	}
}

func TestInstallFlow(t *testing.T) {
	var s State
	if s.StartInstall() {
		t.Fatal("install with nothing staged")
	}
	s.StartCheck(false)
	s.CheckDone(&Update{}, nil)
	s.StartDownload()
	s.DownloadDone("/tmp/x.dmg", "", nil)
	if !s.StartInstall() || s.StartInstall() {
		t.Fatal("install should start exactly once")
	}
	s.Dismiss()
	if !s.Open {
		t.Fatal("dismissed while installing")
	}
	s.InstallDone(errors.New("signature check failed"))
	if s.Phase != Failed || s.Staged == "" {
		t.Fatalf("failed install: %+v", s)
	}
	if s.StartDownload() || s.StartInstall() {
		t.Fatal("a failed install should fall back to the installer, not retry")
	}
	s = State{Phase: Installing, Staged: "/tmp/x.dmg", Update: &Update{}}
	s.InstallDone(nil)
	if s.Phase != Restarting {
		t.Fatalf("got %v", s.Phase)
	}
	s.Dismiss()
	if !s.Open {
		t.Fatal("dismissed while restarting")
	}
}

func TestBlockedInstallOffersInstaller(t *testing.T) {
	s := State{Phase: Downloading, Update: &Update{}}
	s.DownloadDone("/tmp/x.dmg", "not an installed app", nil)
	if s.Phase != Ready || s.Blocker == "" || s.StartInstall() {
		t.Fatalf("blocked install started: %+v", s)
	}
}

func TestCheckDuringInstallKeepsState(t *testing.T) {
	s := State{Phase: Installing, Staged: "/tmp/x.dmg", Update: &Update{}, Open: true}
	if s.StartCheck(true) || s.Phase != Installing || s.Staged == "" {
		t.Fatalf("check clobbered install: %+v", s)
	}
}
