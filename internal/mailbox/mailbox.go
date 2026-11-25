// Package mailbox mirrors the selected IMAP inbox into a private DuckDB cache.
package mailbox

import (
	"context"
	"crypto/tls"
	"database/sql"
	"fmt"
	"net"
	"slices"
	"strings"
	"time"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
	"github.com/emersion/go-message/charset"
)

func init() { imap.CharsetReader = charset.Reader }

// Sync opens INBOX read-only. The caller holds the cache lock through sync
// and snapshot creation. Each committed batch can be reused after reconnect.
func Sync(ctx context.Context, cache *sql.DB, address, username, password string) error {
	dialer := tls.Dialer{NetDialer: &net.Dialer{Timeout: 20 * time.Second}, Config: &tls.Config{MinVersion: tls.VersionTLS12}}
	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return err
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	if err := conn.SetDeadline(time.Now().Add(30 * time.Second)); err != nil {
		return err
	}
	c, err := client.New(conn)
	if err != nil {
		return err
	}
	c.Timeout = 90 * time.Second
	if err := syncInbox(ctx, cache, c, username, password); err != nil {
		return err
	}
	_ = c.Logout()
	return nil
}

func syncInbox(ctx context.Context, cache *sql.DB, c *client.Client, username, password string) error {
	if err := c.Login(username, password); err != nil {
		return fmt.Errorf("IMAP login: %w", err)
	}
	status, err := c.Select("INBOX", true)
	if err != nil {
		return err
	}
	if status.UidValidity == 0 {
		return fmt.Errorf("server omitted inbox UIDVALIDITY")
	}
	ids, err := c.UidSearch(imap.NewSearchCriteria())
	if err != nil {
		return err
	}
	return reconcile(ctx, cache, status.UidValidity, ids, func(ids []uint32) ([]message, error) { return fetchMessages(c, ids) })
}

type message struct {
	uid                                          uint32
	messageID, sender, recipients, subject, body string
	received                                     time.Time
}

func reconcile(ctx context.Context, cache *sql.DB, validity uint32, ids []uint32, fetch func([]uint32) ([]message, error)) error {
	_, err := cache.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS messages (
 uid UINTEGER PRIMARY KEY, message_id VARCHAR, received_at TIMESTAMPTZ, sender VARCHAR,
 recipients VARCHAR, subject VARCHAR, body_text VARCHAR);
 CREATE TABLE IF NOT EXISTS sync_status (uid_validity UINTEGER, last_synced_at TIMESTAMPTZ)`)
	if err != nil {
		return err
	}
	var old uint32
	err = cache.QueryRowContext(ctx, "SELECT uid_validity FROM sync_status").Scan(&old)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if old != validity {
		tx, err := cache.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		if _, err = tx.ExecContext(ctx, "DELETE FROM messages; DELETE FROM sync_status"); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO sync_status VALUES (?, NULL)", validity); err != nil {
			return err
		}
		if err = tx.Commit(); err != nil {
			return err
		}
	}
	rows, err := cache.QueryContext(ctx, "SELECT uid FROM messages")
	if err != nil {
		return err
	}
	known := make(map[uint32]bool)
	for rows.Next() {
		var uid uint32
		if err := rows.Scan(&uid); err != nil {
			rows.Close()
			return err
		}
		known[uid] = true
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	var missing []uint32
	for _, uid := range ids {
		if !known[uid] {
			missing = append(missing, uid)
		}
		delete(known, uid)
	}
	for batch := range slices.Chunk(missing, 50) {
		msgs, err := fetch(batch)
		if err != nil {
			return err
		}
		if err := writeBatch(ctx, cache, msgs); err != nil {
			return err
		}
	}
	tx, err := cache.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for uid := range known {
		if _, err := tx.ExecContext(ctx, "DELETE FROM messages WHERE uid = ?", uid); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, "UPDATE sync_status SET last_synced_at = current_timestamp"); err != nil {
		return err
	}
	return tx.Commit()
}

func writeBatch(ctx context.Context, cache *sql.DB, msgs []message) error {
	tx, err := cache.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, m := range msgs {
		if _, err := tx.ExecContext(ctx, "INSERT INTO messages VALUES (?, ?, ?, ?, ?, ?, ?)", m.uid, m.messageID, m.received, m.sender, m.recipients, m.subject, m.body); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func fetchMessages(c *client.Client, ids []uint32) ([]message, error) {
	set := new(imap.SeqSet)
	set.AddNum(ids...)
	// The channel is drained concurrently because the server can also send
	// unsolicited FETCH updates during a command.
	meta, err := fetch(c, set, []imap.FetchItem{imap.FetchUid, imap.FetchEnvelope, imap.FetchInternalDate, imap.FetchBodyStructure})
	if err != nil {
		return nil, err
	}
	var out []message
	for _, m := range meta {
		if m.Uid == 0 || !set.Contains(m.Uid) || m.Envelope == nil || m.BodyStructure == nil {
			continue
		}
		body, err := fetchText(c, m)
		if err != nil {
			return nil, fmt.Errorf("inbox UID %d: %w", m.Uid, err)
		}
		e := m.Envelope
		out = append(out, message{uid: m.Uid, messageID: e.MessageId, received: m.InternalDate, sender: addresses(e.From), recipients: addresses(e.To), subject: e.Subject, body: body})
	}
	return out, nil
}

func fetch(c *client.Client, set *imap.SeqSet, items []imap.FetchItem) ([]*imap.Message, error) {
	ch := make(chan *imap.Message)
	done := make(chan error, 1)
	go func() { done <- c.UidFetch(set, items, ch) }()
	var messages []*imap.Message
	for m := range ch {
		messages = append(messages, m)
	}
	return messages, <-done
}

func addresses(addrs []*imap.Address) string {
	var result []string
	for _, a := range addrs {
		if a != nil && a.HostName != "" {
			result = append(result, a.Address())
		}
	}
	return strings.Join(result, ", ")
}
