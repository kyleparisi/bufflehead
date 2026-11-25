package mailbox

import (
	"context"
	"errors"
	"net"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"bufflehead/internal/db"
	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/backend/memory"
	"github.com/emersion/go-imap/client"
	"github.com/emersion/go-imap/server"
)

func TestInboxToSQL(t *testing.T) {
	// Serve a real IMAP conversation on loopback. The memory backend supplies
	// one inbox with a known sender, message ID and single-part text body.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := server.New(memory.New())
	srv.AllowInsecureAuth = true
	go srv.Serve(listener)
	defer srv.Close()
	c, err := client.Dial(listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Logout()
	c.Timeout = 5 * time.Second
	path := filepath.Join(t.TempDir(), "inbox.duckdb")
	cache, err := db.OpenMailboxWriter(path)
	if err != nil {
		t.Fatal(err)
	}
	defer cache.Close()
	ctx := context.Background()
	if err := syncInbox(ctx, cache, c, "username", "password"); err != nil {
		t.Fatal(err)
	}
	if err := cache.Close(); err != nil {
		t.Fatal(err)
	}
	snap, err := db.MailboxSnapshot(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer snap.Close()
	rows, err := snap.Query(ctx, "SELECT sender, body_text FROM messages WHERE uid = 6", 0, 10)
	if err != nil || !reflect.DeepEqual(rows.Rows, [][]string{{"contact@example.org", "Hi there :)"}}) {
		t.Fatalf("query = %#v, %v", rows, err)
	}
	for _, query := range []string{"DELETE FROM messages", "SELECT 1; SELECT 2"} {
		if _, err := snap.Query(ctx, query, 0, 1); err == nil {
			t.Fatalf("accepted %q", query)
		}
	}
	if _, err := snap.Query(ctx, "SELECT * FROM read_text('/etc/hosts')", 0, 1); err == nil {
		t.Fatal("snapshot allowed external access")
	}
	cache, err = db.OpenMailboxWriter(path)
	if err != nil {
		t.Fatal(err)
	}
	defer cache.Close()
	if err := reconcile(ctx, cache, 1, []uint32{6}, func([]uint32) ([]message, error) { return nil, errors.New("unchanged body fetched again") }); err != nil {
		t.Fatal(err)
	}
	// The server resets its UID namespace and reuses a UID for a new message.
	if err := reconcile(ctx, cache, 2, []uint32{6}, func(ids []uint32) ([]message, error) {
		return []message{{uid: ids[0], subject: "replacement", received: time.Now()}}, nil
	}); err != nil {
		t.Fatal(err)
	}
	var subject string
	if err := cache.QueryRow("SELECT subject FROM messages").Scan(&subject); err != nil || subject != "replacement" {
		t.Fatalf("reset = %q, %v", subject, err)
	}
	if err := reconcile(ctx, cache, 2, nil, nil); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := cache.QueryRow("SELECT COUNT(*) FROM messages").Scan(&count); err != nil || count != 0 {
		t.Fatalf("expunged count = %d, %v", count, err)
	}
	rows, err = snap.Query(ctx, "SELECT body_text FROM messages", 0, 10)
	if err != nil || rows.Total != 1 {
		t.Fatalf("previous snapshot changed: %v %v", rows, err)
	}
}

func TestInlineBodyParts(t *testing.T) {
	plain := &imap.BodyStructure{MIMEType: "text", MIMESubType: "plain"}
	html := &imap.BodyStructure{MIMEType: "text", MIMESubType: "html", Encoding: "quoted-printable", Params: map[string]string{"charset": "iso-8859-1"}}
	mixed := &imap.BodyStructure{MIMEType: "multipart", MIMESubType: "mixed", Parts: []*imap.BodyStructure{
		{MIMEType: "multipart", MIMESubType: "alternative", Parts: []*imap.BodyStructure{plain, html}},
		{MIMEType: "text", MIMESubType: "plain", Disposition: "attachment"},
	}}
	parts := textParts(mixed, nil)
	if len(parts) != 1 || !reflect.DeepEqual(parts[0].path, []int{1, 1}) {
		t.Fatalf("selected parts %v", parts)
	}
	got, err := decodeText(html, strings.NewReader("<p>caf=E9 &amp; tea</p>"))
	if err != nil || got != "café & tea" {
		t.Fatalf("html = %q, %v", got, err)
	}
}

func TestResumeInterruptedInbox(t *testing.T) {
	cache, err := db.OpenMailboxWriter(filepath.Join(t.TempDir(), "resume.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	defer cache.Close()
	ctx := context.Background()
	ids := make([]uint32, 51)
	for i := range ids {
		ids[i] = uint32(i + 1)
	}
	interrupted := errors.New("connection closed")
	fetch := func(ids []uint32) ([]message, error) {
		msgs := make([]message, 0, len(ids))
		for _, uid := range ids {
			msgs = append(msgs, message{uid: uid, received: time.Now()})
		}
		return msgs, nil
	}
	err = reconcile(ctx, cache, 1, ids, func(batch []uint32) ([]message, error) {
		if batch[0] == 51 {
			return nil, interrupted
		}
		return fetch(batch)
	})
	if !errors.Is(err, interrupted) {
		t.Fatalf("partial sync returned %v", err)
	}
	var unfinished bool
	if err := cache.QueryRow("SELECT last_synced_at IS NULL FROM sync_status").Scan(&unfinished); err != nil || !unfinished {
		t.Fatalf("partial sync marked complete: %v", err)
	}
	err = reconcile(ctx, cache, 1, ids, func(batch []uint32) ([]message, error) {
		if !reflect.DeepEqual(batch, []uint32{51}) {
			t.Fatalf("retry fetched %v", batch)
		}
		return fetch(batch)
	})
	if err != nil {
		t.Fatal(err)
	}
	var count int
	if err := cache.QueryRow("SELECT COUNT(*) FROM messages").Scan(&count); err != nil || count != 51 {
		t.Fatalf("resume count %d: %v", count, err)
	}
}
