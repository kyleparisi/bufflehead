// Package license resolves what the running copy of Bufflehead is entitled to,
// across every channel we ship through: internal/coworker builds, the Mac App
// Store, Setapp, corporate MDM deployments, and direct self-distribution.
//
// One binary serves all of them. Which channel is in play is decided at runtime
// by DetectProvider, which walks the providers in a fixed order and stops at the
// first one whose channel signal is present.
//
// # Detection and validation are deliberately separate
//
// Provider.Detect answers "are we running under this channel?" and
// Provider.Validate answers "is this copy entitled?". They are separate calls
// because collapsing them is unsafe: if a torn or unreadable Mac App Store
// receipt caused the chain to fall through to the next provider, a legitimately
// broken MAS install would be re-examined as if it were an MDM or direct-sale
// install. Detection uses structural, hard-to-forge signals (a receipt file the
// installer wrote, the Setapp runtime); once a channel claims the process, its
// verdict is final and the chain stops. A detected-but-unlicensed channel yields
// an unlicensed Status, never a fallthrough.
package license

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Channel identifies the distribution channel a running copy came from.
type Channel string

const (
	// ChannelInternal is a coworker/company build entitled by an internal key
	// or a company email identity. Checked first; overrides every other channel.
	ChannelInternal Channel = "internal"
	// ChannelMAS is a Mac App Store install, entitled by its _MASReceipt.
	ChannelMAS Channel = "mas"
	// ChannelSetapp is a copy running under the Setapp umbrella.
	ChannelSetapp Channel = "setapp"
	// ChannelMDM is a corporate deployment entitled by a signed license file
	// dropped by an MDM (Kandji, Jamf) configuration profile.
	ChannelMDM Channel = "mdm"
	// ChannelDirect is self-distribution, entitled by a license key the user
	// entered and our /validate API confirmed.
	ChannelDirect Channel = "direct"
	// ChannelUnknown is used when no provider claimed the process.
	ChannelUnknown Channel = "unknown"
)

// State is the entitlement verdict for a resolved channel.
type State string

const (
	// StateLicensed means the copy is fully entitled right now.
	StateLicensed State = "licensed"
	// StateGrace means we could not reach the authority (offline, API down)
	// but a previously cached validation is still within its grace window.
	// Full access; the app should re-validate before GraceUntil.
	StateGrace State = "grace"
	// StateUnlicensed means the channel was identified but the copy is not
	// entitled. The app nags rather than blocking — see Status.Allowed.
	StateUnlicensed State = "unlicensed"
)

// Status is the result of resolving entitlement. The zero value is an
// unlicensed status on an unknown channel.
type Status struct {
	// Channel is the provider that claimed the process.
	Channel Channel
	// State is the entitlement verdict.
	State State
	// Plan is the channel-specific product tier, e.g. "internal" or "pro".
	// Empty when unlicensed.
	Plan string
	// Subject is an opaque, non-secret identifier safe to show in an About
	// window or paste into a support ticket: a masked license key, a receipt
	// digest, a company email. Never a raw license key.
	Subject string
	// Expires is when the entitlement lapses. Zero means it never expires
	// (internal keys, Mac App Store, Setapp).
	Expires time.Time
	// GraceUntil is set when State is StateGrace: the deadline for getting a
	// fresh validation before access drops to StateUnlicensed.
	GraceUntil time.Time
	// Reason is a short human-readable explanation, suitable for the nag
	// screen or an About window. Always set.
	Reason string
	// Diag carries a non-fatal diagnostic from validation — a network error, an
	// unreadable file. It explains a StateGrace or a fail-open StateUnlicensed
	// and is for logs and support, not for gating access.
	Diag error

	// NeedsReceiptRefresh is set only by the Mac App Store path, when the
	// bundled receipt is missing or did not validate. A MAS build must respond
	// by exiting with MASExitCodeRefreshReceipt so macOS issues a fresh receipt
	// and relaunches — showing a nag screen instead would strand the user with
	// no way to ever obtain one. Ignored on every other channel.
	NeedsReceiptRefresh bool
}

// Allowed reports whether the app should run unrestricted. Grace counts as
// allowed: the whole point of the grace window is that a customer who is
// offline, or whose validation server is down, keeps working.
func (s Status) Allowed() bool {
	return s.State == StateLicensed || s.State == StateGrace
}

// ShouldNag reports whether the app should show the unlicensed nag screen.
func (s Status) ShouldNag() bool { return !s.Allowed() }

// Expired reports whether a dated entitlement has lapsed as of now. Statuses
// with a zero Expires never expire.
func (s Status) Expired(now time.Time) bool {
	return !s.Expires.IsZero() && now.After(s.Expires)
}

// String renders a one-line summary for logs and the control API.
func (s Status) String() string {
	out := fmt.Sprintf("%s/%s", s.Channel, s.State)
	if s.Plan != "" {
		out += " plan=" + s.Plan
	}
	if s.Subject != "" {
		out += " subject=" + s.Subject
	}
	if !s.Expires.IsZero() {
		out += " expires=" + s.Expires.UTC().Format(time.RFC3339)
	}
	if !s.GraceUntil.IsZero() {
		out += " grace_until=" + s.GraceUntil.UTC().Format(time.RFC3339)
	}
	return out
}

// Provider resolves entitlement for exactly one distribution channel.
//
// Implementations must be safe for concurrent use and must not block
// indefinitely; Validate is given a context and is expected to honour it.
type Provider interface {
	// Channel reports which channel this provider speaks for.
	Channel() Channel

	// Detect reports whether this channel is the one in play. It must be
	// cheap, side-effect free, and must answer only the question "are we
	// running under this channel?" — never "is the user licensed?". A
	// provider that conflates the two turns a broken entitlement into a
	// silent fallthrough to the next channel.
	//
	// InternalProvider is the documented exception: there is no structural
	// signal for "this is a coworker", so possession of a valid internal
	// credential is itself the channel signal. See its doc comment.
	Detect(ctx context.Context) bool

	// Validate determines entitlement. It is only called when Detect
	// returned true, so it may assume its channel's preconditions hold.
	//
	// A returned error means validation was inconclusive (network failure,
	// unreadable file) — the caller applies fail-open handling. A definite
	// negative verdict is reported as a StateUnlicensed Status with a nil
	// error, not as an error.
	Validate(ctx context.Context) (Status, error)
}

// ErrNoChannel is returned by DetectProvider when no provider claimed the
// process.
var ErrNoChannel = errors.New("license: no distribution channel detected")
