package tools

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

func rejectProtectedMutationPath(path string) error {
	protected, matched := matchedProtectedPath(path)
	if !matched {
		return nil
	}
	return fmt.Errorf("protected path blocked: %s is protected from write/edit/delete operations", protected)
}

func rejectProtectedShellMutation(command string) error {
	if !looksDestructiveShellCommand(command) {
		return nil
	}
	if shellCopyOnlyReadsProtectedSources(command) {
		return nil
	}
	for _, protected := range protectedPathVariants() {
		if protected == "" {
			continue
		}
		if strings.Contains(strings.ToLower(command), strings.ToLower(protected)) {
			return fmt.Errorf("protected path blocked: shell command appears to modify/delete protected path %s", protected)
		}
	}
	return nil
}

func shellCopyOnlyReadsProtectedSources(command string) bool {
	fields := shellLikeFields(command)
	if len(fields) < 3 {
		return false
	}
	for i, field := range fields {
		cmd := strings.ToLower(filepath.Base(strings.TrimSpace(field)))
		if cmd != "copy-item" && cmd != "cp" && cmd != "copy" {
			continue
		}
		args := positionalCopyArgs(fields[i+1:])
		if len(args) < 2 {
			return false
		}
		dest := args[len(args)-1]
		if _, protectedDest := matchedProtectedPath(dest); protectedDest {
			return false
		}
		for _, src := range args[:len(args)-1] {
			if _, protectedSrc := matchedProtectedPath(src); !protectedSrc {
				return false
			}
		}
		return true
	}
	return false
}

func positionalCopyArgs(fields []string) []string {
	args := make([]string, 0, len(fields))
	takeNext := false
	for _, field := range fields {
		if takeNext {
			args = append(args, strings.Trim(field, `"'`))
			takeNext = false
			continue
		}
		lower := strings.ToLower(field)
		if strings.HasPrefix(lower, "-") || strings.HasPrefix(lower, "/") && len(lower) == 2 {
			switch lower {
			case "-path", "-literalpath", "-destination", "-dest":
				takeNext = true
			}
			continue
		}
		if strings.ContainsAny(field, ";&|<>") {
			break
		}
		args = append(args, strings.Trim(field, `"'`))
	}
	return args
}

func shellLikeFields(command string) []string {
	var fields []string
	var b strings.Builder
	var quote rune
	escaped := false
	for _, r := range command {
		if escaped {
			b.WriteRune(r)
			escaped = false
			continue
		}
		if r == '`' {
			escaped = true
			continue
		}
		if quote != 0 {
			if r == quote {
				quote = 0
			} else {
				b.WriteRune(r)
			}
			continue
		}
		if r == '\'' || r == '"' {
			quote = r
			continue
		}
		if r == ' ' || r == '\t' || r == '\r' || r == '\n' {
			if b.Len() > 0 {
				fields = append(fields, b.String())
				b.Reset()
			}
			continue
		}
		if strings.ContainsRune(";&|<>", r) {
			if b.Len() > 0 {
				fields = append(fields, b.String())
				b.Reset()
			}
			fields = append(fields, string(r))
			continue
		}
		b.WriteRune(r)
	}
	if b.Len() > 0 {
		fields = append(fields, b.String())
	}
	return fields
}

func matchedProtectedPath(path string) (string, bool) {
	candidate := normaliseComparePath(path)
	for _, protected := range configuredProtectedPaths() {
		protectedCmp := normaliseComparePath(protected)
		if protectedCmp == "" {
			continue
		}
		if candidate == protectedCmp || strings.HasPrefix(candidate, protectedCmp+"/") {
			return protected, true
		}
	}
	return "", false
}

func protectedPathVariants() []string {
	var out []string
	seen := map[string]bool{}
	for _, path := range configuredProtectedPaths() {
		for _, variant := range []string{
			path,
			NormalizeHostPath(path),
			filepath.ToSlash(NormalizeHostPath(path)),
			WindowsPathToWSL(NormalizeHostPath(path)),
		} {
			variant = strings.TrimSpace(variant)
			key := strings.ToLower(variant)
			if variant == "" || seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, variant)
		}
	}
	return out
}

func normaliseComparePath(path string) string {
	path = NormalizeHostPath(strings.TrimSpace(path))
	if path == "" {
		return ""
	}
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	path = filepath.Clean(path)
	path = filepath.ToSlash(path)
	return strings.TrimRight(strings.ToLower(path), "/")
}

var destructiveShellRE = regexp.MustCompile(`(?i)(^|[;&|]\s*|\s)(rm|rmdir|del|erase|move|mv|ren|rename|copy|cp|shred|truncate|tee|sed\s+-i|perl\s+-pi|chmod|chown|icacls|attrib|set-content|add-content|out-file|new-item|remove-item|move-item|copy-item)\b|>>|>\s*`)

func looksDestructiveShellCommand(command string) bool {
	return destructiveShellRE.MatchString(command)
}
