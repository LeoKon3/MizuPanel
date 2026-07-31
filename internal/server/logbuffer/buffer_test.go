package logbuffer

import (
	"fmt"
	"strings"
	"sync"
	"testing"
)

func TestBufferReturnsNewestLinesAndReportsTruncation(t *testing.T) {
	buffer := New(10, 1024)
	_, _ = buffer.Write([]byte("first\nsecond\nthird\n"))

	snapshot := buffer.Snapshot(2)
	if snapshot.Content != "second\nthird\n" {
		t.Fatalf("content = %q", snapshot.Content)
	}
	if snapshot.ReturnedLines != 2 || !snapshot.Truncated {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	if snapshot.StartedAt.IsZero() {
		t.Fatal("started_at is zero")
	}
}

func TestBufferEvictsByEntryAndByteLimits(t *testing.T) {
	buffer := New(2, 8)
	_, _ = buffer.Write([]byte("one\n"))
	_, _ = buffer.Write([]byte("two\n"))
	_, _ = buffer.Write([]byte("three\n"))

	snapshot := buffer.Snapshot(10)
	if snapshot.Content != "three\n" || snapshot.ReturnedLines != 1 || !snapshot.Truncated {
		t.Fatalf("snapshot = %#v", snapshot)
	}
}

func TestBufferBoundsOversizedWrite(t *testing.T) {
	buffer := New(10, 5)
	_, _ = buffer.Write([]byte("0123456789"))
	snapshot := buffer.Snapshot(10)
	if snapshot.Content != "56789" || !snapshot.Truncated {
		t.Fatalf("snapshot = %#v", snapshot)
	}
}

func TestBufferSupportsConcurrentWritersAndReaders(t *testing.T) {
	buffer := New(2000, 1<<20)
	var wait sync.WaitGroup
	for writer := 0; writer < 8; writer++ {
		wait.Add(1)
		go func(writer int) {
			defer wait.Done()
			for line := 0; line < 100; line++ {
				_, _ = fmt.Fprintf(buffer, "%d-%d\n", writer, line)
				_ = buffer.Snapshot(50)
			}
		}(writer)
	}
	wait.Wait()

	snapshot := buffer.Snapshot(2000)
	if got := strings.Count(snapshot.Content, "\n"); got != 800 {
		t.Fatalf("line count = %d, want 800", got)
	}
}
