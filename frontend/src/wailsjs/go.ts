// Type-safe wrappers around the Wails-generated Go bindings.
// The actual `window.go.app.App.*` functions are injected at runtime by Wails.

type GoBindings = Record<string, Record<string, Record<string, (...args: unknown[]) => Promise<unknown>>>>

function call<T>(method: string, ...args: unknown[]): Promise<T> {
  const [pkg, cls, fn] = method.split('.')
  const go = window.go as GoBindings | undefined
  if (go?.[pkg]?.[cls]?.[fn]) {
    return go[pkg][cls][fn](...args) as Promise<T>
  }
  return Promise.reject(new Error(`Wails binding not found: ${method}`))
}

export interface Settings {
  active_profile: string
  tools: {
    enabled: boolean
    confirm_reads: boolean
    confirm_writes: boolean
    confirm_exec: boolean
    bash_timeout: number
    shell_backend: string
    artifact_timeout: number
    web_engine: string
    web_base_url: string
    web_api_key_env: string
    brave_api_key: string
    max_searches: number
    max_fetches: number
    max_failed_fetches: number
    max_browser_actions: number
    max_tool_result_chars: number
    active_toolset: string
    toolsets: Record<string, string[]>
    enabled_tools: Record<string, boolean>
    safe_rules: ToolSafeRule[]
  }
  agents: {
    mode_override: string
    default_autonomy: string
    offline_only: boolean
    max_tool_calls: number
    require_plan: boolean
    no_think_after_tool_calls: number
    presets: Record<string, AgentModePreset>
  }
  context: {
    auto_inject_file: boolean
    auto_inject_cursor: boolean
    compaction_at: number
    show_compaction: boolean
    mauler_md_path: string
    project_doc_max_bytes: number
    project_doc_fallback_filenames: string[]
    workspace_dir: string
  }
  memory: {
    enabled: boolean
    auto_inject: boolean
    max_entries: number
    max_inject: number
    max_entry_chars: number
  }
  ui: {
    theme: string
    accent_color: string
    primary_color: string
    status_bar: boolean
    token_counter: boolean
    think_indicator: boolean
    syntax_highlight: boolean
    diff_colours: boolean
    chat_timestamps: boolean
    tree_width: number
    chat_width: number
    artifact_width: number
  }
  skills: {
    enabled: boolean
    auto_inject: boolean
    max_inject: number
    skills_dir: string
  }
  image: {
    vision_enabled: boolean
    clipboard_method: string
    display_method: string
    max_display_width: number
    wsl_path_translate: boolean
  }
  logging: {
    enabled: boolean
    log_tool_inputs: boolean
    log_tool_results: boolean
    log_responses: boolean
    max_runs: number
  }
  log_level: string
}

export interface AgentModePreset {
  enabled: boolean
  profile: string
  context_budget: number
  autonomy: string
  toolset: string
  instructions: string
  tool_permissions: Record<string, boolean>
}

export interface ToolSafeRule {
  id: string
  tool: string
  input_hash: string
  label: string
  created_at: string
}

export interface GenerationParams {
  temperature: number
  top_p: number
  top_k: number
  min_p: number
  presence_penalty: number
  max_tokens: number
  seed: number
}

export interface Profile {
  name: string
  provider: string
  model_id: string
  ctx_tokens: number
  thinking: boolean
  preserve_thinking: boolean
  mmproj: string
  thinking_general: GenerationParams
  thinking_coding: GenerationParams
  nothinking: GenerationParams
  spec_type: string        // "" | "draft-mtp"
  spec_draft_n_max: number // draft tokens per step, 0 = server default
}

export interface Provider {
  name: string
  backend: string
  base_url: string
  api_key_env: string
}

export interface ProfilesFile {
  providers: Record<string, Provider>
  profiles: Record<string, Profile>
}

export interface FileNode {
  name: string
  path: string
  isDir: boolean
  children?: FileNode[]
}

export interface HistoryStats {
  token_count: number
  budget: number
  fraction: number
  rollback_len: number
}

export interface SessionChatMessage {
  role: ChatRole
  content: string
  images?: string[]
}

export interface ChatAttachment {
  id?: string
  name: string
  kind: string
  mime?: string
  content?: string
  path?: string
  size?: number
  truncated?: boolean
}

export interface MemoryEntry {
  id: string
  scope: string
  title: string
  content: string
  tags: string[]
  kind: string
  importance: number
  pinned: boolean
  created_at: string
  updated_at: string
  last_used_at: string
}

