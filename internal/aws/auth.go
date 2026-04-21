package aws

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	"github.com/aws/aws-sdk-go-v2/service/sso"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/aws/aws-sdk-go-v2/service/ssooidc"
	ssooidctypes "github.com/aws/aws-sdk-go-v2/service/ssooidc/types"
)

// CredentialStatus represents the state of AWS credentials for a profile.
type CredentialStatus int

const (
	CredsValid         CredentialStatus = iota // Credentials are valid and not expired
	CredsExpired                               // Credentials exist but are expired
	CredsNoCredentials                         // No credentials found
)

const ssoSessionName = "bufflehead"

func (s CredentialStatus) String() string {
	switch s {
	case CredsValid:
		return "Valid"
	case CredsExpired:
		return "Expired"
	default:
		return "No credentials"
	}
}

// AuthManager handles AWS credential checking and SSO login for a profile.
type AuthManager struct {
	profile string
	region  string
	cfg     aws.Config
}

// NewAuthManager creates an AuthManager for the given AWS profile and region.
func NewAuthManager(profile, region string) *AuthManager {
	return &AuthManager{
		profile: profile,
		region:  region,
	}
}

// Profile returns the AWS profile name.
func (a *AuthManager) Profile() string { return a.profile }

// Region returns the AWS region.
func (a *AuthManager) Region() string { return a.region }

// loadConfig loads the AWS SDK config for this manager's profile and region.
func (a *AuthManager) loadConfig(ctx context.Context) (aws.Config, error) {
	opts := []func(*config.LoadOptions) error{
		config.WithRegion(a.region),
	}
	if a.profile != "" {
		opts = append(opts, config.WithSharedConfigProfile(a.profile))
	}
	return config.LoadDefaultConfig(ctx, opts...)
}

// Status returns the current credential state for the profile.
// It attempts to retrieve credentials from the default provider chain.
func (a *AuthManager) Status() CredentialStatus {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cfg, err := a.loadConfig(ctx)
	if err != nil {
		return CredsNoCredentials
	}

	creds, err := cfg.Credentials.Retrieve(ctx)
	if err != nil {
		return CredsExpired
	}

	if creds.Expired() {
		return CredsExpired
	}

	a.cfg = cfg
	return CredsValid
}

// SSOLoginResult is sent on the channel returned by SSOLogin.
type SSOLoginResult struct {
	Err  error
	Line string // status line from aws sso login (URL, code, success message)
}

// SSOLogin starts an OIDC device authorization flow for the profile's SSO session.
// Returns a channel that streams status lines (authorization URL, user code),
// then a final result with Err set to nil on success or the error on failure.
func (a *AuthManager) SSOLogin() <-chan SSOLoginResult {
	// Read the profile's sso-session config to get startURL + region
	home, _ := os.UserHomeDir()
	configPath := filepath.Join(home, ".aws", "config")
	data, _ := os.ReadFile(configPath)
	content := string(data)

	profileHeader := "[profile " + a.profile + "]"
	idx := strings.Index(content, profileHeader)
	if idx == -1 {
		ch := make(chan SSOLoginResult, 1)
		ch <- SSOLoginResult{Err: fmt.Errorf("profile %q not found in AWS config", a.profile)}
		close(ch)
		return ch
	}

	// Find sso_session name in the profile section
	section := content[idx:]
	if nextIdx := strings.Index(section[1:], "\n["); nextIdx != -1 {
		section = section[:nextIdx+1]
	}
	sessionName := parseConfigValue(section, "sso_session")
	if sessionName == "" {
		ch := make(chan SSOLoginResult, 1)
		ch <- SSOLoginResult{Err: fmt.Errorf("profile %q has no sso_session configured", a.profile)}
		close(ch)
		return ch
	}

	// Find the sso-session block to get startURL and region
	sessionHeader := "[sso-session " + sessionName + "]"
	sidx := strings.Index(content, sessionHeader)
	if sidx == -1 {
		ch := make(chan SSOLoginResult, 1)
		ch <- SSOLoginResult{Err: fmt.Errorf("sso-session %q not found in AWS config", sessionName)}
		close(ch)
		return ch
	}
	sessionSection := content[sidx:]
	if nextIdx := strings.Index(sessionSection[1:], "\n["); nextIdx != -1 {
		sessionSection = sessionSection[:nextIdx+1]
	}
	startURL := parseConfigValue(sessionSection, "sso_start_url")
	ssoRegion := parseConfigValue(sessionSection, "sso_region")
	if ssoRegion == "" {
		ssoRegion = a.region
	}

	return runOIDCDeviceAuth(startURL, ssoRegion, sessionName)
}

