// Command bufflehead-mcp is the stdio MCP bridge for Bufflehead.
//
// Claude Desktop (and other MCP clients) launch local servers as stdio
// subprocesses, but Bufflehead is a Godot GUI app whose control port and key
// change every launch. This binary is what the client spawns: it finds the
// running app through the discovery file the app writes on startup, then
// serves the same MCP tool set the app exposes at /mcp — over stdio, backed by
// the app's REST control API.
//
// It is built with CGO_ENABLED=0 and must never import internal/db or
// internal/ui.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"sync"

	"bufflehead/internal/buildinfo"
	"bufflehead/internal/configdir"
	"bufflehead/internal/control"
	"bufflehead/internal/mcpserver"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	showVersion := flag.Bool("version", false, "print the bridge version and exit")
	check := flag.Bool("check", false, "find the running Bufflehead, verify the key, print status and exit")
	printConfig := flag.Bool("print-config", false, "print a claude_desktop_config.json snippet for this binary and exit")
	flag.Parse()

	// stdout is the MCP transport; everything human-readable goes to stderr.
	log.SetOutput(os.Stderr)
	log.SetFlags(0)
	log.SetPrefix("bufflehead-mcp: ")

	switch {
	case *showVersion:
		fmt.Println(buildinfo.Version)
		return
	case *printConfig:
		fmt.Println(configSnippet())
		return
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	backend := &lazyBackend{getenv: os.Getenv, dir: configdir.Dir()}

	if *check {
		client, err := backend.resolve(ctx)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("Bufflehead reachable at %s (key accepted)\n", client.BaseURL)
		return
	}

	// Don't exit if the app isn't up: the MCP client keeps this process alive
	// for its whole session, so serve anyway and let each tool call re-resolve.
	if _, err := backend.resolve(ctx); err != nil {
		log.Printf("%v — serving anyway; each tool call retries discovery", err)
	}

	srv := mcpserver.New(backend, buildinfo.Version)
	if err := srv.Run(ctx, &mcp.StdioTransport{}); err != nil && !errors.Is(err, context.Canceled) {
		log.Fatal(err)
	}
}

// lazyBackend implements mcpserver.Backend by resolving the running app on
// every tool call instead of once at startup: the MCP client keeps this
// process alive for a whole session, while Bufflehead's control port and key
// rotate each launch. Resolution is one small file read plus a localhost ping,
// so a failed call heals itself as soon as the app is running again.
type lazyBackend struct {
	getenv func(string) string
	dir    string
}

func (b *lazyBackend) resolve(ctx context.Context) (*control.Client, error) {
	return resolveTarget(ctx, b.getenv, b.dir)
}

func (b *lazyBackend) Connections(ctx context.Context, includeColumns bool) ([]control.ConnectionInfo, error) {
	c, err := b.resolve(ctx)
	if err != nil {
		return nil, err
	}
	return c.Connections(ctx, includeColumns)
}

func (b *lazyBackend) ExecSQL(ctx context.Context, req control.SQLRequest) (*control.SQLResult, error) {
	c, err := b.resolve(ctx)
	if err != nil {
		return nil, err
	}
	return c.ExecSQL(ctx, req)
}

func (b *lazyBackend) CancelSQL(ctx context.Context, conn string) error {
	c, err := b.resolve(ctx)
	if err != nil {
		return err
	}
	return c.CancelSQL(ctx, conn)
}

func (b *lazyBackend) GetS3Object(ctx context.Context, req control.S3GetObjectRequest) (*control.S3GetObjectResult, error) {
	c, err := b.resolve(ctx)
	if err != nil {
		return nil, err
	}
	return c.GetS3Object(ctx, req)
}

func (b *lazyBackend) Reconnect(ctx context.Context, conn string) (*control.ReconnectResult, error) {
	c, err := b.resolve(ctx)
	if err != nil {
		return nil, err
	}
	return c.Reconnect(ctx, conn)
}

// versionWarn keeps the bridge/app version-mismatch warning to one line per
// process, since resolveTarget now runs on every tool call.
var versionWarn sync.Once

// resolveTarget builds a control client for the running app and proves it is
// reachable. BUFFLEHEAD_CONTROL_ADDR + BUFFLEHEAD_CONTROL_KEY (as set by the
// integration harness or a developer) win over the discovery file so the
// bridge can be pointed at any instance explicitly.
func resolveTarget(ctx context.Context, getenv func(string) string, dir string) (*control.Client, error) {
	addr, key := getenv("BUFFLEHEAD_CONTROL_ADDR"), getenv("BUFFLEHEAD_CONTROL_KEY")
	var (
		client *control.Client
		source string
		d      *control.Discovery
	)
	if addr != "" && key != "" {
		client = control.NewClient(addr, key)
		source = "BUFFLEHEAD_CONTROL_ADDR"
	} else {
		var err error
		d, err = control.ReadDiscovery(dir)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil, fmt.Errorf("Bufflehead is not running (no %s). Start Bufflehead and retry", control.DiscoveryPath(dir))
			}
			return nil, err
		}
		client = control.NewClient(d.Addr, d.Key)
		source = control.DiscoveryPath(dir)
		if d.PID > 0 && !processAlive(d.PID) {
			return nil, fmt.Errorf("Bufflehead is not running: %s names pid %d, which has exited (the file is stale). Start Bufflehead and retry", source, d.PID)
		}
	}

	if err := client.Ping(ctx); err != nil {
		if errors.Is(err, control.ErrUnauthorized) {
			return nil, fmt.Errorf("Bufflehead at %s rejected the control key from %s; restart Bufflehead so it publishes a fresh key", client.BaseURL, source)
		}
		return nil, fmt.Errorf("Bufflehead is not reachable at %s (from %s): %v. Start Bufflehead and retry", client.BaseURL, source, err)
	}
	if d != nil && d.Version != "" && d.Version != buildinfo.Version {
		versionWarn.Do(func() {
			log.Printf("warning: bridge %s talking to Bufflehead %s", buildinfo.Version, d.Version)
		})
	}
	return client, nil
}

// configSnippet is the claude_desktop_config.json entry for this executable.
func configSnippet() string {
	exe, err := os.Executable()
	if err != nil {
		exe = "bufflehead-mcp"
	} else if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	cfg := map[string]any{
		"mcpServers": map[string]any{
			"bufflehead": map[string]any{"command": exe},
		},
	}
	b, _ := json.MarshalIndent(cfg, "", "  ")
	return string(b)
}
