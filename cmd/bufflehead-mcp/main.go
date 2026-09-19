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

	client, err := resolveTarget(ctx, os.Getenv, configdir.Dir())
	if err != nil {
		log.Fatal(err)
	}

	if *check {
		fmt.Printf("Bufflehead reachable at %s (key accepted)\n", client.BaseURL)
		return
	}

	srv := mcpserver.New(client, buildinfo.Version)
	if err := srv.Run(ctx, &mcp.StdioTransport{}); err != nil && !errors.Is(err, context.Canceled) {
		log.Fatal(err)
	}
}

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
		log.Printf("warning: bridge %s talking to Bufflehead %s", buildinfo.Version, d.Version)
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