// parseConfigValue extracts a value for a key from an INI section string.
func parseConfigValue(section, key string) string {
	for _, line := range strings.Split(section, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, key+" ") || strings.HasPrefix(line, key+"=") {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				return strings.TrimSpace(parts[1])
			}
		}
	}
	return ""
}

// EnsureSSOSession checks if a [sso-session bufflehead] block exists in
// ~/.aws/config for the given start URL. If not, it appends one.
// Returns the sso-session name.
func EnsureSSOSession(startURL, ssoRegion string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("home dir: %w", err)
	}
	awsDir := filepath.Join(home, ".aws")
	configPath := filepath.Join(awsDir, "config")

	// Read existing config
	data, _ := os.ReadFile(configPath)
	content := string(data)

	// Check if sso-session block already exists for this URL
	sessionHeader := "[sso-session " + ssoSessionName + "]"
	if strings.Contains(content, sessionHeader) {
		// Already configured
		return ssoSessionName, nil
	}

	// Append the sso-session block
	os.MkdirAll(awsDir, 0700)
	block := fmt.Sprintf("\n%s\nsso_start_url = %s\nsso_region = %s\nsso_registration_scopes = sso:account:access\n",
		sessionHeader, startURL, ssoRegion)

	f, err := os.OpenFile(configPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return "", fmt.Errorf("open aws config: %w", err)
	}
	defer f.Close()

	if _, err := f.WriteString(block); err != nil {
		return "", fmt.Errorf("write aws config: %w", err)
	}

	return ssoSessionName, nil
}

// SSOSessionLogin starts an OIDC device authorization flow for the named sso-session.
// Returns a channel that streams status lines (authorization URL, user code)
// and a final result.
func SSOSessionLogin(sessionName string) <-chan SSOLoginResult {
	// Read the sso-session block from ~/.aws/config
	home, _ := os.UserHomeDir()
	configPath := filepath.Join(home, ".aws", "config")
	data, _ := os.ReadFile(configPath)
	content := string(data)

	sessionHeader := "[sso-session " + sessionName + "]"
	idx := strings.Index(content, sessionHeader)
	if idx == -1 {
		ch := make(chan SSOLoginResult, 1)
		ch <- SSOLoginResult{Err: fmt.Errorf("sso-session %q not found in AWS config", sessionName)}
		close(ch)
		return ch
	}

	section := content[idx:]
	if nextIdx := strings.Index(section[1:], "\n["); nextIdx != -1 {
		section = section[:nextIdx+1]
	}
	startURL := parseConfigValue(section, "sso_start_url")
	ssoRegion := parseConfigValue(section, "sso_region")

	return runOIDCDeviceAuth(startURL, ssoRegion, sessionName)
}

