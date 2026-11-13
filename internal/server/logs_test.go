package server

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLogStore(t *testing.T) {
	dir := t.TempDir()
	var l logStore
	l.open(dir)
	for i := 0; i < 10; i++ {
		l.add(fmt.Sprintf("line %d\nwith newline", i))
	}
	got := l.since(0)
	if len(got) != 10 || got[0].Text != "line 0 ⏎ with newline" || got[9].Seq != 10 {
		t.Fatalf("since(0): %+v", got)
	}
	if n := len(l.since(7)); n != 3 {
		t.Fatalf("since(7) = %d lines", n)
	}
	l.close()

	// kept across restarts
	var l2 logStore
	l2.open(dir)
	defer l2.close()
	if got := l2.since(0); len(got) != 10 || got[3].Text != "line 3 ⏎ with newline" {
		t.Fatalf("after reopen: %d lines", len(got))
	}
	l2.add("after restart")
	if got := l2.since(10); len(got) != 1 || got[0].Text != "after restart" {
		t.Fatalf("new line: %+v", got)
	}

	// rotation at the size limit, memory stays bounded
	big := strings.Repeat("x", 2000)
	for i := 0; i < 3000; i++ {
		l2.add(big)
	}
	if _, err := os.Stat(filepath.Join(dir, "logs", "server.log.1")); err != nil {
		t.Fatal("log file not rotated")
	}
	if len(l2.lines) > logKeep+logKeep/10 {
		t.Fatalf("memory not bounded: %d", len(l2.lines))
	}
	if n := len(l2.since(0)); n != logPageSize {
		t.Fatalf("page size %d", n)
	}
}
