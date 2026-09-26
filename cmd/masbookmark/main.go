package main

import (
	"bufflehead/internal/sandbox"
	"context"
	"fmt"
	"github.com/progrium/darwinkit/macos/appkit"
	"os"
	"path/filepath"
	"runtime"
)

func main() {
	runtime.LockOSThread()
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "Usage: BookmarkTest /path/to/outside.csv (select its parent folder on first run; run again to test persistence)")
		os.Exit(2)
	}
	if os.Args[1] == "--check-sandbox" {
		fmt.Printf("sandbox detected: %v\n", sandbox.Enabled())
		return
	}
	appkit.Application_SharedApplication().SetActivationPolicy(appkit.ApplicationActivationPolicyRegular)
	fmt.Printf("sandbox detected: %v\n", sandbox.Enabled())
	if !sandbox.Enabled() {
		fmt.Fprintln(os.Stderr, "Sandbox detection failed; refusing to report a grant success")
		os.Exit(1)
	}
	tmp := os.TempDir()
	store := sandbox.NewBookmarkStoreAt(filepath.Join(tmp, "bufflehead-spike-grants.json"))
	g := sandbox.New(store)
	fmt.Printf("saved grant present before test: %v\n", g.Granted(os.Args[1]))
	release, err := g.Ensure(context.Background(), os.Args[1])
	if err != nil {
		panic(err)
	}
	defer release()
	data, err := os.ReadFile(os.Args[1])
	if err != nil {
		panic(err)
	}
	fmt.Printf("PASS: read %d bytes after scoped access\n", len(data))
}