// runOIDCDeviceAuth performs the OIDC device authorization flow using the AWS SDK.
// It registers a client, starts device authorization, polls for the token, and
// writes the token to the SSO cache.
func runOIDCDeviceAuth(startURL, ssoRegion, sessionName string) <-chan SSOLoginResult {
	ch := make(chan SSOLoginResult, 16)
	go func() {
		defer close(ch)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()

		cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(ssoRegion))
		if err != nil {
			ch <- SSOLoginResult{Err: fmt.Errorf("load aws config: %w", err)}
			return
		}

		client := ssooidc.NewFromConfig(cfg)

		// Step 1: Register client
		ch <- SSOLoginResult{Line: "Registering OIDC client..."}
		reg, err := client.RegisterClient(ctx, &ssooidc.RegisterClientInput{
			ClientName: aws.String("bufflehead"),
			ClientType: aws.String("public"),
			Scopes:     []string{"sso:account:access"},
		})
		if err != nil {
			ch <- SSOLoginResult{Err: fmt.Errorf("register client: %w", err)}
			return
		}

		// Step 2: Start device authorization
		deviceAuth, err := client.StartDeviceAuthorization(ctx, &ssooidc.StartDeviceAuthorizationInput{
			ClientId:     reg.ClientId,
			ClientSecret: reg.ClientSecret,
			StartUrl:     aws.String(startURL),
		})
		if err != nil {
			ch <- SSOLoginResult{Err: fmt.Errorf("start device authorization: %w", err)}
			return
		}

		// Open the authorization URL in the user's browser
		var authURL string
		if deviceAuth.VerificationUriComplete != nil {
			authURL = *deviceAuth.VerificationUriComplete
		} else if deviceAuth.VerificationUri != nil {
			authURL = *deviceAuth.VerificationUri
		}

		if authURL != "" {
			openBrowser(authURL)
			ch <- SSOLoginResult{Line: fmt.Sprintf("Opened browser for authorization:\n%s", authURL)}
			if deviceAuth.UserCode != nil {
				ch <- SSOLoginResult{Line: fmt.Sprintf("Confirmation code: %s", *deviceAuth.UserCode)}
			}
		}

		ch <- SSOLoginResult{Line: "Waiting for authorization..."}

		// Step 3: Poll CreateToken until authorized
		interval := time.Duration(deviceAuth.Interval) * time.Second
		if interval == 0 {
			interval = 5 * time.Second
		}
		deadline := time.Now().Add(time.Duration(deviceAuth.ExpiresIn) * time.Second)

		var token *ssooidc.CreateTokenOutput
		for time.Now().Before(deadline) {
			token, err = client.CreateToken(ctx, &ssooidc.CreateTokenInput{
				ClientId:     reg.ClientId,
				ClientSecret: reg.ClientSecret,
				DeviceCode:   deviceAuth.DeviceCode,
				GrantType:    aws.String("urn:ietf:params:oauth:grant-type:device_code"),
			})
			if err == nil {
				break
			}

			var pendingErr *ssooidctypes.AuthorizationPendingException
			if errors.As(err, &pendingErr) {
				time.Sleep(interval)
				continue
			}

			var slowDown *ssooidctypes.SlowDownException
			if errors.As(err, &slowDown) {
				interval *= 2
				time.Sleep(interval)
				continue
			}

			ch <- SSOLoginResult{Err: fmt.Errorf("create token: %w", err)}
			return
		}

		if token == nil {
			ch <- SSOLoginResult{Err: fmt.Errorf("device authorization timed out")}
			return
		}

		// Step 4: Write token to SSO cache
		expiresAt := time.Now().UTC().Add(time.Duration(token.ExpiresIn) * time.Second)
		cacheEntry := ssoTokenCache{
			StartUrl:               startURL,
			Region:                 ssoRegion,
			AccessToken:            deref(token.AccessToken),
			ExpiresAt:              expiresAt.Format(time.RFC3339),
			ClientID:               deref(reg.ClientId),
			ClientSecret:           deref(reg.ClientSecret),
			RegistrationExpiresAt:  time.Unix(reg.ClientSecretExpiresAt, 0).UTC().Format(time.RFC3339),
			RefreshToken:           deref(token.RefreshToken),
		}

		if err := writeSSOCache(sessionName, cacheEntry); err != nil {
			ch <- SSOLoginResult{Err: fmt.Errorf("write sso cache: %w", err)}
			return
		}

		ch <- SSOLoginResult{Line: "Successfully logged in!"}
		ch <- SSOLoginResult{} // success (nil error)
	}()
	return ch
}

// ssoTokenCache is the JSON structure for ~/.aws/sso/cache/*.json files.
type ssoTokenCache struct {
	StartUrl               string `json:"startUrl"`
	Region                 string `json:"region"`
	AccessToken            string `json:"accessToken"`
	ExpiresAt              string `json:"expiresAt"`
	ClientID               string `json:"clientId,omitempty"`
	ClientSecret           string `json:"clientSecret,omitempty"`
	RegistrationExpiresAt  string `json:"registrationExpiresAt,omitempty"`
	RefreshToken           string `json:"refreshToken,omitempty"`
}

