package ui

import (
	"io"
	"log"
	"os"
	"strings"
	"sync"
)

// logSink is a thread-safe, bounded in-memory buffer of recent log/console
// lines. It implements io.Writer so it can be installed as (part of) the
// standard logger's output, and it accumulates app-level messages (errors,
// status) posted via LogConsole. The ConsolePanel drains new lines on the main
// thread each frame.
type logSink struct {
	mu       sync.Mutex
	lines    []string
	maxLines int
	seq      uint64 // monotonically increasing count of appended lines
}

// consoleLog is the process-wide console sink. It is safe to use before the UI
// is built; the ConsolePanel attaches to it when created.
var consoleLog = &logSink{maxLines: 2000}

// InstallConsoleLog redirects the standard logger to also feed the in-app
// console, while preserving normal stderr logging. Call once at startup.
func InstallConsoleLog() {
	log.SetOutput(io.MultiWriter(os.Stderr, consoleLog))
}

// Write implements io.Writer. Each call may contain one or more newline
// separated log records; they are split into individual lines.
func (s *logSink) Write(p []byte) (int, error) {
	text := strings.TrimRight(string(p), "\n")
	if text != "" {
		s.append(text)
	}
	return len(p), nil
}

// LogConsole appends an application message (e.g. an error) to the console. The
// message may span multiple lines; each is stored separately.
func LogConsole(msg string) {
	if msg == "" {
		return
	}
	consoleLog.append(msg)
}

func (s *logSink) append(text string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, line := range strings.Split(text, "\n") {
		s.lines = append(s.lines, line)
		s.seq++
	}
	if len(s.lines) > s.maxLines {
		drop := len(s.lines) - s.maxLines
		s.lines = append(s.lines[:0], s.lines[drop:]...)
	}
}

// snapshot returns a copy of all buffered lines and the current sequence number.
func (s *logSink) snapshot() ([]string, uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.lines))
	copy(out, s.lines)
	return out, s.seq
}

// seqNum returns the current append sequence number without copying lines.
func (s *logSink) seqNum() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.seq
}

// clear empties the buffer.
func (s *logSink) clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lines = nil
}
