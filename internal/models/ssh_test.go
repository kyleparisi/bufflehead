package models

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestSSHTunnelDefaults(t *testing.T) {
	s := &SSHTunnel{Host: "jump.example.com"}
	if got := s.EffectivePort(); got != 22 {
		t.Errorf("EffectivePort() = %d, want 22", got)
	}
	if s.EffectiveUser() == "" {
		t.Error("EffectiveUser() should fall back to the OS user")
	}

	s = &SSHTunnel{Host: "jump.example.com", Port: 2222, User: "deploy"}
	if got := s.EffectivePort(); got != 2222 {
		t.Errorf("EffectivePort() = %d, want 2222", got)
	}
	if got := s.Describe(); got != "deploy@jump.example.com:2222" {
		t.Errorf("Describe() = %q", got)
	}
}

func TestSSHTunnelValidate(t *testing.T) {
	tests := []struct {
		name    string
		tunnel  SSHTunnel
		wantErr string
	}{
		{"ok agent", SSHTunnel{Host: "jump", User: "me"}, ""},
		{"ok key", SSHTunnel{Host: "jump", User: "me", AuthMethod: SSHAuthKey, KeyPath: "~/.ssh/id_rsa"}, ""},
		{"no host", SSHTunnel{User: "me"}, "host is required"},
		{"blank host", SSHTunnel{Host: "   ", User: "me"}, "host is required"},
		{"bad port", SSHTunnel{Host: "jump", User: "me", Port: 99999}, "out of range"},
		{"key without path", SSHTunnel{Host: "jump", User: "me", AuthMethod: SSHAuthKey}, "key file is required"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.tunnel.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("Validate() = %v, want it to contain %q", err, tc.wantErr)
			}
		})
	}
}

// TestSSHSecretNeverPersisted is the important one: an in-memory passphrase must
// not reach gateway.yaml or bookmarks.json.
func TestSSHSecretNeverPersisted(t *testing.T) {
	entry := GatewayEntry{
		Name: "prod",
		Kind: KindPostgres,
		SSH:  &SSHTunnel{Host: "jump", User: "me", Secret: "super-secret-passphrase"},
	}
	y, err := yaml.Marshal(&GatewayConfig{Gateways: []GatewayEntry{entry}})
	if err != nil {
		t.Fatalf("marshal yaml: %v", err)
	}
	if strings.Contains(string(y), "super-secret-passphrase") {
		t.Errorf("SSH secret leaked into gateway.yaml:\n%s", y)
	}

	bm := Bookmark{Label: "prod", Kind: KindPostgres, SSH: entry.SSH}
	j, err := json.Marshal(bm)
	if err != nil {
		t.Fatalf("marshal json: %v", err)
	}
	if strings.Contains(string(j), "super-secret-passphrase") {
		t.Errorf("SSH secret leaked into bookmarks.json:\n%s", j)
	}
}

func TestSSHTunnelResolveSecret(t *testing.T) {
	// In-memory value wins.
	s := &SSHTunnel{Host: "jump", Secret: "in-memory"}
	if got := s.ResolveSecret("prod"); got != "in-memory" {
		t.Errorf("ResolveSecret() = %q, want the in-memory value", got)
	}

	// Env var, when that's where the secret lives.
	t.Setenv("BUFFLEHEAD_TEST_SSH_PASS", "from-env")
	s = &SSHTunnel{Host: "jump", SecretKind: SecretEnv, SecretEnv: "BUFFLEHEAD_TEST_SSH_PASS"}
	if got := s.ResolveSecret("prod"); got != "from-env" {
		t.Errorf("ResolveSecret() = %q, want %q", got, "from-env")
	}

	// Nothing configured.
	s = &SSHTunnel{Host: "jump"}
	if got := s.ResolveSecret("prod"); got != "" {
		t.Errorf("ResolveSecret() = %q, want empty", got)
	}
}

