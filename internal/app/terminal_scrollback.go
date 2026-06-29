package app

import "sync"

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

// length reports how many lines are currently buffered.
func (s *terminalScrollback) length() int {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.lines)
}
