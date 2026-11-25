package app

import "strings"

// verificationCommandEvidence recognizes direct checks in a conservative shell
// subset. Every command must propagate failure through &&. Pipes, fallbacks,
// unconditional continuations, substitutions and nested shells require their
// own verifier contract; the outer shell's exit-zero cannot prove their checks.
func verificationCommandEvidence(command string) (check, compile bool) {
	commands, ok := failurePropagatingCommands(command)
	if !ok {
		return false, false
	}
	for _, words := range commands {
		if len(words) == 0 {
			continue
		}
		name := strings.ToLower(words[0])
		// -n is a Go compiler dry-run flag, but is an ordinary line-count
		// argument for content checks such as tail/head. Keep it command-specific.
		for _, word := range words[1:] {
			switch strings.ToLower(word) {
			case "--help", "--version", "-version", "--dry-run":
				return false, false
			case "-n", "-list":
				if name == "go" {
					return false, false
				}
			}
		}
		arg := func(index int) string {
			if index >= len(words) {
				return ""
			}
			return strings.ToLower(words[index])
		}
		switch name {
		case "go":
			if arg(1) == "build" || arg(1) == "test" || arg(1) == "vet" {
				check, compile = true, true
			}
		case "cargo", "dotnet":
			if arg(1) == "build" || arg(1) == "test" || arg(1) == "check" {
				check, compile = true, true
			}
		case "mvn", "gradle", "./gradlew":
			if arg(1) == "package" || arg(1) == "build" || arg(1) == "test" {
				check, compile = true, true
			}
		case "npm", "pnpm", "yarn":
			script := arg(1)
			if script == "run" {
				script = arg(2)
			}
			if script == "build" || script == "test" || script == "check-types" {
				check, compile = true, true
			} else if script == "lint" {
				check = true
			}
		case "tsc":
			check, compile = true, true
		case "pytest", "grep", "wc", "tail", "head", "diff", "findstr", "select-string", "test":
			check = true
		case "python", "python3":
			if arg(1) == "-m" && (arg(2) == "compileall" || arg(2) == "pytest") {
				check, compile = true, true
			}
		}
	}
	return check, compile
}

func failurePropagatingCommands(command string) ([][]string, bool) {
	var commands [][]string
	var words []string
	var word strings.Builder
	quote := byte(0)
	flushWord := func() {
		if word.Len() > 0 {
			words = append(words, word.String())
			word.Reset()
		}
	}
	flushCommand := func() bool {
		flushWord()
		if len(words) == 0 {
			return false
		}
		switch strings.ToLower(words[0]) {
		case "exit", "return", "exec", "eval", "source", ".", "alias", "trap", "set":
			return false
		}
		commands = append(commands, words)
		words = nil
		return true
	}
	command = strings.TrimSpace(command)
	for i := 0; i < len(command); i++ {
		c := command[i]
		if quote != 0 {
			if c == quote {
				quote = 0
			} else {
				if quote == '"' && (c == '$' || c == '`' || c == '\\') {
					return nil, false
				}
				word.WriteByte(c)
			}
			continue
		}
		switch c {
		case '\'', '"':
			quote = c
		case ' ', '\t':
			flushWord()
		case '&':
			if i > 0 && command[i-1] == '>' { // 2>&1, not a background job
				word.WriteByte(c)
				continue
			}
			if i+1 >= len(command) || command[i+1] != '&' || !flushCommand() {
				return nil, false
			}
			i++
		case ';', '|', '\r', '\n', '(', ')', '{', '}', '$', '`', '#', '\\':
			return nil, false
		default:
			word.WriteByte(c)
		}
	}
	if quote != 0 || !flushCommand() {
		return nil, false
	}
	return commands, true
}