export interface SessionSearchResult {
  session_id: string
  session_name: string
  message_id: number
  role: string
  content: string
  tool_name?: string
  rank?: string
  updated_at?: string
}

export interface Skill {
  name: string
  description: string
  version: string
  tags: string[]
  body: string
  raw: string
  created_at: string
  updated_at: string
}

export interface SkillSuggestion {
  type: string    // "skill" | "memory"
  title: string
  reason: string
  template: string
}

export interface TodoItem {
  id: string
  text: string
  status: string
  detail?: string
  created_at: string
  updated_at: string
}

export interface TaskToolEvent {
  name: string
  input?: string
  result?: string
  status: string
  timestamp: string
  duration_ms?: number
}

export interface TaskRunEvent {
  kind: string
  message: string
  timestamp: string
  detail?: string
}

export interface TaskRun {
  id: string
  prompt: string
  mode: string
  profile: string
  model?: string
  status: string
  state?: string
  stop_reason?: string
  stop_detail?: string
  started_at: string
  ended_at?: string
  duration_ms?: number
  prompt_tokens?: number
  completion_tokens?: number
  total_tokens?: number
  summary?: string
  response?: string
  tools?: TaskToolEvent[]
  events?: TaskRunEvent[]
}

export type ChatRole = 'user' | 'assistant' | 'tool_call' | 'tool_result' | 'system'

// --- Bindings ---

export const GetSettings = (): Promise<Settings> =>
  call('app.App.GetSettings')

export const UpdateSettings = (cfg: Settings): Promise<void> =>
  call('app.App.UpdateSettings', cfg)

export const GetProfiles = (): Promise<ProfilesFile> =>
  call('app.App.GetProfiles')

export const UpdateProfiles = (pf: ProfilesFile): Promise<void> =>
  call('app.App.UpdateProfiles', pf)

export const GetProfileNames = (): Promise<string[]> =>
  call('app.App.GetProfileNames')

export const SwitchProfile = (name: string): Promise<void> =>
  call('app.App.SwitchProfile', name)

export const SetAutonomous = (enabled: boolean): Promise<void> =>
  call('app.App.SetAutonomous', enabled)

export const GetAutonomous = (): Promise<boolean> =>
  call('app.App.GetAutonomous')

export const GetAgentMode = (): Promise<string> =>
  call('app.App.GetAgentMode')

export const SetAutoAgents = (enabled: boolean): Promise<void> =>
  call('app.App.SetAutoAgents', enabled)

export const GetAutoAgents = (): Promise<boolean> =>
  call('app.App.GetAutoAgents')

export const SetAgentModeOverride = (mode: string): Promise<void> =>
  call('app.App.SetAgentModeOverride', mode)

export const ApplySafetyPreset = (name: string): Promise<void> =>
  call('app.App.ApplySafetyPreset', name)

export const AddToolSafeRule = (toolName: string, input: string): Promise<void> =>
  call('app.App.AddToolSafeRule', toolName, input)

export const UseProfile = (name: string, cfg: Settings, pf: ProfilesFile): Promise<void> =>
  call('app.App.UseProfile', name, cfg, pf)

export const GetHistoryStats = (): Promise<HistoryStats> =>
  call('app.App.GetHistoryStats')

export const ClearHistory = (): Promise<void> =>
  call('app.App.ClearHistory')

export const SaveSession = (name: string): Promise<void> =>
  call('app.App.SaveSession', name)

export const LoadSession = (name: string): Promise<SessionChatMessage[]> =>
  call('app.App.LoadSession', name)

export const ListSessions = (): Promise<string[]> =>
  call('app.App.ListSessions')

export const DeleteSession = (name: string): Promise<void> =>
  call('app.App.DeleteSession', name)

export const ListMemory = (): Promise<MemoryEntry[]> =>
  call('app.App.ListMemory')

export const SaveMemoryEntry = (entry: MemoryEntry): Promise<MemoryEntry> =>
  call('app.App.SaveMemoryEntry', entry)

export const DeleteMemoryEntry = (id: string): Promise<void> =>
  call('app.App.DeleteMemoryEntry', id)

export const ClearMemoryEntries = (): Promise<void> =>
  call('app.App.ClearMemoryEntries')

export const AddMemory = (title: string, content: string, tags: string[]): Promise<MemoryEntry> =>
  call('app.App.AddMemory', title, content, tags)

export const SearchSessionRecall = (query: string, limit: number): Promise<SessionSearchResult[]> =>
  call('app.App.SearchSessionRecall', query, limit)

