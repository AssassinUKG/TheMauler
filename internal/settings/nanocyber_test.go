package settings

import "testing"

func TestValidateNanoCyberConfigAcceptsExplicitLoopback(t *testing.T) {
	config, err := ValidateNanoCyberConfig(NanoCyberConfig{
		Enabled: true, Endpoint: " http://127.0.0.1:9443/ ", APIVersion: "v1",
		CredentialEnv: "NANOCYBER_API_TOKEN", CredentialRef: "nanocyber",
	})
	if err != nil {
		t.Fatal(err)
	}
	if config.Endpoint != "http://127.0.0.1:9443" {
		t.Fatalf("canonical endpoint = %q", config.Endpoint)
	}
}

func TestValidateNanoCyberConfigRejectsUnsafeEndpoints(t *testing.T) {
	tests := []NanoCyberConfig{
		{Enabled: true, Endpoint: "https://127.0.0.1:9443", APIVersion: "v1"},
		{Enabled: true, Endpoint: "http://example.test:9443", APIVersion: "v1"},
		{Enabled: true, Endpoint: "http://127.0.0.1", APIVersion: "v1"},
		{Enabled: true, Endpoint: "http://127.0.0.1:9443/v1", APIVersion: "v1"},
		{Enabled: true, Endpoint: "http://user:secret@127.0.0.1:9443", APIVersion: "v1"},
		{Enabled: true, Endpoint: "http://127.0.0.1:9443", APIVersion: "v2"},
	}
	for _, test := range tests {
		if _, err := ValidateNanoCyberConfig(test); err == nil {
			t.Fatalf("unsafe config accepted: %#v", test)
		}
	}
}

func TestValidateNanoCyberConfigAllowsDisabledUnconfiguredService(t *testing.T) {
	config, err := ValidateNanoCyberConfig(NanoCyberConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if config.Enabled || config.Endpoint != "" || config.APIVersion != "v1" {
		t.Fatalf("unexpected disabled config: %#v", config)
	}
}

func TestValidateNanoCyberCredentialMatchesV1Boundary(t *testing.T) {
	if value, err := ValidateNanoCyberCredential("  fixture-token-with-at-least-thirty-two-bytes  "); err != nil || value != "fixture-token-with-at-least-thirty-two-bytes" {
		t.Fatalf("valid token rejected: value=%q err=%v", value, err)
	}
	for _, value := range []string{"short", "fixture-token-with-at-least-thirty-two-bytes\nheader"} {
		if _, err := ValidateNanoCyberCredential(value); err == nil {
			t.Fatalf("invalid token accepted: %q", value)
		}
	}
}
