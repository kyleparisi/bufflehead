package mcpmode

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	bfaws "bufflehead/internal/aws"
)

// awsLoginWait bounds how long a tool call waits for the user to approve an
// SSO login in the browser. It stays well under typical MCP client timeouts;
// past it the call returns and the login keeps polling in the background, so
// a retry succeeds once the user approves.
const awsLoginWait = 45 * time.Second

// awsLogins starts AWS SSO logins on demand, at most one per profile at a
// time, so a model retrying a tool call doesn't open a browser tab per try.
type awsLogins struct {
	mu       sync.Mutex
	inFlight map[string]*ssoLogin // keyed by profile

	// Seams for tests; production uses bfaws.AuthManager.
	status func(profile, region string) bfaws.CredentialStatus
	start  func(profile, region string) <-chan bfaws.SSOLoginResult
	wait   time.Duration
}

// ssoLogin is one device-authorization flow in progress.
type ssoLogin struct {
	done chan struct{} // closed when the flow ends; err is set before
	err  error

	mu   sync.Mutex
	url  string
	code string
}

func newAWSLogins() *awsLogins {
	return &awsLogins{
		inFlight: map[string]*ssoLogin{},
		status: func(profile, region string) bfaws.CredentialStatus {
			return bfaws.NewAuthManager(profile, region).Status()
		},
		start: func(profile, region string) <-chan bfaws.SSOLoginResult {
			return bfaws.NewAuthManager(profile, region).SSOLogin()
		},
		wait: awsLoginWait,
	}
}

// ensure returns nil once profile has usable credentials. When they are
// expired or missing it starts an SSO login (which opens the browser) or
// joins the one already running, then waits up to l.wait for approval.
func (l *awsLogins) ensure(ctx context.Context, profile, region string) error {
	if l.status(profile, region) == bfaws.CredsValid {
		return nil
	}
	login := l.login(profile, region)

	timer := time.NewTimer(l.wait)
	defer timer.Stop()
	select {
	case <-login.done:
		if login.err != nil {
			return fmt.Errorf("AWS SSO login for profile %q failed: %w (if this profile doesn't use SSO, refresh its credentials yourself, e.g. `aws sso login --profile %s`)", profile, login.err, profile)
		}
		return nil
	case <-timer.C:
		return login.pendingErr(profile)
	case <-ctx.Done():
		return ctx.Err()
	}
}

// login returns the running login for profile, starting one if none is.
func (l *awsLogins) login(profile, region string) *ssoLogin {
	l.mu.Lock()
	defer l.mu.Unlock()
	if s := l.inFlight[profile]; s != nil {
		return s
	}
	s := &ssoLogin{done: make(chan struct{})}
	l.inFlight[profile] = s
	go func() {
		s.err = s.follow(l.start(profile, region))
		l.mu.Lock()
		delete(l.inFlight, profile)
		l.mu.Unlock()
		close(s.done)
	}()
	return s
}

// follow drains a login's progress, recording the URL and code to show the
// user, and returns its outcome. A message with no Line is the final one.
func (s *ssoLogin) follow(ch <-chan bfaws.SSOLoginResult) error {
	for r := range ch {
		if r.Line == "" {
			return r.Err
		}
		s.mu.Lock()
		if url, ok := strings.CutPrefix(r.Line, "Opened browser for authorization:\n"); ok {
			s.url = url
		}
		if code, ok := strings.CutPrefix(r.Line, "Confirmation code: "); ok {
			s.code = code
		}
		s.mu.Unlock()
	}
	return errors.New("login ended without a result")
}

// pendingErr tells the model (and through it, the user) what to do while the
// login waits for approval.
func (s *ssoLogin) pendingErr(profile string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	msg := fmt.Sprintf("AWS credentials for profile %q have expired. An AWS SSO login is open in the browser", profile)
	if s.code != "" {
		msg += fmt.Sprintf(" (confirmation code %s)", s.code)
	}
	msg += ". Ask the user to approve it, then retry this call."
	if s.url != "" {
		msg += " If no browser tab opened, the login page is " + s.url
	}
	return errors.New(msg)
}
