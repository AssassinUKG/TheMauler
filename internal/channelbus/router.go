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
	if looksEmergencyStopRequest(lower) {
		return Route{Lane: LaneControl, Command: "stop", Reason: "emergency stop request", FromVoice: fromVoice}
	}
	if looksQuickTerminalAction(lower) {
		return Route{Lane: LaneQuick, Command: "quick_terminal", Argument: text, Policy: WorkStartNow, Reason: "simple terminal action", FromVoice: fromVoice}
	}
	if looksCapabilityQuestion(lower) {
		return Route{Lane: LaneSideChat, Command: "chat", Argument: text, ReadOnly: true, Reason: "capability question", FromVoice: fromVoice}
	}
	if strings.HasPrefix(lower, "run ") || strings.HasPrefix(lower, "do ") || strings.HasPrefix(lower, "ops ") {
		return Route{Lane: LaneWork, Command: "cmd", Argument: text, Policy: WorkQueueIfBusy, Reason: "imperative work request", FromVoice: fromVoice}
	}
	if looksLocalSystemInfoRequest(lower) {
		return Route{Lane: LaneWork, Command: "cmd", Argument: text, Policy: WorkQueueIfBusy, Reason: "local system information request", FromVoice: fromVoice}
	}
	if looksLiveInformationRequest(lower) {
		return Route{Lane: LaneWork, Command: "cmd", Argument: text, Policy: WorkQueueIfBusy, Reason: "live information request", FromVoice: fromVoice}
	}
	if looksNaturalWorkRequest(lower) {
		return Route{Lane: LaneWork, Command: "cmd", Argument: text, Policy: WorkQueueIfBusy, Reason: "natural language work request", FromVoice: fromVoice}
	}
	if strings.Contains(lower, "ask the running agent") || strings.HasPrefix(lower, "interrupt ") {
		arg := strings.TrimSpace(strings.TrimPrefix(text, "interrupt "))
		return Route{Lane: LaneInterrupt, Command: "interrupt", Argument: arg, Reason: "explicit interrupt request", FromVoice: fromVoice}
	}
	return Route{Lane: LaneSideChat, Command: "chat", Argument: text, ReadOnly: true, Reason: "remote side question", FromVoice: fromVoice}
}

func looksEmergencyStopRequest(lower string) bool {
	lower = strings.TrimSpace(lower)
	return hasAny(lower,
		"stop the run",
		"cancel the run",
		"stop all",
		"cancel all",
		"stop everything",
		"cancel everything",
		"panic stop",
		"emergency stop",
		"kill all tasks",
		"stop all tasks",
		"cancel all tasks",
		"stop all ai",
		"stop the ai",
	)
}

func looksCapabilityQuestion(lower string) bool {
	lower = strings.TrimSpace(lower)
	if !strings.Contains(lower, "?") {
		return false
	}
	questionPrefix := strings.HasPrefix(lower, "can you ") ||
		strings.HasPrefix(lower, "could you ") ||
		strings.HasPrefix(lower, "do you ") ||
		strings.HasPrefix(lower, "are you ") ||
		strings.HasPrefix(lower, "will you ") ||
		strings.HasPrefix(lower, "what can you ") ||
		strings.HasPrefix(lower, "what are you able")
	if !questionPrefix {
		return false
	}
	if hasAny(lower,
		"what can you do",
		"what are you able",
		"are you able to",
		"can you use tools",
		"can you access tools",
		"can you run tools",
		"can you run commands",
		"can you run normal system commands",
		"can you run system commands",
		"could you run commands",
		"do you have tools",
		"do you have access",
	) {
		return true
	}
	return false
}

func looksLocalSystemInfoRequest(lower string) bool {
	if hasAny(lower,
		"what time is it",
		"what's the time",
		"whats the time",
		"current time",
		"local time",
		"tell me the time",
		"get the time",
		"check the time",
		"what date is it",
		"what's the date",
		"whats the date",
		"current date",
		"today's date",
		"todays date",
		"tell me the date",
		"get the date",
		"what day is it",
	) {
		return true
	}
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
	workVerbs := []string{
		"add", "save", "remember", "memorise", "memorize", "open", "start", "launch", "run", "execute", "create", "write", "edit", "fix", "build",
		"install", "delete", "remove", "move", "copy", "download", "upload", "browse", "inspect",
		"enumerate", "scan", "test", "check", "continue", "hack", "connect", "restart", "kill",
		"get", "find", "search", "research", "look up", "analyse", "analyze", "review", "summarise", "summarize",
	}
	for _, verb := range workVerbs {
		if matchedPrefix && (strings.Contains(lower, " "+verb+" ") || strings.HasSuffix(lower, " "+verb)) {
			return true
		}
		trimmed := strings.TrimLeft(lower, " \t\r\n")
		if trimmed == verb || strings.HasPrefix(trimmed, verb+" ") {
			return true
		}
	}
	return false
}

// looksLiveInformationRequest keeps changing external facts out of the
// no-tools side-chat lane. It is intentionally narrower than a generic factual
// question: stable explanations remain conversational, while current weather,
// news, prices, scores, and travel conditions get a real evidence-backed run.
func looksLiveInformationRequest(lower string) bool {
	lower = strings.ToLower(strings.TrimSpace(lower))
	if lower == "" {
		return false
	}
	weather := hasAny(lower, "weather", "forecast", "temperature", "humidity", "rain", "rainfall", "wind speed")
	if weather && hasAny(lower,
		"current", "currently", "today", "today's", "todays", "tomorrow", "tonight", "this week",
		"next week", "next few days", "next seven days", "next 7 days", "coming days", "coming week",
		"over the next", "day forecast", "-day forecast", "what's the", "whats the", "what is the", "get the", "show me the",
	) {
		return true
	}
	liveTopic := hasAny(lower,
		"news", "headlines", "stock price", "share price", "exchange rate", "crypto price",
		"score", "scores", "fixture", "fixtures", "traffic", "train time", "flight status",
	)
	return liveTopic && hasAny(lower,
		"latest", "live", "current", "currently", "today", "today's", "todays", "now", "this week", "get", "find", "show",
	)
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
	case "stop", "pause", "resume", "stopall", "panic", "abort", "cancel":
		if cmd == "stopall" || cmd == "panic" || cmd == "abort" || cmd == "cancel" {
			cmd = "stop"
		}
		return Route{Lane: LaneControl, Command: cmd, Argument: arg, Reason: "run control command", FromVoice: fromVoice}
	case "terminal_send":
		return Route{Lane: LaneControl, Command: cmd, Argument: arg, Reason: "terminal control command", FromVoice: fromVoice}
	case "cmd", "run", "ops":
		return Route{Lane: LaneWork, Command: "cmd", Argument: arg, Policy: WorkQueueIfBusy, Reason: "explicit work command", FromVoice: fromVoice}
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
