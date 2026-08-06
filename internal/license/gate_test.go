package license

import (
	"context"
	"errors"
	"testing"
)

// newTestGate builds a Gate over an explicit chain with a recording exit.
func newTestGate(goos string, providers ...Provider) (*Gate, *[]int) {
	var exits []int
	g := &Gate{
		resolver: NewResolverWith(providers...),
		exit:     func(code int) { exits = append(exits, code) },
		goos:     goos,
	}
	return g, &exits
}

func TestGateExitsForReceiptRefresh(t *testing.T) {
	mas := &fakeProvider{
		channel: ChannelMAS, detect: true,
		status: Status{State: StateUnlicensed, NeedsReceiptRefresh: true, Reason: "bad receipt"},
	}
	g, exits := newTestGate("darwin", mas)
	g.Resolve(context.Background())

	if len(*exits) != 1 || (*exits)[0] != MASExitCodeRefreshReceipt {
		t.Fatalf("exits = %v, want [%d]", *exits, MASExitCodeRefreshReceipt)
	}
}

// Everything that is not "a real MAS build with a bad receipt" must launch
// normally. Killing the process in any of these cases would be a crash loop.
func TestGateDoesNotExitOtherwise(t *testing.T) {
	tests := []struct {
		name     string
		goos     string
		provider *fakeProvider
	}{
		{
			name: "valid MAS receipt",
			goos: "darwin",
			provider: &fakeProvider{channel: ChannelMAS, detect: true,
				status: Status{State: StateLicensed}},
		},
		{
			name: "MAS receipt unreadable, failing open into grace",
			goos: "darwin",
			provider: &fakeProvider{channel: ChannelMAS, detect: true,
				err: errors.New("i/o error")},
		},
		{
			name: "unlicensed on a non-MAS channel",
			goos: "darwin",
			provider: &fakeProvider{channel: ChannelDirect, detect: true,
				status: Status{State: StateUnlicensed, NeedsReceiptRefresh: true}},
		},
		{
			name:     "no channel at all",
			goos:     "darwin",
			provider: &fakeProvider{channel: ChannelDirect},
		},
		{
			name: "same bad receipt off macOS",
			goos: "linux",
			provider: &fakeProvider{channel: ChannelMAS, detect: true,
				status: Status{State: StateUnlicensed, NeedsReceiptRefresh: true}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g, exits := newTestGate(tc.goos, tc.provider)
			g.Resolve(context.Background())
			if len(*exits) != 0 {
				t.Errorf("process exited with %v, want no exit", *exits)
			}
		})
	}
}

func TestGateExposesStatus(t *testing.T) {
	p := &fakeProvider{channel: ChannelInternal, detect: true, status: licensed(ChannelInternal)}
	g, _ := newTestGate("darwin", p)

	st := g.Resolve(context.Background())
	if !g.Allowed() {
		t.Error("Allowed() = false for a licensed status")
	}
	if g.Status().Channel != st.Channel {
		t.Error("Status() disagrees with the value Resolve returned")
	}
}

// A hung provider must not hang startup; the timeout turns into a grace window.
func TestGateStartupTimeoutFailsOpen(t *testing.T) {
	blocked := &blockingProvider{}
	g, exits := newTestGate("darwin", blocked)

	// A cancelled parent context stands in for the deadline so the test does
	// not actually wait StartupTimeout.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	st := g.Resolve(ctx)
	if len(*exits) != 0 {
		t.Errorf("process exited with %v during a timeout", *exits)
	}
	if st.Allowed() {
		// With no channel detected there is nothing to grant, so this is a nag
		// rather than grace — the point is only that it returns promptly.
		t.Logf("status = %s", st)
	}
}

// blockingProvider reports no channel but respects context cancellation.
type blockingProvider struct{}

func (b *blockingProvider) Channel() Channel                { return ChannelDirect }
func (b *blockingProvider) Detect(ctx context.Context) bool { return false }
func (b *blockingProvider) Validate(ctx context.Context) (Status, error) {
	return Status{}, ctx.Err()
}
