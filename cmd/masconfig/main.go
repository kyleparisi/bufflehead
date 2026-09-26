package main

import (
	"bufflehead/internal/models"
	"bufflehead/internal/sandbox"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	if !sandbox.Enabled() {
		panic("requires sandbox")
	}
	if os.Getenv("BUFFLEHEAD_SPIKE_CONFIG_DIR") != "" {
		panic("test must not use override")
	}
	dir := models.ConfigDir()
	if !strings.Contains(dir, "/Library/Containers/com.bufflehead.sandbox-config-test/Data/") {
		panic("not container-local: " + dir)
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		panic(err)
	}
	p := filepath.Join(dir, "disposable-config-probe")
	if err := os.WriteFile(p, []byte("test"), 0600); err != nil {
		panic(err)
	}
	if err := os.Remove(p); err != nil {
		panic(err)
	}
	fmt.Println("PASS: native store config resolves inside sandbox container and is writable")
}
