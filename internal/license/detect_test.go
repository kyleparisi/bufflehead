package license

import (
	"context"
	"errors"
	"testing"
	"time"
)

// fakeProvider is a scriptable Provider for exercising chain ordering and the
// fail-open policy.
type fakeProvider struct {
	channel   Channel
	detect    bool
	status    Status
	err       error
	detected  int
	validated int
}

func (f *fakeProvider) Channel() Channel { return f.channel }

func (f *fakeProvider) Detect(context.Context) bool {
	f.detected++
	return f.detect
}

func (f *fakeProvider) Validate(context.Context) (Status, error) {
	f.validated++
	return f.status, f.err
}

func licensed(ch Channel) Status {
	return Status{Channel: ch, State: StateLicensed, Plan: string(ch)}
}

// The chain must stop at the first detecting provider and never consult later
// ones — that is what keeps a broken MAS install from being re-examined as an
// MDM or direct-sale install.
func TestResolverStopsAtFirstDetectedChannel(t *testing.T) {
	first := &fakeProvider{channel: ChannelInternal, detect: false}
	second := &fakeProvider{channel: ChannelMAS, detect: true, status: licensed(ChannelMAS)}
	third := &fakeProvider{channel: ChannelMDM, detect: true, status: licensed(ChannelMDM)}

	st := NewResolverWith(first, second, third).Resolve(context.Background())

	if st.Channel != ChannelMAS {
		t.Errorf("Channel = %q, want %q", st.Channel, ChannelMAS)
	}
	if third.detected != 0 {
		t.Error("a provider after the first match was consulted")
	}
	if third.validated != 0 {
		t.Error("a provider after the first match was validated")
	}
	if second.validated != 1 {
		t.Errorf("matched provider validated %d times, want 1", second.validated)
	}
}

// A detected channel owns the verdict. An unlicensed MAS install must NOT fall
// through to the next provider.
func TestResolverDoesNotFallThroughOnUnlicensed(t *testing.T) {
	mas := &fakeProvider{
		channel: ChannelMAS, detect: true,
		status: Status{State: StateUnlicensed, Reason: "bad receipt"},
	}
	direct := &fakeProvider{channel: ChannelDirect, detect: true, status: licensed(ChannelDirect)}

	st := NewResolverWith(mas, direct).Resolve(context.Background())

	if st.Channel != ChannelMAS {
		t.Errorf("Channel = %q, want %q", st.Channel, ChannelMAS)
	}
	if st.State != StateUnlicensed {
		t.Errorf("State = %q, want %q", st.State, StateUnlicensed)
	}
	if direct.detected != 0 {
		t.Error("chain fell through to the next channel after an unlicensed verdict")
	}
}

// Internal access overrides every other channel, including an unlicensed one.
func TestResolverInternalOverridesEverything(t *testing.T) {
	internal := &fakeProvider{channel: ChannelInternal, detect: true, status: licensed(ChannelInternal)}
	mas := &fakeProvider{
		channel: ChannelMAS, detect: true,
		status: Status{State: StateUnlicensed},
	}

	st := NewResolverWith(internal, mas).Resolve(context.Background())

	if st.Channel != ChannelInternal || st.State != StateLicensed {
		t.Errorf("got %s, want internal/licensed", st)
	}
	if mas.detected != 0 {
		t.Error("internal match did not short-circuit the chain")
	}
}

func TestResolverNoChannelNags(t *testing.T) {
	st := NewResolverWith(
		&fakeProvider{channel: ChannelInternal},
		&fakeProvider{channel: ChannelDirect},
	).Resolve(context.Background())

	if st.Channel != ChannelUnknown {
		t.Errorf("Channel = %q, want %q", st.Channel, ChannelUnknown)
	}
	if !st.ShouldNag() {
		t.Error("ShouldNag() = false with no channel detected")
	}
	if !errors.Is(st.Diag, ErrNoChannel) {
		t.Errorf("Diag = %v, want ErrNoChannel", st.Diag)
	}
}

// Inconclusive validation opens a grace window rather than locking anyone out.
func TestResolverFailsOpenOnInconclusiveValidation(t *testing.T) {
	now := time.Now()
	p := &fakeProvider{
		channel: ChannelDirect, detect: true,
		err: errors.New("network unreachable"),
	}
	r := NewResolverWith(p)
	r.now = func() time.Time { return now }

	st := r.Resolve(context.Background())

	if st.State != StateGrace {
		t.Fatalf("State = %q, want %q", st.State, StateGrace)
	}
	if !st.Allowed() {
		t.Error("Allowed() = false during grace")
	}
	if st.GraceUntil.IsZero() {
		t.Error("GraceUntil not set")
	}
	if want := now.Add(DefaultGrace); !st.GraceUntil.Equal(want) {
		t.Errorf("GraceUntil = %v, want %v", st.GraceUntil, want)
	}
	if st.Diag == nil {
		t.Error("Diag not set for a fail-open status")
	}
}

