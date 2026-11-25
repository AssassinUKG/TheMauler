package settings

import (
	"fmt"
	"net"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	MCPTransportStdio          = "stdio"
	MCPTransportSSE            = "sse"
	MCPTransportStreamableHTTP = "streamable_http"
	defaultMCPTimeoutSeconds   = 30
	maxMCPServers              = 16
)

var (
	mcpServerIDPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)
	mcpEnvNamePattern  = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)
)

// ValidateMCPConfig validates and canonicalises the external MCP boundary.
// Network transports are deliberately local-only: endpoints must use
// localhost, loopback, or a literal private-LAN IP. Mauler does not create
// tunnels, relays, or implicit remote routes for MCP.
func ValidateMCPConfig(config MCPConfig) (MCPConfig, error) {
	if len(config.Servers) > maxMCPServers {
		return config, fmt.Errorf("mcp supports at most %d configured servers", maxMCPServers)
	}
	seen := make(map[string]struct{}, len(config.Servers))
	for i := range config.Servers {
		server, err := validateMCPServerConfig(config.Servers[i])
		if err != nil {
			return config, fmt.Errorf("mcp server %d: %w", i+1, err)
		}
		key := strings.ToLower(server.ID)
		if _, ok := seen[key]; ok {
			return config, fmt.Errorf("mcp server %d: duplicate id %q", i+1, server.ID)
		}
		seen[key] = struct{}{}
		config.Servers[i] = server
	}
	return config, nil
}

func validateMCPServerConfig(server MCPServerConfig) (MCPServerConfig, error) {
	server.ID = strings.ToLower(strings.TrimSpace(server.ID))
	server.Name = strings.TrimSpace(server.Name)
	server.Transport = strings.ToLower(strings.TrimSpace(server.Transport))
	server.Command = strings.TrimSpace(server.Command)
	server.WorkingDir = filepath.Clean(strings.TrimSpace(server.WorkingDir))
	server.URL = strings.TrimSpace(server.URL)
	server.BearerTokenEnv = strings.TrimSpace(server.BearerTokenEnv)
	server.HTTPHostHeader = strings.TrimSpace(server.HTTPHostHeader)
	server.HTTPOrigin = strings.TrimSpace(server.HTTPOrigin)
	if server.WorkingDir == "." {
		server.WorkingDir = ""
	}
	if !mcpServerIDPattern.MatchString(server.ID) {
		return server, fmt.Errorf("id must match %s", mcpServerIDPattern.String())
	}
	if server.Name == "" {
		server.Name = server.ID
	}
	if len(server.Name) > 80 || strings.ContainsAny(server.Name, "\r\n\x00") {
		return server, fmt.Errorf("name must be a single line of at most 80 characters")
	}
	if server.TimeoutSeconds == 0 {
		server.TimeoutSeconds = defaultMCPTimeoutSeconds
	}
	if server.TimeoutSeconds < 5 || server.TimeoutSeconds > 300 {
		return server, fmt.Errorf("timeout_seconds must be between 5 and 300")
	}
	if server.BearerTokenEnv != "" && !mcpEnvNamePattern.MatchString(server.BearerTokenEnv) {
		return server, fmt.Errorf("bearer_token_env must be a valid environment variable name")
	}
	server.EnvAllowlist = normaliseMCPEnvAllowlist(server.EnvAllowlist)
	for _, name := range server.EnvAllowlist {
		if !mcpEnvNamePattern.MatchString(name) {
			return server, fmt.Errorf("env_allowlist contains invalid environment variable name %q", name)
		}
	}
	if len(server.Args) > 64 {
		return server, fmt.Errorf("args supports at most 64 entries")
	}
	for _, arg := range server.Args {
		if len(arg) > 4096 || strings.ContainsRune(arg, '\x00') {
			return server, fmt.Errorf("args entries must be at most 4096 characters and contain no NUL")
		}
	}

	switch server.Transport {
	case MCPTransportStdio:
		if server.Command == "" {
			return server, fmt.Errorf("command is required for stdio transport")
		}
		if len(server.Command) > 2048 || strings.ContainsAny(server.Command, "\r\n\x00") {
			return server, fmt.Errorf("command must be a single line of at most 2048 characters")
		}
		if server.URL != "" || server.BearerTokenEnv != "" || server.HTTPHostHeader != "" || server.HTTPOrigin != "" {
			return server, fmt.Errorf("stdio transport cannot define HTTP fields")
		}
	case MCPTransportSSE, MCPTransportStreamableHTTP:
		if server.Command != "" || len(server.Args) != 0 || server.WorkingDir != "" || len(server.EnvAllowlist) != 0 {
			return server, fmt.Errorf("HTTP transports cannot define command, args, working_dir, or env_allowlist")
		}
		endpoint, err := validateMCPHTTPURL(server.URL)
		if err != nil {
			return server, err
		}
		server.URL = endpoint
		if err := validateMCPCompatibilityHeaders(&server); err != nil {
			return server, err
		}
	default:
		return server, fmt.Errorf("transport must be %q, %q, or %q", MCPTransportStdio, MCPTransportSSE, MCPTransportStreamableHTTP)
	}
	return server, nil
}