// openBrowser opens a URL in the user's default browser.
func openBrowser(url string) {
	switch runtime.GOOS {
	case "darwin":
		exec.Command("open", url).Start()
	case "linux":
		exec.Command("xdg-open", url).Start()
	case "windows":
		exec.Command("cmd", "/c", "start", url).Start()
	}
}

// writeSSOCache writes the token cache file to ~/.aws/sso/cache/<sha1(key)>.json.
func writeSSOCache(cacheKey string, entry ssoTokenCache) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	cacheDir := filepath.Join(home, ".aws", "sso", "cache")
	os.MkdirAll(cacheDir, 0700)

	h := sha1.New()
	h.Write([]byte(cacheKey))
	filename := strings.ToLower(hex.EncodeToString(h.Sum(nil))) + ".json"

	data, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(filepath.Join(cacheDir, filename), data, 0600)
}

// SSOAccount represents an AWS account available via SSO.
type SSOAccount struct {
	AccountID   string
	AccountName string
	EmailAddr   string
}

// SSORole represents a role available in an SSO account.
type SSORole struct {
	RoleName  string
	AccountID string
}

// ReadCachedAccessToken reads the SSO access token from ~/.aws/sso/cache/
// by finding the cache file that matches the given start URL.
func ReadCachedAccessToken(startURL string) (string, string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", err
	}
	cacheDir := filepath.Join(home, ".aws", "sso", "cache")
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		return "", "", fmt.Errorf("read sso cache: %w", err)
	}

	// Normalize: strip trailing hash fragments and slashes for comparison
	normalizeURL := func(u string) string {
		u = strings.TrimRight(u, "/")
		if idx := strings.Index(u, "#"); idx != -1 {
			u = u[:idx]
		}
		return strings.TrimRight(u, "/")
	}
	wantURL := normalizeURL(startURL)

	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(cacheDir, e.Name()))
		if err != nil {
			continue
		}
		var cached struct {
			StartUrl    string `json:"startUrl"`
			Region      string `json:"region"`
			AccessToken string `json:"accessToken"`
			ExpiresAt   string `json:"expiresAt"`
		}
		if err := json.Unmarshal(data, &cached); err != nil {
			continue
		}
		if normalizeURL(cached.StartUrl) != wantURL {
			continue
		}
		if cached.AccessToken == "" {
			continue
		}
		// Check expiry
		if t, err := time.Parse(time.RFC3339, cached.ExpiresAt); err == nil {
			if time.Now().After(t) {
				continue
			}
		}
		return cached.AccessToken, cached.Region, nil
	}
	return "", "", fmt.Errorf("no valid cached token for %s", startURL)
}

// ListSSOAccounts lists all AWS accounts available via the SSO access token.
func ListSSOAccounts(accessToken, region string) ([]SSOAccount, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
	if err != nil {
		return nil, err
	}

	client := sso.NewFromConfig(cfg)
	var accounts []SSOAccount
	var nextToken *string

	for {
		out, err := client.ListAccounts(ctx, &sso.ListAccountsInput{
			AccessToken: &accessToken,
			NextToken:   nextToken,
		})
		if err != nil {
			return nil, fmt.Errorf("list accounts: %w", err)
		}
		for _, a := range out.AccountList {
			accounts = append(accounts, SSOAccount{
				AccountID:   deref(a.AccountId),
				AccountName: deref(a.AccountName),
				EmailAddr:   deref(a.EmailAddress),
			})
		}
		if out.NextToken == nil {
			break
		}
		nextToken = out.NextToken
	}
	return accounts, nil
}

