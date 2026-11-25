// Package mcpclient implements TheMauler's bounded external MCP client
// boundary. It supports stdio, legacy HTTP+SSE, and current Streamable HTTP
// without turning server-controlled tool descriptions into native Mauler tool
// definitions.
package mcpclient

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"mauler/internal/settings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	maxEventBytes       = 1024 * 1024
	maxListedTools      = 100
	maxListPages        = 16
	maxDescriptionRunes = 500
	maxSchemaBytes      = 12 * 1024
	maxTextRunes        = 64 * 1024
	maxStructuredBytes  = 64 * 1024
)

// ServerStatus is a secret-free runtime snapshot safe to return to React.
type ServerStatus struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Transport       string `json:"transport"`
	Configured      bool   `json:"configured"`
	Connected       bool   `json:"connected"`
	State           string `json:"state"`
	ToolCount       int    `json:"tool_count"`
	ProtocolVersion string `json:"protocol_version,omitempty"`
	ServerName      string `json:"server_name,omitempty"`
	ServerVersion   string `json:"server_version,omitempty"`
	LastError       string `json:"last_error,omitempty"`
	UpdatedAt       string `json:"updated_at,omitempty"`
}

// ToolSummary is a bounded, explicitly untrusted view of one remote tool.
type ToolSummary struct {
	Name        string          `json:"name"`
	Title       string          `json:"title,omitempty"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema,omitempty"`
	SchemaNote  string          `json:"schema_note,omitempty"`
}

// ListResult is returned through the mcp boundary tool.
type ListResult struct {
	Trust     string        `json:"trust"`
	Server    string        `json:"server"`
	Transport string        `json:"transport"`
	Tools     []ToolSummary `json:"tools"`
	Truncated bool          `json:"truncated"`
}

// CallResult is a bounded representation of a remote result. Binary content
// is represented by metadata and a digest rather than base64 entering prompt
// context.
type CallResult struct {
	Trust             string         `json:"trust"`
	Server            string         `json:"server"`
	Tool              string         `json:"tool"`
	IsError           bool           `json:"is_error"`
	NeedsInput        bool           `json:"needs_input,omitempty"`
	InputRequestCount int            `json:"input_request_count,omitempty"`
	Content           []ContentBlock `json:"content"`
	StructuredContent any            `json:"structured_content,omitempty"`
	Truncated         bool           `json:"truncated,omitempty"`
}

