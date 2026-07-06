package app

import (
	"strings"
	"sync"
	"unicode/utf8"
)

// terminalScreen is a small headless terminal model for the agent-side "screen"
// view. It is intentionally conservative: xterm.js remains the human renderer,
// while this applies the common controls that make prompts, progress bars, REPLs,
// and simple TUIs readable to tools.
type terminalScreen struct {
	mu          sync.Mutex
	rows        int
	cols        int
	cursorRow   int
	cursorCol   int
	cells       [][]rune
	generation  uint64
	escapeBytes []byte
}

func newTerminalScreen(cols, rows int) *terminalScreen {
	if cols <= 0 {
		cols = 200
	}
	if rows <= 0 {
		rows = 50
	}
	s := &terminalScreen{}
	s.resize(cols, rows)
	return s
}

func (s *terminalScreen) resize(cols, rows int) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resizeLocked(cols, rows)
}

func (s *terminalScreen) resizeLocked(cols, rows int) {
	if cols <= 0 {
		cols = 200
	}
	if rows <= 0 {
		rows = 50
	}
	old := s.cells
	s.cols = cols
	s.rows = rows
	s.cells = make([][]rune, rows)
	for r := 0; r < rows; r++ {
		s.cells[r] = make([]rune, cols)
		for c := range s.cells[r] {
			s.cells[r][c] = ' '
		}
	}
	copyRows := min(len(old), rows)
	for r := 0; r < copyRows; r++ {
		copy(s.cells[r], old[r][:min(len(old[r]), cols)])
	}
	if s.cursorRow >= rows {
		s.cursorRow = rows - 1
	}
	if s.cursorCol >= cols {
		s.cursorCol = cols - 1
	}
	s.generation++
}

func (s *terminalScreen) write(b []byte) {
	if s == nil || len(b) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	changed := false
	for len(b) > 0 {
		if len(s.escapeBytes) > 0 {
			next := append(s.escapeBytes, b[0])
			b = b[1:]
			if terminalEscapeComplete(next) {
				s.applyEscapeLocked(next)
				s.escapeBytes = nil
				changed = true
			} else if len(next) > 128 {
				s.escapeBytes = nil
			} else {
				s.escapeBytes = next
			}
			continue
		}

		if b[0] == 0x1b {
			s.escapeBytes = []byte{0x1b}
			b = b[1:]
			continue
		}
		r, size := utf8.DecodeRune(b)
		if r == utf8.RuneError && size == 1 {
			b = b[1:]
			continue
		}
		b = b[size:]
		if s.applyRuneLocked(r) {
			changed = true
		}
	}
	if changed {
		s.generation++
	}
}

func terminalEscapeComplete(b []byte) bool {
	if len(b) < 2 || b[0] != 0x1b {
		return false
	}
	if b[1] == ']' {
		return b[len(b)-1] == 0x07 || (len(b) >= 2 && b[len(b)-2] == 0x1b && b[len(b)-1] == '\\')
	}
	if b[1] == '[' {
		if len(b) < 3 {
			return false
		}
		last := b[len(b)-1]
		return last >= 0x40 && last <= 0x7e
	}
	return true
}

func (s *terminalScreen) applyRuneLocked(r rune) bool {
	switch r {
	case '\r':
		s.cursorCol = 0
		return true
	case '\n':
		s.newlineLocked()
		return true
	case '\b':
		if s.cursorCol > 0 {
			s.cursorCol--
		}
		return true
	case '\t':
		next := min(s.cols-1, ((s.cursorCol/8)+1)*8)
		for s.cursorCol < next {
			s.putRuneLocked(' ')
		}
		return true
	default:
		if r < 32 || r == 127 {
			return false
		}
		s.putRuneLocked(r)
		return true
	}
}

