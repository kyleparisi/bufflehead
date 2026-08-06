package license

import (
	"context"
	"fmt"
	"os"
	"time"
)

// DefaultGrace is how long a copy keeps working after validation last
// succeeded but can no longer be confirmed. Applies to the fail-open path and
// as the default for cached remote validations.
const DefaultGrace = 14 * 24 * time.Hour

// Resolver walks a fixed provider chain and produces a Status.
//
// The zero Resolver is not usable; build one with NewResolver.
type Resolver struct {
	// providers is the ordered detection chain. Order is the contract:
	// internal first (it overrides everything), then the two channels with
	// authoritative structural signals, then MDM, then direct sale as the
	// fallback.
	providers []Provider

	// now is injectable for tests.
	now func() time.Time
}

// NewResolver builds the standard provider chain in detection order:
//
//  0. InternalProvider   — coworker access, overrides everything
//  1. StoreKitProvider   — Mac App Store (_MASReceipt), structural
//  2. SetappProvider     — Setapp runtime, structural
//  3. OfflineFileProvider— corporate MDM signed license file
//  4. RemoteAPIProvider  — self-distribution, /validate + cached token
//
// Pass a Config to point the providers at the right bundle IDs, key
// allowlists and API endpoints.
func NewResolver(cfg Config) *Resolver {
	cfg = cfg.withDefaults()
	return &Resolver{
		providers: []Provider{
			NewInternalProvider(cfg),
			NewStoreKitProvider(cfg),
			NewSetappProvider(cfg),
			NewOfflineFileProvider(cfg),
			NewRemoteAPIProvider(cfg),
		},
		now: time.Now,
	}
}

// NewResolverWith builds a Resolver over an explicit provider chain. Used by
// tests and by any caller that needs to shrink the chain (for example, a
// Windows build that has no MAS or Setapp channel).
func NewResolverWith(providers ...Provider) *Resolver {
	return &Resolver{providers: providers, now: time.Now}
}

// DetectProvider returns the first provider in the chain whose channel signal
// is present. It performs no entitlement checks — see the package doc for why
// detection and validation are separate.
//
// It returns ErrNoChannel when nothing claimed the process, which is the normal
// case for a fresh self-distribution install before the user has entered a key.
func (r *Resolver) DetectProvider(ctx context.Context) (Provider, error) {
	for _, p := range r.providers {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if p.Detect(ctx) {
			return p, nil
		}
	}
	return nil, ErrNoChannel
}

// Resolve detects the channel and validates entitlement, applying fail-open
// policy. It never returns an error: the Status is always usable, and any
// diagnostic is carried in Status.Diag.
//
// Fail-open policy, in order of precedence:
//
//   - No channel detected → unlicensed, nag. Nothing was spoofed and nothing
//     failed; the user simply has not entered a key yet.
//   - Channel detected, validation conclusive → that verdict stands, licensed
//     or not. A forged MDM license file is rejected outright; it does not get
//     a grace window.
//   - Channel detected, validation inconclusive (network down, unreadable
//     file, missing trust anchor) → grace, because locking out a paying
//     customer is worse than a few days of unverified use. The grace deadline
//     comes from the provider when it has a cached token, otherwise from
//     DefaultGrace.
func (r *Resolver) Resolve(ctx context.Context) Status {
	now := r.now()

	p, err := r.DetectProvider(ctx)
	if err != nil {
		return Status{
			Channel: ChannelUnknown,
			State:   StateUnlicensed,
			Reason:  "No license found. Enter a license key to unlock Bufflehead.",
			Diag:    err,
		}
	}

	st, err := p.Validate(ctx)
	st.Channel = p.Channel()

	if err != nil {
		// Inconclusive: fail open into a grace window rather than locking out
		// someone who has already paid.
		if st.GraceUntil.IsZero() {
			st.GraceUntil = now.Add(DefaultGrace)
		}
		if now.Before(st.GraceUntil) {
			st.State = StateGrace
			if st.Reason == "" {
				st.Reason = fmt.Sprintf(
					"Could not confirm your license (%v). Bufflehead keeps working until %s.",
					err, st.GraceUntil.Format("Jan 2"))
			}
		} else {
			st.State = StateUnlicensed
			if st.Reason == "" {
				st.Reason = "Your license could not be confirmed and the offline grace period has expired."
			}
		}
		st.Diag = err
		return st
	}

	// Conclusive verdict. Enforce expiry centrally so no provider can forget to.
	if st.State == StateLicensed && st.Expired(now) {
		st.State = StateUnlicensed
		st.Reason = "Your license expired on " + st.Expires.Format("Jan 2, 2006") + "."
	}
	if st.Reason == "" {
		switch st.State {
		case StateLicensed:
			st.Reason = "Licensed via " + string(st.Channel) + "."
		default:
			st.Reason = "This copy of Bufflehead is not licensed."
		}
	}
	return st
}

