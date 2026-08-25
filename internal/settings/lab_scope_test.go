package settings

import (
	"reflect"
	"testing"
)

func TestNormaliseLabScopeMigratesCommaSeparatedLegacyTargets(t *testing.T) {
	target, hostname, entries := NormaliseLabScope("13.134.229.195, 3.9.20.132", "api.client.example", nil)
	if target != "13.134.229.195" || hostname != "api.client.example" {
		t.Fatalf("primary target/hostname = %q / %q", target, hostname)
	}
	want := []string{"13.134.229.195", "3.9.20.132", "api.client.example"}
	got := make([]string, 0, len(entries))
	for _, entry := range entries {
		got = append(got, entry.Value)
		if entry.Kind == "invalid" {
			t.Fatalf("migrated valid target became invalid: %#v", entry)
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("entries = %#v, want %#v", got, want)
	}
}

func TestNormaliseLabScopePreservesExplicitExclusionAndMetadata(t *testing.T) {
	target, hostname, entries := NormaliseLabScope("legacy.invalid", "legacy.example", []LabScopeTarget{
		{Value: "10.20.0.0/16", Environment: "internal", Label: "Office", Notes: "Authorised subnet"},
		{Value: "10.20.10.5", Excluded: true, Environment: "internal", Label: "Printer"},
	})
	if target != "10.20.0.0/16" || hostname != "" || len(entries) != 2 {
		t.Fatalf("normalised scope = target %q hostname %q entries %#v", target, hostname, entries)
	}
	if entries[0].Kind != "cidr" || entries[0].Environment != "internal" || entries[1].Kind != "ip" || !entries[1].Excluded {
		t.Fatalf("metadata/exclusion not preserved: %#v", entries)
	}
	if got := LabScopeValues(LabContext{ScopeTargets: entries}); !reflect.DeepEqual(got, []string{"10.20.0.0/16", "!10.20.10.5"}) {
		t.Fatalf("locked values = %#v", got)
	}
}

func TestDetectLabScopeKindAndValidation(t *testing.T) {
	cases := map[string]string{
		"10.20.30.40":                "ip",
		"10.20.0.0/16":               "cidr",
		"api.client.example:8443":    "hostname",
		"https://api.example/admin/": "url",
		"two.example, evil.example":  "invalid",
		"ftp://files.example":        "invalid",
	}
	for value, want := range cases {
		if got := DetectLabScopeKind(value); got != want {
			t.Errorf("DetectLabScopeKind(%q) = %q, want %q", value, got, want)
		}
	}
	if err := ValidateLabScopeTargets([]LabScopeTarget{{Value: "two.example, evil.example"}}); err == nil {
		t.Fatal("invalid combined target was accepted")
	}
}

func TestLabScopeSettingsRoundTrip(t *testing.T) {
	t.Setenv("MAULER_CONFIG_DIR", t.TempDir())
	cfg := DefaultSettings()
	cfg.Context.Lab = LabContext{ID: "client", Name: "Client", ScopeTargets: []LabScopeTarget{
		{Value: "203.0.113.10", Kind: "ip", Environment: "external", Label: "Public web"},
		{Value: "10.30.0.0/24", Kind: "cidr", Environment: "internal", Label: "Office"},
	}}
	cfg.Context.LabProfiles = []LabProfile{{
		ID: "client", Name: "Client", WorkspaceDir: "C:/client", ScopeTargets: append([]LabScopeTarget(nil), cfg.Context.Lab.ScopeTargets...),
	}}
	if err := Save(&cfg); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Context.Lab.ScopeTargets) != 2 || loaded.Context.Lab.Target != "203.0.113.10" {
		t.Fatalf("active scope round trip = %#v", loaded.Context.Lab)
	}
	if len(loaded.Context.LabProfiles) != 1 || len(loaded.Context.LabProfiles[0].ScopeTargets) != 2 {
		t.Fatalf("project scope round trip = %#v", loaded.Context.LabProfiles)
	}
}
