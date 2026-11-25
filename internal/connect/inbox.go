package connect

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"bufflehead/internal/db"
	"bufflehead/internal/mailbox"
	"bufflehead/internal/models"
	"github.com/gofrs/flock"
)

func openInbox(ctx context.Context, entry models.GatewayEntry) (db.Querier, []db.TableInfo, error) {
	port := entry.RDSPort
	if port == 0 {
		port = 993
	}
	endpoint := net.JoinHostPort(entry.RDSHost, strconv.Itoa(port))
	password := entry.ResolvePassword()
	if entry.RDSHost == "" || entry.DBUser == "" || password == "" {
		return nil, nil, fmt.Errorf("IMAP requires a host, username and password")
	}
	dir := filepath.Join(models.ConfigDir(), "inboxes")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, nil, err
	}
	id := sha256.Sum256([]byte(strings.ToLower(endpoint) + "\x00" + entry.DBUser))
	path := filepath.Join(dir, fmt.Sprintf("%x.duckdb", id))
	lock := flock.New(path + ".lock")
	locked, err := lock.TryLockContext(ctx, 100*time.Millisecond)
	if err != nil {
		return nil, nil, err
	}
	if !locked {
		return nil, nil, fmt.Errorf("could not lock inbox cache")
	}
	defer lock.Unlock()
	cache, err := db.OpenMailboxWriter(path)
	if err != nil {
		return nil, nil, err
	}
	err = mailbox.Sync(ctx, cache, endpoint, entry.DBUser, password)
	closeErr := cache.Close()
	if err != nil {
		return nil, nil, fmt.Errorf("sync INBOX: %w", err)
	}
	if closeErr != nil {
		return nil, nil, closeErr
	}
	snap, err := db.MailboxSnapshot(ctx, path)
	if err != nil {
		return nil, nil, err
	}
	tables, err := snap.Tables()
	for i := range tables {
		if err != nil {
			break
		}
		tables[i].Columns, err = snap.TableSchema(tables[i].Name)
	}
	if err != nil {
		snap.Close()
		return nil, nil, err
	}
	return snap, tables, nil
}