// Config carries the per-channel settings the providers need. Most fields have
// build-time defaults injected via -ldflags (see vars.go); Config exists so
// tests and the control API can override them without touching globals.
type Config struct {
	// BundleID is the app's CFBundleIdentifier, checked against the Mac App
	// Store receipt. A receipt for a different bundle must not validate.
	BundleID string
	// BundleVersion is the CFBundleShortVersionString to check against the
	// receipt. Empty skips the version check.
	BundleVersion string

	// BundlePath overrides the detected .app bundle root. Empty means derive
	// it from the running executable. Tests set this.
	BundlePath string

	// AppleRootCAs are the pinned trust anchors for Mac App Store receipt
	// verification, PEM or DER encoded. Empty means load the anchors embedded
	// in the binary (see anchors.go).
	AppleRootCAs [][]byte

	// InternalKeyHashes is the allowlist of internal/coworker license keys,
	// stored as lowercase hex SHA-256 digests — never the keys themselves, so
	// `strings` on the shipped binary does not hand out coworker access.
	InternalKeyHashes []string
	// InternalEmailDomains is the company email domain allowlist, e.g.
	// {"example.com"}. Only consulted when Identity is set.
	InternalEmailDomains []string
	// Identity returns the signed-in user's verified email, if the app has an
	// identity step. Nil when it does not — which is the case today, so the
	// email path stays inert and internal access runs on keys.
	Identity func(context.Context) (email string, ok bool)

	// LicenseFilePath is where an MDM drops the signed corporate license.
	LicenseFilePath string
	// LicenseSigningKeys are the Ed25519 public keys trusted to sign MDM
	// license files and cached remote tokens.
	LicenseSigningKeys [][]byte

	// ValidateURL is our own /validate endpoint for self-distribution keys.
	ValidateURL string
	// CacheDir is where the short-lived signed token is cached. Empty means
	// the app config dir.
	CacheDir string
	// Grace is how long a cached remote validation stays good offline.
	Grace time.Duration
}

func (c Config) withDefaults() Config {
	if c.BundleID == "" {
		c.BundleID = defaultBundleID
	}
	if c.BundleVersion == "" {
		c.BundleVersion = defaultBundleVersion
	}
	if len(c.InternalKeyHashes) == 0 {
		c.InternalKeyHashes = splitList(internalKeyHashes)
	}
	if len(c.InternalEmailDomains) == 0 {
		c.InternalEmailDomains = splitList(internalEmailDomains)
	}
	if c.LicenseFilePath == "" {
		c.LicenseFilePath = defaultLicenseFilePath
	}
	if c.ValidateURL == "" {
		c.ValidateURL = defaultValidateURL
	}
	if c.Grace == 0 {
		c.Grace = DefaultGrace
	}
	return c
}

// bundleRoot returns the .app bundle directory for the running process, i.e.
// the parent of Contents/. It returns "" when the executable is not inside a
// bundle, which is the normal case for `go test` and `gd run`.
func (c Config) bundleRoot() string {
	if c.BundlePath != "" {
		return c.BundlePath
	}
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return bundleRootFor(exe)
}
