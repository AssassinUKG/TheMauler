package mcpclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"testing"
	"time"

	"mauler/internal/settings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type echoInput struct {
	Text string `json:"text" jsonschema:"text to echo"`
}

func testMCPServer() *mcp.Server {
	server := mcp.NewServer(
		&mcp.Implementation{Name: "mauler-test-server", Version: "1.0.0"},
		&mcp.ServerOptions{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))},
	)
	mcp.AddTool(server, &mcp.Tool{Name: "echo", Description: "echo untrusted text"}, func(_ context.Context, _ *mcp.CallToolRequest, input echoInput) (*mcp.CallToolResult, any, error) {
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: input.Text}}}, nil, nil
	})
	return server
}

func TestManagerStreamableHTTPMCPPath(t *testing.T) {
	server := testMCPServer()
	expectedHost := ""
	expectedOrigin := ""
	mux := http.NewServeMux()
	streamable := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil)
	mux.Handle("/mcp", http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Host != expectedHost || request.Header.Get("Origin") != expectedOrigin {
			http.Error(response, "blocked compatibility headers", http.StatusForbidden)
			return
		}
		streamable.ServeHTTP(response, request)
	}))
	httpServer := httptest.NewServer(mux)
	t.Cleanup(httpServer.Close)
	parsedURL, err := url.Parse(httpServer.URL)
	if err != nil {
		t.Fatal(err)
	}
	expectedHost = "localhost:" + parsedURL.Port()
	expectedOrigin = "http://" + expectedHost

	manager := New(settings.MCPConfig{Enabled: true, Servers: []settings.MCPServerConfig{{
		ID: "streamable", Name: "Streamable test", Enabled: true,
		Transport: settings.MCPTransportStreamableHTTP, URL: httpServer.URL + "/mcp", TimeoutSeconds: 10,
		HTTPHostHeader: expectedHost, HTTPOrigin: expectedOrigin,
	}}})
	t.Cleanup(func() { _ = manager.Close() })

	assertEchoRoundTrip(t, manager, "streamable")
	status := manager.Statuses()[0]
	if !status.Connected || status.ProtocolVersion == "" || status.ToolCount != 1 {
		t.Fatalf("unexpected streamable status: %#v", status)
	}
}

func TestManagerLegacySSE(t *testing.T) {
	server := testMCPServer()
	httpServer := httptest.NewServer(mcp.NewSSEHandler(func(*http.Request) *mcp.Server { return server }, nil))
	t.Cleanup(httpServer.Close)

	manager := New(settings.MCPConfig{Enabled: true, Servers: []settings.MCPServerConfig{{
		ID: "legacy", Name: "Legacy SSE test", Enabled: true,
		Transport: settings.MCPTransportSSE, URL: httpServer.URL, TimeoutSeconds: 10,
	}}})
	t.Cleanup(func() { _ = manager.Close() })

	assertEchoRoundTrip(t, manager, "legacy")
}

func TestManagerStdio(t *testing.T) {
	t.Setenv("MAULER_MCP_HELPER", "1")
	manager := New(settings.MCPConfig{Enabled: true, Servers: []settings.MCPServerConfig{{
		ID: "stdio", Name: "stdio test", Enabled: true,
		Transport: settings.MCPTransportStdio, Command: os.Args[0],
		Args: []string{"-test.run=^TestMCPStdioHelperProcess$"}, EnvAllowlist: []string{"MAULER_MCP_HELPER"}, TimeoutSeconds: 10,
	}}})
	t.Cleanup(func() { _ = manager.Close() })

	assertEchoRoundTrip(t, manager, "stdio")
}

func TestMCPStdioHelperProcess(t *testing.T) {
	if os.Getenv("MAULER_MCP_HELPER") != "1" {
		return
	}
	if err := testMCPServer().Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
}

