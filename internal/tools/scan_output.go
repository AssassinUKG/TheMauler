package tools

import (
	"regexp"
	"strings"
)

// Scan tools (ffuf, gobuster, feroxbuster, …) emit carriage-return progress
// bars and banners that, when captured for the model, drown the actual findings
// and blow the tool-result truncation budget on noise. A real run was observed
// looping on ffuf because every result was banner+`:: Progress:` spam with the
// one `admin [Status: 200]` line truncated away. These helpers clean that output
// (collapse \r overwrites, drop progress lines) and quiet the tools at the source.

// CollapseLineCarriageReturns applies terminal overwrite semantics to a single
// logical line: a real terminal renders only the text after the last carriage
// return, so `\rProgress 1\rProgress 2\rdone` displays as `done`. We keep the
// segment after the final interior CR (after trimming a trailing CR first).
func CollapseLineCarriageReturns(line string) string {
	line = strings.TrimRight(line, "\r")
	if i := strings.LastIndexByte(line, '\r'); i >= 0 {
		line = line[i+1:]
	}
	return line
}

var (
	// ffuf: ":: Progress: [1/4614] :: Job [1/1] :: 0 req/sec ::"
	ffufProgressRE = regexp.MustCompile(`^\s*::\s*Progress:\s*\[`)
	// gobuster / dirb: "Progress: 1234 / 4614 (26.7%)"
	genericProgressRE = regexp.MustCompile(`^\s*Progress:\s*\d+\s*/\s*\d+`)
	// feroxbuster / generic spinner+bar lines: "[####>-------] - 12s ..."
	barProgressRE = regexp.MustCompile(`^\s*[\[#█▓▒░>\-= ]{6,}\]?\s*(?:-|\d+[smh])`)
)

// IsScanProgressLine reports whether a (CR-collapsed) line is pure scan progress
// noise that the model never needs to see.
func IsScanProgressLine(line string) bool {
	t := strings.TrimSpace(line)
	if t == "" {
		return false
	}
	if strings.Contains(t, "req/sec ::") {
		return true
	}
	return ffufProgressRE.MatchString(t) || genericProgressRE.MatchString(t) || barProgressRE.MatchString(t)
}

// CleanCommandOutput collapses carriage returns, drops scan progress lines and
// low-signal banner art (walls of '?' from non-UTF-8 box-drawing), and removes
// consecutive duplicate lines. It preserves real result/finding lines.
func CleanCommandOutput(s string) string {
	if s == "" || (!strings.Contains(s, "\r") && !strings.Contains(s, "Progress:") && !strings.Contains(s, "req/sec") && !strings.Contains(s, "??")) {
		return s
	}
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	prev := ""
	for _, ln := range lines {
		ln = CollapseLineCarriageReturns(ln)
		if IsScanProgressLine(ln) || isLowSignalArtLine(ln) {
			continue
		}
		if ln != "" && ln == prev {
			continue
		}
		out = append(out, ln)
		if strings.TrimSpace(ln) != "" {
			prev = ln
		}
	}
	return strings.Join(out, "\n")
}

// isLowSignalArtLine reports whether a line is mostly replacement-char banner art
// (e.g. "?????????? ?????  ?? ???"), which carries no information for the model and
// only wastes its context. Conservative: only long lines that are overwhelmingly
// '?' among their non-space characters qualify, so real text is never dropped.
func isLowSignalArtLine(line string) bool {
	t := strings.TrimSpace(line)
	if len(t) < 16 {
		return false
	}
	nonSpace := 0
	q := 0
	for _, r := range t {
		if r == ' ' || r == '\t' {
			continue
		}
		nonSpace++
		if r == '?' {
			q++
		}
	}
	return nonSpace > 0 && q*100/nonSpace >= 80
}

var ffufInvocationRE = regexp.MustCompile(`(^|[\s;&|(])ffuf(\s+)`)

// ApplyScanToolHygiene quiets noisy scan tools at the source so their output is
// findings-only. Currently: ffuf gets `-s` (silent: no banner, no progress, just
// matched paths). Idempotent — skips a command that already passes -s. Other
// tools (gobuster, feroxbuster, nmap) are handled by CleanCommandOutput instead,
// since their quiet flags are positional/fragile to inject.
func ApplyScanToolHygiene(command string) string {
	if !ffufInvocationRE.MatchString(command) {
		return command
	}
	if hasShellFlag(command, "-s") || hasShellFlag(command, "-silent") {
		return command
	}
	return ffufInvocationRE.ReplaceAllString(command, "${1}ffuf${2}-s ")
}

var scanToolInvocationRE = regexp.MustCompile(`(^|[\s;&|(])(ffuf|gobuster|feroxbuster|wfuzz|dirb|dirbuster)(\s|$)`)

// IsScanCommand reports whether a command invokes a progress-heavy fuzzer whose
// carriage-return progress UI corrupts when captured through a PTY. Such commands
// are better routed through isolated (non-TTY) exec, where the tool auto-disables
// its live progress and prints clean, findings-only output.
func IsScanCommand(command string) bool {
	return scanToolInvocationRE.MatchString(command)
}

// hasShellFlag reports whether a flag token (e.g. "-s") appears as its own
// whitespace-delimited token in the command.
func hasShellFlag(command, flag string) bool {
	for _, tok := range strings.Fields(command) {
		if tok == flag {
			return true
		}
	}
	return false
}
