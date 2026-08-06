package license

import (
	"context"
	"errors"
)

// The three channels below are scaffolded so the detection chain has its real
// shape and ordering today, but none of them is implemented yet.
//
// Every one of them reports Detect == false on purpose, and that choice is
// load-bearing rather than lazy. A provider that detected its channel but could
// not conclusively validate would hit the resolver's fail-open path and be
// granted a grace window. For OfflineFileProvider in particular that would be a
// live privilege-escalation bug: anyone could `touch` a file at
// /Library/Application Support/bufflehead/license.plist and be handed two weeks
// of free access, which is precisely the spoofing this channel has to defend
// against. A provider here starts detecting only in the same change that lands
// its verification.

// ErrProviderNotImplemented is returned by a scaffolded provider's Validate.
var ErrProviderNotImplemented = errors.New("license: provider not implemented")

// SetappProvider will entitle copies running under the Setapp umbrella.
//
// Detection is authoritative and cheap once wired: Setapp's SDK exposes a
// runtime check (isRunningUnderSetapp) backed by the Setapp agent and the
// bundle's provenance, so there is no false-positive risk. The work is that the
// SDK is an Objective-C framework, so this needs a cgo shim in the Godot host
// alongside a Setapp-specific build of the app.
type SetappProvider struct{ cfg Config }

// NewSetappProvider builds the (not yet implemented) Setapp provider.
func NewSetappProvider(cfg Config) *SetappProvider { return &SetappProvider{cfg: cfg.withDefaults()} }

// Channel implements Provider.
func (p *SetappProvider) Channel() Channel { return ChannelSetapp }

// Detect implements Provider. Always false until the SDK shim lands.
func (p *SetappProvider) Detect(ctx context.Context) bool { return false }

// Validate implements Provider.
func (p *SetappProvider) Validate(ctx context.Context) (Status, error) {
	return Status{Channel: ChannelSetapp}, ErrProviderNotImplemented
}

// OfflineFileProvider will entitle corporate MDM deployments from a signed
// license file that Kandji or Jamf drops via a configuration profile.
//
// This is the one channel with a real spoofing threat: the file sits at a fixed
// path on disk and its presence is the only channel signal. So detection must
// mean "a file is there whose Ed25519 signature verifies against a key pinned
// in this binary", and the payload must carry an issuer field (for example
// "kandji-mdm"), the licensed organisation, and an expiry — signed as one
// canonical blob so no field can be edited independently. File-existence alone
// must never entitle anything.
type OfflineFileProvider struct{ cfg Config }

// NewOfflineFileProvider builds the (not yet implemented) MDM provider.
func NewOfflineFileProvider(cfg Config) *OfflineFileProvider {
	return &OfflineFileProvider{cfg: cfg.withDefaults()}
}

// Channel implements Provider.
func (p *OfflineFileProvider) Channel() Channel { return ChannelMDM }

// Detect implements Provider. Always false until signature verification lands —
// see the note at the top of this file for why it must not merely stat().
func (p *OfflineFileProvider) Detect(ctx context.Context) bool { return false }

// Validate implements Provider.
func (p *OfflineFileProvider) Validate(ctx context.Context) (Status, error) {
	return Status{Channel: ChannelMDM}, ErrProviderNotImplemented
}

// RemoteAPIProvider will entitle self-distribution installs by exchanging a
// user-entered license key with our /validate endpoint, then caching a
// short-lived Ed25519-signed token so the app keeps working offline for the
// grace window before requiring re-validation.
//
// As the last link in the chain it is also the one that must not over-claim: it
// detects only when the user has actually entered a key or a cached token
// exists, so a fresh install with no key falls off the end of the chain and
// gets the nag screen rather than a spurious channel match.
type RemoteAPIProvider struct{ cfg Config }

// NewRemoteAPIProvider builds the (not yet implemented) direct-sale provider.
func NewRemoteAPIProvider(cfg Config) *RemoteAPIProvider {
	return &RemoteAPIProvider{cfg: cfg.withDefaults()}
}

// Channel implements Provider.
func (p *RemoteAPIProvider) Channel() Channel { return ChannelDirect }

// Detect implements Provider. Always false until the key store and token cache
// land.
func (p *RemoteAPIProvider) Detect(ctx context.Context) bool { return false }

// Validate implements Provider.
func (p *RemoteAPIProvider) Validate(ctx context.Context) (Status, error) {
	return Status{Channel: ChannelDirect}, ErrProviderNotImplemented
}
