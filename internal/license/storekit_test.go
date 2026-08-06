package license

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

var testGUID = []byte{0xAC, 0xDE, 0x48, 0x00, 0x11, 0x22}

const (
	testBundleID = "com.kyleparisi.bufflehead"
	testVersion  = "1.4.0"
)

// newTestStoreKit writes a receipt into a fake .app bundle and returns a
// provider wired to it.
func newTestStoreKit(t *testing.T, receipt []byte, roots [][]byte, at time.Time) *StoreKitProvider {
	t.Helper()

	bundle := filepath.Join(t.TempDir(), "Bufflehead.app")
	if err := os.MkdirAll(filepath.Join(bundle, "Contents", "_MASReceipt"), 0o755); err != nil {
		t.Fatalf("mkdir bundle: %v", err)
	}
	if receipt != nil {
		if err := os.WriteFile(filepath.Join(bundle, receiptRelPath), receipt, 0o644); err != nil {
			t.Fatalf("write receipt: %v", err)
		}
	}

	p := NewStoreKitProvider(Config{
		BundleID:      testBundleID,
		BundleVersion: testVersion,
		BundlePath:    bundle,
		AppleRootCAs:  roots,
	})
	p.deviceGUID = func() ([]byte, error) { return testGUID, nil }
	p.now = func() time.Time { return at }
	p.goos = "darwin"
	return p
}

// validReceipt builds a well-formed receipt plus the root that signs it.
func validReceipt(t *testing.T, p receiptParams, opts signOpts, notBefore, notAfter time.Time) ([]byte, [][]byte) {
	t.Helper()
	chain := newTestChain(t, notBefore, notAfter)
	payload := buildPayload(t, p)
	return buildPKCS7(t, chain, payload, opts), [][]byte{chain.rootDER}
}

func defaultParams() receiptParams {
	return receiptParams{
		BundleID:        testBundleID,
		AppVersion:      testVersion,
		OriginalVersion: "1.0.0",
		Opaque:          []byte("opaque-value-1234"),
		GUID:            testGUID,
	}
}

func TestStoreKitValidatesGenuineReceipt(t *testing.T) {
	now := time.Now()
	blob, roots := validReceipt(t, defaultParams(), signOpts{}, now.Add(-time.Hour), now.Add(24*time.Hour))
	p := newTestStoreKit(t, blob, roots, now)

	if !p.Detect(context.Background()) {
		t.Fatal("Detect = false, want true for a bundle containing a receipt")
	}

	st, err := p.Validate(context.Background())
	if err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
	if st.State != StateLicensed {
		t.Errorf("State = %q, want %q (reason: %s)", st.State, StateLicensed, st.Reason)
	}
	if !st.Allowed() {
		t.Error("Allowed() = false, want true")
	}
	if st.Channel != ChannelMAS {
		t.Errorf("Channel = %q, want %q", st.Channel, ChannelMAS)
	}
	if !st.Expires.IsZero() {
		t.Errorf("Expires = %v, want zero for a paid-up-front receipt", st.Expires)
	}
	if st.NeedsReceiptRefresh {
		t.Error("NeedsReceiptRefresh = true on a valid receipt")
	}
}

func TestStoreKitHonoursReceiptExpiration(t *testing.T) {
	now := time.Now()
	params := defaultParams()
	params.Expiration = now.Add(48 * time.Hour)

	blob, roots := validReceipt(t, params, signOpts{}, now.Add(-time.Hour), now.Add(72*time.Hour))
	st, err := newTestStoreKit(t, blob, roots, now).Validate(context.Background())
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if st.Expires.IsZero() {
		t.Fatal("Expires is zero, want the receipt's expiration date")
	}
	if st.Expired(now) {
		t.Error("Expired(now) = true for a receipt valid another 48h")
	}
	if !st.Expired(now.Add(72 * time.Hour)) {
		t.Error("Expired() = false past the receipt expiration")
	}
}

