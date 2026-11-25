package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"mauler/internal/llm"
	"mauler/internal/mcpclient"
	"mauler/internal/settings"
	"mauler/internal/tools"
)

func TestMCPToolRegistersOnlyForEnabledConfiguredBoundary(t *testing.T) {
	app := &App{registry: tools.NewEmpty(), mcpManager: mcpclient.New(settings.MCPConfig{})}
	app.syncMCPToolRegistration()
	if _, ok := app.registry.Get("mcp"); ok {
		t.Fatal("disabled MCP boundary was advertised")
	}

	config := settings.MCPConfig{Enabled: true, Servers: []settings.MCPServerConfig{{
		ID: "lan", Name: "LAN", Enabled: true, Transport: settings.MCPTransportStreamableHTTP,
		URL: "http://192.168.1.53:9876/mcp", TimeoutSeconds: 30,
	}}}
	if err := app.mcpManager.Configure(config); err != nil {
		t.Fatal(err)
	}
	app.syncMCPToolRegistration()
	tool, ok := app.registry.Get("mcp")
	if !ok || !tool.Destructive() {
		t.Fatalf("enabled MCP boundary not registered safely: tool=%#v ok=%v", tool, ok)
	}
}

func TestMCPToolStatusDoesNotConnect(t *testing.T) {
	config := settings.MCPConfig{Enabled: true, Servers: []settings.MCPServerConfig{{
		ID: "lan", Name: "LAN", Enabled: true, Transport: settings.MCPTransportStreamableHTTP,
		URL: "http://192.168.1.53:9876/mcp", TimeoutSeconds: 30,
	}}}
	app := &App{mcpManager: mcpclient.New(config)}
	out, err := (&mcpTool{app: app}).Run(context.Background(), json.RawMessage(`{"action":"status","server":"lan"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"state":"disconnected"`) || !strings.Contains(out, `"trust":"code_owned_status"`) {
		t.Fatalf("unexpected status result: %s", out)
	}
}

func TestMCPAlwaysRequiresConfirmationUnlessExactSafeRuleExists(t *testing.T) {
	tool := &mcpTool{}
	cfg := settings.DefaultSettings()
	cfg.Tools.ConfirmExec = false
	cfg.Tools.ConfirmWrites = false
	call := llm.ToolCallDef{Function: llm.FunctionCall{Name: "mcp", Arguments: json.RawMessage(`{"action":"list","server":"lan"}`)}}
	if !shouldConfirmTool(tool, &cfg, call) {
		t.Fatal("MCP call bypassed confirmation")
	}
	cfg.Tools.SafeRules = []settings.ToolSafeRule{{Tool: "mcp", InputHash: safeToolInputHash(string(call.Function.Arguments))}}
	if shouldConfirmTool(tool, &cfg, call) {
		t.Fatal("exact approved MCP call did not use its safe rule")
	}
}

func TestMCPPromptRoutesCompactBoundaryTool(t *testing.T) {
	cfg := settings.DefaultSettings().Tools
	selected := selectToolsForTurn(cfg, "use the MCP server at /mcp and list its tools", 0, 0)
	if !selected["mcp"] {
		t.Fatalf("MCP request did not route mcp tool: %#v", selected)
	}
}