func TestSSHSecretLabelIsDistinct(t *testing.T) {
	if SSHSecretLabel("prod") == "prod" {
		t.Error("the SSH keychain account must not collide with the database password's")
	}
}

func TestSSHTunnelCloneIsDeep(t *testing.T) {
	orig := &SSHTunnel{Host: "jump", Secret: "s"}
	clone := orig.Clone()
	clone.Host = "other"
	clone.Secret = "t"
	if orig.Host != "jump" || orig.Secret != "s" {
		t.Error("Clone() shares state with the original")
	}
	if (*SSHTunnel)(nil).Clone() != nil {
		t.Error("Clone() of nil should be nil")
	}
	if got := orig.Redacted(); got.Secret != "" || got.Host != "jump" {
		t.Errorf("Redacted() = %+v, want the secret cleared and the rest kept", got)
	}
}

func TestGatewayEntrySSHSupport(t *testing.T) {
	tunnel := &SSHTunnel{Host: "jump", User: "me"}
	tests := []struct {
		kind          ConnKind
		wantSupported bool
	}{
		{KindPostgres, true},
		{KindMySQL, true},
		{KindBigQuery, false},
		{KindAWSGateway, false},
	}
	for _, tc := range tests {
		e := GatewayEntry{Kind: tc.kind, SSH: tunnel}
		if got := e.SupportsSSHTunnel(); got != tc.wantSupported {
			t.Errorf("kind %q: SupportsSSHTunnel() = %v, want %v", tc.kind, got, tc.wantSupported)
		}
		if got := e.UsesSSH(); got != tc.wantSupported {
			t.Errorf("kind %q: UsesSSH() = %v, want %v", tc.kind, got, tc.wantSupported)
		}
	}

	// No tunnel configured, and a tunnel with no host, are both "not using SSH".
	if (&GatewayEntry{Kind: KindPostgres}).UsesSSH() {
		t.Error("UsesSSH() should be false with no tunnel")
	}
	if (&GatewayEntry{Kind: KindPostgres, SSH: &SSHTunnel{}}).UsesSSH() {
		t.Error("UsesSSH() should be false with a hostless tunnel")
	}
}

func TestGatewayEntryDialTarget(t *testing.T) {
	// No tunnel: dial the database host itself.
	e := GatewayEntry{Kind: KindPostgres, RDSHost: "db.internal", RDSPort: 5432}
	host, port, err := e.DialTarget()
	if err != nil || host != "db.internal" || port != 5432 {
		t.Errorf("DialTarget() = %s:%d, %v; want db.internal:5432", host, port, err)
	}

	// Tunnel established: dial its local end.
	e.SSH = &SSHTunnel{Host: "jump"}
	e.LocalPort = 54321
	host, port, err = e.DialTarget()
	if err != nil || host != "127.0.0.1" || port != 54321 {
		t.Errorf("DialTarget() = %s:%d, %v; want 127.0.0.1:54321", host, port, err)
	}

	// An AWS gateway keeps its existing behaviour (its own tunnel, not SSH).
	aws := GatewayEntry{Kind: KindAWSGateway, RDSHost: "rds.amazonaws.com", RDSPort: 5432, LocalPort: 5000, SSH: &SSHTunnel{Host: "jump"}}
	host, port, err = aws.DialTarget()
	if err != nil || host != "rds.amazonaws.com" || port != 5432 {
		t.Errorf("AWS DialTarget() = %s:%d, %v; want the RDS endpoint", host, port, err)
	}
}

