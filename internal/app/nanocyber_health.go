package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"mauler/internal/nanocyber"
	"mauler/internal/settings"
)

const nanoCyberMaxHealthResponseBytes = 64 * 1024

var nanoCyberRequiredCapabilities = []string{
	"subagent_leases",
	"typed_jobs",
	"receipt_evidence",
	"lease_cancellation",
	"cleanup_status",
}

// NanoCyberHealth is safe to return to the UI: it contains configuration and
// capability metadata only, never the bearer credential.
type NanoCyberHealth struct {
	Enabled              bool     `json:"enabled"`
	Compatible           bool     `json:"compatible"`
	Endpoint             string   `json:"endpoint,omitempty"`
	APIVersion           string   `json:"api_version,omitempty"`
	CredentialConfigured bool     `json:"credential_configured"`
	Status               string   `json:"status"`
	Summary              string   `json:"summary"`
	Detail               string   `json:"detail,omitempty"`
	Capabilities         []string `json:"capabilities,omitempty"`
	EnvironmentProfiles  []string `json:"environment_profiles,omitempty"`
	LeaseSchemaVersion   uint32   `json:"lease_schema_version,omitempty"`
}

type nanoCyberHealthPayload struct {
	Status string `json:"status"`
}

type nanoCyberCapabilitiesPayload struct {
	Product             string   `json:"product"`
	APIVersion          string   `json:"api_version"`
	LeaseSchemaVersion  uint32   `json:"lease_schema_version"`
	Capabilities        []string `json:"capabilities"`
	EnvironmentProfiles []string `json:"environment_profiles"`
}

// GetNanoCyberHealth probes the optional local service without exposing its
// credential. Missing or incompatible capability inventory fails closed.
func (a *App) GetNanoCyberHealth() NanoCyberHealth {
	a.mu.Lock()
	config := a.cfg.NanoCyber
	a.mu.Unlock()
	credential := settings.ResolveServiceCredential(config.CredentialRef, config.CredentialEnv)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return probeNanoCyber(ctx, config, credential)
}

func probeNanoCyber(ctx context.Context, config settings.NanoCyberConfig, credential string) NanoCyberHealth {
	result := NanoCyberHealth{
		Enabled: config.Enabled, Endpoint: strings.TrimSpace(config.Endpoint),
		APIVersion:           strings.TrimSpace(config.APIVersion),
		CredentialConfigured: strings.TrimSpace(credential) != "",
		Status:               "disabled", Summary: "NanoCyber sandbox service is disabled",
	}
	validated, err := settings.ValidateNanoCyberConfig(config)
	if err != nil {
		result.Status = "error"
		result.Summary = "NanoCyber configuration is invalid"
		result.Detail = err.Error()
		return result
	}
	result.Endpoint, result.APIVersion = validated.Endpoint, validated.APIVersion
	if !validated.Enabled {
		return result
	}
	if strings.TrimSpace(credential) == "" {
		result.Status = "unavailable"
		result.Summary = "NanoCyber credential is not configured"
		result.Detail = fmt.Sprintf("Set %s or store a token in Settings > Sandbox.", validated.CredentialEnv)
		return result
	}

	client, endpoint, err := nanocyber.NewLoopbackHTTPClient(validated.Endpoint)
	if err != nil {
		result.Status, result.Summary, result.Detail = "error", "NanoCyber client configuration failed", err.Error()
		return result
	}
	var health nanoCyberHealthPayload
	if err := nanoCyberGetJSON(ctx, client, endpoint+"/healthz", credential, &health); err != nil {
		result.Status, result.Summary, result.Detail = "unavailable", "NanoCyber health check failed", err.Error()
		return result
	}
	if !strings.EqualFold(strings.TrimSpace(health.Status), "ok") {
		result.Status, result.Summary = "unavailable", "NanoCyber did not report healthy"
		result.Detail = fmt.Sprintf("health status=%q", health.Status)
		return result
	}

	var capabilities nanoCyberCapabilitiesPayload
	capabilityURL := endpoint + "/" + validated.APIVersion + "/capabilities"
	if err := nanoCyberGetJSON(ctx, client, capabilityURL, credential, &capabilities); err != nil {
		result.Status, result.Summary = "incompatible", "NanoCyber capability inventory is unavailable"
		result.Detail = err.Error()
		return result
	}
	result.Capabilities = cleanStringSet(capabilities.Capabilities)
	result.EnvironmentProfiles = cleanStringSet(capabilities.EnvironmentProfiles)
	result.LeaseSchemaVersion = capabilities.LeaseSchemaVersion
	if !strings.EqualFold(strings.TrimSpace(capabilities.Product), "nanocyber") {
		result.Status, result.Summary = "incompatible", "Endpoint is not a compatible NanoCyber service"
		return result
	}
	if capabilities.APIVersion != validated.APIVersion || capabilities.LeaseSchemaVersion != 1 {
		result.Status, result.Summary = "incompatible", "NanoCyber API or lease schema is incompatible"
		result.Detail = fmt.Sprintf("api=%q lease_schema=%d", capabilities.APIVersion, capabilities.LeaseSchemaVersion)
		return result
	}
	missing := missingStrings(result.Capabilities, nanoCyberRequiredCapabilities)
	if !nanoCyberContainsString(result.EnvironmentProfiles, "offline-code") {
		missing = append(missing, "environment_profile:offline-code")
	}
	if len(missing) > 0 {
		result.Status, result.Summary = "incompatible", "NanoCyber is missing required sandbox capabilities"
		result.Detail = "missing: " + strings.Join(missing, ", ")
		return result
	}
	result.Compatible = true
	result.Status = "ready"
	result.Summary = "Authenticated NanoCyber sandbox API is healthy and compatible"
	return result
}

func nanoCyberGetJSON(ctx context.Context, client *http.Client, target, credential string, output any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+credential)
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s returned HTTP %d", request.URL.Path, response.StatusCode)
	}
	contentType := strings.ToLower(response.Header.Get("Content-Type"))
	if !strings.HasPrefix(contentType, "application/json") {
		return fmt.Errorf("GET %s returned a non-JSON response", request.URL.Path)
	}
	reader := io.LimitReader(response.Body, nanoCyberMaxHealthResponseBytes+1)
	data, err := io.ReadAll(reader)
	if err != nil {
		return err
	}
	if len(data) > nanoCyberMaxHealthResponseBytes {
		return fmt.Errorf("GET %s exceeded the response limit", request.URL.Path)
	}
	if err := json.Unmarshal(data, output); err != nil {
		return fmt.Errorf("decode GET %s: %w", request.URL.Path, err)
	}
	return nil
}

func cleanStringSet(values []string) []string {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			set[value] = struct{}{}
		}
	}
	result := make([]string, 0, len(set))
	for value := range set {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func missingStrings(values, required []string) []string {
	missing := make([]string, 0)
	for _, requiredValue := range required {
		if !nanoCyberContainsString(values, requiredValue) {
			missing = append(missing, requiredValue)
		}
	}
	return missing
}

func nanoCyberContainsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
