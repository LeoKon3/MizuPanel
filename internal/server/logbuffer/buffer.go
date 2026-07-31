package logbuffer

import (
	"bytes"
	"strings"
	"sync"
	"time"
)

const (
	DefaultMaxEntries = 10_000
	DefaultMaxBytes   = 2 << 20
)

type Snapshot struct {
	Content       string
	ReturnedLines int
	StartedAt     time.Time
	Truncated     bool
}

// Buffer keeps a bounded, in-memory copy of complete log writes. It is safe
// for concurrent use and deliberately has no persistence behavior.
type Buffer struct {
	mu         sync.RWMutex
	entries    []string
	totalBytes int
	maxEntries int
	maxBytes   int
	startedAt  time.Time
	evicted    bool
}

func New(maxEntries, maxBytes int) *Buffer {
	if maxEntries <= 0 {
		maxEntries = DefaultMaxEntries
	}
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBytes
	}
	return &Buffer{
		entries:    make([]string, 0, min(maxEntries, 256)),
		maxEntries: maxEntries,
		maxBytes:   maxBytes,
		startedAt:  time.Now().UTC(),
	}
}

func (b *Buffer) Write(data []byte) (int, error) {
	written := len(data)
	if written == 0 {
		return 0, nil
	}

	parts := bytes.SplitAfter(data, []byte{'\n'})
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, part := range parts {
		if len(part) == 0 {
			continue
		}
		b.appendLocked(string(bytes.Clone(part)))
	}
	return written, nil
}

func (b *Buffer) Snapshot(limit int) Snapshot {
	b.mu.RLock()
	defer b.mu.RUnlock()

	start := 0
	if limit > 0 && limit < len(b.entries) {
		start = len(b.entries) - limit
	}
	entries := append([]string(nil), b.entries[start:]...)
	return Snapshot{
		Content:       strings.Join(entries, ""),
		ReturnedLines: len(entries),
		StartedAt:     b.startedAt,
		Truncated:     b.evicted || start > 0,
	}
}

func (b *Buffer) appendLocked(entry string) {
	if len(entry) > b.maxBytes {
		entry = entry[len(entry)-b.maxBytes:]
		b.evicted = true
	}
	b.entries = append(b.entries, entry)
	b.totalBytes += len(entry)

	for len(b.entries) > b.maxEntries || b.totalBytes > b.maxBytes {
		b.totalBytes -= len(b.entries[0])
		b.entries[0] = ""
		b.entries = b.entries[1:]
		b.evicted = true
	}
}
