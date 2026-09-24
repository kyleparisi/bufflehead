package control

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// DiscoveryFileName is the file the running app writes so the MCP bridge can
// find it without any per-launch configuration.
const DiscoveryFileName = "control.json"

// Discovery is what the app publishes about its control server. The bearer key
// is included — that is the whole point (the bridge needs it) — so the file is
// written owner-only and removed when the app exits. Multiple instances:
// last writer wins, and each removes only its own file (see
// RemoveDiscoveryIfOwned).
type Discovery struct {
	Addr    string `json:"addr"` // host:port, e.g. 127.0.0.1:54321
	Key     string `json:"key"`
	PID     int    `json:"pid"`
	Version string `json:"version"`
}

// DiscoveryPath returns the discovery file's location under dir.
func DiscoveryPath(dir string) string {
	return filepath.Join(dir, DiscoveryFileName)
}

// WriteDiscovery atomically writes d as dir/control.json with mode 0600.
func WriteDiscovery(dir string, d Discovery) error {
	if d.Addr == "" || d.Key == "" {
		return errors.New("discovery: addr and key are required")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".control-*.json")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	// Write, chmod, close, rename: readers never see a partial or world-readable file.
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, DiscoveryPath(dir)); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}

// ReadDiscovery loads dir/control.json. A missing file is reported with
// os.ErrNotExist so callers can distinguish "not running" from "corrupt".
func ReadDiscovery(dir string) (*Discovery, error) {
	data, err := os.ReadFile(DiscoveryPath(dir))
	if err != nil {
		return nil, err
	}
	var d Discovery
	if err := json.Unmarshal(data, &d); err != nil {
		return nil, fmt.Errorf("discovery: parse %s: %w", DiscoveryPath(dir), err)
	}
	if d.Addr == "" || d.Key == "" {
		return nil, fmt.Errorf("discovery: %s is missing addr or key", DiscoveryPath(dir))
	}
	return &d, nil
}

// RemoveDiscoveryIfOwned deletes the discovery file only when it names pid —
// so an instance that exits after a newer one launched does not yank the
// newer one's file out from under the bridge. Missing file is not an error.
func RemoveDiscoveryIfOwned(dir string, pid int) error {
	d, err := ReadDiscovery(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		// Unreadable/corrupt: nothing we can safely claim as ours.
		return err
	}
	if d.PID != pid {
		return nil
	}
	if err := os.Remove(DiscoveryPath(dir)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
