package settings

import "testing"

func TestProviderSecretsRoundTripAndEnvironmentPrecedence(t *testing.T) {
	t.Setenv("MAULER_CONFIG_DIR", t.TempDir())
	const provider = "openrouter"
	const envName = "MAULER_TEST_OPENROUTER_KEY"
	t.Setenv(envName, "")

	if err := SaveProviderAPIKey(provider, "stored-key"); err != nil {
		t.Fatal(err)
	}
	if got := ResolveProviderAPIKey(provider, envName); got != "stored-key" {
		t.Fatalf("stored key = %q", got)
	}
	status := GetProviderAPIKeyStatus(provider, envName)
	if !status.StoredConfigured || status.EnvironmentConfigured || !status.EffectiveConfigured {
		t.Fatalf("unexpected stored status: %#v", status)
	}

	t.Setenv(envName, "environment-key")
	if got := ResolveProviderAPIKey(provider, envName); got != "environment-key" {
		t.Fatalf("environment key did not win: %q", got)
	}
	status = GetProviderAPIKeyStatus(provider, envName)
	if !status.StoredConfigured || !status.EnvironmentConfigured || !status.EffectiveConfigured {
		t.Fatalf("unexpected combined status: %#v", status)
	}

	if err := ClearProviderAPIKey(provider); err != nil {
		t.Fatal(err)
	}
	t.Setenv(envName, "")
	if got := ResolveProviderAPIKey(provider, envName); got != "" {
		t.Fatalf("cleared key = %q", got)
	}
}
