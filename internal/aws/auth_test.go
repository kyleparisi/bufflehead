package aws

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestNormalizeStartURL(t *testing.T) {
	cases := map[string]string{
		"https://org.awsapps.com/start/#/":         "https://org.awsapps.com/start",
		"https://org.awsapps.com/start/":           "https://org.awsapps.com/start",
		"https://org.awsapps.com/start":            "https://org.awsapps.com/start",
		"  https://org.awsapps.com/start/#/  ":     "https://org.awsapps.com/start",
		"https://org.awsapps.com/start/#/accounts": "https://org.awsapps.com/start",
	}
	for in, want := range cases {
		if got := normalizeStartURL(in); got != want {
			t.Errorf("normalizeStartURL(%q) = %q, want %q", in, got, want)
		}
	}
}

// isolateAWSHome points ~/.aws at a temp dir for the duration of the test,
// covering both the $HOME (unix) and %USERPROFILE% (windows) lookups used by
// os.UserHomeDir so this test runs on the Windows CI job too.
func isolateAWSHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", home)
	}
	return home
}

func readSSOSession(t *testing.T, home string) (url, region string, blocks int) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(home, ".aws", "config"))
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	content := string(data)
	header := "[sso-session " + ssoSessionName + "]"
	blocks = strings.Count(content, header)
	idx := strings.Index(content, header)
	if idx == -1 {
		return "", "", blocks
	}
	section := content[idx:]
	if next := strings.Index(section[1:], "\n["); next != -1 {
		section = section[:next+1]
	}
	return parseConfigValue(section, "sso_start_url"), parseConfigValue(section, "sso_region"), blocks
}

func TestEnsureSSOSession_WritesNormalized(t *testing.T) {
	home := isolateAWSHome(t)

	if _, err := EnsureSSOSession("https://org.awsapps.com/start/#/", "us-east-1"); err != nil {
		t.Fatalf("EnsureSSOSession: %v", err)
	}
	url, region, blocks := readSSOSession(t, home)
	if url != "https://org.awsapps.com/start" {
		t.Errorf("sso_start_url = %q, want normalized .../start", url)
	}
	if region != "us-east-1" {
		t.Errorf("sso_region = %q, want us-east-1", region)
	}
	if blocks != 1 {
		t.Errorf("expected 1 sso-session block, got %d", blocks)
	}
}

// TestEnsureSSOSession_CorrectsStaleURL reproduces the real-world bug: a stale,
// truncated sso_start_url (host only, no /start) previously stuck forever and
// made StartDeviceAuthorization return 400. A subsequent call with the correct
// URL must overwrite it in place without duplicating the block or clobbering
// unrelated config sections.
func TestEnsureSSOSession_CorrectsStaleURL(t *testing.T) {
	home := isolateAWSHome(t)
	awsDir := filepath.Join(home, ".aws")
	if err := os.MkdirAll(awsDir, 0700); err != nil {
		t.Fatal(err)
	}
	stale := "[profile keep]\nregion = eu-west-1\n\n" +
		"[sso-session " + ssoSessionName + "]\n" +
		"sso_start_url = https://advisorarch.awsapps.com\n" +
		"sso_region = us-east-1\n" +
		"sso_registration_scopes = sso:account:access\n"
	if err := os.WriteFile(filepath.Join(awsDir, "config"), []byte(stale), 0600); err != nil {
		t.Fatal(err)
	}

	if _, err := EnsureSSOSession("https://advisorarch.awsapps.com/start/#/", "us-east-1"); err != nil {
		t.Fatalf("EnsureSSOSession: %v", err)
	}

	url, region, blocks := readSSOSession(t, home)
	if url != "https://advisorarch.awsapps.com/start" {
		t.Errorf("sso_start_url = %q, want corrected .../start", url)
	}
	if region != "us-east-1" {
		t.Errorf("sso_region = %q, want us-east-1", region)
	}
	if blocks != 1 {
		t.Errorf("expected 1 sso-session block after rewrite, got %d", blocks)
	}
	data, _ := os.ReadFile(filepath.Join(awsDir, "config"))
	if !strings.Contains(string(data), "[profile keep]") ||
		!strings.Contains(string(data), "region = eu-west-1") {
		t.Errorf("unrelated [profile keep] section was clobbered:\n%s", data)
	}
}

func TestEnsureSSOSession_ReusesMatchingBlock(t *testing.T) {
	home := isolateAWSHome(t)

	if _, err := EnsureSSOSession("https://org.awsapps.com/start", "us-east-1"); err != nil {
		t.Fatalf("first: %v", err)
	}
	before, err := os.ReadFile(filepath.Join(home, ".aws", "config"))
	if err != nil {
		t.Fatal(err)
	}
	// A second call with an equivalent (fragment-y) URL must be a no-op.
	if _, err := EnsureSSOSession("https://org.awsapps.com/start/#/", "us-east-1"); err != nil {
		t.Fatalf("second: %v", err)
	}
	after, _ := os.ReadFile(filepath.Join(home, ".aws", "config"))
	if string(before) != string(after) {
		t.Errorf("matching block was rewritten:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}
