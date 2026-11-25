package settings

import (
	"strings"
	"testing"
)

func TestValidateMCPConfigAcceptsAllSupportedTransports(t *testing.T) {
	config, err := ValidateMCPConfig(MCPConfig{Enabled: true, Servers: []MCPServerConfig{
		{ID: "port_swigger", Name: "PortSwigger", Enabled: true, Transport: MCPTransportStreamableHTTP, URL: "http://192.168.1.53:9876/mcp/", HTTPHostHeader: "localhost:9876", HTTPOrigin: "http://localhost:9876"},
		{ID: "legacy_sse", Enabled: true, Transport: MCPTransportSSE, URL: "http://127.0.0.1:9876/"},
		{ID: "local_stdio", Enabled: true, Transport: MCPTransportStdio, Command: "java", Args: []string{"-jar", "mcp-proxy.jar"}, EnvAllowlist: []string{"JAVA_HOME", "JAVA_HOME"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if got := config.Servers[0].URL; got != "http://192.168.1.53:9876/mcp" {
		t.Fatalf("streamable URL = %q", got)
	}
	if got := config.Servers[1].URL; got != "http://127.0.0.1:9876" {
		t.Fatalf("SSE URL = %q", got)
	}
	if got := config.Servers[2].TimeoutSeconds; got != 30 {
		t.Fatalf("stdio timeout = %d", got)
	}
	if got := config.Servers[2].EnvAllowlist; len(got) != 1 || got[0] != "JAVA_HOME" {
		t.Fatalf("stdio env allowlist = %#v", got)
	}
}

func TestValidateMCPConfigRejectsUnsafeCompatibilityHeaders(t *testing.T) {
	tests := []MCPServerConfig{
		{ID: "remote_host", Transport: MCPTransportStreamableHTTP, URL: "http://192.168.1.53:9876/mcp", HTTPHostHeader: "example.test:9876"},
		{ID: "wrong_port", Transport: MCPTransportStreamableHTTP, URL: "http://192.168.1.53:9876/mcp", HTTPHostHeader: "localhost:9999"},
		{ID: "remote_origin", Transport: MCPTransportStreamableHTTP, URL: "http://192.168.1.53:9876/mcp", HTTPOrigin: "https://example.test:9876"},
	}
	for _, server := range tests {
		if _, err := ValidateMCPConfig(MCPConfig{Servers: []MCPServerConfig{server}}); err == nil {
			t.Fatalf("unsafe compatibility headers accepted: %#v", server)
		}
	}
}

func TestValidateMCPConfigRejectsRemoteNetworkAndEmbeddedSecrets(t *testing.T) {
	tests := []MCPServerConfig{
		{ID: "public", Transport: MCPTransportStreamableHTTP, URL: "http://203.0.113.9:9876/mcp"},
		{ID: "hostname", Transport: MCPTransportSSE, URL: "http://mcp.example.test/sse"},
		{ID: "secure_remote", Transport: MCPTransportStreamableHTTP, URL: "https://example.test/mcp"},
		{ID: "secret", Transport: MCPTransportStreamableHTTP, URL: "https://user:password@example.test/mcp"},
		{ID: "query", Transport: MCPTransportStreamableHTTP, URL: "https://example.test/mcp?token=secret"},
	}
	for _, server := range tests {
		_, err := ValidateMCPConfig(MCPConfig{Enabled: true, Servers: []MCPServerConfig{server}})
		if err == nil {
			t.Fatalf("unsafe server accepted: %#v", server)
		}
	}
}

func TestValidateMCPConfigRejectsDuplicateIDsAndMixedTransportFields(t *testing.T) {
	_, err := ValidateMCPConfig(MCPConfig{Servers: []MCPServerConfig{
		{ID: "same", Transport: MCPTransportStdio, Command: "server.exe"},
		{ID: "same", Transport: MCPTransportStdio, Command: "server.exe"},
	}})
	if err == nil || !strings.Contains(err.Error(), "duplicate id") {
		t.Fatalf("duplicate ID error = %v", err)
	}
	_, err = ValidateMCPConfig(MCPConfig{Servers: []MCPServerConfig{{
		ID: "mixed", Transport: MCPTransportStreamableHTTP, URL: "https://example.test/mcp", Command: "server.exe",
	}}})
	if err == nil {
		t.Fatal("mixed HTTP/stdio config was accepted")
	}
}

func TestValidateMCPConfigAllowsDisabledEmptyBoundary(t *testing.T) {
	config, err := ValidateMCPConfig(MCPConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if config.Enabled || len(config.Servers) != 0 {
		t.Fatalf("unexpected config: %#v", config)
	}
}
