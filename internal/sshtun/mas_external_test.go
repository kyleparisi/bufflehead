package sshtun

import (
	"bufflehead/internal/sandbox"
	"context"
	"fmt"
	"golang.org/x/crypto/ssh"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMASExternalGrantedSSHCredentials(t *testing.T) {
	dir := os.Getenv("SPIKE_EXTERNAL_SSH")
	if dir == "" {
		t.Skip("requires disposable external fixture and saved grant")
	}
	if !sandbox.Enabled() {
		t.Fatal("requires enforced sandbox")
	}
	path := filepath.Join(dir, "client")
	if _, err := os.ReadFile(path); err == nil {
		t.Fatal("credential readable before restoring scope")
	}
	grant := sandbox.New(sandbox.NewBookmarkStore())
	release, err := grant.Restore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	readKey := func(name string) ssh.Signer {
		data, e := os.ReadFile(filepath.Join(dir, name))
		if e != nil {
			t.Fatal(e)
		}
		key, e := ssh.ParsePrivateKey(data)
		if e != nil {
			t.Fatal(e)
		}
		return key
	}
	host, client := readKey("host"), readKey("client")
	srv := startSSHServer(t, host, client)
	fmt.Println("FIXTURE_HOST=" + srv.addr())
	ready := os.Getenv("SPIKE_KNOWN_HOSTS_READY")
	deadline := time.Now().Add(15 * time.Second)
	for {
		if _, e := os.Stat(ready); e == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("fixture writer did not finish")
		}
		time.Sleep(50 * time.Millisecond)
	}
	target := echoServer(t)
	cfg := Config{Host: "127.0.0.1", Port: srv.port(), User: "tester", Method: AuthKey, KeyPath: path, KnownHostsPath: filepath.Join(dir, "known_hosts"), RemoteHost: "127.0.0.1", RemotePort: target.Addr().(*net.TCPAddr).Port, Timeout: 5 * time.Second}
	tun, err := Start(context.Background(), cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tun.Stop()
	conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", tun.LocalPort()))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err = conn.Write([]byte("scope-ok")); err != nil {
		t.Fatal(err)
	}
	data := make([]byte, 8)
	if _, err = io.ReadFull(conn, data); err != nil {
		t.Fatal(err)
	}
	if string(data) != "scope-ok" {
		t.Fatal("bad forwarded data")
	}
	t.Log("ungranted credential denied; restored external key and known_hosts; verified SSH transport passed")
}