func assertEchoRoundTrip(t *testing.T, manager *Manager, serverID string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	listed, err := manager.ListTools(ctx, serverID)
	if err != nil {
		t.Fatal(err)
	}
	if listed.Trust != "untrusted_mcp_server" || len(listed.Tools) != 1 || listed.Tools[0].Name != "echo" || len(listed.Tools[0].InputSchema) == 0 {
		t.Fatalf("unexpected tools/list result: %#v", listed)
	}
	called, err := manager.CallTool(ctx, serverID, "echo", map[string]any{"text": "hello from Mauler"})
	if err != nil {
		t.Fatal(err)
	}
	if called.Trust != "untrusted_mcp_server" || called.IsError || len(called.Content) != 1 || called.Content[0].Text != "hello from Mauler" {
		t.Fatalf("unexpected tools/call result: %#v", called)
	}
}

func TestCompactCallResultDoesNotReturnBinaryBase64(t *testing.T) {
	result := compactCallResult("server", "binary", &mcp.CallToolResult{Content: []mcp.Content{
		&mcp.ImageContent{MIMEType: "image/png", Data: []byte("secret-binary")},
	}})
	if len(result.Content) != 1 || result.Content[0].Bytes != len("secret-binary") || result.Content[0].SHA256 == "" || result.Content[0].Text != "" {
		t.Fatalf("binary result was not compacted safely: %#v", result)
	}
}

func TestLiveConfiguredMCPServer(t *testing.T) {
	endpoint := os.Getenv("MAULER_MCP_LIVE_URL")
	if endpoint == "" {
		t.Skip("set MAULER_MCP_LIVE_URL for an operator-authorised live MCP check")
	}
	transport := os.Getenv("MAULER_MCP_LIVE_TRANSPORT")
	if transport == "" {
		transport = settings.MCPTransportStreamableHTTP
	}
	timeoutSeconds := 15
	if raw := os.Getenv("MAULER_MCP_LIVE_TIMEOUT_SECONDS"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed >= 5 && parsed <= 300 {
			timeoutSeconds = parsed
		}
	}
	manager := New(settings.MCPConfig{Enabled: true, Servers: []settings.MCPServerConfig{{
		ID: "live", Name: "Live MCP", Enabled: true,
		Transport: transport, URL: endpoint, TimeoutSeconds: timeoutSeconds,
		HTTPHostHeader: os.Getenv("MAULER_MCP_LIVE_HOST"), HTTPOrigin: os.Getenv("MAULER_MCP_LIVE_ORIGIN"),
	}}})
	t.Cleanup(func() { _ = manager.Close() })
	result, err := manager.ListTools(context.Background(), "live")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Tools) == 0 {
		t.Fatal("live MCP server returned no tools")
	}
	if os.Getenv("MAULER_MCP_LIVE_LOG_TOOLS") == "1" {
		for _, tool := range result.Tools {
			t.Logf("tool=%s description=%s", tool.Name, tool.Description)
		}
	}
	if requested := os.Getenv("MAULER_MCP_LIVE_SCHEMA_TOOL"); requested != "" {
		for _, tool := range result.Tools {
			if tool.Name == requested {
				t.Logf("tool=%s input_schema=%s", tool.Name, string(tool.InputSchema))
			}
		}
	}
	if requested := os.Getenv("MAULER_MCP_LIVE_CALL_TOOL"); requested != "" {
		arguments := map[string]any{}
		if raw := os.Getenv("MAULER_MCP_LIVE_CALL_ARGS"); raw != "" {
			if err := json.Unmarshal([]byte(raw), &arguments); err != nil {
				t.Fatalf("decode MAULER_MCP_LIVE_CALL_ARGS: %v", err)
			}
		}
		called, err := manager.CallTool(context.Background(), "live", requested, arguments)
		if err != nil {
			t.Fatalf("call live tool %s: %v", requested, err)
		}
		encoded, err := json.Marshal(called)
		if err != nil {
			t.Fatalf("encode live tool result: %v", err)
		}
		t.Logf("call_result=%s", encoded)
	}
	t.Logf("live MCP initialize/tools-list passed: transport=%s tools=%d", result.Transport, len(result.Tools))
}