// TestDialTargetRefusesUnestablishedTunnel is a regression test for a wrong-
// database connection. When a tunneled entry had no local forward port,
// DialTarget used to fall back to RDSHost:RDSPort. For the common
// `ssh -L ... localhost:5432` setup that names the database "localhost", so the
// fallback silently connected to a database on the user's OWN machine that
// happened to share the address, and queries returned the wrong data with no
// warning. It must be an error instead.
func TestDialTargetRefusesUnestablishedTunnel(t *testing.T) {
	e := GatewayEntry{
		Name:    "zeplo",
		Kind:    KindPostgres,
		RDSHost: "localhost", // as seen from the jump host
		RDSPort: 5432,
		SSH:     &SSHTunnel{Host: "jump.example.com", User: "me"},
		// LocalPort deliberately unset: the tunnel is not up.
	}
	host, port, err := e.DialTarget()
	if err == nil {
		t.Fatalf("DialTarget() = %s:%d, nil; want an error rather than a fallback to the local host", host, port)
	}
	if host != "" || port != 0 {
		t.Errorf("DialTarget() returned %s:%d alongside the error; want no target at all", host, port)
	}
	for _, want := range []string{"zeplo", "jump.example.com", "reconnect"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should mention %q", err, want)
		}
	}
}

func TestGatewayEntryResolveSSHSecret(t *testing.T) {
	e := GatewayEntry{Name: "prod", Kind: KindPostgres}
	if got := e.ResolveSSHSecret(); got != "" {
		t.Errorf("ResolveSSHSecret() with no tunnel = %q, want empty", got)
	}
	e.SSH = &SSHTunnel{Host: "jump", Secret: "pass"}
	if got := e.ResolveSSHSecret(); got != "pass" {
		t.Errorf("ResolveSSHSecret() = %q, want %q", got, "pass")
	}
}

func TestBookmarkSSHRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/bookmarks.json"

	bs := &BookmarkStore{path: path}
	bm := Bookmark{
		Label:   "tunneled",
		Kind:    KindMySQL,
		RDSHost: "db.internal",
		RDSPort: 3306,
		DBName:  "app",
		DBUser:  "readonly",
		SSH: &SSHTunnel{
			Host:       "jump.example.com",
			Port:       2222,
			User:       "deploy",
			AuthMethod: SSHAuthKey,
			KeyPath:    "~/.ssh/id_ed25519",
			SecretKind: SecretKeychain,
			Secret:     "never-persisted",
		},
	}
	if err := bs.Add(bm); err != nil {
		t.Fatalf("Add: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if strings.Contains(string(raw), "never-persisted") {
		t.Errorf("SSH secret written to disk:\n%s", raw)
	}

	reloaded := &BookmarkStore{path: path}
	reloaded.load()
	got := reloaded.FindByLabel("tunneled")
	if got == nil {
		t.Fatal("bookmark not found after reload")
	}
	if got.SSH == nil {
		t.Fatal("SSH tunnel lost on reload")
	}
	if got.SSH.Host != "jump.example.com" || got.SSH.Port != 2222 || got.SSH.User != "deploy" {
		t.Errorf("SSH tunnel = %+v, want the saved jump host", got.SSH)
	}
	if got.SSH.AuthMethod != SSHAuthKey || got.SSH.KeyPath != "~/.ssh/id_ed25519" {
		t.Errorf("SSH auth = %+v, want the saved key settings", got.SSH)
	}
	if got.SSH.Secret != "" {
		t.Error("SSH secret should not survive a reload")
	}

	// The entry handed to the connection pipeline carries the tunnel, deeply
	// copied so mutating it can't corrupt the stored bookmark.
	entry := got.ToGatewayEntry(0)
	if entry.SSH == nil || entry.SSH.Host != "jump.example.com" {
		t.Fatalf("ToGatewayEntry lost the tunnel: %+v", entry.SSH)
	}
	entry.SSH.Host = "mutated"
	if got.SSH.Host != "jump.example.com" {
		t.Error("ToGatewayEntry shares the tunnel with the stored bookmark")
	}

	// A bookmark with no tunnel stays nil rather than gaining an empty one.
	if err := bs.Add(Bookmark{Label: "plain", Kind: KindPostgres}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if e := bs.FindByLabel("plain").ToGatewayEntry(0); e.SSH != nil {
		t.Errorf("ToGatewayEntry invented a tunnel: %+v", e.SSH)
	}
}
