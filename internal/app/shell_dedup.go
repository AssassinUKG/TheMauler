package app

import (
	"regexp"
	"strings"
)

var shellDisplayPagerTailRe = regexp.MustCompile(`(?i)\s*\|\s*(?:head|tail)(?:\s+-n)?(?:\s+-?\d+)?\s*$|\s*\|\s*(?:less|more|cat)\s*$`)

func normalizeShellCommandForDedup(command string) string {
	command = strings.TrimSpace(command)
	if command == "" {
		return ""
	}
	for {
		next := strings.TrimSpace(shellDisplayPagerTailRe.ReplaceAllString(command, ""))
		if next == command {
			break
		}
		command = next
	}
	command = regexp.MustCompile(`\s+(?:2>&1|1>&2)\s*$`).ReplaceAllString(command, "")
	command = regexp.MustCompile(`\s+`).ReplaceAllString(command, " ")
	return strings.TrimSpace(command)
}
