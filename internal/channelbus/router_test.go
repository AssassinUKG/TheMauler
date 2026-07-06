package channelbus

import "testing"

func TestRouteSideQuestionIsReadOnly(t *testing.T) {
	route := RouteEnvelope(Envelope{Source: "telegram", SessionID: "telegram:direct:1", Text: "what is the current status?"})
	if route.Lane != LaneSideChat || !route.ReadOnly {
		t.Fatalf("expected read-only side chat, got %+v", route)
	}
}

func TestRouteRunQueuesWork(t *testing.T) {
	route := RouteEnvelope(Envelope{Text: "/run enumerate the target"})
	if route.Lane != LaneWork || route.Command != "run" || route.Policy != WorkQueueIfBusy {
		t.Fatalf("expected queued run route, got %+v", route)
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
	if route.Lane != LaneWork || route.Command != "run" || route.Policy != WorkQueueIfBusy {
		t.Fatalf("expected natural language work request to queue work, got %+v", route)
	}
}

func TestRouteNaturalLanguageQuestionStaysSideChat(t *testing.T) {
	route := RouteEnvelope(Envelope{Text: "Can you tell me what you can do?"})
	if route.Lane != LaneSideChat {
		t.Fatalf("expected plain question to stay side chat, got %+v", route)
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
		if route.Lane != LaneWork || route.Command != "run" || route.Policy != WorkQueueIfBusy {
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

func TestVoiceAttachmentMarksRoute(t *testing.T) {
	route := RouteEnvelope(Envelope{Text: "hello", Attachments: []Attachment{{Kind: "voice", ContentType: "audio/ogg"}}})
	if !route.FromVoice {
		t.Fatalf("expected voice route, got %+v", route)
	}
}
