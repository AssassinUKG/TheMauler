package app

import "testing"

func TestInferenceBridgeProgressDetectsStaleLoadingReady(t *testing.T) {
	stats := map[string]any{
		"state": "Loading",
		"stage": "resolving",
		"health": map[string]any{
			"ready": true,
		},
	}
	progress := inferenceBridgeProgress{
		State: firstJSONTextKey(stats, "state", "status"),
		Stage: firstJSONTextKey(stats, "stage", "phase"),
		Ready: findBoolJSONKey(stats, "ready"),
	}
	if progress.State != "Loading" || progress.Stage != "resolving" || !progress.Ready {
		t.Fatalf("bad progress parse: %#v", progress)
	}
}
