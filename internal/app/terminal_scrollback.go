package app

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
)

// terminalScrollback is a fixed-capacity rolling buffer of terminal lines. It is
// populated by pipeShellOutput for every live shell session and snapshotted by
// the interactive terminal_send / terminal_read tools, giving the agent a
// non-blocking, "look at the screen right now" read instead of the marker-framed
// blocking shell call. It is independent of the shellSession.output channel: the
// channel is drained by an active blocking command, whereas this buffer is always
// current even when nothing is reading.
type terminalScrollback struct {
	mu    sync.Mutex
	lines []string
	max   int
}

const defaultScrollbackLines = 2000

func newTerminalScrollback(max int) *terminalScrollback {
	if max <= 0 {
		max = defaultScrollbackLines
	}
	return &terminalScrollback{max: max}
}

// append records one line, trimming the oldest lines past capacity.
func (s *terminalScrollback) append(line string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lines = append(s.lines, line)
	if len(s.lines) > s.max {
		s.lines = append(s.lines[:0:0], s.lines[len(s.lines)-s.max:]...)
	}
}

// tail returns the last n lines (all of them when n<=0 or n exceeds the buffer).
func (s *terminalScrollback) tail(n int) []string {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if n <= 0 || n > len(s.lines) {
		n = len(s.lines)
	}
	out := make([]string, n)
	copy(out, s.lines[len(s.lines)-n:])
	return out
}

// since returns lines appended after a previously captured length. If the buffer
// rolled over, it returns the surviving tail rather than old unrelated lines.
func (s *terminalScrollback) since(before int, limit int) []string {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if before < 0 {
		before = 0
	}
	if before > len(s.lines) {
		before = len(s.lines)
	}
	out := append([]string{}, s.lines[before:]...)
	if limit > 0 && len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out
}

// length reports how many lines are currently buffered.
func (s *terminalScrollback) length() int {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.lines)
}

// search returns matching scrollback lines with stable line numbers from the
// current buffer. pattern is treated as a regexp when valid, otherwise as a
// case-insensitive literal substring.
func (s *terminalScrollback) search(pattern string, limit int) []string {
	if s == nil {
		return nil
	}
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return nil
	}
	if limit <= 0 || limit > maxTerminalReadLines {
		limit = maxTerminalReadLines
	}
	var re *regexp.Regexp
	if compiled, err := regexp.Compile(pattern); err == nil {
		re = compiled
	}
	needle := strings.ToLower(pattern)
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, limit)
	for i, line := range s.lines {
		matched := false
		if re != nil {
			matched = re.MatchString(line)
		} else {
			matched = strings.Contains(strings.ToLower(line), needle)
		}
		if !matched {
			continue
		}
		out = append(out, fmt.Sprintf("%d: %s", i+1, line))
		if len(out) >= limit {
			break
		}
	}
	return out
}
