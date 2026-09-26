package main

import (
	"fmt"
	"github.com/zalando/go-keyring"
	"os"
)

func main() {
	const service = "BuffleheadSandboxSpikeDisposable"
	const account = "v1-feasibility-test"
	const value = "nonsecret-fixture-only"
	if e := keyring.Set(service, account, value); e != nil {
		fmt.Printf("FAIL keychain Set: %v\n", e)
		os.Exit(1)
	}
	defer keyring.Delete(service, account)
	actual, e := keyring.Get(service, account)
	if e != nil || actual != value {
		fmt.Printf("FAIL keychain Get: %v\n", e)
		return
	}
	fmt.Println("PASS keychain round-trip")
}