func validateMCPCompatibilityHeaders(server *MCPServerConfig) error {
	endpoint, err := url.Parse(server.URL)
	if err != nil {
		return err
	}
	if server.HTTPHostHeader != "" {
		hostURL, err := url.Parse("http://" + server.HTTPHostHeader)
		if err != nil || hostURL.Hostname() == "" || hostURL.User != nil || hostURL.Path != "" || hostURL.RawQuery != "" || hostURL.Fragment != "" {
			return fmt.Errorf("http_host_header must be a local host[:port] value")
		}
		if !isPrivateMCPHost(hostURL.Hostname()) {
			return fmt.Errorf("http_host_header must use localhost, loopback, or a literal private-LAN IP")
		}
		if effectiveURLPort(hostURL) != effectiveURLPort(endpoint) {
			return fmt.Errorf("http_host_header port must match the MCP endpoint port")
		}
	}
	if server.HTTPOrigin != "" {
		origin, err := url.Parse(server.HTTPOrigin)
		if err != nil || !origin.IsAbs() || origin.Hostname() == "" || (origin.Scheme != "http" && origin.Scheme != "https") {
			return fmt.Errorf("http_origin must be an absolute local HTTP or HTTPS origin")
		}
		if origin.User != nil || (origin.Path != "" && origin.Path != "/") || origin.RawQuery != "" || origin.Fragment != "" || !isPrivateMCPHost(origin.Hostname()) {
			return fmt.Errorf("http_origin must contain only a local scheme, host, and matching port")
		}
		if effectiveURLPort(origin) != effectiveURLPort(endpoint) {
			return fmt.Errorf("http_origin port must match the MCP endpoint port")
		}
		server.HTTPOrigin = strings.TrimSuffix(origin.String(), "/")
	}
	return nil
}

func effectiveURLPort(value *url.URL) string {
	if value.Port() != "" {
		return value.Port()
	}
	if value.Scheme == "https" {
		return "443"
	}
	return "80"
}

func validateMCPHTTPURL(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || !parsed.IsAbs() || parsed.Hostname() == "" {
		return "", fmt.Errorf("url must be an absolute HTTP or HTTPS endpoint")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("url must use http or https")
	}
	if parsed.User != nil || parsed.Fragment != "" {
		return "", fmt.Errorf("url must not contain embedded credentials or a fragment")
	}
	if parsed.RawQuery != "" {
		return "", fmt.Errorf("url must not contain a query string")
	}
	if !isPrivateMCPHost(parsed.Hostname()) {
		return "", fmt.Errorf("network MCP endpoints must use localhost, loopback, or a literal private-LAN IP")
	}
	return strings.TrimSuffix(parsed.String(), "/"), nil
}

func isPrivateMCPHost(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && (ip.IsLoopback() || ip.IsPrivate())
}

func normaliseMCPEnvAllowlist(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key := strings.ToUpper(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, value)
	}
	return out
}
