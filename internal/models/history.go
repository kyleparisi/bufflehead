package models

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type HistoryEntry struct {
	SQL       string    `json:"sql"`
	FilePath  string    `json:"filePath,omitempty"`
	Timestamp time.Time `json:"timestamp"`
	RowCount  int64     `json:"rowCount,omitempty"`
	DurationMs int64   `json:"durationMs,omitempty"`
}

type QueryHistory struct {
	mu      sync.Mutex
	Entries []HistoryEntry `json:"entries"`
	path    string
	maxSize int
}

func NewQueryHistory() *QueryHistory {
	h := &QueryHistory{maxSize: 500}
	h.path = historyPath()
	h.load()
	return h
}

func (h *QueryHistory) Add(entry HistoryEntry) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.Entries = append(h.Entries, entry)
	if len(h.Entries) > h.maxSize {
		h.Entries = h.Entries[len(h.Entries)-h.maxSize:]
	}
	h.save()
}

func (h *QueryHistory) Recent(n int) []HistoryEntry {
	h.mu.Lock()
	defer h.mu.Unlock()
	if n > len(h.Entries) {
		n = len(h.Entries)
	}
	// Return newest first
	result := make([]HistoryEntry, n)
	for i := 0; i < n; i++ {
		result[i] = h.Entries[len(h.Entries)-1-i]
	}
	return result
}

func (h *QueryHistory) All() []HistoryEntry {
	return h.Recent(len(h.Entries))
}

func (h *QueryHistory) load() {
	data, err := os.ReadFile(h.path)
	if err != nil {
		return
	}
	json.Unmarshal(data, &h.Entries)
}

func (h *QueryHistory) save() {
	dir := filepath.Dir(h.path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "history: failed to create dir %s: %v\n", dir, err)
		return
	}
	data, err := json.MarshalIndent(h.Entries, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "history: failed to marshal: %v\n", err)
		return
	}
	if err := os.WriteFile(h.path, data, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "history: failed to write %s: %v\n", h.path, err)
	}
}

func historyPath() string {
	return filepath.Join(ConfigDir(), "history.json")
}
