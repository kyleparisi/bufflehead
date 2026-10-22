package mcpmode

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"bufflehead/internal/connect"
	"bufflehead/internal/models"
)

// pingTimeout bounds the liveness check before a cached connection is reused.
const pingTimeout = 5 * time.Second

// conns opens bookmarks on first use and keeps them open for the life of the
// process. Each bookmark has its own slot lock, so a slow tunnel to one
// bookmark never blocks queries on another.
type conns struct {
	mu     sync.Mutex
	slots  map[string]*slot
	logins *awsLogins
}

type slot struct {
	mu      sync.Mutex
	conn    *connect.Conn
	updated time.Time // the bookmark's UpdatedAt when conn was opened
}

func newConns() *conns {
	return &conns{slots: map[string]*slot{}, logins: newAWSLogins()}
}

// bookmarks re-reads the store on every call: the app may add or edit
// bookmarks while this process runs.
func bookmarks() *models.BookmarkStore {
	return models.NewBookmarkStore()
}

// get returns an open connection for label, connecting on first use. A cached
// connection is reopened when its ping fails or the bookmark was edited since.
func (c *conns) get(ctx context.Context, label string) (*connect.Conn, error) {
	store := bookmarks()
	bm := store.FindByLabel(label)
	if bm == nil {
		labels := make([]string, 0)
		for _, b := range store.All() {
			labels = append(labels, b.Label)
		}
		return nil, fmt.Errorf("no bookmark %q; saved bookmarks: %s", label, strings.Join(labels, ", "))
	}

	c.mu.Lock()
	s := c.slots[label]
	if s == nil {
		s = &slot{}
		c.slots[label] = s
	}
	c.mu.Unlock()

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.conn != nil {
		if s.updated.Equal(bm.UpdatedAt) {
			pingCtx, cancel := context.WithTimeout(ctx, pingTimeout)
			err := s.conn.DB.Ping(pingCtx)
			cancel()
			if err == nil {
				return s.conn, nil
			}
			log.Printf("%s: ping failed, reconnecting: %v", label, err)
		}
		s.conn.Close()
		s.conn = nil
	}

	entry := bm.ToGatewayEntry(0)
	// AWS gateways need live SSO credentials for the bastion lookup, the SSM
	// tunnel and IAM database auth. They expire daily; log in again first
	// rather than failing later with an error from one of those steps.
	if !entry.IsBigQuery() && !entry.IsDirect() && !entry.IsMySQL() {
		if err := c.logins.ensure(ctx, entry.AWSProfile, entry.AWSRegion); err != nil {
			return nil, err
		}
	}

	conn, err := connect.Open(ctx, entry, "mcp", func(msg string) {
		log.Printf("%s: %s", label, msg)
	})
	if err != nil {
		return nil, fmt.Errorf("connect %s: %w", label, err)
	}
	s.conn = conn
	s.updated = bm.UpdatedAt
	return conn, nil
}

// connected reports whether label currently has an open connection. A slot
// that is busy connecting or querying reports false rather than waiting.
func (c *conns) connected(label string) bool {
	c.mu.Lock()
	s := c.slots[label]
	c.mu.Unlock()
	if s == nil || !s.mu.TryLock() {
		return false
	}
	defer s.mu.Unlock()
	return s.conn != nil
}

// closeAll closes every connection and stops its tunnels.
func (c *conns) closeAll() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, s := range c.slots {
		s.mu.Lock()
		if s.conn != nil {
			s.conn.Close()
			s.conn = nil
		}
		s.mu.Unlock()
	}
}
