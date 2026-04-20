package aws

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	"github.com/aws/aws-sdk-go-v2/service/sso"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
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

// SSOLogin launches `aws sso login --profile <name>` as a subprocess.
// Returns a channel that streams each line of output (so the UI can show
// the authorization URL and user code), then a final result with Err set
// to nil on success or the error on failure.
func (a *AuthManager) SSOLogin() <-chan SSOLoginResult {
	ch := make(chan SSOLoginResult, 16)
	go func() {
		defer close(ch)
		args := []string{"sso", "login"}
		if a.profile != "" {
			args = append(args, "--profile", a.profile)
		}
		cmd := exec.Command("aws", args...)

		stdout, err := cmd.StdoutPipe()
		if err != nil {
			ch <- SSOLoginResult{Err: fmt.Errorf("stdout pipe: %w", err)}
			return
		}
		cmd.Stderr = cmd.Stdout

		if err := cmd.Start(); err != nil {
			ch <- SSOLoginResult{Err: fmt.Errorf("aws sso login: %w", err)}
			return
		}

		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line != "" {
				ch <- SSOLoginResult{Line: line}
			}
		}

		if err := cmd.Wait(); err != nil {
			ch <- SSOLoginResult{Err: fmt.Errorf("aws sso login: %w", err)}
		} else {
			ch <- SSOLoginResult{}
		}
	}()
	return ch
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

// SSOSessionLogin launches `aws sso login --sso-session <name>`.
// Returns a channel that streams output lines (authorization URL, user code)
// and a final result.
func SSOSessionLogin(sessionName string) <-chan SSOLoginResult {
	ch := make(chan SSOLoginResult, 16)
	go func() {
		defer close(ch)
		cmd := exec.Command("aws", "sso", "login", "--sso-session", sessionName)

		stdout, err := cmd.StdoutPipe()
		if err != nil {
			ch <- SSOLoginResult{Err: fmt.Errorf("stdout pipe: %w", err)}
			return
		}
		cmd.Stderr = cmd.Stdout

		if err := cmd.Start(); err != nil {
			ch <- SSOLoginResult{Err: fmt.Errorf("aws sso login: %w", err)}
			return
		}

		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line != "" {
				ch <- SSOLoginResult{Line: line}
			}
		}

		if err := cmd.Wait(); err != nil {
			ch <- SSOLoginResult{Err: fmt.Errorf("aws sso login: %w", err)}
		} else {
			ch <- SSOLoginResult{}
		}
	}()
	return ch
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