// ListSSORoles lists roles for a specific account via the SSO access token.
func ListSSORoles(accessToken, region, accountID string) ([]SSORole, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
	if err != nil {
		return nil, err
	}

	client := sso.NewFromConfig(cfg)
	var roles []SSORole
	var nextToken *string

	for {
		out, err := client.ListAccountRoles(ctx, &sso.ListAccountRolesInput{
			AccessToken: &accessToken,
			AccountId:   &accountID,
			NextToken:   nextToken,
		})
		if err != nil {
			return nil, fmt.Errorf("list roles: %w", err)
		}
		for _, r := range out.RoleList {
			roles = append(roles, SSORole{
				RoleName:  deref(r.RoleName),
				AccountID: deref(r.AccountId),
			})
		}
		if out.NextToken == nil {
			break
		}
		nextToken = out.NextToken
	}
	return roles, nil
}

// WriteProfile writes (or overwrites) a named profile in ~/.aws/config that
// references the bufflehead sso-session with the given account and role.
func WriteProfile(profileName, accountID, roleName, region string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	configPath := filepath.Join(home, ".aws", "config")

	data, _ := os.ReadFile(configPath)
	content := string(data)

	header := "[profile " + profileName + "]"
	block := fmt.Sprintf("%s\nsso_session = %s\nsso_account_id = %s\nsso_role_name = %s\nregion = %s\n",
		header, ssoSessionName, accountID, roleName, region)

	// If profile already exists, replace it
	if idx := strings.Index(content, header); idx != -1 {
		// Find end of this section (next [ or EOF)
		end := len(content)
		rest := content[idx+len(header):]
		if nextIdx := strings.Index(rest, "\n["); nextIdx != -1 {
			end = idx + len(header) + nextIdx + 1 // keep the newline before [
		}
		content = content[:idx] + block + content[end:]
	} else {
		// Append
		content += "\n" + block
	}

	return os.WriteFile(configPath, []byte(content), 0600)
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// Config returns the loaded AWS config. Must call Status() first to populate it.
func (a *AuthManager) Config() aws.Config {
	return a.cfg
}

// EnsureConfig loads the AWS config if not already loaded.
func (a *AuthManager) EnsureConfig(ctx context.Context) error {
	cfg, err := a.loadConfig(ctx)
	if err != nil {
		return err
	}
	a.cfg = cfg
	return nil
}

// SSMInstance represents an EC2 instance registered with SSM.
type SSMInstance struct {
	InstanceID   string
	Name         string // Name tag value
	PlatformType string // e.g. "Linux", "Windows"
	PingStatus   string // e.g. "Online", "ConnectionLost"
	IPAddress    string
}

// ListSSMInstances lists instances managed by SSM that are currently online.
// It enriches results with EC2 Name tags.
func (a *AuthManager) ListSSMInstances() ([]SSMInstance, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := a.EnsureConfig(ctx); err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}

	ssmClient := ssm.NewFromConfig(a.cfg)
	var instances []SSMInstance
	var nextToken *string

	for {
		out, err := ssmClient.DescribeInstanceInformation(ctx, &ssm.DescribeInstanceInformationInput{
			NextToken:  nextToken,
			MaxResults: aws.Int32(50),
		})
		if err != nil {
			return nil, fmt.Errorf("describe instance information: %w", err)
		}

		for _, info := range out.InstanceInformationList {
			inst := SSMInstance{
				InstanceID:   deref(info.InstanceId),
				Name:         deref(info.Name),
				PlatformType: string(info.PlatformType),
				PingStatus:   string(info.PingStatus),
				IPAddress:    deref(info.IPAddress),
			}
			if inst.PingStatus == "Online" {
				instances = append(instances, inst)
			}
		}

		if out.NextToken == nil {
			break
		}
		nextToken = out.NextToken
	}

	// Enrich with EC2 Name tags
	if len(instances) > 0 {
		var ids []string
		for _, inst := range instances {
			ids = append(ids, inst.InstanceID)
		}
		ec2Client := ec2.NewFromConfig(a.cfg)
		out, err := ec2Client.DescribeInstances(ctx, &ec2.DescribeInstancesInput{
			InstanceIds: ids,
		})
		if err == nil {
			nameMap := make(map[string]string)
			for _, res := range out.Reservations {
				for _, inst := range res.Instances {
					for _, tag := range inst.Tags {
						if deref(tag.Key) == "Name" {
							nameMap[deref(inst.InstanceId)] = deref(tag.Value)
						}
					}
				}
			}
			for i := range instances {
				if name, ok := nameMap[instances[i].InstanceID]; ok {
					instances[i].Name = name
				}
			}
		}
		// Non-fatal if EC2 enrichment fails — we still have instance IDs
	}

	return instances, nil
}