func (s *terminalScreen) putRuneLocked(r rune) {
	if s.rows <= 0 || s.cols <= 0 {
		return
	}
	if s.cursorRow < 0 {
		s.cursorRow = 0
	}
	if s.cursorRow >= s.rows {
		s.cursorRow = s.rows - 1
	}
	if s.cursorCol < 0 {
		s.cursorCol = 0
	}
	if s.cursorCol >= s.cols {
		s.newlineLocked()
	}
	s.cells[s.cursorRow][s.cursorCol] = r
	s.cursorCol++
	if s.cursorCol >= s.cols {
		s.newlineLocked()
	}
}

func (s *terminalScreen) newlineLocked() {
	s.cursorCol = 0
	s.cursorRow++
	if s.cursorRow < s.rows {
		return
	}
	copy(s.cells[0:], s.cells[1:])
	last := make([]rune, s.cols)
	for i := range last {
		last[i] = ' '
	}
	s.cells[s.rows-1] = last
	s.cursorRow = s.rows - 1
}

func (s *terminalScreen) applyEscapeLocked(seq []byte) {
	if len(seq) < 2 {
		return
	}
	if seq[1] == ']' {
		return
	}
	if seq[1] != '[' {
		return
	}
	body := string(seq[2 : len(seq)-1])
	final := seq[len(seq)-1]
	nums := parseCSIParams(body)
	arg := func(idx int, fallback int) int {
		if idx < len(nums) && nums[idx] > 0 {
			return nums[idx]
		}
		return fallback
	}
	switch final {
	case 'A':
		s.cursorRow = max(0, s.cursorRow-arg(0, 1))
	case 'B':
		s.cursorRow = min(s.rows-1, s.cursorRow+arg(0, 1))
	case 'C':
		s.cursorCol = min(s.cols-1, s.cursorCol+arg(0, 1))
	case 'D':
		s.cursorCol = max(0, s.cursorCol-arg(0, 1))
	case 'G':
		s.cursorCol = min(s.cols-1, max(0, arg(0, 1)-1))
	case 'H', 'f':
		s.cursorRow = min(s.rows-1, max(0, arg(0, 1)-1))
		s.cursorCol = min(s.cols-1, max(0, arg(1, 1)-1))
	case 'K':
		mode := arg(0, 0)
		switch mode {
		case 1:
			for c := 0; c <= s.cursorCol && c < s.cols; c++ {
				s.cells[s.cursorRow][c] = ' '
			}
		case 2:
			for c := range s.cells[s.cursorRow] {
				s.cells[s.cursorRow][c] = ' '
			}
		default:
			for c := s.cursorCol; c < s.cols; c++ {
				s.cells[s.cursorRow][c] = ' '
			}
		}
	case 'J':
		mode := arg(0, 0)
		if mode == 2 || mode == 3 {
			for r := range s.cells {
				for c := range s.cells[r] {
					s.cells[r][c] = ' '
				}
			}
			s.cursorRow, s.cursorCol = 0, 0
		}
	case 'm', 'h', 'l', '?':
		// Styling/mode changes do not affect the plain-text screen capture.
	}
}

func parseCSIParams(body string) []int {
	body = strings.TrimPrefix(body, "?")
	if body == "" {
		return nil
	}
	parts := strings.Split(body, ";")
	nums := make([]int, 0, len(parts))
	for _, part := range parts {
		n := 0
		for _, r := range part {
			if r < '0' || r > '9' {
				break
			}
			n = n*10 + int(r-'0')
		}
		nums = append(nums, n)
	}
	return nums
}

func (s *terminalScreen) render(lines int) []string {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	start := 0
	if lines > 0 && lines < s.rows {
		start = s.rows - lines
	}
	out := make([]string, 0, s.rows-start)
	for r := start; r < s.rows; r++ {
		line := strings.TrimRight(string(s.cells[r]), " ")
		out = append(out, line)
	}
	for len(out) > 0 && strings.TrimSpace(out[0]) == "" {
		out = out[1:]
	}
	for len(out) > 0 && strings.TrimSpace(out[len(out)-1]) == "" {
		out = out[:len(out)-1]
	}
	return out
}

func (s *terminalScreen) generationValue() uint64 {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.generation
}