// Once the grace deadline passes, an unconfirmable license does stop working.
func TestResolverGraceExpires(t *testing.T) {
	now := time.Now()
	p := &fakeProvider{
		channel: ChannelDirect, detect: true,
		status: Status{GraceUntil: now.Add(-time.Hour)},
		err:    errors.New("still offline"),
	}
	r := NewResolverWith(p)
	r.now = func() time.Time { return now }

	st := r.Resolve(context.Background())
	if st.State != StateUnlicensed {
		t.Errorf("State = %q, want %q", st.State, StateUnlicensed)
	}
	if st.Allowed() {
		t.Error("Allowed() = true past the grace deadline")
	}
}

// Expiry is enforced centrally so an individual provider cannot forget to.
func TestResolverEnforcesExpiryCentrally(t *testing.T) {
	now := time.Now()
	p := &fakeProvider{
		channel: ChannelDirect, detect: true,
		// Provider wrongly reports "licensed" with a past expiry.
		status: Status{State: StateLicensed, Expires: now.Add(-24 * time.Hour)},
	}
	r := NewResolverWith(p)
	r.now = func() time.Time { return now }

	st := r.Resolve(context.Background())
	if st.State != StateUnlicensed {
		t.Errorf("State = %q, want %q for an expired license", st.State, StateUnlicensed)
	}
	if st.Allowed() {
		t.Error("Allowed() = true for an expired license")
	}
}

// Every status handed to the UI needs a non-empty explanation.
func TestResolverAlwaysSetsReason(t *testing.T) {
	cases := map[string]*fakeProvider{
		"licensed":   {channel: ChannelDirect, detect: true, status: Status{State: StateLicensed}},
		"unlicensed": {channel: ChannelDirect, detect: true, status: Status{State: StateUnlicensed}},
		"error":      {channel: ChannelDirect, detect: true, err: errors.New("boom")},
		"no channel": {channel: ChannelDirect},
	}
	for name, p := range cases {
		if st := NewResolverWith(p).Resolve(context.Background()); st.Reason == "" {
			t.Errorf("%s: Reason is empty", name)
		}
	}
}

// The default chain must be in the documented order.
func TestDefaultChainOrder(t *testing.T) {
	r := NewResolver(Config{})
	want := []Channel{ChannelInternal, ChannelMAS, ChannelSetapp, ChannelMDM, ChannelDirect}
	if len(r.providers) != len(want) {
		t.Fatalf("chain has %d providers, want %d", len(r.providers), len(want))
	}
	for i, ch := range want {
		if got := r.providers[i].Channel(); got != ch {
			t.Errorf("provider %d is %q, want %q", i, got, ch)
		}
	}
}

// The scaffolded providers must not claim a channel they cannot verify.
// OfflineFileProvider especially: detecting on file existence alone would hand
// two weeks of free access to anyone who can write the license path.
func TestPendingProvidersDoNotDetect(t *testing.T) {
	cfg := Config{}
	providers := []Provider{
		NewSetappProvider(cfg),
		NewOfflineFileProvider(cfg),
		NewRemoteAPIProvider(cfg),
	}
	for _, p := range providers {
		if p.Detect(context.Background()) {
			t.Errorf("%s: Detect = true before verification is implemented", p.Channel())
		}
		if _, err := p.Validate(context.Background()); !errors.Is(err, ErrProviderNotImplemented) {
			t.Errorf("%s: Validate err = %v, want ErrProviderNotImplemented", p.Channel(), err)
		}
	}
}

// The default chain on a machine with nothing configured must nag, not crash
// and not grant access.
func TestDefaultChainOnBareMachine(t *testing.T) {
	st := NewResolver(Config{}).Resolve(context.Background())
	if st.Allowed() {
		t.Errorf("bare machine got %s, want an unlicensed status", st)
	}
	if !st.ShouldNag() {
		t.Error("ShouldNag() = false on a bare machine")
	}
}

func TestStatusString(t *testing.T) {
	st := Status{Channel: ChannelMAS, State: StateLicensed, Plan: "mas", Subject: "mas:x@1.0"}
	got := st.String()
	if got != "mas/licensed plan=mas subject=mas:x@1.0" {
		t.Errorf("String() = %q", got)
	}
}
