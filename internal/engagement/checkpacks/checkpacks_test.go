package checkpacks

import (
	"strings"
	"testing"

	"mauler/internal/engagement"
)

func TestDefaultRegistryPinsApprovedSourcesAndPaths(t *testing.T) {
	registry, err := DefaultRegistry()
	if err != nil {
		t.Fatal(err)
	}
	if len(registry.List()) < 5 {
		t.Fatalf("registry sources = %d", len(registry.List()))
	}
	err = registry.ValidateProvenance(engagement.DefinitionSource{
		ID: "owasp-wstg", Repository: "https://github.com/OWASP/wstg", Path: "document/4-Web_Application_Security_Testing/README.md",
		Ref: "v4.2", License: "CC-BY-SA-4.0", Trust: engagement.PackTrustOfficial,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.ValidateProvenance(engagement.DefinitionSource{
		ID: "shiftgrid", Repository: "https://github.com/BuFuuu/shiftgrid", Path: "Workflows/simple.json",
		Ref: "953e23e9212fe86532f9c2a8dbffa46546fb9836", License: "MIT", Trust: engagement.PackTrustCurated,
	}); err != nil {
		t.Fatalf("embedded Shiftgrid provenance was not approved: %v", err)
	}
	bad := engagement.DefinitionSource{ID: "owasp-wstg", Repository: "https://github.com/OWASP/wstg", Path: ".github/workflows/release.yml", Ref: "master", License: "CC-BY-SA-4.0", Trust: engagement.PackTrustOfficial}
	if err := registry.ValidateProvenance(bad); err == nil || !strings.Contains(err.Error(), "outside the approved paths") {
		t.Fatalf("unsafe path validation = %v", err)
	}
}

func TestSelectUsesApplicabilityAndExcludesExperimentalByDefault(t *testing.T) {
	checklist := engagement.ChecklistDefinition{Items: []engagement.CheckDefinition{
		{ID: "headers", Title: "Headers", Scope: "global", AppliesWhen: engagement.CheckApplicability{TargetKinds: []string{"web"}, Protocols: []string{"https"}}},
		{ID: "graphql", Title: "GraphQL", Scope: "global", Maturity: "experimental", AppliesWhen: engagement.CheckApplicability{AnyFeatures: []string{"graphql"}}},
		{ID: "api-auth", Title: "API auth", Scope: "global", AppliesWhen: engagement.CheckApplicability{TargetKinds: []string{"api"}, Authentication: "authenticated"}},
	}}
	selection := Select(checklist, TargetContext{TargetKind: "web", Protocols: []string{"HTTPS"}, Features: []string{"graphql"}, Authenticated: true}, SelectionOptions{})
	if len(selection.Applicable) != 1 || selection.Applicable[0].ID != "headers" {
		t.Fatalf("applicable = %#v", selection.Applicable)
	}
	if len(selection.Excluded) != 2 {
		t.Fatalf("excluded = %#v", selection.Excluded)
	}
}

func TestReviewChecklistRequiresEvidenceMappingsAndReproductionSignals(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Add(SourceSpec{ID: "source", Repository: "https://github.com/example/source", Kind: SourceKindStandard, Trust: engagement.PackTrustCurated, License: "MIT"}); err != nil {
		t.Fatal(err)
	}
	checklist := engagement.ChecklistDefinition{
		SchemaVersion: 2, ID: "web-core", Version: "1.0.0", Name: "Web core",
		Source: engagement.DefinitionSource{ID: "source", Repository: "https://github.com/example/source", Ref: "v1.0.0", License: "MIT", Trust: engagement.PackTrustCurated},
		Items: []engagement.CheckDefinition{{
			ID: "headers", Title: "Headers", Scope: "global", Safety: engagement.SafetyPassive, Maturity: "stable",
			Mappings:  []engagement.CheckMapping{{Framework: "WSTG", ID: "WSTG-CONF-14"}},
			Procedure: []string{"Request the representative pages."}, ExpectedSignals: []string{"Required response headers are absent."},
			NegativeSignals: []string{"Headers are present with valid values."}, FalsePositiveNotes: []string{"Check redirects and the final response."},
			Evidence: engagement.CheckEvidencePolicy{RequiredKinds: []string{"http_capture"}, Minimum: 1, Reproduce: true},
		}},
	}
	report := ReviewChecklist(checklist, registry)
	if !report.Ready || report.Score != 100 || report.Error() != nil {
		t.Fatalf("quality report = %#v err=%v", report, report.Error())
	}

	checklist.Items[0].Mappings = nil
	report = ReviewChecklist(checklist, registry)
	if report.Ready || report.Score >= 100 || report.Error() == nil {
		t.Fatalf("weak quality report = %#v", report)
	}
}

func TestDiffReportsCheckLevelChangesWithoutMutatingLivePack(t *testing.T) {
	from := engagement.ChecklistDefinition{Version: "1.0.0", Items: []engagement.CheckDefinition{
		{ID: "a", Title: "A", Scope: "global", Procedure: []string{"one"}},
		{ID: "removed", Title: "Removed", Scope: "global"},
	}}
	to := engagement.ChecklistDefinition{Version: "1.1.0", Items: []engagement.CheckDefinition{
		{ID: "a", Title: "A", Scope: "global", Procedure: []string{"one", "two"}, Verified: true},
		{ID: "added", Title: "Added", Scope: "global"},
	}}
	diff := Diff(from, to)
	if diff.Empty() || len(diff.Added) != 1 || diff.Added[0] != "added" || len(diff.Removed) != 1 || diff.Removed[0] != "removed" {
		t.Fatalf("diff = %#v", diff)
	}
	if len(diff.Changed) != 1 || diff.Changed[0].ID != "a" || !contains(diff.Changed[0].Fields, "procedure") || !contains(diff.Changed[0].Fields, "lifecycle") {
		t.Fatalf("changed = %#v", diff.Changed)
	}
}

func TestEmbeddedHTTPFixturesVerifyOnlyCuratedChecksWithPositiveAndNegativeControls(t *testing.T) {
	catalog, err := engagement.LoadEmbeddedCatalog()
	if err != nil {
		t.Fatal(err)
	}
	_, checklist, err := catalog.Bundle("webapp-simple")
	if err != nil {
		t.Fatal(err)
	}
	suite, err := LoadEmbeddedFixtureSuite()
	if err != nil {
		t.Fatal(err)
	}
	report := RunFixtureSuite(checklist, suite, DefaultFixtureAdapters())
	if !report.Ready || len(report.Cases) != 4 || len(report.VerifiedChecks) != 2 {
		t.Fatalf("fixture report = %#v", report)
	}
	for _, result := range report.Cases {
		if !result.Passed || result.Actual != result.Expected || len(result.Signals) == 0 {
			t.Fatalf("fixture result = %#v", result)
		}
	}
	for _, check := range checklist.Items[:2] {
		if !check.Verified {
			t.Fatalf("curated check %q is not labelled fixture-verified", check.ID)
		}
		score, issues := reviewCheck(check)
		if score != 100 || len(issues) != 0 {
			t.Fatalf("curated check %q score=%d issues=%#v", check.ID, score, issues)
		}
	}

	missingNegative := suite
	missingNegative.Fixtures = append([]CheckFixture(nil), suite.Fixtures[:len(suite.Fixtures)-1]...)
	failed := RunFixtureSuite(checklist, missingNegative, DefaultFixtureAdapters())
	if failed.Ready || !fixtureIssueContains(failed.Issues, "positive and negative") {
		t.Fatalf("missing negative fixture report = %#v", failed)
	}
}

func TestFixtureHarnessRejectsUnpinnedOrMissingAdapters(t *testing.T) {
	checklist := engagement.ChecklistDefinition{Items: []engagement.CheckDefinition{{
		ID: "headers", Title: "Headers", Scope: "global", Maturity: "stable", Verified: true,
		Automation: []engagement.CheckAutomationReference{{Adapter: AdapterSecurityHeadersV1, ID: "native"}},
	}}}
	fixture := CheckFixture{
		ID: "headers-positive", CheckID: "headers", Case: FixturePositive, Adapter: AdapterSecurityHeadersV1,
		ExpectedStatus: engagement.StatusVulnerable, RequestURL: "https://fixture.invalid/",
		Response: FixtureHTTPResponse{StatusCode: 200, Headers: map[string][]string{}},
	}
	suite := FixtureSuite{SchemaVersion: 1, ID: "test", Version: "1", Fixtures: []CheckFixture{fixture}}
	missing := RunFixtureSuite(checklist, suite, map[string]FixtureAdapter{})
	if missing.Ready || !fixtureIssueContains(missing.Issues, "not registered") {
		t.Fatalf("missing adapter report = %#v", missing)
	}

	checklist.Items[0].Automation = []engagement.CheckAutomationReference{{Adapter: AdapterCookieFlagsV1, ID: "wrong"}}
	unpinned := RunFixtureSuite(checklist, suite, DefaultFixtureAdapters())
	if unpinned.Ready || !fixtureIssueContains(unpinned.Issues, "does not pin") {
		t.Fatalf("unpinned adapter report = %#v", unpinned)
	}
}

func TestReviewChecklistAcceptsLocalProvenanceWithoutUpstreamAllowList(t *testing.T) {
	checklist := engagement.ChecklistDefinition{
		SchemaVersion: engagement.CurrentDefinitionSchemaVersion,
		ID:            "local-checks", Version: "0.1.0", Name: "Local checks",
		Source: engagement.DefinitionSource{ID: "local-pack", Repository: "mauler://pack-library", Ref: "0.1.0", License: "Local", Trust: engagement.PackTrustLocal},
		Items: []engagement.CheckDefinition{{
			ID: "local-check", Title: "Local check", Scope: "global",
			Mappings:  []engagement.CheckMapping{{Framework: "OWASP-WSTG", ID: "WSTG-TEST-01"}},
			Procedure: []string{"Perform the test."}, ExpectedSignals: []string{"Unsafe behavior is reproduced."},
			NegativeSignals: []string{"The fixed control blocks the behavior."}, FalsePositiveNotes: []string{"Repeat the test."},
			Safety: engagement.SafetyPassive, Evidence: engagement.CheckEvidencePolicy{RequiredKinds: []string{"http_capture"}, Minimum: 1},
			Maturity: "experimental",
		}},
	}
	report := ReviewChecklist(checklist, nil)
	if !report.Ready || report.Score < MinimumPublishScore {
		t.Fatalf("local checklist should pass without an upstream registry: %+v", report)
	}
}

func fixtureIssueContains(issues []FixtureIssue, want string) bool {
	for _, issue := range issues {
		if strings.Contains(issue.Message, want) {
			return true
		}
	}
	return false
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
