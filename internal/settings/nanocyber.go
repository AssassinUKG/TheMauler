package settings

import (
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

const NanoCyberAPIVersionV1 = "v1"

var (
	nanoCyberCredentialEnvPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)
	nanoCyberCredentialRefPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
)

// ValidateNanoCyberCredential mirrors the companion API's bounded bearer-token
// contract without persisting or returning the value.
func ValidateNanoCyberCredential(value string) (string, error) {
	value = strings.TrimSpace(value)
	if len(value) < 32 || len(value) > 512 || strings.ContainsAny(value, "\r\n\x00") {
		return "", fmt.Errorf("nanocyber token must contain between 32 and 512 non-control characters")
	}
	return value, nil
}

// ValidateNanoCyberConfig validates and canonicalises the optional local
// NanoCyber service boundary. NanoCyber is deliberately loopback-only in v1:
// it is an authenticated local sandbox authority, not a remote model provider.
func ValidateNanoCyberConfig(config NanoCyberConfig) (NanoCyberConfig, error) {
	config.Endpoint = strings.TrimSpace(config.Endpoint)
	config.APIVersion = strings.TrimSpace(config.APIVersion)
	config.CredentialEnv = strings.TrimSpace(config.CredentialEnv)
	config.CredentialRef = strings.TrimSpace(config.CredentialRef)

	if config.APIVersion == "" {
		config.APIVersion = NanoCyberAPIVersionV1
	}
	if config.APIVersion != NanoCyberAPIVersionV1 {
		return config, fmt.Errorf("nanocyber api_version must be %q", NanoCyberAPIVersionV1)
	}
	if config.CredentialEnv == "" {
		config.CredentialEnv = "NANOCYBER_API_TOKEN"
	}
	if !nanoCyberCredentialEnvPattern.MatchString(config.CredentialEnv) {
		return config, fmt.Errorf("nanocyber credential_env must be a valid environment variable name")
	}
	if config.CredentialRef == "" {
		config.CredentialRef = "nanocyber"
	}
	if !nanoCyberCredentialRefPattern.MatchString(config.CredentialRef) {
		return config, fmt.Errorf("nanocyber credential_ref must be a bounded opaque label")
	}
	if config.Endpoint == "" {
		if config.Enabled {
			return config, fmt.Errorf("nanocyber endpoint is required while the service is enabled")
		}
		return config, nil
	}

	parsed, err := url.Parse(config.Endpoint)
	if err != nil || !parsed.IsAbs() {
		return config, fmt.Errorf("nanocyber endpoint must be an absolute loopback URL")
	}
	if parsed.Scheme != "http" {
		return config, fmt.Errorf("nanocyber endpoint must use http on loopback")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return config, fmt.Errorf("nanocyber endpoint must not contain credentials, a path, query, or fragment")
	}
	hostname := strings.ToLower(strings.TrimSpace(parsed.Hostname()))
	if hostname != "localhost" {
		ip := net.ParseIP(hostname)
		if ip == nil || !ip.IsLoopback() {
			return config, fmt.Errorf("nanocyber endpoint must use localhost or a loopback IP")
		}
	}
	portText := parsed.Port()
	port, portErr := strconv.Atoi(portText)
	if portErr != nil || port < 1 || port > 65535 {
		return config, fmt.Errorf("nanocyber endpoint must include an explicit TCP port")
	}
	config.Endpoint = strings.TrimSuffix(parsed.String(), "/")
	return config, nil
}
