package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"mauler/internal/settings"
)

const nanoCyberFixtureToken = "fixture-nanocyber-token-with-at-least-32-bytes"

func nanoCyberFixtureConfig(endpoint string) settings.NanoCyberConfig {
	return settings.NanoCyberConfig{
		Enabled: true, Endpoint: endpoint, APIVersion: "v1",
		CredentialEnv: "NANOCYBER_API_TOKEN", CredentialRef: "nanocyber",
	}
}

func newNanoCyberFixture(t *testing.T, mutate func(*nanoCyberCapabilitiesPayload)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer "+nanoCyberFixtureToken {
			t.Errorf("authorization header was not supplied")
		}
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/healthz":
			_, _ = writer.Write([]byte(`{"status":"ok"}`))
		case "/v1/capabilities":
			payload := nanoCyberCapabilitiesPayload{
				Product: "nanocyber", APIVersion: "v1", LeaseSchemaVersion: 1,
				Capabilities:        append([]string(nil), nanoCyberRequiredCapabilities...),
				EnvironmentProfiles: []string{"offline-code"},
			}
			if mutate != nil {
				mutate(&payload)
			}
			_ = json.NewEncoder(writer).Encode(payload)
		default:
			http.NotFound(writer, request)
		}
	}))
}

func TestProbeNanoCyberRequiresHealthyCompatibleInventory(t *testing.T) {
	server := newNanoCyberFixture(t, nil)
	defer server.Close()
	result := probeNanoCyber(context.Background(), nanoCyberFixtureConfig(server.URL), nanoCyberFixtureToken)
	if !result.Compatible || result.Status != "ready" || result.LeaseSchemaVersion != 1 {
		t.Fatalf("unexpected health: %#v", result)
	}
}

func TestProbeNanoCyberFailsClosedOnMissingCapability(t *testing.T) {
	server := newNanoCyberFixture(t, func(payload *nanoCyberCapabilitiesPayload) {
		payload.Capabilities = []string{"subagent_leases"}
	})
	defer server.Close()
	result := probeNanoCyber(context.Background(), nanoCyberFixtureConfig(server.URL), nanoCyberFixtureToken)
	if result.Compatible || result.Status != "incompatible" || !strings.Contains(result.Detail, "typed_jobs") {
		t.Fatalf("unexpected health: %#v", result)
	}
}

func TestProbeNanoCyberFailsClosedOnMissingLifecycleCapability(t *testing.T) {
	server := newNanoCyberFixture(t, func(payload *nanoCyberCapabilitiesPayload) {
		payload.Capabilities = []string{"subagent_leases", "typed_jobs", "receipt_evidence"}
	})
	defer server.Close()
	result := probeNanoCyber(context.Background(), nanoCyberFixtureConfig(server.URL), nanoCyberFixtureToken)
	if result.Compatible || result.Status != "incompatible" || !strings.Contains(result.Detail, "lease_cancellation") || !strings.Contains(result.Detail, "cleanup_status") {
		t.Fatalf("unexpected health: %#v", result)
	}
}

func TestProbeNanoCyberFailsClosedWithoutCredential(t *testing.T) {
	result := probeNanoCyber(context.Background(), nanoCyberFixtureConfig("http://127.0.0.1:9443"), "")
	if result.Compatible || result.Status != "unavailable" || !strings.Contains(result.Summary, "credential") {
		t.Fatalf("unexpected health: %#v", result)
	}
}

func TestProbeNanoCyberRejectsRedirects(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Redirect(writer, request, "/elsewhere", http.StatusFound)
	}))
	defer server.Close()
	result := probeNanoCyber(context.Background(), nanoCyberFixtureConfig(server.URL), nanoCyberFixtureToken)
	if result.Compatible || result.Status != "unavailable" || !strings.Contains(result.Detail, "redirects") {
		t.Fatalf("unexpected health: %#v", result)
	}
}

func TestProbeNanoCyberReportsCurrentAPIMissingInventoryAsIncompatible(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if request.URL.Path == "/healthz" {
			_, _ = writer.Write([]byte(`{"status":"ok"}`))
			return
		}
		http.NotFound(writer, request)
	}))
	defer server.Close()
	result := probeNanoCyber(context.Background(), nanoCyberFixtureConfig(server.URL), nanoCyberFixtureToken)
	if result.Compatible || result.Status != "incompatible" || !strings.Contains(result.Summary, "inventory") {
		t.Fatalf("unexpected health: %#v", result)
	}
}
