package app

import (
	"testing"

	"mauler/internal/runtimeprofile"
	"mauler/internal/settings"
)

func mtpRuntimeProfile(supportsMTP bool) (runtimeprofile.RuntimeProfile, bool) {
	return runtimeprofile.RuntimeProfile{
		Name:    "qwen3.6-27b",
		Family:  "qwen3.6",
		Backend: "llama.cpp",
		Supports: runtimeprofile.Supports{
			Tools:    true,
			Thinking: true,
			MTP:      supportsMTP,
		},
	}, true
}

func TestResolveSpecPlan_UserOverrideWins(t *testing.T) {
	p := settings.Profile{ModelID: "qwen3.6-27b"} // no MTP marker, no probe
	rp, ok := mtpRuntimeProfile(false)

	on := ResolveSpecPlan(p, rp, ok, runtimeprofile.ModelMTPInfo{}, "on", "")
	if !on.Enabled || on.SpecType != specTypeMTP || !on.Locked || on.Source != SpecSourceManual {
		t.Fatalf("override on: %+v", on)
	}
	off := ResolveSpecPlan(p, rp, ok, runtimeprofile.ModelMTPInfo{HasMTPHeads: true, Confident: true}, "off", "")
	if off.Enabled || off.SpecType != "" || !off.Locked || off.Source != SpecSourceManual {
		t.Fatalf("override off must win even over a positive probe: %+v", off)
	}
}

func TestResolveSpecPlan_ConfidentProbeIsAuthoritative(t *testing.T) {
	p := settings.Profile{ModelID: "some-model"}
	rp, ok := mtpRuntimeProfile(false) // registry says family lacks MTP...

	pos := ResolveSpecPlan(p, rp, ok, runtimeprofile.ModelMTPInfo{HasMTPHeads: true, Confident: true}, "auto", "")
	if !pos.Enabled || pos.Source != SpecSourceProbe {
		t.Fatalf("confident positive probe should enable: %+v", pos)
	}

	rp2, ok2 := mtpRuntimeProfile(true) // ...registry says it does, but probe denies
	neg := ResolveSpecPlan(settings.Profile{ModelID: "qwen3.6-27b-mtp"}, rp2, ok2,
		runtimeprofile.ModelMTPInfo{HasMTPHeads: false, Confident: true}, "auto", "")
	if neg.Enabled || neg.Source != SpecSourceProbe {
		t.Fatalf("confident negative probe should disable even with mtp name: %+v", neg)
	}
}

func TestResolveSpecPlan_NameFallbackWhenNoProbe(t *testing.T) {
	rp, ok := mtpRuntimeProfile(true)

	named := ResolveSpecPlan(settings.Profile{ModelID: "Qwen3.6-27B-MTP-UD-Q4_K_XL"}, rp, ok,
		runtimeprofile.ModelMTPInfo{}, "auto", "")
	if !named.Enabled || named.Source != SpecSourceName {
		t.Fatalf("mtp name should enable via name fallback: %+v", named)
	}

	plain := ResolveSpecPlan(settings.Profile{ModelID: "Qwen3.6-27B-UD-Q4_K_XL"}, rp, ok,
		runtimeprofile.ModelMTPInfo{}, "auto", "")
	if plain.Enabled || plain.Source != SpecSourceName {
		t.Fatalf("non-mtp name with mtp-capable family should stay disabled: %+v", plain)
	}
}

func TestResolveSpecPlan_RegistryNoMTPDisablesWithoutProbe(t *testing.T) {
	rp, ok := mtpRuntimeProfile(false)
	plan := ResolveSpecPlan(settings.Profile{ModelID: "gemma4-26b-a4b-qat"}, rp, ok,
		runtimeprofile.ModelMTPInfo{}, "auto", "")
	if plan.Enabled || plan.Source != SpecSourceRegistry {
		t.Fatalf("non-MTP family without probe should disable via registry: %+v", plan)
	}
}

func TestResolveSpecPlan_GuardDisablesButUserOverrideStillWins(t *testing.T) {
	rp, ok := mtpRuntimeProfile(true)
	mtp := runtimeprofile.ModelMTPInfo{HasMTPHeads: true, Confident: true}
	p := settings.Profile{ModelID: "x-mtp"}

	guarded := ResolveSpecPlan(p, rp, ok, mtp, "auto", "tripped after truncations")
	if guarded.Enabled || guarded.Source != SpecSourceGuard {
		t.Fatalf("guard should disable even with a positive probe: %+v", guarded)
	}

	forced := ResolveSpecPlan(p, rp, ok, mtp, "on", "tripped after truncations")
	if !forced.Enabled || forced.Source != SpecSourceManual {
		t.Fatalf("user override must win over guard: %+v", forced)
	}
}

func TestResolveSpecPlan_NMaxDefaultsAndClears(t *testing.T) {
	rp, ok := mtpRuntimeProfile(true)
	enabled := ResolveSpecPlan(settings.Profile{ModelID: "x-mtp"}, rp, ok,
		runtimeprofile.ModelMTPInfo{HasMTPHeads: true, Confident: true}, "auto", "")
	if got := specNMaxFor(enabled); got != defaultSpecNMax {
		t.Fatalf("expected default n=%d, got %d", defaultSpecNMax, got)
	}

	withN := settings.Profile{ModelID: "x-mtp", SpecDraftNMax: 4}
	tuned := ResolveSpecPlan(withN, rp, ok,
		runtimeprofile.ModelMTPInfo{HasMTPHeads: true, Confident: true}, "auto", "")
	if got := specNMaxFor(tuned); got != 4 {
		t.Fatalf("expected preserved n=4, got %d", got)
	}

	disabled := ResolveSpecPlan(withN, rp, ok,
		runtimeprofile.ModelMTPInfo{HasMTPHeads: false, Confident: true}, "auto", "")
	if got := specNMaxFor(disabled); got != 0 {
		t.Fatalf("disabled plan must persist n=0, got %d", got)
	}
}
