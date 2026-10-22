package mcpmode

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	bfaws "bufflehead/internal/aws"
)

// fakeLogin is a scripted SSO device flow: it reports the URL and code, then
// finishes when release is closed with result.
type fakeLogin struct {
	starts  atomic.Int32
	release chan struct{}
	result  error
}

func (f *fakeLogin) start(profile, region string) <-chan bfaws.SSOLoginResult {
	f.starts.Add(1)
	ch := make(chan bfaws.SSOLoginResult)
	go func() {
		defer close(ch)
		ch <- bfaws.SSOLoginResult{Line: "Opened browser for authorization:\nhttps://device.sso.example/?user_code=ABCD-EFGH"}
		ch <- bfaws.SSOLoginResult{Line: "Confirmation code: ABCD-EFGH"}
		<-f.release
		ch <- bfaws.SSOLoginResult{Err: f.result}
	}()
	return ch
}

func newTestLogins(status bfaws.CredentialStatus, f *fakeLogin, wait time.Duration) *awsLogins {
	l := newAWSLogins()
	l.status = func(string, string) bfaws.CredentialStatus { return status }
	l.start = f.start
	l.wait = wait
	return l
}

func TestEnsureSkipsLoginWhenCredentialsValid(t *testing.T) {
	f := &fakeLogin{release: make(chan struct{})}
	l := newTestLogins(bfaws.CredsValid, f, time.Second)
	if err := l.ensure(context.Background(), "dev", "us-east-1"); err != nil {
		t.Fatal(err)
	}
	if n := f.starts.Load(); n != 0 {
		t.Errorf("started %d logins, want 0", n)
	}
}

func TestEnsureWaitsForApproval(t *testing.T) {
	f := &fakeLogin{release: make(chan struct{})}
	l := newTestLogins(bfaws.CredsExpired, f, 5*time.Second)
	go func() {
		time.Sleep(50 * time.Millisecond)
		close(f.release)
	}()
	if err := l.ensure(context.Background(), "dev", "us-east-1"); err != nil {
		t.Fatalf("ensure: %v", err)
	}
}

func TestEnsureTimesOutWithInstructionsAndKeepsLoggingIn(t *testing.T) {
	f := &fakeLogin{release: make(chan struct{})}
	l := newTestLogins(bfaws.CredsExpired, f, 30*time.Millisecond)

	err := l.ensure(context.Background(), "dev", "us-east-1")
	if err == nil {
		t.Fatal("want an error while the login is pending")
	}
	for _, want := range []string{`profile "dev"`, "ABCD-EFGH", "approve", "retry"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q is missing %q", err, want)
		}
	}

	// The retry joins the same login instead of opening another browser tab,
	// and succeeds once the user approves.
	l.wait = 5 * time.Second
	go func() {
		time.Sleep(50 * time.Millisecond)
		close(f.release)
	}()
	if err := l.ensure(context.Background(), "dev", "us-east-1"); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if n := f.starts.Load(); n != 1 {
		t.Errorf("started %d logins, want 1", n)
	}
}

func TestConcurrentCallsShareOneLogin(t *testing.T) {
	f := &fakeLogin{release: make(chan struct{})}
	l := newTestLogins(bfaws.CredsExpired, f, 5*time.Second)

	var wg sync.WaitGroup
	errs := make([]error, 5)
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = l.ensure(context.Background(), "dev", "us-east-1")
		}()
	}
	time.Sleep(50 * time.Millisecond)
	close(f.release)
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Errorf("call %d: %v", i, err)
		}
	}
	if n := f.starts.Load(); n != 1 {
		t.Errorf("started %d logins, want 1", n)
	}
}

func TestFailedLoginReportsAndAllowsANewAttempt(t *testing.T) {
	f := &fakeLogin{release: make(chan struct{}), result: errors.New("profile \"dev\" has no sso_session configured")}
	close(f.release)
	l := newTestLogins(bfaws.CredsExpired, f, 5*time.Second)

	err := l.ensure(context.Background(), "dev", "us-east-1")
	if err == nil || !strings.Contains(err.Error(), "no sso_session") || !strings.Contains(err.Error(), "aws sso login --profile dev") {
		t.Fatalf("err = %v", err)
	}
	if err := l.ensure(context.Background(), "dev", "us-east-1"); err == nil {
		t.Fatal("second attempt: want the same failure")
	}
	if n := f.starts.Load(); n != 2 {
		t.Errorf("started %d logins, want 2 (a failed login must not stay cached)", n)
	}
}

func TestEnsureHonoursCancellation(t *testing.T) {
	f := &fakeLogin{release: make(chan struct{})}
	defer close(f.release)
	l := newTestLogins(bfaws.CredsExpired, f, 5*time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := l.ensure(ctx, "dev", "us-east-1"); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}
