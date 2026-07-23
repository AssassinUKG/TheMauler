package tools

// ToolMetadata describes runtime behavior used by routers, diagnostics, and UI.
// It is intentionally optional so existing tools do not need to widen Tool.
type ToolMetadata struct {
	AccessClass       string
	LatencyClass      string
	OutputClass       string
	RequiresNetwork   bool
	RequiresShell     string
	Resumable         bool
	SideEffects       []string
	PreferredNext     []string
	EvidenceKind      string
	UnrestrictedReady bool
}

type MetadataProvider interface {
	Metadata() ToolMetadata
}

func MetadataFor(t Tool) ToolMetadata {
	if t == nil {
		return ToolMetadata{}
	}
	if provider, ok := t.(MetadataProvider); ok {
		return provider.Metadata()
	}
	meta, ok := defaultToolMetadata[t.Name()]
	if ok {
		return meta
	}
	access := "read"
	if t.Destructive() {
		access = "write"
	}
	return ToolMetadata{
		AccessClass:       access,
		LatencyClass:      "short",
		OutputClass:       "normal",
		UnrestrictedReady: true,
	}
}

var defaultToolMetadata = map[string]ToolMetadata{
	"read":               {AccessClass: "read", LatencyClass: "instant", OutputClass: "large", PreferredNext: []string{"grep", "glob"}, UnrestrictedReady: true},
	"write":              {AccessClass: "write", LatencyClass: "instant", OutputClass: "tiny", SideEffects: []string{"filesystem_write"}, PreferredNext: []string{"shell", "file_changes"}, UnrestrictedReady: true},
	"edit":               {AccessClass: "write", LatencyClass: "instant", OutputClass: "tiny", SideEffects: []string{"filesystem_write"}, PreferredNext: []string{"shell", "file_changes"}, UnrestrictedReady: true},
	"browser":            {AccessClass: "browser", LatencyClass: "short", OutputClass: "large", RequiresNetwork: true, Resumable: true, SideEffects: []string{"browser_interaction"}, PreferredNext: []string{"read_tool_result", "task"}, UnrestrictedReady: true},
	"sqlite":             {AccessClass: "read", LatencyClass: "short", OutputClass: "large", PreferredNext: []string{"read_tool_result"}, UnrestrictedReady: true},
	"skill":              {AccessClass: "memory", LatencyClass: "instant", OutputClass: "large", PreferredNext: []string{"read_tool_result"}, UnrestrictedReady: true},
	"todo_write":         {AccessClass: "memory", LatencyClass: "instant", OutputClass: "normal", UnrestrictedReady: true},
	"engagement":         {AccessClass: "memory", LatencyClass: "instant", OutputClass: "normal", SideEffects: []string{"engagement_state"}, PreferredNext: []string{"shell", "http_probe", "evidence_bundle"}, UnrestrictedReady: true},
	"task":               {AccessClass: "write", LatencyClass: "long", OutputClass: "normal", RequiresNetwork: true, Resumable: true, PreferredNext: []string{"read", "file_changes"}, UnrestrictedReady: true},
	"read_file":          {AccessClass: "read", LatencyClass: "instant", OutputClass: "normal", PreferredNext: []string{"grep", "file_outline", "read_chunks"}, UnrestrictedReady: true},
	"read_many":          {AccessClass: "read", LatencyClass: "short", OutputClass: "large", PreferredNext: []string{"grep", "file_outline"}, UnrestrictedReady: true},
	"file_outline":       {AccessClass: "read", LatencyClass: "instant", OutputClass: "normal", PreferredNext: []string{"read_chunks", "read_file"}, UnrestrictedReady: true},
	"read_chunks":        {AccessClass: "read", LatencyClass: "instant", OutputClass: "normal", PreferredNext: []string{"read_file"}, UnrestrictedReady: true},
	"read_pdf":           {AccessClass: "read", LatencyClass: "short", OutputClass: "large", EvidenceKind: "document", UnrestrictedReady: true},
	"glob":               {AccessClass: "read", LatencyClass: "instant", OutputClass: "normal", PreferredNext: []string{"read_file", "grep"}, UnrestrictedReady: true},
	"grep":               {AccessClass: "read", LatencyClass: "short", OutputClass: "large", PreferredNext: []string{"read_file", "read_chunks"}, UnrestrictedReady: true},
	"write_file":         {AccessClass: "write", LatencyClass: "instant", OutputClass: "tiny", SideEffects: []string{"filesystem_write"}, PreferredNext: []string{"shell", "file_changes"}, UnrestrictedReady: true},
	"edit_file":          {AccessClass: "write", LatencyClass: "instant", OutputClass: "tiny", SideEffects: []string{"filesystem_write"}, PreferredNext: []string{"shell", "file_changes"}, UnrestrictedReady: true},
	"shell":              {AccessClass: "exec", LatencyClass: "short", OutputClass: "large", RequiresShell: "any", Resumable: true, SideEffects: []string{"process", "filesystem", "network"}, PreferredNext: []string{"terminal_send", "terminal_read", "read_tool_result", "evidence_bundle"}, UnrestrictedReady: true},
	"run_script":         {AccessClass: "exec", LatencyClass: "short", OutputClass: "large", RequiresShell: "python", Resumable: true, SideEffects: []string{"process", "filesystem", "network"}, PreferredNext: []string{"file_changes", "read_tool_result", "evidence_bundle"}, UnrestrictedReady: true},
	"terminal_send":      {AccessClass: "exec", LatencyClass: "instant", OutputClass: "tiny", RequiresShell: "any", SideEffects: []string{"process"}, PreferredNext: []string{"terminal_read"}, UnrestrictedReady: true},
	"terminal_read":      {AccessClass: "exec", LatencyClass: "instant", OutputClass: "large", RequiresShell: "any", Resumable: true, PreferredNext: []string{"terminal_send", "read_tool_result"}, UnrestrictedReady: true},
	"start_listener":     {AccessClass: "exec", LatencyClass: "background", OutputClass: "normal", RequiresShell: "any", Resumable: true, SideEffects: []string{"network_listener"}, PreferredNext: []string{"terminal_read"}, UnrestrictedReady: true},
	"web_search":         {AccessClass: "network", LatencyClass: "short", OutputClass: "normal", RequiresNetwork: true, PreferredNext: []string{"fetch_url", "browser_open"}, UnrestrictedReady: true},
	"fetch_url":          {AccessClass: "network", LatencyClass: "short", OutputClass: "large", RequiresNetwork: true, PreferredNext: []string{"read_tool_result", "web_search"}, UnrestrictedReady: true},
	"browser_open":       {AccessClass: "browser", LatencyClass: "short", OutputClass: "normal", RequiresNetwork: true, PreferredNext: []string{"browser_snapshot", "browser_extract"}, UnrestrictedReady: true},
	"browser_snapshot":   {AccessClass: "browser", LatencyClass: "short", OutputClass: "large", RequiresNetwork: true, PreferredNext: []string{"browser_click", "browser_extract", "read_tool_result"}, UnrestrictedReady: true},
	"browser_click":      {AccessClass: "browser", LatencyClass: "short", OutputClass: "normal", RequiresNetwork: true, SideEffects: []string{"browser_interaction"}, PreferredNext: []string{"browser_snapshot"}, UnrestrictedReady: true},
	"browser_type":       {AccessClass: "browser", LatencyClass: "short", OutputClass: "normal", RequiresNetwork: true, SideEffects: []string{"browser_interaction"}, PreferredNext: []string{"browser_snapshot"}, UnrestrictedReady: true},
	"browser_extract":    {AccessClass: "browser", LatencyClass: "short", OutputClass: "large", RequiresNetwork: true, PreferredNext: []string{"read_tool_result"}, UnrestrictedReady: true},
	"browser_screenshot": {AccessClass: "browser", LatencyClass: "short", OutputClass: "normal", RequiresNetwork: true, EvidenceKind: "screenshot", UnrestrictedReady: true},
	"browser_close":      {AccessClass: "browser", LatencyClass: "instant", OutputClass: "tiny", RequiresNetwork: true, SideEffects: []string{"browser_interaction"}, UnrestrictedReady: true},
	"browser_agent":      {AccessClass: "browser", LatencyClass: "long", OutputClass: "large", RequiresNetwork: true, Resumable: true, SideEffects: []string{"browser_interaction"}, PreferredNext: []string{"browser_snapshot", "read_tool_result"}, UnrestrictedReady: true},
	"session_search":     {AccessClass: "memory", LatencyClass: "instant", OutputClass: "normal", PreferredNext: []string{"read", "memory"}, UnrestrictedReady: true},
	"memory":             {AccessClass: "memory", LatencyClass: "instant", OutputClass: "normal", PreferredNext: []string{"session_search"}, UnrestrictedReady: true},
	"read_tool_result":   {AccessClass: "read", LatencyClass: "instant", OutputClass: "large", PreferredNext: []string{"grep", "write"}, UnrestrictedReady: true},
	"file_changes":       {AccessClass: "evidence", LatencyClass: "instant", OutputClass: "normal", EvidenceKind: "file_change", UnrestrictedReady: true},
	"progress":           {AccessClass: "write", LatencyClass: "instant", OutputClass: "normal", SideEffects: []string{"filesystem_write"}, EvidenceKind: "progress", PreferredNext: []string{"todo_write", "write"}, UnrestrictedReady: true},
	"http_probe":         {AccessClass: "network", LatencyClass: "short", OutputClass: "artifact", RequiresNetwork: true, RequiresShell: "any", EvidenceKind: "http_probe", PreferredNext: []string{"evidence_bundle", "read_tool_result"}, UnrestrictedReady: true},
	"evidence_bundle":    {AccessClass: "evidence", LatencyClass: "short", OutputClass: "artifact", EvidenceKind: "bundle", PreferredNext: []string{"write"}, UnrestrictedReady: true},
	"skills_list":        {AccessClass: "memory", LatencyClass: "instant", OutputClass: "normal", PreferredNext: []string{"skill"}, UnrestrictedReady: true},
	"skill_view":         {AccessClass: "memory", LatencyClass: "instant", OutputClass: "large", PreferredNext: []string{"read_tool_result"}, UnrestrictedReady: true},
	"sqlite_schema":      {AccessClass: "read", LatencyClass: "instant", OutputClass: "normal", PreferredNext: []string{"sqlite_query"}, UnrestrictedReady: true},
	"sqlite_query":       {AccessClass: "read", LatencyClass: "short", OutputClass: "large", PreferredNext: []string{"read_tool_result"}, UnrestrictedReady: true},
	"todo_create":        {AccessClass: "memory", LatencyClass: "instant", OutputClass: "tiny", UnrestrictedReady: true},
	"todo_update":        {AccessClass: "memory", LatencyClass: "instant", OutputClass: "tiny", UnrestrictedReady: true},
	"todo_done":          {AccessClass: "memory", LatencyClass: "instant", OutputClass: "tiny", UnrestrictedReady: true},
	"todo_blocked":       {AccessClass: "memory", LatencyClass: "instant", OutputClass: "tiny", UnrestrictedReady: true},
	"todo_list":          {AccessClass: "memory", LatencyClass: "instant", OutputClass: "normal", UnrestrictedReady: true},
	"todo_clear":         {AccessClass: "memory", LatencyClass: "instant", OutputClass: "tiny", UnrestrictedReady: true},
	"subagent_research":  {AccessClass: "network", LatencyClass: "long", OutputClass: "normal", RequiresNetwork: true, Resumable: true, PreferredNext: []string{"fetch_url", "write_file"}, UnrestrictedReady: true},
	"subagent_explore":   {AccessClass: "read", LatencyClass: "long", OutputClass: "normal", Resumable: true, PreferredNext: []string{"read_file", "grep"}, UnrestrictedReady: true},
	"subagent_review":    {AccessClass: "read", LatencyClass: "long", OutputClass: "normal", Resumable: true, PreferredNext: []string{"edit_file", "write_file"}, UnrestrictedReady: true},
	"subagent_testfix":   {AccessClass: "write", LatencyClass: "long", OutputClass: "normal", Resumable: true, SideEffects: []string{"filesystem_write", "process"}, PreferredNext: []string{"shell", "file_changes"}, UnrestrictedReady: true},
	"subagent_summarize": {AccessClass: "read", LatencyClass: "short", OutputClass: "normal", UnrestrictedReady: true},
}
