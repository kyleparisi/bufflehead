package license

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"os"
	"strings"
)

// internalKeyEnv is the environment variable a coworker can set to supply their
// internal key without storing it. Also how CI runs the app unrestricted.
const internalKeyEnv = "BUFFLEHEAD_INTERNAL_KEY"

// internalKeychainLabel is the OS keychain entry holding a stored internal key.
const internalKeychainLabel = "bufflehead-internal-key"

// InternalProvider grants unrestricted access to coworkers. It is checked
// before every other channel and overrides them: a coworker running a Mac App
// Store build, a Setapp build, or an unlicensed direct build all get full
// access.
//
// Two credentials are supported, in this order:
//
//  1. An internal license key, matched against a build-time allowlist of
//     SHA-256 digests. Keys are issued manually and never expire.
//  2. A verified company email, matched against a domain allowlist.
//
// Today only the key path is live. The email path needs a trustworthy identity
// step, and Bufflehead does not have one: the only sign-in in the app is the
// user's own AWS SSO session, which authenticates them to *their* AWS account
// against an IdP they choose. Anyone can point it at an IdP that asserts
// whatever address they like, so it cannot carry an entitlement. Config.Identity
// is the seam to fill in if a real Bufflehead account system lands later; until
// something sets it, the email allowlist is inert.
//
// # Why this provider detects by credential
//
// Every other provider detects its channel structurally — a file the installer
// wrote, a runtime the host provides. There is no structural signal for "this
// person works here", so possession of a valid internal credential is itself
// the channel signal, and Detect does the same check Validate does. That is
// safe in the direction that matters: an invalid or absent internal key means
// "not the internal channel" and the chain moves on to the real one, so a
// customer who fat-fingers a key is never trapped on a channel they do not
// belong to.
type InternalProvider struct {
	cfg Config
	// keyLookup resolves a candidate internal key from the environment. Split
	// out so tests do not have to touch the process environment or keychain.
	keyLookup func() string
}

// NewInternalProvider builds the internal/coworker provider.
func NewInternalProvider(cfg Config) *InternalProvider {
	return &InternalProvider{cfg: cfg.withDefaults(), keyLookup: lookupInternalKey}
}

// Channel implements Provider.
func (p *InternalProvider) Channel() Channel { return ChannelInternal }

// Detect implements Provider. It reports true only when a credential actually
// matches the allowlist — see the type doc for why detection and validation
// coincide here.
func (p *InternalProvider) Detect(ctx context.Context) bool {
	if _, ok := p.matchKey(); ok {
		return true
	}
	_, ok := p.matchEmail(ctx)
	return ok
}

// Validate implements Provider. Internal grants never expire and never touch
// the network, so this is always conclusive.
func (p *InternalProvider) Validate(ctx context.Context) (Status, error) {
	if subject, ok := p.matchKey(); ok {
		return Status{
			Channel: ChannelInternal,
			State:   StateLicensed,
			Plan:    "internal",
			Subject: subject,
			Reason:  "Internal build — licensed via coworker key.",
		}, nil
	}
	if email, ok := p.matchEmail(ctx); ok {
		return Status{
			Channel: ChannelInternal,
			State:   StateLicensed,
			Plan:    "internal",
			Subject: email,
			Reason:  "Internal build — licensed via company account.",
		}, nil
	}
	// Only reachable if the credential vanished between Detect and Validate.
	return Status{
		Channel: ChannelInternal,
		State:   StateUnlicensed,
		Reason:  "Internal credential is no longer valid.",
	}, nil
}

// matchKey checks a candidate key against the build-time digest allowlist and
// returns a masked form of it for display.
func (p *InternalProvider) matchKey() (string, bool) {
	key := strings.TrimSpace(p.keyLookup())
	if key == "" || len(p.cfg.InternalKeyHashes) == 0 {
		return "", false
	}

	sum := sha256.Sum256([]byte(key))
	got := sum[:]

	// Compare against every entry without short-circuiting, so timing does not
	// leak which entry matched or how many entries there are.
	matched := 0
	for _, want := range p.cfg.InternalKeyHashes {
		wantRaw, err := hex.DecodeString(want)
		if err != nil || len(wantRaw) != len(got) {
			continue
		}
		matched |= subtle.ConstantTimeCompare(got, wantRaw)
	}
	if matched != 1 {
		return "", false
	}
	return "internal:" + maskKey(key), true
}

// matchEmail checks the configured identity against the company domain
// allowlist. Inert while Config.Identity is nil.
func (p *InternalProvider) matchEmail(ctx context.Context) (string, bool) {
	if p.cfg.Identity == nil || len(p.cfg.InternalEmailDomains) == 0 {
		return "", false
	}
	email, ok := p.cfg.Identity(ctx)
	if !ok {
		return "", false
	}
	email = strings.ToLower(strings.TrimSpace(email))
	at := strings.LastIndex(email, "@")
	if at < 1 || at == len(email)-1 {
		return "", false
	}
	domain := email[at+1:]
	for _, want := range p.cfg.InternalEmailDomains {
		// Exact domain match only. Suffix matching would let evil-example.com
		// through an "example.com" allowlist.
		if domain == want {
			return email, true
		}
	}
	return "", false
}

// lookupInternalKey resolves an internal key from the environment, then from
// the OS keychain entry a coworker may have saved.
func lookupInternalKey() string {
	if v := strings.TrimSpace(os.Getenv(internalKeyEnv)); v != "" {
		return v
	}
	return storedInternalKey()
}

// maskKey renders a key for display without disclosing it: first four and last
// four characters, everything else elided. Short keys are fully elided.
func maskKey(key string) string {
	const keep = 4
	if len(key) < 2*keep+4 {
		return strings.Repeat("•", 8)
	}
	return key[:keep] + "…" + key[len(key)-keep:]
}
