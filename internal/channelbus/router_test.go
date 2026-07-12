package channelbus

import "testing"

func TestRouteSideQuestionIsReadOnly(t *testing.T) {
	route := RouteEnvelope(Envelope{Source: "telegram", SessionID: "telegram:direct:1", Text: "what is the current status?"})
	if route.Lane != LaneSideChat || !route.ReadOnly {
		t.Fatalf("expected read-only side chat, got %+v", route)
	}
}

func TestRouteCmdQueuesWork(t *testing.T) {
	route := RouteEnvelope(Envelope{Text: "/cmd enumerate the target"})
	if route.Lane != LaneWork || route.Command != "cmd" || route.Policy != WorkQueueIfBusy {
		t.Fatalf("expected queued cmd route, got %+v", route)
	}
}

func TestRouteRunAndOpsAliasCmd(t *testing.T) {
	for _, text := range []string{"/run enumerate the target", "/ops enumerate the target"} {
		route := RouteEnvelope(Envelope{Text: text})
		if route.Lane != LaneWork || route.Command != "cmd" || route.Policy != WorkQueueIfBusy {
			t.Fatalf("expected %q to alias queued cmd route, got %+v", text, route)
		}
	}
}

func TestRouteNaturalLanguageQuickTerminalAction(t *testing.T) {
	route := RouteEnvelope(Envelope{Text: "Can you open a tmux terminal session in WSL Kali for me and open on my desktop?"})
	if route.Lane != LaneQuick || route.Command != "quick_terminal" || route.Policy != WorkStartNow {
		t.Fatalf("expected quick terminal action route, got %+v", route)
	}
}

func TestRouteNaturalLanguageWorkRequestQueuesWork(t *testing.T) {
	route := RouteEnvelope(Envelope{Text: "Can you inspect the current project and update the docs?"})
	if route.Lane != LaneWork || route.Command != "cmd" || route.Policy != WorkQueueIfBusy {
		t.Fatalf("expected natural language work request to queue work, got %+v", route)
	}
}

func TestRouteNaturalLanguageQuestionStaysSideChat(t *testing.T) {
	route := RouteEnvelope(Envelope{Text: "Can you tell me what you can do?"})
	if route.Lane != LaneSideChat {
		t.Fatalf("expected plain question to stay side chat, got %+v", route)
	}
}

func TestRouteCapabilityQuestionStaysSideChat(t *testing.T) {
	cases := []string{
		"Can you run normal system commands?",
		"Can you run commands?",
		"Do you have access to tools?",
		"What can you do with tools?",
	}
	for _, text := range cases {
		route := RouteEnvelope(Envelope{Source: "telegram", SessionID: "telegram:direct:1", Text: text})
		if route.Lane != LaneSideChat || !route.ReadOnly {
			t.Fatalf("expected capability question %q to stay side chat, got %+v", text, route)
		}
	}
}

func TestRouteLocalSystemInfoUsesWorkLane(t *testing.T) {
	cases := []string{
		"What's my PC specs?",
		"You do via term and run commands",
		"Can you check my GPU and RAM?",
		"show local system specs",
	}
	for _, text := range cases {
		route := RouteEnvelope(Envelope{Source: "telegram", SessionID: "telegram:direct:1", Text: text})
		if route.Lane != LaneWork || route.Command != "cmd" || route.Policy != WorkQueueIfBusy {
			t.Fatalf("expected %q to route to work, got %+v", text, route)
		}
	}
}

func TestRouteControlStop(t *testing.T) {
	route := RouteEnvelope(Envelope{Text: "/stop"})
	if route.Lane != LaneControl || route.Command != "stop" || route.ReadOnly {
		t.Fatalf("expected stop control route, got %+v", route)
	}
}

func TestRouteEmergencyStopAliases(t *testing.T) {
	for _, text := range []string{"/stopall", "/panic", "/abort", "/cancel", "cancel all tasks", "stop everything"} {
		route := RouteEnvelope(Envelope{Text: text})
		if route.Lane != LaneControl || route.Command != "stop" || route.ReadOnly {
			t.Fatalf("expected emergency stop route for %q, got %+v", text, route)
		}
	}
}

func TestVoiceAttachmentMarksRoute(t *testing.T) {
	route := RouteEnvelope(Envelope{Text: "hello", Attachments: []Attachment{{Kind: "voice", ContentType: "audio/ogg"}}})
	if !route.FromVoice {
		t.Fatalf("expected voice route, got %+v", route)
	}
}