export const ReindexSessionRecall = (): Promise<number> =>
  call('app.App.ReindexSessionRecall')

export const ClearSessionRecall = (): Promise<void> =>
  call('app.App.ClearSessionRecall')

export const ListTodos = (): Promise<TodoItem[]> =>
  call('app.App.ListTodos')

export const ClearTodos = (): Promise<void> =>
  call('app.App.ClearTodos')

export const ListTaskRuns = (): Promise<TaskRun[]> =>
  call('app.App.ListTaskRuns')

export const ClearTaskRuns = (): Promise<void> =>
  call('app.App.ClearTaskRuns')

export const SendMessage = (text: string, images: string[], attachments: ChatAttachment[] = []): Promise<void> =>
  call('app.App.SendMessage', text, images, attachments)

export const StopAgent = (): Promise<void> =>
  call('app.App.StopAgent')

export const RespondConfirm = (allow: boolean): Promise<void> =>
  call('app.App.RespondConfirm', allow)

export const Undo = (): Promise<string> =>
  call('app.App.Undo')

export const RollbackDepth = (): Promise<number> =>
  call('app.App.RollbackDepth')

export const GetFileTree = (dir: string): Promise<FileNode[]> =>
  call('app.App.GetFileTree', dir)

export const ReadFileContent = (path: string): Promise<string> =>
  call('app.App.ReadFileContent', path)

export const SaveFileContent = (path: string, content: string): Promise<void> =>
  call('app.App.SaveFileContent', path, content)

export const GetWorkingDir = (): Promise<string> =>
  call('app.App.GetWorkingDir')

export const SetWorkingDir = (dir: string): Promise<void> =>
  call('app.App.SetWorkingDir', dir)

export const SelectWorkingDir = (defaultDir: string): Promise<string> =>
  call('app.App.SelectWorkingDir', defaultDir)

export const PickSaveFilePath = (defaultName: string): Promise<string> =>
  call<string>('app.App.PickSaveFilePath', defaultName)

export const GetHomeDir = (): Promise<string> =>
  call('app.App.GetHomeDir')

export const EncodeFileBase64 = (path: string): Promise<string> =>
  call('app.App.EncodeFileBase64', path)

export const RenameFile = (oldPath: string, newPath: string): Promise<void> =>
  call('app.App.RenameFile', oldPath, newPath)

export const DeleteFile = (path: string): Promise<void> =>
  call('app.App.DeleteFile', path)

export const CreateFile = (path: string): Promise<void> =>
  call('app.App.CreateFile', path)

export const CreateDir = (path: string): Promise<void> =>
  call('app.App.CreateDir', path)

export const RunArtifact = (lang: string, code: string): Promise<void> =>
  call('app.App.RunArtifact', lang, code)

export const StopArtifact = (): Promise<void> =>
  call('app.App.StopArtifact')

export const Ping = (): Promise<string> =>
  call('app.App.Ping')

export const ListModels = (): Promise<string[]> =>
  call('app.App.ListModels')

export const PingProvider = (provider: Provider): Promise<string> =>
  call('app.App.PingProvider', provider)

export const ListModelsForProvider = (provider: Provider): Promise<string[]> =>
  call('app.App.ListModelsForProvider', provider)

export interface DoctorCheck {
  name: string
  status: string  // ok | warn | fail | info
  message: string
  detail?: string
}

export interface DoctorResult {
  checks: DoctorCheck[]
  score: number
  grade: string  // OK | WARN | FAIL
}

export const RunDoctor = (): Promise<DoctorResult> =>
  call('app.App.RunDoctor')

// User profile bindings
export const GetUserProfile = (): Promise<string> =>
  call('app.App.GetUserProfile')

export const SaveUserProfile = (content: string): Promise<void> =>
  call('app.App.SaveUserProfile', content)

// Skill bindings
export const ListSkills = (): Promise<Skill[]> =>
  call('app.App.ListSkills')

export const GetSkill = (name: string): Promise<Skill> =>
  call('app.App.GetSkill', name)

export const SaveSkill = (skill: Skill): Promise<Skill> =>
  call('app.App.SaveSkill', skill)

export const DeleteSkill = (name: string): Promise<void> =>
  call('app.App.DeleteSkill', name)

// Terminal shell bindings
export const OpenShell = (): Promise<string> =>
  call('app.App.OpenShell')

export const ShellInput = (id: string, text: string): Promise<void> =>
  call('app.App.ShellInput', id, text)

export const ShellClose = (id: string): Promise<void> =>
  call('app.App.ShellClose', id)
