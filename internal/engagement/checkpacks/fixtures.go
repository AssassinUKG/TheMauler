package checkpacks

import (
	"embed"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"mauler/internal/engagement"
)

const (
	FixtureSchemaVersion = 1
	FixturePositive      = "positive"
	FixtureNegative      = "negative"

	AdapterSecurityHeadersV1 = "http_security_headers_v1"
	AdapterCookieFlagsV1     = "http_cookie_flags_v1"
)

//go:embed fixtures/*.json
var embeddedFixtures embed.FS

type FixtureSuite struct {
	SchemaVersion int            `json:"schema_version"`
	ID            string         `json:"id"`
	Version       string         `json:"version"`
	Fixtures      []CheckFixture `json:"fixtures"`
}

type CheckFixture struct {
	ID              string              `json:"id"`
	CheckID         string              `json:"check_id"`
	Case            string              `json:"case"`
	Adapter         string              `json:"adapter"`
	ExpectedStatus  string              `json:"expected_status"`
	ExpectedSignals []string            `json:"expected_signals,omitempty"`
	RequestURL      string              `json:"request_url"`
	Response        FixtureHTTPResponse `json:"response"`
}

type FixtureHTTPResponse struct {
	StatusCode int                 `json:"status_code"`
	Headers    map[string][]string `json:"headers"`
	Body       string              `json:"body,omitempty"`
}

type AdapterResult struct {
	Status  string   `json:"status"`
	Signals []string `json:"signals"`
}

type FixtureAdapter interface {
	ID() string
	Evaluate(CheckFixture) (AdapterResult, error)
}

type FixtureCaseResult struct {
	FixtureID     string   `json:"fixture_id"`
	CheckID       string   `json:"check_id"`
	Case          string   `json:"case"`
	Expected      string   `json:"expected"`
	Actual        string   `json:"actual,omitempty"`
	Signals       []string `json:"signals,omitempty"`
	Passed        bool     `json:"passed"`
	FailureReason string   `json:"failure_reason,omitempty"`
}

type FixtureIssue struct {
	FixtureID string `json:"fixture_id,omitempty"`
	CheckID   string `json:"check_id,omitempty"`
	Message   string `json:"message"`
}

type FixtureReport struct {
	SuiteID        string              `json:"suite_id"`
	Ready          bool                `json:"ready"`
	VerifiedChecks []string            `json:"verified_checks,omitempty"`
	Cases          []FixtureCaseResult `json:"cases"`
	Issues         []FixtureIssue      `json:"issues,omitempty"`
}

type PublicationReport struct {
	Ready    bool          `json:"ready"`
	Quality  QualityReport `json:"quality"`
	Fixtures FixtureReport `json:"fixtures"`
}

func LoadEmbeddedFixtureSuite() (FixtureSuite, error) {
	data, err := embeddedFixtures.ReadFile("fixtures/http-core-v1.json")
	if err != nil {
		return FixtureSuite{}, err
	}
	var suite FixtureSuite
	if err := json.Unmarshal(data, &suite); err != nil {
		return FixtureSuite{}, fmt.Errorf("parse fixture suite: %w", err)
	}
	if suite.SchemaVersion != FixtureSchemaVersion {
		return FixtureSuite{}, fmt.Errorf("fixture suite schema %d is unsupported", suite.SchemaVersion)
	}
	if strings.TrimSpace(suite.ID) == "" || strings.TrimSpace(suite.Version) == "" || len(suite.Fixtures) == 0 {
		return FixtureSuite{}, fmt.Errorf("fixture suite id, version, and cases are required")
	}
	return suite, nil
}

func DefaultFixtureAdapters() map[string]FixtureAdapter {
	adapters := []FixtureAdapter{securityHeadersAdapter{}, cookieFlagsAdapter{}}
	registry := make(map[string]FixtureAdapter, len(adapters))
	for _, adapter := range adapters {
		registry[adapter.ID()] = adapter
	}
	return registry
}

// RunFixtureSuite executes inert, structured response fixtures only. It does
// not make network requests and detector output never confirms a live finding.
// A check labelled fixture-verified must have passing positive and negative cases.
func RunFixtureSuite(checklist engagement.ChecklistDefinition, suite FixtureSuite, adapters map[string]FixtureAdapter) FixtureReport {
	report := FixtureReport{SuiteID: suite.ID, Cases: []FixtureCaseResult{}}
	checks := make(map[string]engagement.CheckDefinition, len(checklist.Items))
	for _, check := range checklist.Items {
		checks[check.ID] = check
	}
	coverage := map[string]map[string]bool{}
	fixtureIDs := map[string]bool{}
	for _, fixture := range suite.Fixtures {
		result := FixtureCaseResult{FixtureID: fixture.ID, CheckID: fixture.CheckID, Case: fixture.Case, Expected: fixture.ExpectedStatus}
		fail := func(message string) {
			if result.FailureReason == "" {
				result.FailureReason = message
			}
			report.Issues = append(report.Issues, FixtureIssue{FixtureID: fixture.ID, CheckID: fixture.CheckID, Message: message})
		}
		if strings.TrimSpace(fixture.ID) == "" || fixtureIDs[fixture.ID] {
			fail("fixture id is missing or duplicated")
		} else {
			fixtureIDs[fixture.ID] = true
		}
		check, ok := checks[fixture.CheckID]
		if !ok {
			fail("fixture references an unknown check")
		}
		if fixture.Case != FixturePositive && fixture.Case != FixtureNegative {
			fail("fixture case must be positive or negative")
		}
		adapter := adapters[fixture.Adapter]
		if adapter == nil {
			fail("fixture adapter is not registered")
		}
		if ok && !checkReferencesAdapter(check, fixture.Adapter) {
			fail("check does not pin this fixture adapter")
		}
		if result.FailureReason == "" {
			actual, err := adapter.Evaluate(fixture)
			if err != nil {
				fail(err.Error())
			} else {
				result.Actual = actual.Status
				result.Signals = append([]string(nil), actual.Signals...)
				if actual.Status != fixture.ExpectedStatus {
					fail(fmt.Sprintf("expected status %s, got %s", fixture.ExpectedStatus, actual.Status))
				}
				for _, signal := range fixture.ExpectedSignals {
					if !containsFold(actual.Signals, signal) {
						fail(fmt.Sprintf("expected signal %q was not produced", signal))
					}
				}
			}
		}
		result.Passed = result.FailureReason == ""
		if result.Passed {
			if coverage[fixture.CheckID] == nil {
				coverage[fixture.CheckID] = map[string]bool{}
			}
			coverage[fixture.CheckID][fixture.Case] = true
		}
		report.Cases = append(report.Cases, result)
	}
	for _, check := range checklist.Items {
		if !check.Verified {
			continue
		}
		report.VerifiedChecks = append(report.VerifiedChecks, check.ID)
		if !coverage[check.ID][FixturePositive] || !coverage[check.ID][FixtureNegative] {
			report.Issues = append(report.Issues, FixtureIssue{CheckID: check.ID, Message: "fixture-verified check requires passing positive and negative fixtures"})
		}
	}
	sort.Strings(report.VerifiedChecks)
	report.Ready = len(report.Issues) == 0
	return report
}

