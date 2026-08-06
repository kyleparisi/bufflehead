package license

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

func hashKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

// newTestInternal builds an InternalProvider with an injected key, bypassing
// the environment and keychain.
func newTestInternal(cfg Config, key string) *InternalProvider {
	p := NewInternalProvider(cfg)
	p.keyLookup = func() string { return key }
	return p
}

func TestInternalProviderAcceptsAllowlistedKey(t *testing.T) {
	const key = "BUFF-INTERNAL-7Y2K-QW9F-ZZ31"
	cfg := Config{InternalKeyHashes: []string{hashKey("some-other-key"), hashKey(key)}}

	p := newTestInternal(cfg, key)
	ctx := context.Background()

	if !p.Detect(ctx) {
		t.Fatal("Detect = false for an allowlisted key")
	}
	st, err := p.Validate(ctx)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if st.State != StateLicensed {
		t.Errorf("State = %q, want %q", st.State, StateLicensed)
	}
	if st.Plan != "internal" {
		t.Errorf("Plan = %q, want internal", st.Plan)
	}
	if !st.Expires.IsZero() {
		t.Error("internal grants must not expire")
	}
	// The subject is shown in the UI and pasted into support tickets, so it
	// must never contain the key itself.
	if strings.Contains(st.Subject, key) {
		t.Errorf("Subject %q leaks the full internal key", st.Subject)
	}
	if !strings.Contains(st.Subject, "…") {
		t.Errorf("Subject = %q, want a masked key", st.Subject)
	}
}

func TestInternalProviderRejects(t *testing.T) {
	const good = "BUFF-INTERNAL-7Y2K-QW9F-ZZ31"
	cfg := Config{InternalKeyHashes: []string{hashKey(good)}}

	tests := []struct {
		name string
		cfg  Config
		key  string
	}{
		{"wrong key", cfg, "BUFF-INTERNAL-0000-0000-0000"},
		{"empty key", cfg, ""},
		{"whitespace only", cfg, "   "},
		{"key is the hash itself", cfg, hashKey(good)},
		{"prefix of a valid key", cfg, good[:len(good)-1]},
		{"no allowlist configured", Config{}, good},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := newTestInternal(tc.cfg, tc.key)
			if p.Detect(context.Background()) {
				t.Error("Detect = true, want false")
			}
		})
	}
}

// A malformed allowlist entry must be ignored, not crash or match everything.
func TestInternalProviderIgnoresMalformedHashes(t *testing.T) {
	const key = "BUFF-INTERNAL-7Y2K-QW9F-ZZ31"
	cfg := Config{InternalKeyHashes: []string{"not-hex", "aabb", "", hashKey(key)}}

	if !newTestInternal(cfg, key).Detect(context.Background()) {
		t.Error("Detect = false; malformed neighbours must not break a good entry")
	}
	if newTestInternal(cfg, "wrong").Detect(context.Background()) {
		t.Error("Detect = true for a wrong key alongside malformed entries")
	}
}

func TestInternalProviderEmailDomain(t *testing.T) {
	base := Config{InternalEmailDomains: []string{"example.com", "example.co.uk"}}

	tests := []struct {
		name  string
		email string
		ok    bool
		want  bool
	}{
		{"allowlisted domain", "coworker@example.com", true, true},
		{"case insensitive", "CoWorker@Example.COM", true, true},
		{"second domain", "someone@example.co.uk", true, true},
		{"other domain", "someone@gmail.com", true, false},
		// The classic suffix-matching bug: evil-example.com must not pass an
		// example.com allowlist.
		{"lookalike suffix domain", "attacker@evil-example.com", true, false},
		{"subdomain is not the domain", "attacker@mail.example.com", true, false},
		{"domain in the local part", "example.com@evil.net", true, false},
		{"trailing dot", "someone@example.com.", true, false},
		{"no domain", "someone@", true, false},
		{"no local part", "@example.com", true, false},
		{"not an email", "someone", true, false},
		{"identity unavailable", "coworker@example.com", false, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := base
			cfg.Identity = func(context.Context) (string, bool) { return tc.email, tc.ok }
			p := newTestInternal(cfg, "")
			if got := p.Detect(context.Background()); got != tc.want {
				t.Errorf("Detect(%q) = %v, want %v", tc.email, got, tc.want)
			}
		})
	}
}

// With no Identity hook wired the email path must stay completely inert, even
// when a domain allowlist is compiled in.
func TestInternalProviderEmailInertWithoutIdentity(t *testing.T) {
	cfg := Config{InternalEmailDomains: []string{"example.com"}}
	if newTestInternal(cfg, "").Detect(context.Background()) {
		t.Error("Detect = true with no Identity hook configured")
	}
}

func TestMaskKey(t *testing.T) {
	tests := []struct{ in, want string }{
		{"BUFF-INTERNAL-7Y2K-QW9F-ZZ31", "BUFF…ZZ31"},
		{"short", "••••••••"},
		{"", "••••••••"},
	}
	for _, tc := range tests {
		if got := maskKey(tc.in); got != tc.want {
			t.Errorf("maskKey(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestSplitList(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"  ", nil},
		{",,", nil},
		{"AABB", []string{"aabb"}},
		{" a , B ,, c ", []string{"a", "b", "c"}},
	}
	for _, tc := range tests {
		got := splitList(tc.in)
		if len(got) != len(tc.want) {
			t.Errorf("splitList(%q) = %v, want %v", tc.in, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("splitList(%q)[%d] = %q, want %q", tc.in, i, got[i], tc.want[i])
			}
		}
	}
}
