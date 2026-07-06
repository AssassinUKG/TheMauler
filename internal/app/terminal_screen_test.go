package app

import (
	"strings"
	"testing"
)

func TestTerminalScreenCarriageReturnRepaint(t *testing.T) {
	s := newTerminalScreen(40, 5)
	s.write([]byte("progress 10\rprogress 90\x1b[K"))
	lines := strings.Join(s.render(5), "\n")
	if !strings.Contains(lines, "progress 90") {
		t.Fatalf("missing latest repaint: %q", lines)
	}
	if strings.Contains(lines, "progress 10") {
		t.Fatalf("old repaint survived: %q", lines)
	}
}

func TestTerminalScreenCursorUpEraseLine(t *testing.T) {
	s := newTerminalScreen(40, 5)
	s.write([]byte("one\ntwo\nthree\x1b[A\x1b[2Kreplaced"))
	lines := strings.Join(s.render(5), "\n")
	if !strings.Contains(lines, "one") || !strings.Contains(lines, "replaced") {
		t.Fatalf("expected retained and replaced lines, got %q", lines)
	}
	if strings.Contains(lines, "two") {
		t.Fatalf("erased line survived: %q", lines)
	}
}

func TestTerminalScreenClearScreen(t *testing.T) {
	s := newTerminalScreen(40, 5)
	s.write([]byte("old\x1b[2Jnew"))
	lines := strings.Join(s.render(5), "\n")
	if lines != "new" {
		t.Fatalf("clear screen render = %q, want new", lines)
	}
}
