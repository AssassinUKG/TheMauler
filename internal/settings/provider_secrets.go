package settings

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const providerSecretsFilename = "provider-secrets.json"

var providerSecretsMu sync.Mutex

// ProviderAPIKeyStatus reports whether a provider can authenticate without
// ever returning the secret itself to the UI.
type ProviderAPIKeyStatus struct {
	Provider              string `json:"provider"`
	EnvironmentConfigured bool   `json:"environment_configured"`
	StoredConfigured      bool   `json:"stored_configured"`
	EffectiveConfigured   bool   `json:"effective_configured"`
}

type providerSecretsFile struct {
	Providers map[string]string `json:"providers"`
}

// ResolveProviderAPIKey gives an environment variable precedence over the
// UI-managed local secret store.
func ResolveProviderAPIKey(providerName, envName string) string {
	if name := strings.TrimSpace(envName); name != "" {
		if value := strings.TrimSpace(os.Getenv(name)); value != "" {
			return value
		}
	}
	providerSecretsMu.Lock()
	defer providerSecretsMu.Unlock()
	secrets, err := loadProviderSecretsLocked()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(secrets.Providers[strings.TrimSpace(providerName)])
}

// GetProviderAPIKeyStatus returns only presence flags suitable for Wails.
func GetProviderAPIKeyStatus(providerName, envName string) ProviderAPIKeyStatus {
	providerName = strings.TrimSpace(providerName)
	environmentConfigured := false
	if name := strings.TrimSpace(envName); name != "" {
		environmentConfigured = strings.TrimSpace(os.Getenv(name)) != ""
	}
	providerSecretsMu.Lock()
	secrets, err := loadProviderSecretsLocked()
	providerSecretsMu.Unlock()
	storedConfigured := err == nil && strings.TrimSpace(secrets.Providers[providerName]) != ""
	return ProviderAPIKeyStatus{
		Provider:              providerName,
		EnvironmentConfigured: environmentConfigured,
		StoredConfigured:      storedConfigured,
		EffectiveConfigured:   environmentConfigured || storedConfigured,
	}
}

// SaveProviderAPIKey persists a provider key outside profiles.toml so normal
// profile round-trips cannot expose or accidentally erase it.
func SaveProviderAPIKey(providerName, apiKey string) error {
	providerName = strings.TrimSpace(providerName)
	apiKey = strings.TrimSpace(apiKey)
	if providerName == "" {
		return fmt.Errorf("provider name is required")
	}
	if apiKey == "" {
		return fmt.Errorf("API key is required")
	}
	providerSecretsMu.Lock()
	defer providerSecretsMu.Unlock()
	secrets, err := loadProviderSecretsLocked()
	if err != nil {
		return err
	}
	secrets.Providers[providerName] = apiKey
	return writeProviderSecretsLocked(secrets)
}

// ClearProviderAPIKey removes only the UI-managed key. An environment variable
// remains authoritative and is never modified by Mauler.
func ClearProviderAPIKey(providerName string) error {
	providerName = strings.TrimSpace(providerName)
	if providerName == "" {
		return fmt.Errorf("provider name is required")
	}
	providerSecretsMu.Lock()
	defer providerSecretsMu.Unlock()
	secrets, err := loadProviderSecretsLocked()
	if err != nil {
		return err
	}
	delete(secrets.Providers, providerName)
	return writeProviderSecretsLocked(secrets)
}

func loadProviderSecretsLocked() (providerSecretsFile, error) {
	secrets := providerSecretsFile{Providers: make(map[string]string)}
	dir, err := ConfigDir()
	if err != nil {
		return secrets, err
	}
	data, err := os.ReadFile(filepath.Join(dir, providerSecretsFilename))
	if os.IsNotExist(err) {
		return secrets, nil
	}
	if err != nil {
		return secrets, fmt.Errorf("read provider secrets: %w", err)
	}
	if err := json.Unmarshal(data, &secrets); err != nil {
		return secrets, fmt.Errorf("parse provider secrets: %w", err)
	}
	if secrets.Providers == nil {
		secrets.Providers = make(map[string]string)
	}
	return secrets, nil
}

func writeProviderSecretsLocked(secrets providerSecretsFile) error {
	dir, err := ConfigDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	data, err := json.MarshalIndent(secrets, "", "  ")
	if err != nil {
		return fmt.Errorf("encode provider secrets: %w", err)
	}
	data = append(data, '\n')
	dst := filepath.Join(dir, providerSecretsFilename)
	tmp := dst + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write provider secrets: %w", err)
	}
	if err := os.Rename(tmp, dst); err != nil {
		if removeErr := os.Remove(dst); removeErr != nil && !os.IsNotExist(removeErr) {
			_ = os.Remove(tmp)
			return fmt.Errorf("replace provider secrets: %w", err)
		}
		if retryErr := os.Rename(tmp, dst); retryErr != nil {
			_ = os.Remove(tmp)
			return fmt.Errorf("replace provider secrets: %w", retryErr)
		}
	}
	return nil
}
