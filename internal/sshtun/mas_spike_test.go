package sshtun

import (
	"bufflehead/internal/db"
	"context"
	"os"
	"strconv"
	"testing"
)

func TestMASSpikePostgresThroughSSH(t *testing.T) {
	port, e := strconv.Atoi(os.Getenv("SPIKE_PG_PORT"))
	if e != nil {
		t.Skip("SPIKE_PG_PORT not set")
	}
	host, _ := newKey(t)
	client, pem := newKey(t)
	srv := startSSHServer(t, host, client)
	cfg := testConfig(t, srv, host, pem, echoServer(t))
	cfg.RemotePort = port
	tunnel, e := Start(context.Background(), cfg, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer tunnel.Stop()
	pg, e := db.NewPostgresDirect("127.0.0.1", tunnel.LocalPort(), "postgres", "spike", "", "disable")
	if e != nil {
		t.Fatal(e)
	}
	defer pg.Close()
	r, e := pg.Query(context.Background(), "SELECT 42 AS answer, current_setting('default_transaction_read_only') AS read_only", 0, 10)
	if e != nil {
		t.Fatal(e)
	}
	if len(r.Rows) != 1 || r.Rows[0][0] != "42" || r.Rows[0][1] != "on" {
		t.Fatalf("unexpected result: %+v", r)
	}
	t.Log("Postgres through verified SSH tunnel: answer=42, read_only=on")
}
