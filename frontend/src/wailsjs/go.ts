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
    shell_mode: string
    shell_distro: string
    shell_user: string
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
    tool_result_preview_chars: number
    tool_result_aggregate_chars: number
    redact_secrets: boolean
    protected_paths: string[]
    active_toolset: string
    task_routing_mode: string
    toolsets: Record<string, string[]>
    enabled_tools: Record<string, boolean>
    safe_rules: ToolSafeRule[]
    tool_grammar_constraint: boolean
  }
  agents: {
    mode_override: string
    default_conversation_mode: 'adaptive' | 'direct' | 'agent' | string
    default_autonomy: string
    offline_only: boolean
    max_tool_calls: number
    max_run_seconds: number
    escalation_profile: string
    require_plan: boolean
    no_think_after_tool_calls: number
    reasoning_effort: string
    thinking_mode: string
    review_loop: ReviewLoopConfig
    presets: Record<string, AgentModePreset>
  }
  environment: EnvironmentConfig
  context: {
    auto_inject_file: boolean
    auto_inject_cursor: boolean
    compaction_at: number
    show_compaction: boolean
    mauler_md_path: string
    project_doc_max_bytes: number
    project_doc_fallback_filenames: string[]
    workspace_dir: string
    open_folders: WorkspaceFolder[]
    workspace_preferences: WorkspacePreference[]
    repository_index_source_sets: RepositoryIndexSourceSet[]
    scratch_workspace_dir: string
    scratch_workspace_name: string
    scratch_workspace_created_unix: number
    scratch_workspace_review_unix: number
    lab: LabContext
    active_lab_profile: string
    lab_profiles: LabProfile[]
  }
  memory: {
    enabled: boolean
    auto_inject: boolean
    disable_auto_distill: boolean
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
    tool_countdown: boolean
    terminal_default_open: boolean
    terminal_height: number
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
    video_enabled: boolean
    video_max_frames: number
    video_frame_width: number
    video_transcribe: boolean
  }
  telegram: TelegramConfig
  audio: AudioConfig
  logging: {
    enabled: boolean
    log_tool_inputs: boolean
    log_tool_results: boolean
    log_responses: boolean
    max_runs: number
  }
  log_level: string
}

export interface AudioConfig {
  enabled: boolean
  mode: string
  stt_engine: string
  tts_engine: string
  voice: string
  speed: number
  input_device: string
  vad_threshold: number
  barge_in: boolean
  speak_replies: boolean
  speak_tool_notes: boolean
  clause_min_chars: number
}

export interface SpeechAudio {
  data_uri: string
  engine: string
  voice: string
}

export interface AudioHealth {
  enabled: boolean
  overall: string
  configured_tts: string
  actual_tts: string
  voice: string
  stt_engine: string
  stt_ready: boolean
  worker_state: string
  worker_pid: number
  last_success: string
  last_error: string
  speak_replies: boolean
  worker_hidden: boolean
  stt_worker_state: string
  stt_worker_pid: number
  stt_model: string
  stt_last_duration_ms: number
  stt_last_audio_ms: number
  stt_last_success: string
  stt_last_error: string
}

export interface ServiceHealth {
  id: string
  name: string
  status: string
  summary: string
  detail?: string
  updated_at: string
  metadata?: Record<string, string>
}

export interface StaleRunEventRejection {
  event: string
  run_id?: string
  generation?: number
  run_epoch?: number
  conversation_epoch: number
  rejected_at: string
}

export interface RunEventDiagnostics {
  conversation_epoch: number
  stale_events_dropped: number
  last_rejection: StaleRunEventRejection
}

export interface RepositoryIndexOmission {
  path: string
  status: string
  detail?: string
  size?: number
}

export interface RepositoryIndexStatus {
  available: boolean
  active: boolean
  workspace: string
  generation_id?: string
  manifest_digest?: string
  policy_digest?: string
  status: string
  complete: boolean
  files_seen: number
  files_indexed: number
  bytes_read: number
  chunk_count: number
  omission_count: number
  omissions_truncated: boolean
  omissions: RepositoryIndexOmission[]
  error?: string
  started_at?: string
  completed_at?: string
	indexing: boolean
	can_cancel: boolean
	current_path?: string
	progress_files_seen: number
	progress_files_indexed: number
	progress_bytes_read: number
	progress_chunk_count: number
	sources: RepositoryIndexSource[]
	refresh_mode?: string
	files_reused: number
	files_changed: number
	files_deleted: number
	watch_enabled: boolean
	watch_state?: string
	watch_error?: string
	watch_last_check?: string
	health: string
	health_detail: string
}

export interface ReviewShardPreview {
  id: string
  digest: string
  ordinal: number
  file_count: number
  chunk_count: number
  bytes: number
  paths: string[]
  languages?: string[]
  evidence_digest: string
}

export interface SplitReviewPreview {
  version: number
  generation_id: string
  manifest_digest: string
  plan_digest: string
  requested_shards: number
  shard_count: number
  file_count: number
  chunk_count: number
  bytes: number
  shards: ReviewShardPreview[]
}

export interface ReviewFindingEvidence {
  chunk_id: string
  file_sha256: string
  start_line: number
  end_line: number
}

export interface MergedReviewFinding {
  id?: string
  claim: string
  severity: string
  confidence: string
  evidence: ReviewFindingEvidence[]
  follow_up?: string
  state: string
  evidence_valid: boolean
  shard_ids: string[]
  claimants: string[]
}

export interface ReviewConflictClaim {
  claim_id: string
  claim: string
  severity: string
  confidence: string
  shard_ids: string[]
  claimants: string[]
}

export interface RepositoryReviewConflictStatus {
  id: string
  evidence_digest: string
  evidence: ReviewFindingEvidence[]
  claims: ReviewConflictClaim[]
  state: string
  attempt: number
  checker_id?: string
  verdict?: string
  supported_claim_ids: string[]
  reason?: string
  error?: string
}

export interface RepositoryReviewShardStatus {
  id: string
  digest: string
  ordinal: number
  state: string
  attempt: number
  claimant_id?: string
  file_count: number
  chunk_count: number
  bytes: number
  paths: string[]
  findings: number
  error?: string
}

export interface RepositoryReviewStatus {
  available: boolean
  workspace: string
  review_id?: string
  generation_id?: string
  manifest_digest?: string
  plan_digest?: string
  requested_shards: number
  state: string
  phase?: string
  can_cancel: boolean
  can_resume: boolean
  current_shard?: string
  current_conflict?: string
  started_at?: string
  completed_at?: string
  shards: RepositoryReviewShardStatus[]
  findings: MergedReviewFinding[]
  accepted: number
  rejected: number
  duplicates: number
  conflicts: number
  conflict_checks: RepositoryReviewConflictStatus[]
  error?: string
}

