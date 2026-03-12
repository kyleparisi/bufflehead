package models

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
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
	os.MkdirAll(filepath.Dir(h.path), 0755)
	data, err := json.MarshalIndent(h.Entries, "", "  ")
	if err != nil {
		return
	}
	os.WriteFile(h.path, data, 0644)
}

func historyPath() string {
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
	return filepath.Join(base, "history.json")
}
