package server

// Server logs: every log line goes to Logf (stdout / journald, as before),
// to <data>/logs/server.log (kept across restarts, rotated at 5 MB) and to
// an in-memory buffer of the last lines, which admins with the
// "server.logs" permission can read in the app.

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"securespeak/internal/proto"
)

const (
	logKeep     = 5000
	logFileMax  = 5 << 20
	logPageSize = 1000
)

type logStore struct {
	mu    sync.Mutex
	lines []proto.LogLine
	seq   uint64
	file  *os.File
	path  string
	size  int64
}

func (l *logStore) open(dir string) {
	if dir == "" {
		return
	}
	os.MkdirAll(filepath.Join(dir, "logs"), 0700)
	l.path = filepath.Join(dir, "logs", "server.log")
	l.loadTail()
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return
	}
	if fi, err := f.Stat(); err == nil {
		l.size = fi.Size()
	}
	l.file = f
}

// loadTail reads the last lines of the previous runs into memory.
func (l *logStore) loadTail() {
	var all []string
	for _, p := range []string{l.path + ".1", l.path} {
		f, err := os.Open(p)
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 64*1024), 64*1024)
		for sc.Scan() {
			all = append(all, sc.Text())
			if len(all) > 2*logKeep {
				all = all[len(all)-logKeep:]
			}
		}
		f.Close()
	}
	if len(all) > logKeep {
		all = all[len(all)-logKeep:]
	}
	for _, line := range all {
		ts, text, ok := strings.Cut(line, " ")
		t, err := time.Parse(time.RFC3339, ts)
		if !ok || err != nil {
			continue
		}
		l.seq++
		l.lines = append(l.lines, proto.LogLine{Seq: l.seq, Time: t.Unix(), Text: text})
	}
}

func (l *logStore) add(text string) {
	text = strings.ReplaceAll(strings.TrimRight(text, "\n"), "\n", " ⏎ ")
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	l.seq++
	l.lines = append(l.lines, proto.LogLine{Seq: l.seq, Time: now.Unix(), Text: text})
	if len(l.lines) > logKeep+logKeep/10 {
		l.lines = append([]proto.LogLine(nil), l.lines[len(l.lines)-logKeep:]...)
	}
	if l.file != nil {
		n, _ := fmt.Fprintf(l.file, "%s %s\n", now.Format(time.RFC3339), text)
		l.size += int64(n)
		if l.size > logFileMax {
			l.file.Close()
			os.Rename(l.path, l.path+".1")
			l.file, _ = os.OpenFile(l.path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
			l.size = 0
		}
	}
}

// since returns lines newer than seq (at most one page, the newest).
func (l *logStore) since(seq uint64) []proto.LogLine {
	l.mu.Lock()
	defer l.mu.Unlock()
	i := len(l.lines)
	for i > 0 && l.lines[i-1].Seq > seq {
		i--
	}
	out := l.lines[i:]
	if len(out) > logPageSize {
		out = out[len(out)-logPageSize:]
	}
	return append([]proto.LogLine(nil), out...)
}

func (l *logStore) close() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file != nil {
		l.file.Close()
		l.file = nil
	}
}

// logf logs a line: to Logf (stdout/journald) and to the log store.
func (s *Server) logf(format string, args ...any) {
	text := fmt.Sprintf(format, args...)
	s.logs.add(text)
	if s.Logf != nil {
		s.Logf("%s", text)
	}
}

func (s *Server) sendLogs(c *Client, since uint64) {
	s.mu.RLock()
	ok := s.hasLocked(c, proto.PermServerLogs)
	s.mu.RUnlock()
	if !ok {
		c.sendErr("permission", "you are not allowed to view the server logs")
		return
	}
	c.send(proto.TLogs, proto.Logs{Lines: s.logs.since(since)})
}
