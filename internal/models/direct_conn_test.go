package models

import "testing"

func directBookmark(label string) Bookmark {
	return Bookmark{
		Label:      label,
		Kind:       KindPostgres,
		Env:        "dev",
		RDSHost:    "localhost",
		RDSPort:    5432,
		DBName:     "postgres",
		DBUser:     "postgres",
		SSLMode:    "require",
		SecretKind: SecretEnv,
	}
}

// TestDirectBookmarkRoundtrip verifies the new direct-Postgres fields persist
// and reload intact.
func TestDirectBookmarkRoundtrip(t *testing.T) {
	bs := newTestStore(t)
	if err := bs.Add(directBookmark("local-dev")); err != nil {
		t.Fatalf("Add: %v", err)
	}

	reloaded := &BookmarkStore{path: bs.Path()}
	reloaded.load()

	got := reloaded.FindByLabel("local-dev")
	if got == nil {
		t.Fatal("local-dev not found after reload")
	}
	if got.Kind != KindPostgres {
		t.Errorf("Kind = %q, want %q", got.Kind, KindPostgres)
	}
	if got.SSLMode != "require" {
		t.Errorf("SSLMode = %q, want require", got.SSLMode)
	}
	if got.SecretKind != SecretEnv {
		t.Errorf("SecretKind = %q, want env", got.SecretKind)
	}
}

// TestToGatewayEntryDirect verifies the bookmark→entry mapping carries the
// direct-connection fields.
func TestToGatewayEntryDirect(t *testing.T) {
	bm := directBookmark("vpn-db")
	entry := bm.ToGatewayEntry(0)

	if !entry.IsDirect() {
		t.Error("entry should report IsDirect() for a KindPostgres bookmark")
	}
	if entry.RDSHost != "localhost" || entry.RDSPort != 5432 {
		t.Errorf("host/port mismatch: %s:%d", entry.RDSHost, entry.RDSPort)
	}
	if entry.SSLMode != "require" {
		t.Errorf("SSLMode = %q, want require", entry.SSLMode)
	}
}

// TestEffectiveSSLModeDefault verifies the empty sslmode falls back to prefer.
func TestEffectiveSSLModeDefault(t *testing.T) {
	e := GatewayEntry{Kind: KindPostgres}
	if got := e.EffectiveSSLMode(); got != "prefer" {
		t.Errorf("EffectiveSSLMode() = %q, want prefer", got)
	}
	e.SSLMode = "disable"
	if got := e.EffectiveSSLMode(); got != "disable" {
		t.Errorf("EffectiveSSLMode() = %q, want disable", got)
	}
}

// TestAWSGatewayIsNotDirect guards the backward-compat default: an empty Kind
// (existing saved configs) must still be treated as an AWS gateway.
func TestAWSGatewayIsNotDirect(t *testing.T) {
	e := GatewayEntry{} // zero value, Kind == KindAWSGateway
	if e.IsDirect() {
		t.Error("zero-value entry should not be direct (AWS gateway default)")
	}
}

// TestResolveSecretEnv verifies env-var secret resolution.
func TestResolveSecretEnv(t *testing.T) {
	t.Setenv("BF_TEST_PGPASS", "hunter2")
	if got := ResolveSecret(SecretEnv, "any", "BF_TEST_PGPASS"); got != "hunter2" {
		t.Errorf("ResolveSecret(env) = %q, want hunter2", got)
	}
	if got := ResolveSecret(SecretNone, "any", "BF_TEST_PGPASS"); got != "" {
		t.Errorf("ResolveSecret(none) = %q, want empty", got)
	}
}