// The rejection cases below must all be *conclusive*: a nil error plus an
// unlicensed status. If any of them returned an error instead, the resolver
// would fail open and hand a forged receipt a two-week grace window.
func TestStoreKitRejectsBadReceipts(t *testing.T) {
	now := time.Now()
	past, future := now.Add(-time.Hour), now.Add(24*time.Hour)

	tests := []struct {
		name    string
		receipt func(t *testing.T) ([]byte, [][]byte)
	}{
		{
			name: "payload tampered after signing",
			receipt: func(t *testing.T) ([]byte, [][]byte) {
				return validReceipt(t, defaultParams(),
					signOpts{TamperPayloadAfterSigning: true}, past, future)
			},
		},
		{
			name: "signed by an untrusted root",
			receipt: func(t *testing.T) ([]byte, [][]byte) {
				blob, _ := validReceipt(t, defaultParams(), signOpts{}, past, future)
				// Hand the verifier a different root than the one that signed.
				other := newTestChain(t, past, future)
				return blob, [][]byte{other.rootDER}
			},
		},
		{
			name: "chain missing its intermediate",
			receipt: func(t *testing.T) ([]byte, [][]byte) {
				return validReceipt(t, defaultParams(),
					signOpts{OmitIntermediate: true}, past, future)
			},
		},
		{
			name: "more than one signer",
			receipt: func(t *testing.T) ([]byte, [][]byte) {
				return validReceipt(t, defaultParams(), signOpts{TwoSigners: true}, past, future)
			},
		},
		{
			name: "receipt for a different app",
			receipt: func(t *testing.T) ([]byte, [][]byte) {
				p := defaultParams()
				p.BundleID = "com.someone.else"
				return validReceipt(t, p, signOpts{}, past, future)
			},
		},
		{
			name: "receipt for a different version",
			receipt: func(t *testing.T) ([]byte, [][]byte) {
				p := defaultParams()
				p.AppVersion = "9.9.9"
				return validReceipt(t, p, signOpts{}, past, future)
			},
		},
		{
			name: "receipt issued for another Mac",
			receipt: func(t *testing.T) ([]byte, [][]byte) {
				p := defaultParams()
				p.GUID = []byte{0x00, 0x11, 0x22, 0x33, 0x44, 0x55}
				return validReceipt(t, p, signOpts{}, past, future)
			},
		},
		{
			name: "device hash does not match its own contents",
			receipt: func(t *testing.T) ([]byte, [][]byte) {
				p := defaultParams()
				p.CorruptDeviceHash = true
				return validReceipt(t, p, signOpts{}, past, future)
			},
		},
		{
			name: "not a PKCS#7 structure at all",
			receipt: func(t *testing.T) ([]byte, [][]byte) {
				chain := newTestChain(t, past, future)
				return []byte("this is not a receipt"), [][]byte{chain.rootDER}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			blob, roots := tc.receipt(t)
			st, err := newTestStoreKit(t, blob, roots, now).Validate(context.Background())
			if err != nil {
				t.Fatalf("Validate returned error %v; a bad receipt must be a conclusive "+
					"unlicensed verdict, not an inconclusive one that fails open", err)
			}
			if st.State != StateUnlicensed {
				t.Errorf("State = %q, want %q", st.State, StateUnlicensed)
			}
			if st.Allowed() {
				t.Error("Allowed() = true for an invalid receipt")
			}
			if !st.NeedsReceiptRefresh {
				t.Error("NeedsReceiptRefresh = false; a MAS build must exit 173 to get a new receipt")
			}
		})
	}
}

// An expired Apple chain must be inconclusive, not a rejection — this is the
// February 2023 WWDR expiry scenario.
func TestStoreKitExpiredChainFailsOpen(t *testing.T) {
	now := time.Now()
	blob, roots := validReceipt(t, defaultParams(), signOpts{},
		now.Add(-72*time.Hour), now.Add(-48*time.Hour))

	p := newTestStoreKit(t, blob, roots, now)
	_, err := p.Validate(context.Background())
	if err == nil {
		t.Fatal("Validate returned no error for an expired chain; it must be inconclusive")
	}
	if !errors.Is(err, errChainExpired) {
		t.Fatalf("error = %v, want it to wrap errChainExpired", err)
	}

	// End to end: the resolver turns that into a grace window, not a lockout.
	r := NewResolverWith(p)
	st := r.Resolve(context.Background())
	if st.State != StateGrace {
		t.Errorf("State = %q, want %q", st.State, StateGrace)
	}
	if !st.Allowed() {
		t.Error("Allowed() = false; an expired Apple intermediate must not lock out a customer")
	}
}

