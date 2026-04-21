package models

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sync"
	"time"
)

var labelRegexp = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,62}$`)

// ValidateLabel checks that a bookmark label is safe for use in URLs, JSON, and the HTTP API.
func ValidateLabel(label string) error {
	if !labelRegexp.MatchString(label) {
		return fmt.Errorf("label must start with a letter or number, contain only letters/numbers/dashes/underscores, and be 1-63 chars")
	}
	return nil
}

// Bookmark represents a saved gateway connection.
type Bookmark struct {
	Label        string            `json:"label"`
	Env          string            `json:"env,omitempty"`
	AWSProfile   string            `json:"aws_profile"`
	AWSRegion    string            `json:"aws_region"`
	InstanceID   string            `json:"instance_id,omitempty"`
	InstanceTags map[string]string `json:"instance_tags,omitempty"`
	RDSHost      string            `json:"rds_host"`
	RDSPort      int               `json:"rds_port"`
	DBName       string            `json:"db_name"`
	DBUser       string            `json:"db_user"`
	DBPasswordEnv string           `json:"db_password_env,omitempty"`
	AuthMode     string            `json:"auth_mode,omitempty"`
	CreatedAt    time.Time         `json:"created_at"`
	UpdatedAt    time.Time         `json:"updated_at"`
}

// ToGatewayEntry converts a bookmark to a GatewayEntry for the connection pipeline.
// localPort is assigned at connection time via FindFreePort().
func (b *Bookmark) ToGatewayEntry(localPort int) GatewayEntry {
	return GatewayEntry{
		Name:          b.Label,
		AWSProfile:    b.AWSProfile,
		AWSRegion:     b.AWSRegion,
		InstanceID:    b.InstanceID,
		InstanceTags:  b.InstanceTags,
		RDSHost:       b.RDSHost,
		RDSPort:       b.RDSPort,
		LocalPort:     localPort,
		DBName:        b.DBName,
		DBUser:        b.DBUser,
		DBPasswordEnv: b.DBPasswordEnv,
		AuthMode:      b.AuthMode,
	}
}

// BookmarkStore manages bookmark persistence to a JSON file.
type BookmarkStore struct {
	mu        sync.Mutex
	Bookmarks []Bookmark `json:"bookmarks"`
	path      string
}

// NewBookmarkStore loads bookmarks from the default config path.
func NewBookmarkStore() *BookmarkStore {
	bs := &BookmarkStore{}
	bs.path = bookmarkPath()
	bs.load()
	return bs
}

// Add saves a new bookmark. If a bookmark with the same label exists, it is updated.
func (bs *BookmarkStore) Add(b Bookmark) error {
	if err := ValidateLabel(b.Label); err != nil {
		return err
	}

	bs.mu.Lock()
	defer bs.mu.Unlock()

	now := time.Now()
	for i, existing := range bs.Bookmarks {
		if existing.Label == b.Label {
			b.CreatedAt = existing.CreatedAt
			b.UpdatedAt = now
			bs.Bookmarks[i] = b
			bs.save()
			return nil
		}
	}

	b.CreatedAt = now
	b.UpdatedAt = now
	bs.Bookmarks = append(bs.Bookmarks, b)
	bs.save()
	return nil
}

// Remove deletes a bookmark by label.
func (bs *BookmarkStore) Remove(label string) error {
	bs.mu.Lock()
	defer bs.mu.Unlock()

	for i, b := range bs.Bookmarks {
		if b.Label == label {
			bs.Bookmarks = append(bs.Bookmarks[:i], bs.Bookmarks[i+1:]...)
			bs.save()
			return nil
		}
	}
	return fmt.Errorf("bookmark %q not found", label)
}

// FindByLabel returns a pointer to the bookmark with the given label, or nil.
func (bs *BookmarkStore) FindByLabel(label string) *Bookmark {
	bs.mu.Lock()
	defer bs.mu.Unlock()

	for i := range bs.Bookmarks {
		if bs.Bookmarks[i].Label == label {
			return &bs.Bookmarks[i]
		}
	}
	return nil
}

// All returns a copy of all bookmarks.
func (bs *BookmarkStore) All() []Bookmark {
	bs.mu.Lock()
	defer bs.mu.Unlock()

	result := make([]Bookmark, len(bs.Bookmarks))
	copy(result, bs.Bookmarks)
	return result
}

func (bs *BookmarkStore) load() {
	data, err := os.ReadFile(bs.path)
	if err != nil {
		return
	}
	json.Unmarshal(data, &bs.Bookmarks)
}

func (bs *BookmarkStore) save() {
	os.MkdirAll(filepath.Dir(bs.path), 0755)
	data, err := json.MarshalIndent(bs.Bookmarks, "", "  ")
	if err != nil {
		return
	}
	os.WriteFile(bs.path, data, 0644)
}

func bookmarkPath() string {
	var base string
	switch runtime.GOOS {
	case "darwin":
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, "Library", "Application Support", "Bufflehead")
	case "windows":
		base = filepath.Join(os.Getenv("APPDATA"), "Bufflehead")
	default:
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".config", "bufflehead")
	}
	return filepath.Join(base, "bookmarks.json")
}
