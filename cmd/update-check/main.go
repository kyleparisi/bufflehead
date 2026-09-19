// update-check is an exploration tool, not an installer.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"runtime"

	"bufflehead/internal/updater"
)

func main() {
	current := flag.String("current", "", "installed version (required, e.g. 0.28.0)")
	goos := flag.String("os", runtime.GOOS, "target OS")
	arch := flag.String("arch", runtime.GOARCH, "target architecture")
	stage := flag.String("stage-dir", "", "optionally download and verify into this existing directory")
	flag.Parse()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	c := updater.Client{}
	u, err := c.Check(ctx, *current, *goos, *arch)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if u == nil {
		fmt.Println("No newer stable release.")
		return
	}
	result := struct{ Version, Asset, ReleaseURL, StagedPath string }{Version: u.Release.Tag, Asset: u.Asset.Name, ReleaseURL: u.Release.URL}
	if *stage != "" {
		result.StagedPath, err = c.Stage(ctx, u.Asset, *stage)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
