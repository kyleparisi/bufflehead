package license

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// MASExitCodeRefreshReceipt is the exit status a Mac App Store build must use
// when its receipt is missing or invalid.
//
// This is not a nag case. macOS treats exit(173) from an App Store binary as
// "this copy needs a receipt": it prompts for the user's Apple ID, obtains a
// receipt, writes it into the bundle, and relaunches. An App Store build that
// instead showed a nag screen would never get a receipt, and would also fail
// App Review — the reviewer's first launch is precisely the no-receipt case.
const MASExitCodeRefreshReceipt = 173

// receiptRelPath is where the App Store installer writes the receipt inside the
// bundle, relative to the .app root.
var receiptRelPath = filepath.Join("Contents", "_MASReceipt", "receipt")

// StoreKitProvider entitles Mac App Store installs by validating the receipt
// Apple's installer places in the app bundle.
//
// # Why the receipt file and not the StoreKit 2 API
//
// "StoreKit 2 receipt validation" names two different mechanisms, and only one
// of them is reachable from this app:
//
//   - The receipt at Contents/_MASReceipt/receipt is a PKCS#7 blob written by
//     the App Store at install time. Verifying it is pure computation over
//     bytes on disk — no Apple frameworks, no cgo, works from Go.
//   - StoreKit 2's AppTransaction/Transaction are JWS objects obtained by
//     calling async Swift APIs on macOS 12+. Reaching them from this codebase
//     means a cgo bridge into a Swift/Objective-C shim linked into the Godot
//     host, plus an async call at startup.
//
// Both are authoritative for "did this user get this app from the App Store".
// For a paid-up-front app the receipt file answers the entitlement question
// completely, so this provider uses it and stays cgo-free. StoreKit 2 becomes
// necessary if in-app purchases or subscriptions are ever added, since those
// transactions are not in the app receipt; the seam for that is Validate, which
// can consult a JWS entitlement before falling back to the receipt.
//
// Detection is structural: the file exists only because Apple's installer put
// it there, so its presence cannot be produced by an ordinary direct-download
// user, and this provider claiming the process is safe.
type StoreKitProvider struct {
	cfg Config
	// deviceGUID returns the raw bytes the receipt's device hash is computed
	// over. Injectable so tests do not depend on the host's NICs.
	deviceGUID func() ([]byte, error)
	// now is injectable for tests.
	now func() time.Time
	// goos is the platform this provider believes it is running on. It exists
	// so the darwin-only guard in Detect can be exercised from a Linux CI box;
	// production always leaves it at runtime.GOOS.
	goos string
}

// NewStoreKitProvider builds the Mac App Store provider.
func NewStoreKitProvider(cfg Config) *StoreKitProvider {
	return &StoreKitProvider{
		cfg:        cfg.withDefaults(),
		deviceGUID: primaryMACAddress,
		now:        time.Now,
		goos:       runtime.GOOS,
	}
}

// Channel implements Provider.
func (p *StoreKitProvider) Channel() Channel { return ChannelMAS }

// receiptPath returns the absolute path to the bundled receipt, or "" when the
// process is not running from a macOS app bundle.
func (p *StoreKitProvider) receiptPath() string {
	root := p.cfg.bundleRoot()
	if root == "" {
		return ""
	}
	return filepath.Join(root, receiptRelPath)
}

// Detect implements Provider. It reports whether a receipt file is present —
// structural evidence of an App Store install — without reading or validating
// it.
func (p *StoreKitProvider) Detect(ctx context.Context) bool {
	// The receipt is a macOS-only artifact. Checking the path on other
	// platforms would let a crafted directory tree claim the MAS channel.
	if p.goos != "darwin" {
		return false
	}
	path := p.receiptPath()
	if path == "" {
		return false
	}
	fi, err := os.Stat(path)
	return err == nil && fi.Mode().IsRegular() && fi.Size() > 0
}

// Validate implements Provider.
//
// A conclusive negative — the receipt is present but forged, is for another
// app, or was issued for another Mac — returns a StateUnlicensed Status with a
// nil error and NeedsReceiptRefresh set, so the caller exits 173 and lets macOS
// re-issue it. Inconclusive failures (no trust anchor compiled in, an expired
// Apple intermediate) return an error so the resolver fails open.
func (p *StoreKitProvider) Validate(ctx context.Context) (Status, error) {
	path := p.receiptPath()
	if path == "" {
		return Status{}, errors.New("storekit: not running from an app bundle")
	}

	blob, err := os.ReadFile(path)
	if err != nil {
		// Detect saw the file, so this is a transient I/O problem, not a
		// verdict. Fail open.
		return Status{}, fmt.Errorf("storekit: read receipt: %w", err)
	}

	roots, err := poolFrom(p.cfg.AppleRootCAs)
	if err != nil {
		// Shipped without an anchor: our bug, not the customer's.
		return Status{}, err
	}

	verified, err := verifyPKCS7(blob, roots, p.now())
	if err != nil {
		if errors.Is(err, errChainExpired) {
			// Apple's own intermediate lapsing must not lock anyone out.
			return Status{}, err
		}
		return p.unlicensed("This App Store receipt could not be verified."), nil
	}

	r, err := parseReceipt(verified.Content)
	if err != nil {
		return p.unlicensed("This App Store receipt could not be read."), nil
	}

	if r.BundleID != p.cfg.BundleID {
		return p.unlicensed(fmt.Sprintf(
			"This receipt is for %s, not Bufflehead.", r.BundleID)), nil
	}
	if p.cfg.BundleVersion != "" && r.AppVersion != p.cfg.BundleVersion {
		return p.unlicensed("This receipt is for a different version of Bufflehead."), nil
	}

	guid, err := p.deviceGUID()
	if err != nil {
		// We could not read a MAC address, so the binding check is impossible
		// to run either way. Inconclusive.
		return Status{}, fmt.Errorf("storekit: device identity: %w", err)
	}
	if err := r.verifyDeviceHash(guid); err != nil {
		return p.unlicensed("This App Store receipt was issued for a different Mac."), nil
	}

	st := Status{
		Channel: ChannelMAS,
		State:   StateLicensed,
		Plan:    "mas",
		Subject: "mas:" + r.BundleID + "@" + r.OriginalVersion,
		Reason:  "Licensed via the Mac App Store.",
	}
	// Paid-up-front apps have no expiration; a dated receipt means a
	// subscription or a time-limited grant, so honour it.
	if !r.Expiration.IsZero() {
		st.Expires = r.Expiration
	}
	return st, nil
}

// unlicensed builds a conclusive negative that also asks macOS for a fresh
// receipt.
func (p *StoreKitProvider) unlicensed(reason string) Status {
	return Status{
		Channel:             ChannelMAS,
		State:               StateUnlicensed,
		Reason:              reason,
		NeedsReceiptRefresh: true,
	}
}