export interface BrowserWorkflowStatus {
  available: boolean
  tool_enabled: boolean
  active: boolean
  visible: boolean
  paused: boolean
  state: string
  url?: string
  title?: string
  last_action?: string
  last_error?: string
  updated_at?: string
  guidance?: string
  active_tab?: string
  tab_count?: number
}

export interface BrowserCheckpointStatus {
  name: string
  url: string
  title?: string
  visible: boolean
  created_at: string
}

export interface JHUTBrowserReport {
  pass: boolean; url: string; desktop_screenshot: string; mobile_screenshot: string
  canvas_width: number; canvas_height: number; pixel_variance: number; pixel_coverage: number
  orbit_changed: boolean; responsive: boolean; console_errors: string[]; runtime_errors: string[]
  failures: string[]; verifier_version: string
}

export interface TelegramConfig {
  enabled: boolean
  token: string
  bot_username: string
  require_mention: boolean
  allow_from: string[]
  default_project: string
  default_profile: string
  default_mode: string
  default_toolset: string
  send_progress: boolean
  progress_interval_s: number
  voice_replies: string
  transcription_mode: string
  transcription_url: string
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

export interface ReviewLoopConfig {
  enabled: boolean
  only_autonomous: boolean
  max_review_cycles: number
  verify_gate: boolean
  verify_commands: string[]
  verify_timeout_sec: number
  completion_rails: boolean
  completion_blocking: boolean
  reviewer_pass: boolean
  reviewer_max_tools: number
}

export interface ToolSafeRule {
  id: string
  tool: string
  input_hash: string
  label: string
  created_at: string
}

export interface WorkspaceFolder {
  path: string
  name: string
  role: string
}

export interface WorkspacePreference {
  path: string
  agent_mode: string
}

export interface RepositoryIndexSource {
  path: string
  kind: 'file' | 'folder' | string
}

export interface RepositoryIndexSourceSet {
  workspace: string
  sources: RepositoryIndexSource[]
  watch: boolean
}

export interface ContextInspectionRange {
  start_line: number
  end_line: number
}

export interface ContextInspectionSource {
  path: string
  display_path: string
  sha256: string
  reason: string
  trust: string
  source_bytes: number
  prompt_bytes: number
  estimated_tokens: number
  partial: boolean
  excerpt_ranges: ContextInspectionRange[]
}

export interface ContextInspectionExclusion {
  path: string
  display_path: string
  source_bytes: number
  reason: string
  large: boolean
}

export interface ContextInspectionBudget {
  core_system_tokens: number
  project_document_tokens: number
  tool_schema_tokens: number
  memory_progress_tokens: number
  skill_tokens: number
  user_profile_tokens: number
  conversation_tokens: number
  user_task_tokens: number
  total_preflight_tokens: number
  remaining_working_tokens: number
  usage_percent: number
}

export interface ContextInspection {
  generated_at: string
  task_text: string
  requested_class: string
  effective_class: string
  pinned_next_class?: string
  policy: string
  route_id?: string
  profile_name: string
  model_id: string
  agent_mode: string
  tool_choice: string
  tool_count: number
  tool_names: string[]
  tool_schema_sha256?: string
  packet_sha256?: string
  context_window_tokens: number
  working_context_tokens: number
  output_reserve_tokens: number
  model_max_output_tokens: number
  packet_limit_tokens: number
  packet_limit_bytes: number
  manifest_status: string
  manifest_path?: string
  manifest_sha256?: string
  fallback_reason?: string
  budget: ContextInspectionBudget
  sources: ContextInspectionSource[]
  excluded_sources: ContextInspectionExclusion[]
  warnings: string[]
  synopsis: string
}

export interface AgentDefinition {
  id: string
  name: string
  description: string
  version: string
  default_profile: string
  default_toolset: string
  default_autonomy: string
  planning_only: boolean
  builtin: boolean
}

export interface LabScopeTarget {
  value: string
  kind: 'ip' | 'cidr' | 'hostname' | 'url' | 'invalid'
  environment: 'auto' | 'external' | 'internal'
  label: string
  notes: string
  excluded: boolean
}

export interface LabContext {
  id: string
  name: string
  target: string
  hostname: string
  scope_targets: LabScopeTarget[]
  vpn_interface: string
  latest_artifact: string
  ops_profile: string
  evidence_policy: string
  access_preference: string
  notes: string
}

export interface LabProfile {
  id: string
  name: string
  workspace_dir: string
  target: string
  hostname: string
  scope_targets: LabScopeTarget[]
  vpn_interface: string
  latest_artifact: string
  ops_profile: string
  evidence_policy: string
  access_preference: string
  notes: string
}

export interface EnvironmentConfig {
  main_os: string
  ai_shell_backend: string
  ai_shell_distro: string
  ai_shell_user: string
  target_work_backend: string
  listener_backend: string
  listener_command: string
  lhost_source: string
  manual_lhost: string
  prefer_terminal_tools: boolean
  reverse_shell_guidance: string
  user_correction_policy: string
}

export interface LabStatus {
  agent_root: string
  lab_id: string
  lab_name: string
  shell_backend: string
  shell_distro: string
  shell_user: string
  target: string
  hostname: string
  scope_targets: LabScopeTarget[]
  vpn_interface: string
  vpn_ip: string
  vpn_cidr: string
  vpn_kind: string
  latest_artifact: string
  ops_profile: string
  evidence_policy: string
  access_preference: string
  notes: string
  listener_backend: string
  listener_command: string
  lhost_source: string
  manual_lhost: string
  open_folders: WorkspaceFolder[]
}

export interface VPNInterfaceInfo {
  name: string
  ip: string
  cidr: string
  kind: string
  likely_vpn: boolean
  label: string
}

export interface GenerationParams {
  temperature: number
  top_p: number
  top_k: number
  min_p: number
  presence_penalty: number
  repeat_penalty: number
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
  kv_cache_precision: string // auto | f16 | bf16 | q8_0 | q4_0 | custom
  kv_cache_type_k: string
  kv_cache_type_v: string
  thinking_general: GenerationParams
  thinking_coding: GenerationParams
  nothinking: GenerationParams
  spec_type: string        // "" | "draft-mtp"
  spec_draft_n_max: number // draft tokens per step, 0 = server default
  spec_draft_model: string // optional GGUF draft model path
}

export interface Provider {
  name: string
  backend: string
  base_url: string
  api_key_env: string
}

export interface ProviderAPIKeyStatus {
  provider: string
  environment_configured: boolean
  stored_configured: boolean
  effective_configured: boolean
}

export interface ModelMetadata {
  id: string
  context_length?: number
  max_completion_tokens?: number
  supported_parameters?: string[]
}

export interface ModelProfileTemplateResult {
  matched: boolean
  template_id?: string
  family?: string
  adapter?: string
  tool_protocol?: string
  chat_template?: string
  requires_jinja?: boolean
  huggingface_repo?: string
  profile: Profile
  notes: string[]
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
  window: number
  reserve: number
  configured_window?: number
}

export interface SessionChatMessage {
  role: ChatRole
  content: string
  thinking?: string
  tool_name?: string
  tool_call_id?: string
  images?: string[]
  attachments?: ChatAttachment[]
}

export interface SessionSummary {
  name: string
  updated_unix: number
  message_count: number
  size_bytes: number
  status: 'saved' | 'needs-review' | string
  tags: string[]
  conversation_mode: 'adaptive' | 'direct' | 'agent' | string
}

export interface ScratchWorkspaceStatus {
  active: boolean
  exists: boolean
  review_due: boolean
  name?: string
  path?: string
  created_unix?: number
  review_after_unix?: number
  retention_policy: string
  promotion_eligible: boolean
}

export interface SessionRepairAction {
  phase: number
  action: string
  index: number
  detail?: string
}

export interface SessionRepairReport {
  name: string
  status: 'clean' | 'repaired' | 'rejected' | string
  valid: boolean
  before_messages: number
  after_messages: number
  actions: SessionRepairAction[]
  diagnostic?: string
  applied: boolean
  backup_path?: string
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
  confidence: string
  source: string
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
  source_path: string
  required_tools: string[]
  shell_backend: string
  needs_network: boolean
  needs_write: boolean
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

export interface TaskContractCheck {
  id: string
  description: string
  verifier: string
  blocking: boolean
  evidence_kinds?: string[]
}

export interface TaskContract {
  version: number
  revision: number
  parent_digest?: string
  run_id: string
  objective: string
  workspace_root: string
  deliverables?: Array<{ id: string; description: string; kind?: string }>
  constraints?: string[]
  protected_resources?: string[]
  allowed_mutations?: Array<{ root: string; access: string }>
  protected_artifacts?: Array<{ path: string; sha256: string; source_run_id: string; generation: number }>
  repair_scope?: string[]
  acceptance_checks?: TaskContractCheck[]
  required_evidence?: string[]
  risk: 'low' | 'medium' | 'high'
  instruction_revision: number
  plan_required: boolean
  budgets: { max_tool_calls?: number; max_run_seconds?: number }
  approval_policy: string
  completion_policy: string
  created_at: string
  digest: string
}

export interface RunControlState {
  version: number
  contract_digest: string
  contract_revision: number
  phase: 'intake' | 'planning' | 'acting' | 'observing' | 'verifying' | 'repairing' | 'awaiting_approval' | 'complete' | 'blocked' | 'cancelled' | 'failed'
  resume_phase?: string
  revision: number
  plan_required: boolean
  plan_accepted: boolean
  blocking_check_ids?: string[]
  satisfied_checks?: Record<string, string[]>
  last_event?: string
  last_detail?: string
  updated_at: string
}

export interface TaskRun {
  id: string
  generation?: number
  conversation_epoch?: number
  parent_run_id?: string
  prompt: string
  mode: string
  profile: string
  model?: string
  claimant_id?: string
  claimant_alias?: string
  origin?: string
  contract?: TaskContract
  control?: RunControlState
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
  finalized_artifacts?: FinalizedArtifact[]
  tools?: TaskToolEvent[]
  events?: TaskRunEvent[]
}

export interface FinalizedArtifact {
  path: string
  sha256: string
  size: number
  run_id: string
  generation: number
  conversation_epoch?: number
  evidence_id: string
  verifier_evidence_ids?: string[]
  finalized_at: string
  fresh: boolean
  freshness: 'fresh' | 'changed' | 'missing' | string
  current_sha256?: string
}

export interface ChannelAttachment {
  kind: string
  file_id?: string
  file_name?: string
  content_type?: string
  path?: string
  text?: string
}

export interface ChannelEnvelope {
  id: string
  source: string
  session_id: string
  user_id?: string
  username?: string
  text: string
  attachments?: ChannelAttachment[]
  metadata?: Record<string, string>
  created_at?: string
}

export interface ChannelRoute {
  lane: string
  command?: string
  argument?: string
  policy?: string
  read_only: boolean
  reason?: string
  project?: string
  mode?: string
  toolset?: string
  from_voice?: boolean
}

export interface ChannelResponse {
  lane: string
  status: string
  message: string
  queued?: boolean
  queue_id?: string
  run_started?: boolean
  data?: Record<string, string>
}

export interface ChannelWorkItem {
  id: string
  envelope: ChannelEnvelope
  route: ChannelRoute
  status: string
  created_at: string
}

export interface AgentEvalResult {
  name: string
  attempt?: number
  pass: boolean
  artifact_pass: boolean
  hygiene_pass: boolean
  status_pass: boolean
  status: string
  tool_calls: number
  tool_success_rate: number
  auto_continues: number
  truncations: number
  tool_errors: number
  repeated_tool_inputs: number
  repeated_skips: number
  repeat_tool_rate: number
  verifier_prompts: number
  max_routed_tools: number
  prompt_warnings: number
  stability_score: number
  false_done: boolean
  policy_violations: number
  human_interventions: number
  recovery_events: number
  recovered: boolean
  duration_ms: number
  fail_reason?: string
  runtime_pass?: boolean
  desktop_screenshot?: string
  mobile_screenshot?: string
  runtime_failures?: string[]
  model_id?: string
  provider?: string
  context_tokens?: number
  seed?: number
  artifact_hash?: string
  verifier_version?: string
  stop_reason?: string
  tool_trace?: TaskToolEvent[]
  response_excerpt?: string
  event_trace?: TaskRunEvent[]
}

export interface AgentEvalReport {
  results: AgentEvalResult[]
  pass_count: number
  total: number
  profile: string
  id?: string
  created_at?: string
  repeats: number
  fixture_count: number
  fixture_pass_count: number
  pass_power: string
  full_pass: boolean
  unsupported_completion_rate: number
  duplicate_action_rate: number
  tool_error_rate: number
  recovery_success_rate: number
  average_tool_calls: number
  average_duration_ms: number
  policy_violations: number
  human_interventions: number
}

export interface ContextQualityVariantResult {
  prompt_index: number
  pass: boolean
  passed_repeats: number
  repeats: number
  stable: boolean
  policy: string
  effective_class: string
  route_id?: string
  agent_mode: string
  project_tokens: number
  tool_names: string[]
  packet_sha256?: string
  tool_schema_sha256?: string
  failures?: string[]
}

export interface ContextQualityFixtureResult {
  id: string
  category: string
  pass: boolean
  pass_power: string
  passed_repeats: number
  repeats: number
  variant_count: number
  hostile_pass: boolean
  policy_pass: boolean
  tool_pass: boolean
  source_pass: boolean
  budget_pass: boolean
  determinism_pass: boolean
  variants: ContextQualityVariantResult[]
  failures?: string[]
}

export interface ContextQualityReport {
  id: string
  created_at: string
  profile: string
  evaluation_envelope: string
  repeats: number
  pass_power: string
  pass: boolean
  pass_count: number
  total: number
  attempt_pass_count: number
  attempt_total: number
  hostile_pass: boolean
  results: ContextQualityFixtureResult[]
}

export interface GrammarToolArgsProbeResult {
  profile: string
  backend: string
  model_id: string
  supported: boolean
  structured_call: boolean
  valid_arguments: boolean
  tool_name?: string
  arguments?: string
  text?: string
  error?: string
  recommendation: string
}

export interface StorageItem {
  id: string
  label: string
  path: string
  kind: string
  bytes: number
  size: string
  clearable: boolean
  description: string
}

export interface RunCheckpoint {
  run_id: string
  name?: string
  conversation_name?: string
  conversation_mode?: 'adaptive' | 'direct' | 'agent' | string
  explicit?: boolean
  prompt: string
  mode: string
  profile: string
  messages: unknown[]
  chat_messages?: SessionChatMessage[]
  run: TaskRun
  saved_at: string
}

export interface LedgerEvent {
  id: string
  run_id?: string
  kind: string
  source?: string
  tool?: string
  status?: string
  state?: string
  message?: string
  detail?: string
  input?: string
  output?: string
  error?: string
  duration_ms?: number
  files?: string[]
  artifacts?: string[]
  metadata?: Record<string, string>
  timestamp: string
}

export interface LearningCandidate {
  id: string
  run_id?: string
  type: string
  title: string
  reason: string
  content: string
  kind: string
  importance: number
  tags: string[]
  evidence?: string[]
  template?: string
  created_at: string
}

export interface EngagementSummary {
  id: string
  name: string
  workspace: string
  workflow_id: string
  workflow_version?: string
  checklist_id: string
  checklist_version?: string
  current_phase: string
  revision: number
  created_at: string
  updated_at: string
}

export interface EngagementSetupWorkflow {
  id: string
  name: string
  version: string
  description: string
  phase_count: number
  checklist_name: string
  checklist_version: string
  check_count: number
}

export interface EngagementSetupArtifact {
  path: string
  name: string
  kind: 'scan' | 'screenshot' | 'report' | 'note' | 'artifact'
  size: number
  modified_at: string
}

export interface EngagementSetupCheck {
  id: string
  label: string
  status: 'ready' | 'info' | 'warning' | 'blocked' | 'unchecked'
  detail: string
  blocking: boolean
}

export interface EngagementSetupPreview {
  project_name: string
  project_id: string
  workspace: string
  target: string
  hostname: string
  scope: string[]
  scope_locked: boolean
  active_profile: string
  model_id: string
  provider: string
  provider_url: string
  shell: string
  vpn: string
  workflows: EngagementSetupWorkflow[]
  candidate_artifacts: EngagementSetupArtifact[]
  checks: EngagementSetupCheck[]
  can_create: boolean
  can_start: boolean
}

export interface EngagementTargetProbe {
  target: string
  status: 'ready' | 'warning' | 'blocked'
  detail: string
  attempted?: string[]
  latency_ms?: number
  http_status?: number
  scope_match?: string
  checked_at: string
}

export interface PackQualityIssue {
  check_id?: string
  field: string
  message: string
}

export interface PackSummary {
  key: string
  id: string
  version: string
  name: string
  description?: string
  scope: 'builtin' | 'personal' | 'project'
  trust: 'official' | 'curated' | 'community' | 'local'
  license: string
  path?: string
  built_in: boolean
  archived: boolean
  active: boolean
  valid: boolean
  validation_error?: string
  workflow_id: string
  workflow_version: string
  checklist_id: string
  checklist_version: string
  workflow_digest: string
  checklist_digest: string
  phase_count: number
  check_count: number
  global_checks: number
  endpoint_checks: number
  automated_checks: number
  quality_score: number
  quality_ready: boolean
  quality_issues?: PackQualityIssue[]
}

export interface PackLibrarySnapshot {
  packs: PackSummary[]
  personal_root: string
  project_root?: string
  built_in_count: number
  active_count: number
  archived_count: number
  invalid_count: number
}

export interface PackCloneInput {
  source_key: string
  scope: 'personal' | 'project'
  id: string
  name: string
  version?: string
}

export interface EngagementWorkRef {
  kind: 'step' | 'global_check' | 'endpoint_check'
  phase_id?: string
  endpoint_id?: string
  id: string
}

export interface EngagementClaim {
  claimant: { id: string; alias?: string }
  claimed_at: string
  heartbeat_at?: string
  lease_until: string
}

export interface EngagementWorkState {
  ref: EngagementWorkRef
  title: string
  status: string
  observation?: string
  runs: number | 'indefinite'
  runs_completed: number
  finished: boolean
  claim?: EngagementClaim
  revision: number
  updated_at: string
}

export interface EngagementEndpoint {
  id: string
  method: string
  url: string
  name?: string
  feature_group?: string
  created_at: string
  updated_at: string
}

export interface EngagementEvidence {
  id: string
  work: EngagementWorkRef
  finding_id?: string
  source_kind: 'ledger_event' | 'artifact' | 'screenshot' | 'http_capture' | 'external_file'
  ledger_event_id?: string
  path?: string
  sha256?: string
  size?: number
  fingerprint_kind?: 'file_sha256' | 'ledger_payload_sha256'
  agent_composed: boolean
  description: string
  run: number
  created_by: { id: string; alias?: string }
  created_at: string
}

export interface EngagementEvidenceFreshness {
  state: 'fresh' | 'stale' | 'immutable' | 'unverifiable'
  detail?: string
  current_sha256?: string
  current_size?: number
}

export interface EngagementFinding {
  id: string
  work: EngagementWorkRef
  title: string
  severity: 'info' | 'low' | 'medium' | 'high' | 'critical'
  state: 'draft' | 'confirmed' | 'rejected'
  description?: string
  impact?: string
  recommendation?: string
  confidence?: string
  reproduction?: string
  evidence_ids: string[]
  operator_waiver?: string
  created_by: { id: string; alias?: string }
  revision: number
  created_at: string
  updated_at: string
}

export interface EngagementEvidenceInput {
  id?: string
  engagement_id: string
  work: EngagementWorkRef
  claimant?: { id: string; alias?: string }
  source_kind: EngagementEvidence['source_kind']
  ledger_event_id?: string
  path?: string
  description: string
  operator_trusted?: boolean
}

export interface EngagementFindingInput {
  id?: string
  engagement_id: string
  work: EngagementWorkRef
  claimant?: { id: string; alias?: string }
  title: string
  severity: EngagementFinding['severity']
  description?: string
  impact?: string
  recommendation?: string
  confidence?: string
  reproduction?: string
  evidence_ids?: string[]
  expected_revision?: number
}

export interface EngagementState {
  id: string
  name: string
  workflow_id: string
  checklist_id: string
  current_phase: string
  scope: string[]
  scope_locked: boolean
  notes?: string
  notes_revision: number
  notes_updated_at?: string
  steps: Record<string, EngagementWorkState>
  global_checks: Record<string, EngagementWorkState>
  global_check_order: string[]
  endpoints: Record<string, EngagementEndpoint>
  endpoint_order: string[]
  endpoint_checks: Record<string, EngagementWorkState>
  endpoint_check_order: string[]
  evidence: Record<string, EngagementEvidence>
  evidence_order: string[]
  findings: Record<string, EngagementFinding>
  finding_order: string[]
  revision: number
  created_at: string
  updated_at: string
}

export interface EngagementRecord {
  workspace: string
  workflow: { id: string; version?: string; name: string; checklist?: string; phases: unknown[]; digest?: string }
  checklist: { id: string; version?: string; name: string; items: Array<{ id: string; title: string; scope: string; category?: string; category_name?: string; verified?: boolean }>; digest?: string }
  state: EngagementState
  evidence_freshness?: Record<string, EngagementEvidenceFreshness>
}

export interface EngagementNextAction {
  action: string
  phase_id?: string
  phase_name?: string
  work?: EngagementWorkState
  phase_complete: boolean
  workflow_done: boolean
}

export interface EngagementAvailableWork {
  phase_id: string
  phase_name: string
  parallel: boolean
  items: EngagementWorkState[]
  blocker?: string
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

export const ListAgentDefinitions = (): Promise<AgentDefinition[]> =>
  call('app.App.ListAgentDefinitions')

export const PreviewContext = (taskText: string, requestedClass: string): Promise<ContextInspection> =>
  call('app.App.PreviewContext', taskText, requestedClass)

export const SetNextContextPacketClass = (requestedClass: string): Promise<string> =>
  call('app.App.SetNextContextPacketClass', requestedClass)

export const GetNextContextPacketClass = (): Promise<string> =>
  call('app.App.GetNextContextPacketClass')

// Auto-speculative (MTP) decoding plan for the active model.
export interface SpecPlan {
  enabled: boolean
  spec_type: string
  n_max: number
  source: string // probe | name | registry | manual | disabled
  reason: string
  locked: boolean
  model_id: string
}

export const GetSpecPlan = (): Promise<SpecPlan> =>
  call('app.App.GetSpecPlan')

// mode: "auto" | "on" | "off"
export const SetSpecMode = (mode: string): Promise<SpecPlan> =>
  call('app.App.SetSpecMode', mode)

export interface SpecCalibrationSample {
  n: number
  tok_per_sec: number
  note?: string
}

export interface SpecCalibration {
  key: string
  model_id: string
  best_n: number
  tok_per_sec: number
  baseline_tok_per_sec: number
  speedup: number
  ran_at: string
  samples: SpecCalibrationSample[]
}

// Sweeps spec_draft_n_max, caches the fastest, applies it. Heavy (reloads the
// model per step); reject if the agent is busy or the model is not MTP-capable.
export const CalibrateSpec = (profileName: string): Promise<SpecCalibration> =>
  call('app.App.CalibrateSpec', profileName)

export const GetSpecCalibration = (): Promise<SpecCalibration> =>
  call('app.App.GetSpecCalibration')

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

export const StartConversation = (title: string): Promise<string> =>
  call('app.App.StartConversation', title)

export const GetConversationMode = (): Promise<string> =>
  call('app.App.GetConversationMode')

export const SetConversationMode = (sessionName: string, mode: string): Promise<void> =>
  call('app.App.SetConversationMode', sessionName, mode)

export const SetSavedConversationMode = (sessionName: string, mode: string): Promise<void> =>
  call('app.App.SetSavedConversationMode', sessionName, mode)

export const SaveSession = (name: string): Promise<void> =>
  call('app.App.SaveSession', name)

export const LoadSession = (name: string): Promise<SessionChatMessage[]> =>
  call('app.App.LoadSession', name)

export const ListSessions = (): Promise<string[]> =>
  call('app.App.ListSessions')

export const ListSessionSummaries = (): Promise<SessionSummary[]> =>
  call('app.App.ListSessionSummaries')

export const RenameSession = (oldName: string, newName: string): Promise<void> =>
  call('app.App.RenameSession', oldName, newName)

export const SetSessionTags = (name: string, tags: string[]): Promise<void> =>
  call('app.App.SetSessionTags', name, tags)

export const DeleteSession = (name: string): Promise<void> =>
  call('app.App.DeleteSession', name)

export const InspectSessionRepair = (name: string): Promise<SessionRepairReport> =>
  call('app.App.InspectSessionRepair', name)

export const RepairSession = (name: string): Promise<SessionRepairReport> =>
  call('app.App.RepairSession', name)

export const ListMemory = (): Promise<MemoryEntry[]> =>
  call('app.App.ListMemory')

export const ExportMemoryJSON = (): Promise<string> =>
  call('app.App.ExportMemoryJSON')

export const ImportMemoryJSON = (raw: string): Promise<number> =>
  call('app.App.ImportMemoryJSON', raw)

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

export const GetEngagementSetupPreview = (): Promise<EngagementSetupPreview> =>
  call('app.App.GetEngagementSetupPreview')

export const CheckEngagementTarget = (): Promise<EngagementTargetProbe> =>
  call('app.App.CheckEngagementTarget')

export const ListPackLibrary = (): Promise<PackLibrarySnapshot> =>
  call('app.App.ListPackLibrary')

export const ClonePack = (input: PackCloneInput): Promise<PackSummary> =>
  call('app.App.ClonePack', input)

export const ImportPackJSON = (scope: 'personal' | 'project', raw: string): Promise<PackSummary> =>
  call('app.App.ImportPackJSON', scope, raw)

export const ExportPackJSON = (key: string): Promise<string> =>
  call('app.App.ExportPackJSON', key)

export const SetPackArchived = (key: string, archived: boolean): Promise<PackSummary> =>
  call('app.App.SetPackArchived', key, archived)

export const ListEngagements = (): Promise<EngagementSummary[]> =>
  call('app.App.ListEngagements')

export const GetEngagement = (id: string): Promise<EngagementRecord> =>
  call('app.App.GetEngagement', id)

export const GetEngagementNext = (id: string): Promise<EngagementNextAction> =>
  call('app.App.GetEngagementNext', id)

export const GetEngagementAvailable = (id: string, limit = 8): Promise<EngagementAvailableWork> =>
  call('app.App.GetEngagementAvailable', id, limit)

export const CreateEngagement = (name: string, workflowID: string, scope: string[]): Promise<EngagementRecord> =>
  call('app.App.CreateEngagement', name, workflowID, scope)

export const ExportEngagementJSON = (id: string): Promise<string> =>
  call('app.App.ExportEngagementJSON', id)

export const ImportEngagementJSON = (raw: string): Promise<EngagementRecord> =>
  call('app.App.ImportEngagementJSON', raw)

export const AddEngagementEndpoint = (id: string, input: Pick<EngagementEndpoint, 'id' | 'method' | 'url' | 'name' | 'feature_group'>): Promise<EngagementEndpoint> =>
  call('app.App.AddEngagementEndpoint', id, input)

export const SetEngagementEndpointGroup = (id: string, endpointID: string, group: string): Promise<EngagementEndpoint> =>
  call('app.App.SetEngagementEndpointGroup', id, endpointID, group)

export const SetEngagementNotes = (id: string, notes: string, expectedNotesRevision: number): Promise<number> =>
  call('app.App.SetEngagementNotes', id, notes, expectedNotesRevision)

export const AddEngagementEvidence = (id: string, input: Omit<EngagementEvidenceInput, 'engagement_id' | 'claimant' | 'operator_trusted'>): Promise<EngagementEvidence> =>
  call('app.App.AddEngagementEvidence', id, input)

export const UpsertEngagementFinding = (id: string, input: Omit<EngagementFindingInput, 'engagement_id' | 'claimant'>): Promise<EngagementFinding> =>
  call('app.App.UpsertEngagementFinding', id, input)

export const ConfirmEngagementFinding = (id: string, findingID: string, expectedRevision: number, operatorWaiver: string): Promise<EngagementFinding> =>
  call('app.App.ConfirmEngagementFinding', id, findingID, expectedRevision, operatorWaiver)

export const ReleaseEngagementClaim = (id: string, claimantID: string): Promise<EngagementWorkState> =>
  call('app.App.ReleaseEngagementClaim', id, claimantID)

export const DeleteEngagement = (id: string): Promise<void> =>
  call('app.App.DeleteEngagement', id)

export const ListTaskRuns = (): Promise<TaskRun[]> =>
  call('app.App.ListTaskRuns')

export const ClearTaskRuns = (): Promise<void> =>
  call('app.App.ClearTaskRuns')

export const ExportTaskRunsJSON = (): Promise<string> =>
  call('app.App.ExportTaskRunsJSON')

export const ImportTaskRunsJSON = (raw: string): Promise<number> =>
  call('app.App.ImportTaskRunsJSON', raw)

export const DispatchChannelMessage = (env: ChannelEnvelope): Promise<ChannelResponse> =>
  call('app.App.DispatchChannelMessage', env)

export const DispatchSideChatMessage = (env: ChannelEnvelope): Promise<ChannelResponse> =>
  call('app.App.DispatchSideChatMessage', env)

export const ListChannelWorkQueue = (): Promise<ChannelWorkItem[]> =>
  call('app.App.ListChannelWorkQueue')

export const GetChannelBusStatus = (): Promise<Record<string, string>> =>
  call('app.App.GetChannelBusStatus')

export const SendTelegramMessage = (chatID: string, text: string): Promise<string> =>
  call('app.App.SendTelegramMessage', chatID, text)

export const DeleteTelegramMessage = (chatID: string, messageID: string): Promise<void> =>
  call('app.App.DeleteTelegramMessage', chatID, messageID)

export const ListLedgerEvents = (limit: number): Promise<LedgerEvent[]> =>
  call('app.App.ListLedgerEvents', limit)

export const ClearLedgerEvents = (): Promise<void> =>
  call('app.App.ClearLedgerEvents')

export const PruneLedgerEvents = (scope: string, ids: string[]): Promise<number> =>
  call('app.App.PruneLedgerEvents', scope, ids)

export const ListLearningCandidates = (limit: number): Promise<LearningCandidate[]> =>
  call('app.App.ListLearningCandidates', limit)

export const RecordLearningDecision = (candidate: LearningCandidate, decision: string, reason: string): Promise<void> =>
  call('app.App.RecordLearningDecision', candidate, decision, reason)

export const SendMessage = (text: string, images: string[], attachments: ChatAttachment[] = []): Promise<void> =>
  call('app.App.SendMessage', text, images, attachments)

export const SendMessageWithProfile = (text: string, images: string[], attachments: ChatAttachment[] = [], profileName: string): Promise<void> =>
  call('app.App.SendMessageWithProfile', text, images, attachments, profileName)

export const StopAgent = (): Promise<void> =>
  call('app.App.StopAgent')

export const TranscribeVoiceClip = (dataURI: string): Promise<string> =>
  call('app.App.TranscribeVoiceClip', dataURI)

export const SynthesizeSpeech = (text: string): Promise<SpeechAudio> =>
  call('app.App.SynthesizeSpeech', text)

export const GetAudioHealth = (): Promise<AudioHealth> =>
  call('app.App.GetAudioHealth')

export const RestartAudioWorker = (): Promise<AudioHealth> =>
  call('app.App.RestartAudioWorker')

export const ListKokoroVoices = (): Promise<string[]> =>
  call('app.App.ListKokoroVoices')

export const GetServiceHealth = (): Promise<ServiceHealth[]> =>
  call('app.App.GetServiceHealth')

export const GetRunEventDiagnostics = (): Promise<RunEventDiagnostics> =>
  call('app.App.GetRunEventDiagnostics')

export const GetRepositoryIndexStatus = (): Promise<RepositoryIndexStatus> =>
  call('app.App.GetRepositoryIndexStatus')

export const PreviewRepositorySplitReview = (shardCount: number): Promise<SplitReviewPreview> =>
  call('app.App.PreviewRepositorySplitReview', shardCount)

export const GetRepositoryReviewStatus = (): Promise<RepositoryReviewStatus> =>
  call('app.App.GetRepositoryReviewStatus')

export const StartRepositorySplitReview = (shardCount: number): Promise<RepositoryReviewStatus> =>
  call('app.App.StartRepositorySplitReview', shardCount)

export const CancelRepositorySplitReview = (): Promise<RepositoryReviewStatus> =>
  call('app.App.CancelRepositorySplitReview')

export const ResumeRepositorySplitReview = (): Promise<RepositoryReviewStatus> =>
  call('app.App.ResumeRepositorySplitReview')

export const RetryRepositoryReviewShard = (shardID: string): Promise<RepositoryReviewStatus> =>
  call('app.App.RetryRepositoryReviewShard', shardID)

export const IndexWorkspaceRepository = (): Promise<RepositoryIndexStatus> =>
  call('app.App.IndexWorkspaceRepository')

export const RefreshWorkspaceRepositoryIndex = (): Promise<RepositoryIndexStatus> =>
  call('app.App.RefreshWorkspaceRepositoryIndex')

export const CancelWorkspaceRepositoryIndex = (): Promise<RepositoryIndexStatus> =>
  call('app.App.CancelWorkspaceRepositoryIndex')

export const SelectRepositoryIndexFolder = (): Promise<RepositoryIndexStatus> =>
  call('app.App.SelectRepositoryIndexFolder')

export const SelectRepositoryIndexFiles = (): Promise<RepositoryIndexStatus> =>
  call('app.App.SelectRepositoryIndexFiles')

export const RemoveRepositoryIndexSource = (path: string): Promise<RepositoryIndexStatus> =>
  call('app.App.RemoveRepositoryIndexSource', path)

export const SetRepositoryIndexWatch = (enabled: boolean): Promise<RepositoryIndexStatus> =>
  call('app.App.SetRepositoryIndexWatch', enabled)

export const GetBrowserWorkflowStatus = (): Promise<BrowserWorkflowStatus> =>
  call('app.App.GetBrowserWorkflowStatus')

export const StartBrowserWorkflow = (url: string): Promise<BrowserWorkflowStatus> =>
  call('app.App.StartBrowserWorkflow', url)

export const PauseBrowserWorkflow = (): Promise<BrowserWorkflowStatus> =>
  call('app.App.PauseBrowserWorkflow')

export const TakeOverBrowserWorkflow = (): Promise<BrowserWorkflowStatus> =>
  call('app.App.TakeOverBrowserWorkflow')

export const ResumeBrowserWorkflow = (): Promise<BrowserWorkflowStatus> =>
  call('app.App.ResumeBrowserWorkflow')

export const ListBrowserCheckpoints = (): Promise<BrowserCheckpointStatus[]> =>
  call('app.App.ListBrowserCheckpoints')

export const SaveBrowserWorkflowCheckpoint = (name: string): Promise<BrowserCheckpointStatus> =>
  call('app.App.SaveBrowserWorkflowCheckpoint', name)

export const ResumeBrowserWorkflowCheckpoint = (name: string): Promise<BrowserWorkflowStatus> =>
  call('app.App.ResumeBrowserWorkflowCheckpoint', name)

export const StopBrowserWorkflow = (): Promise<BrowserWorkflowStatus> =>
  call('app.App.StopBrowserWorkflow')

export const InterruptShellTool = (): Promise<void> =>
  call('app.App.InterruptShellTool')

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

export const CreateScratchWorkspace = (name: string): Promise<ScratchWorkspaceStatus> =>
  call('app.App.CreateScratchWorkspace', name)

export const CreateWorkspaceProject = (name: string, defaultParent: string): Promise<string> =>
  call('app.App.CreateWorkspaceProject', name, defaultParent)

export const GetScratchWorkspaceStatus = (): Promise<ScratchWorkspaceStatus> =>
  call('app.App.GetScratchWorkspaceStatus')

export const PromoteScratchWorkspace = (name: string): Promise<ScratchWorkspaceStatus> =>
  call('app.App.PromoteScratchWorkspace', name)

export const SelectWorkingDir = (defaultDir: string): Promise<string> =>
  call('app.App.SelectWorkingDir', defaultDir)

export const ListWorkspaceFolders = (): Promise<WorkspaceFolder[]> =>
  call('app.App.ListWorkspaceFolders')

export const AddWorkspaceFolder = (path: string, role: string): Promise<WorkspaceFolder[]> =>
  call('app.App.AddWorkspaceFolder', path, role)

export const RemoveWorkspaceFolder = (path: string): Promise<WorkspaceFolder[]> =>
  call('app.App.RemoveWorkspaceFolder', path)

export const SelectWorkspaceFolder = (defaultDir: string): Promise<string> =>
  call('app.App.SelectWorkspaceFolder', defaultDir)

export const GetLabStatus = (): Promise<LabStatus> =>
  call('app.App.GetLabStatus')

export const ListVPNInterfaces = (): Promise<VPNInterfaceInfo[]> =>
  call('app.App.ListVPNInterfaces')

export const UpdateLabContext = (target: string, vpnInterface: string, latestArtifact: string, opsProfile: string): Promise<LabStatus> =>
  call('app.App.UpdateLabContext', target, vpnInterface, latestArtifact, opsProfile)

export const ScaffoldWorkspaceFolders = (root: string, names: string[]): Promise<string[]> =>
  call('app.App.ScaffoldWorkspaceFolders', root, names)

export const SelectProjectInstructionFile = (defaultPath: string): Promise<string> =>
  call('app.App.SelectProjectInstructionFile', defaultPath)

export const SelectProjectInstructionDirectory = (defaultPath: string): Promise<string> =>
  call('app.App.SelectProjectInstructionDirectory', defaultPath)

export const UseProjectInstructionFile = (path: string): Promise<Settings> =>
  call('app.App.UseProjectInstructionFile', path)

export const GetProjectInstructionsSummary = (): Promise<string> =>
  call('app.App.GetProjectInstructionsSummary')

export const PickSaveFilePath = (defaultName: string): Promise<string> =>
  call<string>('app.App.PickSaveFilePath', defaultName)

export const SelectChatFiles = (): Promise<ChatAttachment[]> =>
  call('app.App.SelectChatFiles')

export const PrepareChatAttachmentPath = (path: string): Promise<ChatAttachment> =>
  call('app.App.PrepareChatAttachmentPath', path)

export const GetHomeDir = (): Promise<string> =>
  call('app.App.GetHomeDir')

export const EncodeFileBase64 = (path: string): Promise<string> =>
  call('app.App.EncodeFileBase64', path)

export interface VideoIngest {
  frames: string[]
  transcript: string
  duration: number
  frameCount: number
  note: string
}

export const IngestVideo = (dataURI: string, filename: string): Promise<VideoIngest> =>
  call('app.App.IngestVideo', dataURI, filename)

export const IngestVideoPath = (path: string): Promise<VideoIngest> =>
  call('app.App.IngestVideoPath', path)

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

export const ListModelMetadataForProvider = (provider: Provider): Promise<ModelMetadata[]> =>
  call('app.App.ListModelMetadataForProvider', provider)

export const GetProviderAPIKeyStatus = (provider: Provider): Promise<ProviderAPIKeyStatus> =>
  call('app.App.GetProviderAPIKeyStatus', provider)

export const SetProviderAPIKey = (providerName: string, apiKey: string): Promise<void> =>
  call('app.App.SetProviderAPIKey', providerName, apiKey)

export const ClearProviderAPIKey = (providerName: string): Promise<void> =>
  call('app.App.ClearProviderAPIKey', providerName)

export const ListWSLDistros = (): Promise<string[]> =>
  call('app.App.ListWSLDistros')

export interface MaintenanceResult {
  summary: string
  lines: string[]
}

export interface TerminalRecoveryResult {
  status: string
  summary: string
  lines: string[]
}

export interface TerminalStateSnapshot {
  session: string
  state: string
  summary: string
  lines: string[]
}

export const KillLocalInferenceServers = (): Promise<MaintenanceResult> =>
  call('app.App.KillLocalInferenceServers')

export const RestartWSL = (): Promise<MaintenanceResult> =>
  call('app.App.RestartWSL')

export interface ProfileBenchmarkResult {
  status: string
  id?: string
  created_at?: string
  profile_name?: string
  provider_name?: string
  model_id?: string
  ctx_tokens?: number
  actual_ctx_tokens?: number
  context_tier?: string
  context_role?: string
  score?: number
  summary: string
  notes: string[]
  recommended_profile: Profile
  scenarios: BenchmarkCase[]
  prompt_tokens?: number
  completion_tokens?: number
  ttf_ms?: number
  total_ms?: number
  load_ms?: number
  warmup?: BenchmarkCase
  measured_text_runs?: number
  text_ttf_ms?: number
  text_tokens_per_second?: number
  decode_tokens_per_second?: number
  end_to_end_tokens_per_second?: number
  prompt_tokens_per_second?: number
  prompt_ms?: number
  decode_ms?: number
  timing_source?: string
  tokens_per_second?: number
}

export interface BenchmarkCase {
  name: string
  status: string
  summary: string
  prompt_tokens?: number
  completion_tokens?: number
  iteration?: number
  ttf_ms?: number
  total_ms?: number
  prompt_ms?: number
  decode_ms?: number
  prompt_tokens_per_second?: number
  decode_tokens_per_second?: number
  end_to_end_tokens_per_second?: number
  timing_source?: string
  tokens_per_second?: number
  structured_tools?: number
  repaired_tools?: number
  inline_tool_markup?: boolean
  output_leak?: boolean
  valid_json?: boolean
  expected_json?: boolean
  response_chars?: number
  error?: string
}

export interface BenchmarkSpecInput {
  name: string
  system: string
  user: string
  max_tokens: number
  temperature: number
  top_p: number
  top_k: number
  min_p: number
  presence_penalty: number
  seed: number
  expect_json: boolean
  tool_mode: string
}

export const BenchmarkProfile = (profile: Profile, provider: Provider): Promise<ProfileBenchmarkResult> =>
  call('app.App.BenchmarkProfile', profile, provider)

export const RecommendModelProfileTemplate = (profile: Profile): Promise<ModelProfileTemplateResult> =>
  call('app.App.RecommendModelProfileTemplate', profile)

export const BenchmarkProfileWithCases = (profile: Profile, provider: Provider, cases: BenchmarkSpecInput[]): Promise<ProfileBenchmarkResult> =>
  call('app.App.BenchmarkProfileWithCases', profile, provider, cases)

export const LoadBenchmarkModel = (profile: Profile, provider: Provider): Promise<ProfileBenchmarkResult> =>
  call('app.App.LoadBenchmarkModel', profile, provider)

export const ListBenchmarkRuns = (): Promise<ProfileBenchmarkResult[]> =>
  call('app.App.ListBenchmarkRuns')

export const ClearBenchmarkRuns = (): Promise<void> =>
  call('app.App.ClearBenchmarkRuns')

export const ListStorageItems = (): Promise<StorageItem[]> =>
  call('app.App.ListStorageItems')

export const ClearStorageItem = (id: string): Promise<void> =>
  call('app.App.ClearStorageItem', id)

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

export const RunAgentEval = (profileName: string): Promise<AgentEvalReport> =>
  call('app.App.RunAgentEval', profileName)

export const RunAgentEvalRepeated = (profileName: string, repeats: number): Promise<AgentEvalReport> =>
  call('app.App.RunAgentEvalRepeated', profileName, repeats)

export const RunContextQualityEval = (profileName: string, repeats: number): Promise<ContextQualityReport> =>
  call('app.App.RunContextQualityEval', profileName, repeats)

export const RunEngagementAgentEval = (): Promise<AgentEvalReport> =>
  call('app.App.RunEngagementAgentEval')

export const RunJHUTAgentEval = (profileName: string): Promise<AgentEvalReport> =>
  call('app.App.RunJHUTAgentEval', profileName)

export const RunMiniAgentLoopBenchmark = (profile: Profile, provider: Provider): Promise<AgentEvalResult> =>
  call('app.App.RunMiniAgentLoopBenchmark', profile, provider)

export const RunGrammarToolArgsProbe = (profileName: string): Promise<GrammarToolArgsProbeResult> =>
  call('app.App.RunGrammarToolArgsProbe', profileName)

export const ListResumableRuns = (): Promise<RunCheckpoint[]> =>
  call('app.App.ListResumableRuns')

export const SaveConversationCheckpoint = (name: string, conversationName: string): Promise<RunCheckpoint> =>
  call('app.App.SaveConversationCheckpoint', name, conversationName)

export const ResumeRun = (runID: string): Promise<void> =>
  call('app.App.ResumeRun', runID)

export const DeleteResumableRun = (runID: string): Promise<void> =>
  call('app.App.DeleteResumableRun', runID)

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

export const ShellResize = (id: string, cols: number, rows: number): Promise<void> =>
  call('app.App.ShellResize', id, cols, rows)

export const ShellClose = (id: string): Promise<void> =>
  call('app.App.ShellClose', id)

export const RecoverSharedTerminal = (): Promise<TerminalRecoveryResult> =>
  call('app.App.RecoverSharedTerminal')

export const GetSharedTerminalState = (): Promise<TerminalStateSnapshot> =>
  call('app.App.GetSharedTerminalState')