type ContentBlock struct {
	Type        string `json:"type"`
	Text        string `json:"text,omitempty"`
	MIMEType    string `json:"mime_type,omitempty"`
	URI         string `json:"uri,omitempty"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	Bytes       int    `json:"bytes,omitempty"`
	SHA256      string `json:"sha256,omitempty"`
}

type Manager struct {
	mu      sync.Mutex
	config  settings.MCPConfig
	servers map[string]*managedServer
	closed  bool
}

type managedServer struct {
	mu      sync.Mutex
	config  settings.MCPServerConfig
	session *mcp.ClientSession
	cancel  context.CancelFunc
	status  ServerStatus
}

// New returns a disconnected manager. It performs no process or network IO.
func New(config settings.MCPConfig) *Manager {
	manager := &Manager{}
	manager.configure(config)
	return manager
}

// Configure replaces the server set after closing existing sessions. The
// caller must ensure no agent run is using MCP while reconfiguring.
func (m *Manager) Configure(config settings.MCPConfig) error {
	validated, err := settings.ValidateMCPConfig(config)
	if err != nil {
		return err
	}
	// A stale child may report a non-zero exit while being torn down. That must
	// not leave the freshly validated configuration unusable.
	_ = m.Close()
	m.mu.Lock()
	m.closed = false
	m.configureLocked(validated)
	m.mu.Unlock()
	return nil
}

func (m *Manager) configure(config settings.MCPConfig) {
	validated, err := settings.ValidateMCPConfig(config)
	if err != nil {
		config = settings.MCPConfig{}
	} else {
		config = validated
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.configureLocked(config)
}

func (m *Manager) configureLocked(config settings.MCPConfig) {
	m.config = config
	m.servers = make(map[string]*managedServer, len(config.Servers))
	for _, server := range config.Servers {
		state := "disabled"
		if config.Enabled && server.Enabled {
			state = "disconnected"
		}
		m.servers[server.ID] = &managedServer{
			config: server,
			status: ServerStatus{
				ID: server.ID, Name: server.Name, Transport: server.Transport,
				Configured: server.Enabled, State: state,
			},
		}
	}
}

func (m *Manager) Enabled() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || !m.config.Enabled {
		return false
	}
	for _, server := range m.servers {
		if server.config.Enabled {
			return true
		}
	}
	return false
}

func (m *Manager) Statuses() []ServerStatus {
	m.mu.Lock()
	servers := make([]*managedServer, 0, len(m.servers))
	for _, server := range m.servers {
		servers = append(servers, server)
	}
	m.mu.Unlock()
	out := make([]ServerStatus, 0, len(servers))
	for _, server := range servers {
		server.mu.Lock()
		out = append(out, server.status)
		server.mu.Unlock()
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (m *Manager) Test(ctx context.Context, serverID string) (ServerStatus, error) {
	server, err := m.server(serverID)
	if err != nil {
		return ServerStatus{}, err
	}
	server.mu.Lock()
	defer server.mu.Unlock()
	if err := server.connectLocked(ctx); err != nil {
		return server.status, err
	}
	if _, _, err := server.listToolsLocked(ctx); err != nil {
		server.failLocked(err)
		return server.status, err
	}
	return server.status, nil
}

func (m *Manager) ListTools(ctx context.Context, serverID string) (ListResult, error) {
	server, err := m.server(serverID)
	if err != nil {
		return ListResult{}, err
	}
	server.mu.Lock()
	defer server.mu.Unlock()
	if err := server.connectLocked(ctx); err != nil {
		return ListResult{}, err
	}
	tools, truncated, err := server.listToolsLocked(ctx)
	if err != nil {
		server.failLocked(err)
		return ListResult{}, err
	}
	return ListResult{
		Trust: "untrusted_mcp_server", Server: server.config.ID,
		Transport: server.config.Transport, Tools: tools, Truncated: truncated,
	}, nil
}

func (m *Manager) CallTool(ctx context.Context, serverID, toolName string, arguments map[string]any) (CallResult, error) {
	server, err := m.server(serverID)
	if err != nil {
		return CallResult{}, err
	}
	toolName = strings.TrimSpace(toolName)
	if toolName == "" || len(toolName) > 256 || strings.ContainsAny(toolName, "\r\n\x00") {
		return CallResult{}, fmt.Errorf("mcp tool name is required and must be at most 256 characters")
	}
	server.mu.Lock()
	defer server.mu.Unlock()
	if err := server.connectLocked(ctx); err != nil {
		return CallResult{}, err
	}
	callCtx, cancel := context.WithTimeout(ctx, time.Duration(server.config.TimeoutSeconds)*time.Second)
	defer cancel()
	result, err := server.session.CallTool(callCtx, &mcp.CallToolParams{Name: toolName, Arguments: arguments})
	if err != nil {
		server.failLocked(err)
		return CallResult{}, fmt.Errorf("mcp call %s/%s: %w", server.config.ID, toolName, err)
	}
	server.touchLocked()
	return compactCallResult(server.config.ID, toolName, result), nil
}

func (m *Manager) Disconnect(serverID string) error {
	server, err := m.server(serverID)
	if err != nil {
		return err
	}
	server.mu.Lock()
	defer server.mu.Unlock()
	return server.closeLocked("disconnected")
}

func (m *Manager) Close() error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}
	m.closed = true
	servers := make([]*managedServer, 0, len(m.servers))
	for _, server := range m.servers {
		servers = append(servers, server)
	}
	m.mu.Unlock()
	var errs []string
	for _, server := range servers {
		server.mu.Lock()
		if err := server.closeLocked("closed"); err != nil {
			errs = append(errs, err.Error())
		}
		server.mu.Unlock()
	}
	if len(errs) > 0 {
		return fmt.Errorf("close mcp sessions: %s", strings.Join(errs, "; "))
	}
	return nil
}

func (m *Manager) server(serverID string) (*managedServer, error) {
	serverID = strings.ToLower(strings.TrimSpace(serverID))
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil, fmt.Errorf("mcp manager is closed")
	}
	if !m.config.Enabled {
		return nil, fmt.Errorf("mcp is disabled in Settings")
	}
	server, ok := m.servers[serverID]
	if !ok {
		return nil, fmt.Errorf("unknown mcp server %q", serverID)
	}
	if !server.config.Enabled {
		return nil, fmt.Errorf("mcp server %q is disabled", serverID)
	}
	return server, nil
}

func (s *managedServer) connectLocked(ctx context.Context) error {
	if s.session != nil {
		return nil
	}
	s.status.State = "connecting"
	s.status.LastError = ""
	s.touchLocked()
	transport, err := transportFor(s.config)
	if err != nil {
		s.failLocked(err)
		return err
	}
	client := mcp.NewClient(
		&mcp.Implementation{Name: "TheMauler", Title: "TheMauler", Version: "1"},
		&mcp.ClientOptions{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))},
	)
	sessionCtx, sessionCancel := context.WithCancel(context.Background())
	type connectResult struct {
		session *mcp.ClientSession
		err     error
	}
	resultCh := make(chan connectResult, 1)
	go func() {
		session, connectErr := client.Connect(sessionCtx, transport, nil)
		resultCh <- connectResult{session: session, err: connectErr}
	}()
	timer := time.NewTimer(time.Duration(s.config.TimeoutSeconds) * time.Second)
	defer timer.Stop()
	select {
	case result := <-resultCh:
		if result.err != nil {
			sessionCancel()
			s.failLocked(result.err)
			return fmt.Errorf("connect mcp server %q: %w", s.config.ID, result.err)
		}
		s.session = result.session
		s.cancel = sessionCancel
		s.status.Connected = true
		s.status.State = "connected"
		if initialized := result.session.InitializeResult(); initialized != nil {
			s.status.ProtocolVersion = initialized.ProtocolVersion
			if initialized.ServerInfo != nil {
				s.status.ServerName = cleanOneLine(initialized.ServerInfo.Name, 100)
				s.status.ServerVersion = cleanOneLine(initialized.ServerInfo.Version, 100)
			}
		}
		s.touchLocked()
		return nil
	case <-ctx.Done():
		sessionCancel()
		s.failLocked(ctx.Err())
		return ctx.Err()
	case <-timer.C:
		sessionCancel()
		err := fmt.Errorf("connection timed out after %d seconds", s.config.TimeoutSeconds)
		s.failLocked(err)
		return err
	}
}

func (s *managedServer) listToolsLocked(ctx context.Context) ([]ToolSummary, bool, error) {
	listCtx, cancel := context.WithTimeout(ctx, time.Duration(s.config.TimeoutSeconds)*time.Second)
	defer cancel()
	var out []ToolSummary
	cursor := ""
	truncated := false
	for page := 0; page < maxListPages; page++ {
		result, err := s.session.ListTools(listCtx, &mcp.ListToolsParams{Cursor: cursor})
		if err != nil {
			return nil, false, fmt.Errorf("list mcp tools from %q: %w", s.config.ID, err)
		}
		for _, remote := range result.Tools {
			if remote == nil || len(out) >= maxListedTools {
				truncated = true
				break
			}
			out = append(out, compactTool(remote))
		}
		if truncated || result.NextCursor == "" {
			break
		}
		cursor = result.NextCursor
		if page == maxListPages-1 {
			truncated = true
		}
	}
	s.status.ToolCount = len(out)
	s.touchLocked()
	return out, truncated, nil
}

func (s *managedServer) closeLocked(state string) error {
	var err error
	if s.session != nil {
		err = s.session.Close()
		s.session = nil
	}
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	s.status.Connected = false
	s.status.State = state
	s.touchLocked()
	return err
}

func (s *managedServer) failLocked(err error) {
	if s.session != nil {
		_ = s.session.Close()
		s.session = nil
	}
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	s.status.Connected = false
	s.status.State = "error"
	s.status.LastError = cleanOneLine(err.Error(), 500)
	s.touchLocked()
}

func (s *managedServer) touchLocked() {
	s.status.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
}

func transportFor(config settings.MCPServerConfig) (mcp.Transport, error) {
	switch config.Transport {
	case settings.MCPTransportStdio:
		cmd := exec.Command(config.Command, config.Args...)
		cmd.Dir = config.WorkingDir
		cmd.Env = childEnvironment(config.EnvAllowlist)
		cmd.Stderr = io.Discard
		hideChildWindow(cmd)
		return &mcp.CommandTransport{Command: cmd, TerminateDuration: 2 * time.Second}, nil
	case settings.MCPTransportSSE:
		client, err := mcpHTTPClient(config)
		if err != nil {
			return nil, err
		}
		return &mcp.SSEClientTransport{Endpoint: config.URL, HTTPClient: client, MaxEventSize: maxEventBytes}, nil
	case settings.MCPTransportStreamableHTTP:
		client, err := mcpHTTPClient(config)
		if err != nil {
			return nil, err
		}
		return &mcp.StreamableClientTransport{
			Endpoint: config.URL, HTTPClient: client, MaxRetries: 2,
			DisableStandaloneSSE: false, MaxEventSize: maxEventBytes,
		}, nil
	default:
		return nil, fmt.Errorf("unsupported mcp transport %q", config.Transport)
	}
}

type originRoundTripper struct {
	base         http.RoundTripper
	origin       string
	token        string
	requestHost  string
	originHeader string
}

func (r *originRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	if originFor(request.URL) != r.origin {
		return nil, fmt.Errorf("mcp request blocked: endpoint changed origin")
	}
	clone := request.Clone(request.Context())
	clone.Header = request.Header.Clone()
	if r.token != "" {
		clone.Header.Set("Authorization", "Bearer "+r.token)
	}
	if r.requestHost != "" {
		clone.Host = r.requestHost
	}
	if r.originHeader != "" {
		clone.Header.Set("Origin", r.originHeader)
	}
	return r.base.RoundTrip(clone)
}

func mcpHTTPClient(config settings.MCPServerConfig) (*http.Client, error) {
	endpoint, err := url.Parse(config.URL)
	if err != nil {
		return nil, err
	}
	token := ""
	if config.BearerTokenEnv != "" {
		var ok bool
		token, ok = os.LookupEnv(config.BearerTokenEnv)
		if !ok || strings.TrimSpace(token) == "" {
			return nil, fmt.Errorf("mcp bearer token environment variable %s is not set", config.BearerTokenEnv)
		}
		if strings.ContainsAny(token, "\r\n\x00") {
			return nil, fmt.Errorf("mcp bearer token environment variable %s contains invalid control characters", config.BearerTokenEnv)
		}
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	origin := originFor(endpoint)
	client := &http.Client{
		Transport: &originRoundTripper{
			base: transport, origin: origin, token: token,
			requestHost: config.HTTPHostHeader, originHeader: config.HTTPOrigin,
		},
		Timeout: 0,
	}
	client.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if len(via) >= 3 || originFor(request.URL) != origin {
			return fmt.Errorf("mcp redirect blocked: endpoint changed origin or exceeded redirect limit")
		}
		return nil
	}
	return client, nil
}

func originFor(value *url.URL) string {
	return strings.ToLower(value.Scheme + "://" + value.Host)
}

func childEnvironment(allowlist []string) []string {
	baseNames := []string{"PATH", "PATHEXT", "SystemRoot", "WINDIR", "COMSPEC", "TEMP", "TMP"}
	if runtime.GOOS != "windows" {
		baseNames = []string{"PATH", "HOME", "TMPDIR", "LANG"}
	}
	baseNames = append(baseNames, allowlist...)
	seen := make(map[string]struct{}, len(baseNames))
	out := make([]string, 0, len(baseNames))
	for _, name := range baseNames {
		key := strings.ToUpper(strings.TrimSpace(name))
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		if value, ok := os.LookupEnv(name); ok {
			out = append(out, name+"="+value)
		}
	}
	return out
}

func compactTool(tool *mcp.Tool) ToolSummary {
	summary := ToolSummary{
		Name: cleanOneLine(tool.Name, 256), Title: cleanOneLine(tool.Title, 160),
		Description: cleanText(tool.Description, maxDescriptionRunes),
	}
	if schema, err := json.Marshal(tool.InputSchema); err == nil {
		if len(schema) <= maxSchemaBytes {
			summary.InputSchema = schema
		} else {
			summary.SchemaNote = fmt.Sprintf("input schema omitted: %d bytes exceeds %d-byte limit", len(schema), maxSchemaBytes)
		}
	} else {
		summary.SchemaNote = "input schema could not be encoded"
	}
	return summary
}

func compactCallResult(serverID, toolName string, result *mcp.CallToolResult) CallResult {
	out := CallResult{
		Trust: "untrusted_mcp_server", Server: serverID, Tool: toolName,
		IsError: result.IsError, NeedsInput: len(result.InputRequests) > 0,
		InputRequestCount: len(result.InputRequests), Content: make([]ContentBlock, 0, len(result.Content)),
	}
	remaining := maxTextRunes
	for _, content := range result.Content {
		block, used, truncated := compactContent(content, remaining)
		remaining -= used
		out.Truncated = out.Truncated || truncated
		out.Content = append(out.Content, block)
	}
	if result.StructuredContent != nil {
		encoded, err := json.Marshal(result.StructuredContent)
		if err == nil && len(encoded) <= maxStructuredBytes {
			var value any
			if json.Unmarshal(encoded, &value) == nil {
				out.StructuredContent = value
			}
		} else {
			out.Truncated = true
			out.Content = append(out.Content, ContentBlock{Type: "structured_content_omitted", Text: "structured content exceeded the bounded result limit"})
		}
	}
	return out
}

func compactContent(content mcp.Content, remaining int) (ContentBlock, int, bool) {
	switch value := content.(type) {
	case *mcp.TextContent:
		text, used, truncated := truncateRunes(value.Text, remaining)
		return ContentBlock{Type: "text", Text: text}, used, truncated
	case *mcp.ImageContent:
		return binaryContent("image", value.MIMEType, value.Data), 0, false
	case *mcp.AudioContent:
		return binaryContent("audio", value.MIMEType, value.Data), 0, false
	case *mcp.ResourceLink:
		return ContentBlock{Type: "resource_link", URI: cleanText(value.URI, 2048), Name: cleanOneLine(value.Name, 256), Description: cleanText(value.Description, 500), MIMEType: cleanOneLine(value.MIMEType, 128)}, 0, false
	case *mcp.EmbeddedResource:
		if value.Resource == nil {
			return ContentBlock{Type: "resource", Text: "empty embedded resource"}, 0, false
		}
		if len(value.Resource.Blob) > 0 {
			block := binaryContent("resource", value.Resource.MIMEType, value.Resource.Blob)
			block.URI = cleanText(value.Resource.URI, 2048)
			return block, 0, false
		}
		text, used, truncated := truncateRunes(value.Resource.Text, remaining)
		return ContentBlock{Type: "resource", URI: cleanText(value.Resource.URI, 2048), MIMEType: cleanOneLine(value.Resource.MIMEType, 128), Text: text}, used, truncated
	default:
		return ContentBlock{Type: "unsupported", Text: "unsupported MCP content type omitted"}, 0, false
	}
}

func binaryContent(kind, mime string, data []byte) ContentBlock {
	digest := sha256.Sum256(data)
	return ContentBlock{Type: kind, MIMEType: cleanOneLine(mime, 128), Bytes: len(data), SHA256: hex.EncodeToString(digest[:])}
}

func truncateRunes(value string, limit int) (string, int, bool) {
	if limit <= 0 {
		return "", 0, value != ""
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value, len(runes), false
	}
	return string(runes[:limit]) + "... [truncated]", limit, true
}

func cleanText(value string, limit int) string {
	value = strings.ReplaceAll(value, "\x00", "")
	cleaned, _, _ := truncateRunes(value, limit)
	return cleaned
}

func cleanOneLine(value string, limit int) string {
	return cleanText(strings.Join(strings.Fields(value), " "), limit)
}