// A build shipped without its trust anchor is our bug, so it must fail open.
func TestStoreKitMissingAnchorFailsOpen(t *testing.T) {
	now := time.Now()
	blob, _ := validReceipt(t, defaultParams(), signOpts{}, now.Add(-time.Hour), now.Add(time.Hour))

	// An empty-but-non-nil root list falls through to the embedded anchors,
	// which are absent in this tree.
	p := newTestStoreKit(t, blob, nil, now)
	_, err := p.Validate(context.Background())
	if err == nil {
		t.Skip("an Apple anchor is embedded in this tree, so this path cannot be exercised")
	}
	if !errors.Is(err, ErrNoAnchors) {
		t.Fatalf("error = %v, want ErrNoAnchors", err)
	}
	if st := NewResolverWith(p).Resolve(context.Background()); !st.Allowed() {
		t.Error("Allowed() = false; a missing anchor is our bug and must not lock out a customer")
	}
}

func TestStoreKitDetect(t *testing.T) {
	now := time.Now()
	blob, roots := validReceipt(t, defaultParams(), signOpts{}, now.Add(-time.Hour), now.Add(time.Hour))

	t.Run("no receipt file", func(t *testing.T) {
		p := newTestStoreKit(t, nil, roots, now)
		if p.Detect(context.Background()) {
			t.Error("Detect = true with no receipt present")
		}
	})

	t.Run("empty receipt file", func(t *testing.T) {
		p := newTestStoreKit(t, []byte{}, roots, now)
		if p.Detect(context.Background()) {
			t.Error("Detect = true for a zero-byte receipt")
		}
	})

	t.Run("not running on macOS", func(t *testing.T) {
		p := newTestStoreKit(t, blob, roots, now)
		p.goos = "linux"
		if p.Detect(context.Background()) {
			t.Error("Detect = true off darwin; a crafted directory must not claim the MAS channel")
		}
	})

	t.Run("not in an app bundle", func(t *testing.T) {
		p := NewStoreKitProvider(Config{BundlePath: t.TempDir()})
		p.goos = "darwin"
		if p.Detect(context.Background()) {
			t.Error("Detect = true outside an app bundle")
		}
	})
}

func TestBundleRootFor(t *testing.T) {
	tests := []struct {
		exe  string
		want string
	}{
		{"/Applications/Bufflehead.app/Contents/MacOS/bufflehead", "/Applications/Bufflehead.app"},
		{"/Users/me/Bufflehead.APP/Contents/MacOS/x", "/Users/me/Bufflehead.APP"},
		{"/usr/local/bin/bufflehead", ""},
		{"/Applications/Bufflehead.app/Contents/Resources/x", ""},
		{"/Applications/Bufflehead/Contents/MacOS/x", ""},
		{"bufflehead", ""},
	}
	for _, tc := range tests {
		if got := bundleRootFor(tc.exe); got != tc.want {
			t.Errorf("bundleRootFor(%q) = %q, want %q", tc.exe, got, tc.want)
		}
	}
}

// TestAppleAnchorPresent asserts the shipped trust anchor is sane once someone
// runs bin/fetch-apple-root. It skips while the anchor is absent so the suite
// stays green in a tree that has not fetched it yet.
func TestAppleAnchorPresent(t *testing.T) {
	pool, err := appleRootPool()
	if errors.Is(err, ErrNoAnchors) {
		t.Skip("no Apple trust anchor embedded yet — run bin/fetch-apple-root")
	}
	if err != nil {
		t.Fatalf("loading embedded anchors failed: %v", err)
	}
	if pool == nil || len(pool.Subjects()) == 0 { //nolint:staticcheck // pool introspection is test-only
		t.Fatal("anchor pool is empty")
	}
}
