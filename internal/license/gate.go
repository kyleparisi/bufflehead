package license

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"time"
)

// StartupTimeout bounds how long entitlement resolution may delay launch. The
// app must never hang at startup because a validation endpoint is slow; when
// the deadline passes, resolution comes back inconclusive and the fail-open
// path grants a grace window.
const StartupTimeout = 5 * time.Second

// Gate resolves entitlement once at startup and holds the result for the UI.
//
// Call Resolve early in main, before the window is created, so the Mac App
// Store receipt-refresh protocol can run before anything is drawn. Everything
// afterwards reads Status.
type Gate struct {
	resolver *Resolver
	status   Status

	// exit is the process-exit function, injectable for tests.
	exit func(code int)
	// goos is injectable for tests; production leaves it at runtime.GOOS.
	goos string
}

// NewGate builds the startup gate over the standard provider chain.
func NewGate(cfg Config) *Gate {
	return &Gate{resolver: NewResolver(cfg), exit: os.Exit, goos: runtime.GOOS}
}

// Resolve determines entitlement and applies the Mac App Store receipt-refresh
// protocol. It returns the Status the UI should render.
//
// On a Mac App Store build with a missing or invalid receipt this does not
// return: it exits with MASExitCodeRefreshReceipt, which is macOS's documented
// signal to prompt for an Apple ID, write a fresh receipt into the bundle and
// relaunch. Nagging instead would leave the user with no way to obtain a
// receipt at all, and would fail App Review on the reviewer's first launch.
func (g *Gate) Resolve(ctx context.Context) Status {
	ctx, cancel := context.WithTimeout(ctx, StartupTimeout)
	defer cancel()

	g.status = g.resolver.Resolve(ctx)

	if g.shouldRefreshReceipt() {
		// stderr, not the UI: nothing is on screen yet, and this line is what
		// makes an otherwise silent relaunch loop debuggable.
		fmt.Fprintf(os.Stderr,
			"bufflehead: no valid App Store receipt (%s); exiting %d to request one\n",
			g.status.Reason, MASExitCodeRefreshReceipt)
		g.exit(MASExitCodeRefreshReceipt)
	}
	return g.status
}

// shouldRefreshReceipt reports whether to invoke the exit-173 protocol. It is
// deliberately narrow: only a real macOS Mac App Store build, only when the
// receipt itself was the problem. A developer running `gd run`, or any other
// channel, must never be killed by this path.
func (g *Gate) shouldRefreshReceipt() bool {
	return g.goos == "darwin" &&
		g.status.Channel == ChannelMAS &&
		g.status.NeedsReceiptRefresh &&
		!g.status.Allowed()
}

// Status returns the entitlement resolved at startup.
func (g *Gate) Status() Status { return g.status }

// Allowed reports whether the app should run unrestricted.
func (g *Gate) Allowed() bool { return g.status.Allowed() }
