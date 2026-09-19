// Package updater discovers and stages Bufflehead releases. It does not install them.
package updater

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"golang.org/x/mod/semver"
)

const repository = "kyleparisi/bufflehead"
const maxAssetSize int64 = 1 << 30

// Client uses GitHub's public API without embedding credentials in the application.
// A custom HTTP transport can be supplied for tests; HTTPS remains mandatory.
type Client struct{ HTTP *http.Client }

type Asset struct {
	Name   string `json:"name"`
	URL    string `json:"browser_download_url"`
	Size   int64  `json:"size"`
	Digest string `json:"digest"`
}

type Release struct {
	Tag        string  `json:"tag_name"`
	URL        string  `json:"html_url"`
	Notes      string  `json:"body"`
	Draft      bool    `json:"draft"`
	Prerelease bool    `json:"prerelease"`
	Assets     []Asset `json:"assets"`
}

type Update struct {
	Release Release
	Asset   Asset
}

func version(v string) (string, error) {
	v = "v" + strings.TrimPrefix(v, "v")
	if !semver.IsValid(v) {
		return "", fmt.Errorf("invalid version %q", v)
	}
	return v, nil
}

func assetName(tag, goos, arch string) (string, error) {
	prefix := "Bufflehead-" + strings.TrimPrefix(tag, "v")
	switch goos + "/" + arch {
	case "darwin/amd64", "darwin/arm64":
		return prefix + "-macOS.dmg", nil
	case "windows/amd64":
		return prefix + "-Setup.exe", nil
	case "linux/amd64":
		return prefix + "-x86_64.AppImage", nil
	case "linux/arm64":
		return prefix + "-aarch64.AppImage", nil
	default:
		return "", fmt.Errorf("unsupported platform %s/%s", goos, arch)
	}
}

// Select returns nil when the stable release is not newer. Missing or ambiguous
// platform assets are errors, not a claim that the installation is up to date.
func Select(r Release, current, goos, arch string) (*Update, error) {
	cur, err := version(current)
	if err != nil {
		return nil, err
	}
	latest, err := version(r.Tag)
	if err != nil {
		return nil, err
	}
	if r.Draft || r.Prerelease || semver.Prerelease(latest) != "" || semver.Compare(latest, cur) <= 0 {
		return nil, nil
	}
	name, err := assetName(r.Tag, goos, arch)
	if err != nil {
		return nil, err
	}
	var found *Asset
	for _, a := range r.Assets {
		if a.Name != name {
			continue
		}
		if found != nil {
			return nil, fmt.Errorf("duplicate release asset %q", name)
		}
		copy := a
		found = &copy
	}
	if found == nil {
		return nil, fmt.Errorf("release %s has no asset %q", r.Tag, name)
	}
	if err := validateAsset(*found); err != nil {
		return nil, err
	}
	return &Update{Release: r, Asset: *found}, nil
}

func validateAsset(a Asset) error {
	u, err := url.Parse(a.URL)
	if err != nil || u.Scheme != "https" || u.Host != "github.com" || u.User != nil || !strings.HasPrefix(u.Path, "/"+repository+"/releases/download/") {
		return errors.New("asset URL must be an HTTPS release download from " + repository)
	}
	if a.Size <= 0 || a.Size > maxAssetSize {
		return fmt.Errorf("invalid asset size %d", a.Size)
	}
	if !strings.HasPrefix(a.Digest, "sha256:") {
		return errors.New("release asset has no SHA-256 digest")
	}
	sum, err := hex.DecodeString(strings.TrimPrefix(a.Digest, "sha256:"))
	if err != nil || len(sum) != sha256.Size {
		return errors.New("invalid SHA-256 digest")
	}
	return nil
}

func (c Client) get(ctx context.Context, address string) (*http.Response, error) {
	h := http.Client{Timeout: 10 * time.Minute}
	if c.HTTP != nil {
		h = *c.HTTP
	}
	previous := h.CheckRedirect
	h.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if req.URL.Scheme != "https" {
			return errors.New("refusing non-HTTPS redirect")
		}
		if len(via) >= 10 {
			return errors.New("too many redirects")
		}
		if previous != nil {
			return previous(req, via)
		}
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Bufflehead-updater")
	if req.URL.Host == "api.github.com" {
		req.Header.Set("Accept", "application/vnd.github+json")
	}
	resp, err := h.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("update request: HTTP %d (rate-limit reset: %s)", resp.StatusCode, resp.Header.Get("X-RateLimit-Reset"))
	}
	return resp, nil
}

// Check follows GitHub's designated latest stable release, not tag creation order.
// It uses a short timeout independent of the longer artifact download timeout.
func (c Client) Check(ctx context.Context, current, goos, arch string) (*Update, error) {
	if _, err := version(current); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	resp, err := c.get(ctx, "https://api.github.com/repos/"+repository+"/releases/latest")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, (2<<20)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > 2<<20 {
		return nil, errors.New("release metadata too large")
	}
	var r Release
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("decode release: %w", err)
	}
	return Select(r, current, goos, arch)
}

// Stage downloads to a private temporary file in dir, verifies the exact size
// and SHA-256, and removes partial files on failure. The caller owns the returned
// file and must remove it after use. Nothing is executed or replaced.
func (c Client) Stage(ctx context.Context, a Asset, dir string) (path string, err error) {
	if err := validateAsset(a); err != nil {
		return "", err
	}
	resp, err := c.get(ctx, a.URL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	f, err := os.CreateTemp(dir, "bufflehead-update-*")
	if err != nil {
		return "", err
	}
	defer func() {
		f.Close()
		if err != nil {
			os.Remove(f.Name())
		}
	}()
	hash := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, hash), io.LimitReader(resp.Body, a.Size+1))
	if err != nil {
		return "", err
	}
	if n != a.Size {
		return "", fmt.Errorf("asset size mismatch: got %d, expected %d", n, a.Size)
	}
	if !strings.EqualFold(hex.EncodeToString(hash.Sum(nil)), strings.TrimPrefix(a.Digest, "sha256:")) {
		return "", errors.New("asset SHA-256 mismatch")
	}
	if err := f.Sync(); err != nil {
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	return f.Name(), nil
}