// RDSInstance represents an RDS database instance or cluster.
type RDSInstance struct {
	Identifier string // DB instance or cluster identifier
	Engine     string // e.g. "postgres", "aurora-postgresql", "mysql"
	Endpoint   string // hostname
	Port       int    // e.g. 5432
}

// ListRDSInstances lists RDS instances and Aurora clusters.
func (a *AuthManager) ListRDSInstances() ([]RDSInstance, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := a.EnsureConfig(ctx); err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}

	client := rds.NewFromConfig(a.cfg)
	var results []RDSInstance

	// List Aurora clusters (reader/writer endpoints)
	var clusterToken *string
	for {
		out, err := client.DescribeDBClusters(ctx, &rds.DescribeDBClustersInput{
			Marker: clusterToken,
		})
		if err != nil {
			break // non-fatal, fall through to instances
		}
		for _, c := range out.DBClusters {
			port := 5432
			if c.Port != nil {
				port = int(*c.Port)
			}
			if c.Endpoint != nil {
				results = append(results, RDSInstance{
					Identifier: deref(c.DBClusterIdentifier) + " (writer)",
					Engine:     deref(c.Engine),
					Endpoint:   *c.Endpoint,
					Port:       port,
				})
			}
			if c.ReaderEndpoint != nil {
				results = append(results, RDSInstance{
					Identifier: deref(c.DBClusterIdentifier) + " (reader)",
					Engine:     deref(c.Engine),
					Endpoint:   *c.ReaderEndpoint,
					Port:       port,
				})
			}
		}
		if out.Marker == nil {
			break
		}
		clusterToken = out.Marker
	}

	// List standalone RDS instances
	var instanceToken *string
	for {
		out, err := client.DescribeDBInstances(ctx, &rds.DescribeDBInstancesInput{
			Marker: instanceToken,
		})
		if err != nil {
			break
		}
		for _, db := range out.DBInstances {
			// Skip instances that belong to a cluster (already covered above)
			if db.DBClusterIdentifier != nil {
				continue
			}
			if db.Endpoint == nil {
				continue
			}
			port := 5432
			if db.Endpoint.Port != nil {
				port = int(*db.Endpoint.Port)
			}
			results = append(results, RDSInstance{
				Identifier: deref(db.DBInstanceIdentifier),
				Engine:     deref(db.Engine),
				Endpoint:   deref(db.Endpoint.Address),
				Port:       port,
			})
		}
		if out.Marker == nil {
			break
		}
		instanceToken = out.Marker
	}

	return results, nil
}

// ResolveInstanceID finds a bastion instance by tags using EC2 DescribeInstances.
// If directID is non-empty, it is returned as-is (no API call).
func (a *AuthManager) ResolveInstanceID(directID string, tags map[string]string) (string, error) {
	if directID != "" {
		return directID, nil
	}
	if len(tags) == 0 {
		return "", fmt.Errorf("no instance_id or instance_tags configured")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := a.EnsureConfig(ctx); err != nil {
		return "", fmt.Errorf("load aws config: %w", err)
	}

	client := ec2.NewFromConfig(a.cfg)

	var filters []ec2types.Filter
	for k, v := range tags {
		filters = append(filters, ec2types.Filter{
			Name:   aws.String("tag:" + k),
			Values: []string{v},
		})
	}
	// Only running instances
	filters = append(filters, ec2types.Filter{
		Name:   aws.String("instance-state-name"),
		Values: []string{"running"},
	})

	out, err := client.DescribeInstances(ctx, &ec2.DescribeInstancesInput{
		Filters: filters,
	})
	if err != nil {
		return "", fmt.Errorf("describe instances: %w", err)
	}

	for _, res := range out.Reservations {
		for _, inst := range res.Instances {
			if inst.InstanceId != nil {
				return *inst.InstanceId, nil
			}
		}
	}
	return "", fmt.Errorf("no running instance found matching tags %v", tags)
}