func ReviewForPublication(checklist engagement.ChecklistDefinition, registry *Registry, suite FixtureSuite, adapters map[string]FixtureAdapter) PublicationReport {
	report := PublicationReport{Quality: ReviewChecklist(checklist, registry), Fixtures: RunFixtureSuite(checklist, suite, adapters)}
	report.Ready = report.Quality.Ready && report.Fixtures.Ready
	return report
}

func checkReferencesAdapter(check engagement.CheckDefinition, adapterID string) bool {
	for _, reference := range check.Automation {
		if reference.Adapter == adapterID {
			return true
		}
	}
	return false
}

type securityHeadersAdapter struct{}

func (securityHeadersAdapter) ID() string { return AdapterSecurityHeadersV1 }

func (securityHeadersAdapter) Evaluate(fixture CheckFixture) (AdapterResult, error) {
	if _, err := url.ParseRequestURI(fixture.RequestURL); err != nil {
		return AdapterResult{}, fmt.Errorf("fixture request_url is invalid: %w", err)
	}
	headers := fixtureHeaders(fixture.Response.Headers)
	signals := []string{}
	requestURL, _ := url.Parse(fixture.RequestURL)
	if strings.EqualFold(requestURL.Scheme, "https") && strings.TrimSpace(headers.Get("Strict-Transport-Security")) == "" {
		signals = append(signals, "missing:strict-transport-security")
	}
	csp := strings.TrimSpace(headers.Get("Content-Security-Policy"))
	if csp == "" {
		signals = append(signals, "missing:content-security-policy")
	}
	if !strings.EqualFold(strings.TrimSpace(headers.Get("X-Content-Type-Options")), "nosniff") {
		signals = append(signals, "weak:x-content-type-options")
	}
	frame := strings.ToLower(strings.TrimSpace(headers.Get("X-Frame-Options")))
	if !strings.Contains(strings.ToLower(csp), "frame-ancestors") && frame != "deny" && frame != "sameorigin" {
		signals = append(signals, "missing:frame-protection")
	}
	referrer := strings.ToLower(strings.TrimSpace(headers.Get("Referrer-Policy")))
	if referrer == "" || referrer == "unsafe-url" {
		signals = append(signals, "weak:referrer-policy")
	}
	if len(signals) > 0 {
		return AdapterResult{Status: engagement.StatusVulnerable, Signals: signals}, nil
	}
	return AdapterResult{Status: engagement.StatusPassed, Signals: []string{"required-security-headers-present"}}, nil
}

type cookieFlagsAdapter struct{}

func (cookieFlagsAdapter) ID() string { return AdapterCookieFlagsV1 }

func (cookieFlagsAdapter) Evaluate(fixture CheckFixture) (AdapterResult, error) {
	if _, err := url.ParseRequestURI(fixture.RequestURL); err != nil {
		return AdapterResult{}, fmt.Errorf("fixture request_url is invalid: %w", err)
	}
	response := http.Response{Header: fixtureHeaders(fixture.Response.Headers)}
	cookies := response.Cookies()
	if len(cookies) == 0 {
		return AdapterResult{Status: engagement.StatusNotApplicable, Signals: []string{"no-set-cookie-observed"}}, nil
	}
	signals := []string{}
	for _, cookie := range cookies {
		name := strings.ToLower(cookie.Name)
		if !cookie.Secure {
			signals = append(signals, "cookie:"+name+":missing-secure")
		}
		if !cookie.HttpOnly {
			signals = append(signals, "cookie:"+name+":missing-httponly")
		}
		if cookie.SameSite == 0 || cookie.SameSite == http.SameSiteDefaultMode {
			signals = append(signals, "cookie:"+name+":missing-samesite")
		}
	}
	if len(signals) > 0 {
		return AdapterResult{Status: engagement.StatusVulnerable, Signals: signals}, nil
	}
	return AdapterResult{Status: engagement.StatusPassed, Signals: []string{"observed-cookie-flags-present"}}, nil
}

func fixtureHeaders(values map[string][]string) http.Header {
	headers := http.Header{}
	for name, entries := range values {
		for _, value := range entries {
			headers.Add(name, value)
		}
	}
	return headers
}

func containsFold(values []string, want string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), strings.TrimSpace(want)) {
			return true
		}
	}
	return false
}
