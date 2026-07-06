package channelbus

import (
	"strings"
)

func RouteEnvelope(env Envelope) Route {
	env = NormalizeEnvelope(env)
	text := strings.TrimSpace(env.Text)
	lower := strings.ToLower(text)
	fromVoice := hasVoiceAttachment(env)
	if text == "" && fromVoice {
		return Route{Lane: LaneSideChat, ReadOnly: true, Reason: "voice message awaiting transcription", FromVoice: true}
	}
	if strings.HasPrefix(text, "/") {
		return routeSlashCommand(text, fromVoice)
	}
	if looksQuickTerminalAction(lower) {
		return Route{Lane: LaneQuick, Command: "quick_terminal", Argument: text, Policy: WorkStartNow, Reason: "simple terminal action", FromVoice: fromVoice}
	}
	if strings.HasPrefix(lower, "run ") || strings.HasPrefix(lower, "do ") || strings.HasPrefix(lower, "ops ") {
		return Route{Lane: LaneWork, Command: "run", Argument: text, Policy: WorkQueueIfBusy, Reason: "imperative work request", FromVoice: fromVoice}
	}
	if looksLocalSystemInfoRequest(lower) {
		return Route{Lane: LaneWork, Command: "run", Argument: text, Policy: WorkQueueIfBusy, Reason: "local system information request", FromVoice: fromVoice}
	}
	if looksNaturalWorkRequest(lower) {
		return Route{Lane: LaneWork, Command: "run", Argument: text, Policy: WorkQueueIfBusy, Reason: "natural language work request", FromVoice: fromVoice}
	}
	if strings.Contains(lower, "stop the run") || strings.Contains(lower, "cancel the run") {
		return Route{Lane: LaneControl, Command: "stop", Reason: "natural language stop command", FromVoice: fromVoice}
	}
	if strings.Contains(lower, "ask the running agent") || strings.HasPrefix(lower, "interrupt ") {
		arg := strings.TrimSpace(strings.TrimPrefix(text, "interrupt "))
		return Route{Lane: LaneInterrupt, Command: "interrupt", Argument: arg, Reason: "explicit interrupt request", FromVoice: fromVoice}
	}
	return Route{Lane: LaneSideChat, Command: "chat", Argument: text, ReadOnly: true, Reason: "remote side question", FromVoice: fromVoice}
}

func looksLocalSystemInfoRequest(lower string) bool {
	if hasAny(lower, " via term", " via terminal", "use terminal", "using terminal", "run command", "run commands", "run it locally", "check locally") {
		return true
	}
	localRef := hasAny(lower, " my ", " pc", " computer", " machine", " host", " local", " system", " windows", " wsl")
	systemInfo := hasAny(lower,
		"spec", "specs", "hardware", "cpu", "gpu", "ram", "memory", "vram", "disk", "storage",
		"os version", "windows version", "driver", "nvidia", "cuda", "processor", "motherboard",
	)
	if localRef && systemInfo {
		return true
	}
	return (strings.HasPrefix(lower, "what") || strings.HasPrefix(lower, "show") || strings.HasPrefix(lower, "tell")) &&
		hasAny(lower, "pc specs", "computer specs", "machine specs", "system specs", "hardware specs")
}

func looksNaturalWorkRequest(lower string) bool {
	prefixes := []string{
		"can you ", "could you ", "please ", "pls ", "will you ", "would you ",
		"i need you to ", "i want you to ", "get it to ", "make it ",
	}
	matchedPrefix := false
	for _, prefix := range prefixes {
		if strings.HasPrefix(lower, prefix) {
			matchedPrefix = true
			break
		}
	}
	if !matchedPrefix {
		return false
	}
	workVerbs := []string{
		"add", "save", "remember", "memorise", "memorize", "open", "start", "launch", "run", "execute", "create", "write", "edit", "fix", "build",
		"install", "delete", "remove", "move", "copy", "download", "upload", "browse", "inspect",
		"enumerate", "scan", "test", "check", "continue", "hack", "connect", "restart", "kill",
	}
	for _, verb := range workVerbs {
		if strings.Contains(lower, " "+verb+" ") || strings.HasSuffix(lower, " "+verb) {
			return true
		}
	}
	return false
}

func looksQuickTerminalAction(lower string) bool {
	if !strings.Contains(lower, "tmux") {
		return false
	}
	if !(strings.Contains(lower, " terminal") || strings.Contains(lower, " session") || strings.Contains(lower, " pane")) {
		return false
	}
	return hasAny(lower, "open", "start", "launch", "create", "attach")
}

func hasAny(text string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}

func routeSlashCommand(text string, fromVoice bool) Route {
	fields := strings.Fields(text)
	cmd := strings.TrimPrefix(strings.ToLower(fields[0]), "/")
	arg := strings.TrimSpace(strings.TrimPrefix(text, fields[0]))
	switch cmd {
	case "status", "facts", "plan", "logs", "brain", "terminal", "terminal_read", "files", "file", "artifact", "projects", "help":
		return Route{Lane: LaneControl, Command: cmd, Argument: arg, ReadOnly: true, Reason: "read-only control command", FromVoice: fromVoice}
	case "stop", "pause", "resume":
		return Route{Lane: LaneControl, Command: cmd, Argument: arg, Reason: "run control command", FromVoice: fromVoice}
	case "terminal_send":
		return Route{Lane: LaneControl, Command: cmd, Argument: arg, Reason: "terminal control command", FromVoice: fromVoice}
	case "run", "ops":
		return Route{Lane: LaneWork, Command: "run", Argument: arg, Policy: WorkQueueIfBusy, Reason: "explicit work command", FromVoice: fromVoice}
	case "interrupt":
		return Route{Lane: LaneInterrupt, Command: cmd, Argument: arg, Reason: "explicit interrupt command", FromVoice: fromVoice}
	case "note":
		return Route{Lane: LaneNote, Command: cmd, Argument: arg, Reason: "note for active run", FromVoice: fromVoice}
	case "project":
		return Route{Lane: LaneControl, Command: cmd, Argument: arg, Reason: "project selection command", FromVoice: fromVoice}
	default:
		return Route{Lane: LaneUnknown, Command: cmd, Argument: arg, ReadOnly: true, Reason: "unknown slash command", FromVoice: fromVoice}
	}
}

func hasVoiceAttachment(env Envelope) bool {
	for _, att := range env.Attachments {
		kind := strings.ToLower(strings.TrimSpace(att.Kind))
		ct := strings.ToLower(strings.TrimSpace(att.ContentType))
		if kind == "voice" || kind == "audio" || strings.Contains(ct, "audio/") || strings.Contains(ct, "ogg") || strings.Contains(ct, "opus") {
			return true
		}
	}
	return false
}
