package updater

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
)

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func client(status int, body string) Client {
	return Client{HTTP: &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		if err := r.Context().Err(); err != nil {
			return nil, err
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: r}, nil
	})}}
}
func asset(name, body string) Asset {
	return Asset{Name: name, URL: "https://github.com/kyleparisi/bufflehead/releases/download/v0.30.0/" + name, Size: int64(len(body)), Digest: fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(body)))}
}
func release() Release {
	r := Release{Tag: "v0.30.0"}
	for _, suffix := range []string{"macOS.dmg", "Setup.exe", "x86_64.AppImage", "aarch64.AppImage"} {
		r.Assets = append(r.Assets, asset("Bufflehead-0.30.0-"+suffix, "payload"))
	}
	return r
}
func TestSelectPlatforms(t *testing.T) {
	for _, tc := range []struct{ os, arch, suffix string }{
		{"darwin", "arm64", "macOS.dmg"}, {"darwin", "amd64", "macOS.dmg"},
		{"windows", "amd64", "Setup.exe"}, {"linux", "amd64", "x86_64.AppImage"}, {"linux", "arm64", "aarch64.AppImage"},
	} {
		t.Run(tc.os+tc.arch, func(t *testing.T) {
			u, err := Select(release(), "0.29.0", tc.os, tc.arch)
			if err != nil || u == nil || u.Asset.Name != "Bufflehead-0.30.0-"+tc.suffix {
				t.Fatalf("got %+v, %v", u, err)
			}
		})
	}
}
func TestSelectionPolicy(t *testing.T) {
	cases := []struct {
		name, current         string
		mutate                func(*Release)
		wantError, wantUpdate bool
	}{
		{"newer", "v0.29.0", nil, false, true},
		{"numeric comparison", "0.9.0", nil, false, true},
		{"same", "0.30.0", nil, false, false},
		{"downgrade", "0.31.0", nil, false, false},
		{"build metadata", "0.30.0+local", nil, false, false},
		{"upgrade from prerelease", "0.30.0-rc.1", nil, false, true},
		{"dev", "dev", nil, true, false},
		{"bad tag", "0.29.0", func(r *Release) { r.Tag = "broken" }, true, false},
		{"draft", "0.29.0", func(r *Release) { r.Draft = true }, false, false},
		{"prerelease", "0.29.0", func(r *Release) { r.Prerelease = true }, false, false},
		{"unmarked prerelease", "0.29.0", func(r *Release) { r.Tag = "v0.30.0-rc.1" }, false, false},
		{"missing", "0.29.0", func(r *Release) { r.Assets = nil }, true, false},
		{"duplicate", "0.29.0", func(r *Release) { r.Assets = append(r.Assets, r.Assets[0]) }, true, false},
		{"no digest", "0.29.0", func(r *Release) { r.Assets[0].Digest = "" }, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := release()
			if tc.mutate != nil {
				tc.mutate(&r)
			}
			u, err := Select(r, tc.current, "darwin", "arm64")
			if (err != nil) != tc.wantError || (u != nil) != tc.wantUpdate {
				t.Fatalf("got %+v, %v", u, err)
			}
		})
	}
	if _, err := Select(release(), "0.29.0", "windows", "arm64"); err == nil {
		t.Fatal("accepted unsupported architecture")
	}
}
func TestCheck(t *testing.T) {
	data, _ := json.Marshal(release())
	u, err := client(200, string(data)).Check(context.Background(), "0.29.0", "linux", "arm64")
	if err != nil || u == nil {
		t.Fatalf("%+v %v", u, err)
	}
	for _, tc := range []struct {
		status int
		body   string
	}{{403, "rate limited"}, {404, "missing"}, {500, "unavailable"}, {200, "{"}, {200, strings.Repeat("x", (2<<20)+1)}} {
		if _, err := client(tc.status, tc.body).Check(context.Background(), "0.29.0", "linux", "arm64"); err == nil {
			t.Fatalf("accepted %+v", tc.status)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client(200, string(data)).Check(ctx, "0.29.0", "linux", "amd64"); err == nil {
		t.Fatal("ignored cancellation")
	}
}
func TestStage(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
		ok         bool
	}{
		{"valid", "payload", 200, true}, {"corrupt", "PAYLOAD", 200, false},
		{"truncated", "pay", 200, false}, {"oversized", "payload extra", 200, false}, {"http error", "payload", 503, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path, err := client(tc.status, tc.body).Stage(context.Background(), asset("test", "payload"), dir)
			if (err == nil) != tc.ok {
				t.Fatalf("path %q error %v", path, err)
			}
			entries, _ := os.ReadDir(dir)
			if !tc.ok {
				if len(entries) != 0 {
					t.Fatal("left partial file")
				}
				return
			}
			data, err := os.ReadFile(path)
			if err != nil || string(data) != "payload" {
				t.Fatalf("%q %v", data, err)
			}
		})
	}
}
func TestInvalidAssets(t *testing.T) {
	for _, mutate := range []func(*Asset){
		func(a *Asset) { a.URL = "http://github.com/kyleparisi/bufflehead/releases/download/v1/a" },
		func(a *Asset) { a.URL = "https://example.com/a" },
		func(a *Asset) { a.URL = "https://github.com/other/repo/releases/download/v1/a" },
		func(a *Asset) { a.Digest = "sha256:xyz" }, func(a *Asset) { a.Digest = "" },
		func(a *Asset) { a.Size = 0 }, func(a *Asset) { a.Size = maxAssetSize + 1 },
	} {
		a := asset("test", "payload")
		mutate(&a)
		c := Client{HTTP: &http.Client{Transport: transport(func(*http.Request) (*http.Response, error) { t.Fatal("requested invalid asset"); return nil, nil })}}
		if _, err := c.Stage(context.Background(), a, t.TempDir()); err == nil {
			t.Fatalf("accepted %+v", a)
		}
	}
}
func TestRejectDowngradeRedirect(t *testing.T) {
	c := Client{HTTP: &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Scheme != "https" {
			t.Fatal("followed insecure redirect")
		}
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{"http://example.com/file"}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	})}}
	if _, err := c.Stage(context.Background(), asset("test", "payload"), t.TempDir()); err == nil {
		t.Fatal("accepted insecure redirect")
	}
}
