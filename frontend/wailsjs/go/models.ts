export namespace app {
  export class AgentDefinition {
    id: string;
    name: string;
    description: string;
    version: string;
    default_toolset: string;
    default_autonomy: string;
    planning_only: boolean;
    builtin: boolean;

    static createFrom(source: any = {}) {
      return new AgentDefinition(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.id = source["id"];
      this.name = source["name"];
      this.description = source["description"];
      this.version = source["version"];
      this.default_toolset = source["default_toolset"];
      this.default_autonomy = source["default_autonomy"];
      this.planning_only = source["planning_only"];
      this.builtin = source["builtin"];
    }
  }
  export class TaskRunEvent {
    kind: string;
    message: string;
    timestamp: string;
    detail?: string;

    static createFrom(source: any = {}) {
      return new TaskRunEvent(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.kind = source["kind"];
      this.message = source["message"];
      this.timestamp = source["timestamp"];
      this.detail = source["detail"];
    }
  }
  export class TaskToolEvent {
    name: string;
    input?: string;
    result?: string;
    status: string;
    timestamp: string;
    duration_ms?: number;

    static createFrom(source: any = {}) {
      return new TaskToolEvent(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.name = source["name"];
      this.input = source["input"];
      this.result = source["result"];
      this.status = source["status"];
      this.timestamp = source["timestamp"];
      this.duration_ms = source["duration_ms"];
    }
  }
  export class AgentEvalResult {
    name: string;
    attempt?: number;
    pass: boolean;
    artifact_pass: boolean;
    hygiene_pass: boolean;
    status_pass: boolean;
    status: string;
    tool_calls: number;
    tool_success_rate: number;
    auto_continues: number;
    truncations: number;
    tool_errors: number;
    repeated_tool_inputs: number;
    repeated_skips: number;
    repeat_tool_rate: number;
    verifier_prompts: number;
    max_routed_tools: number;
    prompt_warnings: number;
    stability_score: number;
    false_done: boolean;
    policy_violations: number;
    human_interventions: number;
    recovery_events: number;
    recovered: boolean;
    duration_ms: number;
    fail_reason?: string;
    runtime_pass?: boolean;
    desktop_screenshot?: string;
    mobile_screenshot?: string;
    runtime_failures?: string[];
    model_id?: string;
    provider?: string;
    context_tokens?: number;
    seed?: number;
    artifact_hash?: string;
    verifier_version?: string;
    stop_reason?: string;
    tool_trace?: TaskToolEvent[];
    response_excerpt?: string;
    event_trace?: TaskRunEvent[];

    static createFrom(source: any = {}) {
      return new AgentEvalResult(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.name = source["name"];
      this.attempt = source["attempt"];
      this.pass = source["pass"];
      this.artifact_pass = source["artifact_pass"];
      this.hygiene_pass = source["hygiene_pass"];
      this.status_pass = source["status_pass"];
      this.status = source["status"];
      this.tool_calls = source["tool_calls"];
      this.tool_success_rate = source["tool_success_rate"];
      this.auto_continues = source["auto_continues"];
      this.truncations = source["truncations"];
      this.tool_errors = source["tool_errors"];
      this.repeated_tool_inputs = source["repeated_tool_inputs"];
      this.repeated_skips = source["repeated_skips"];
      this.repeat_tool_rate = source["repeat_tool_rate"];
      this.verifier_prompts = source["verifier_prompts"];
      this.max_routed_tools = source["max_routed_tools"];
      this.prompt_warnings = source["prompt_warnings"];
      this.stability_score = source["stability_score"];
      this.false_done = source["false_done"];
      this.policy_violations = source["policy_violations"];
      this.human_interventions = source["human_interventions"];
      this.recovery_events = source["recovery_events"];
      this.recovered = source["recovered"];
      this.duration_ms = source["duration_ms"];
      this.fail_reason = source["fail_reason"];
      this.runtime_pass = source["runtime_pass"];
      this.desktop_screenshot = source["desktop_screenshot"];
      this.mobile_screenshot = source["mobile_screenshot"];
      this.runtime_failures = source["runtime_failures"];
      this.model_id = source["model_id"];
      this.provider = source["provider"];
      this.context_tokens = source["context_tokens"];
      this.seed = source["seed"];
      this.artifact_hash = source["artifact_hash"];
      this.verifier_version = source["verifier_version"];
      this.stop_reason = source["stop_reason"];
      this.tool_trace = this.convertValues(source["tool_trace"], TaskToolEvent);
      this.response_excerpt = source["response_excerpt"];
      this.event_trace = this.convertValues(
        source["event_trace"],
        TaskRunEvent,
      );
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a;
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs));
      } else if ("object" === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key]);
          }
          return a;
        }
        return new classs(a);
      }
      return a;
    }
  }
  export class AgentEvalReport {
    results: AgentEvalResult[];
    pass_count: number;
    total: number;
    profile: string;
    id?: string;
    created_at?: string;
    repeats: number;
    fixture_count: number;
    fixture_pass_count: number;
    pass_power: string;
    full_pass: boolean;
    unsupported_completion_rate: number;
    duplicate_action_rate: number;
    tool_error_rate: number;
    recovery_success_rate: number;
    average_tool_calls: number;
    average_duration_ms: number;
    policy_violations: number;
    human_interventions: number;

    static createFrom(source: any = {}) {
      return new AgentEvalReport(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.results = this.convertValues(source["results"], AgentEvalResult);
      this.pass_count = source["pass_count"];
      this.total = source["total"];
      this.profile = source["profile"];
      this.id = source["id"];
      this.created_at = source["created_at"];
      this.repeats = source["repeats"];
      this.fixture_count = source["fixture_count"];
      this.fixture_pass_count = source["fixture_pass_count"];
      this.pass_power = source["pass_power"];
      this.full_pass = source["full_pass"];
      this.unsupported_completion_rate = source["unsupported_completion_rate"];
      this.duplicate_action_rate = source["duplicate_action_rate"];
      this.tool_error_rate = source["tool_error_rate"];
      this.recovery_success_rate = source["recovery_success_rate"];
      this.average_tool_calls = source["average_tool_calls"];
      this.average_duration_ms = source["average_duration_ms"];
      this.policy_violations = source["policy_violations"];
      this.human_interventions = source["human_interventions"];
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a;
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs));
      } else if ("object" === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key]);
          }
          return a;
        }
        return new classs(a);
      }
      return a;
    }
  }

  export class AgentSession {
    id: string;
    kind: string;
    state: string;
    port?: number;
    lhost?: string;
    command?: string;
    user?: string;
    hostname?: string;
    started_at?: string;
    updated_at?: string;
    last_evidence?: string;
    terminal_session?: string;

    static createFrom(source: any = {}) {
      return new AgentSession(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.id = source["id"];
      this.kind = source["kind"];
      this.state = source["state"];
      this.port = source["port"];
      this.lhost = source["lhost"];
      this.command = source["command"];
      this.user = source["user"];
      this.hostname = source["hostname"];
      this.started_at = source["started_at"];
      this.updated_at = source["updated_at"];
      this.last_evidence = source["last_evidence"];
      this.terminal_session = source["terminal_session"];
    }
  }
  export class AudioHealth {
    enabled: boolean;
    overall: string;
    configured_tts: string;
    actual_tts: string;
    voice: string;
    stt_engine: string;
    stt_ready: boolean;
    worker_state: string;
    worker_pid: number;
    last_success: string;
    last_error: string;
    speak_replies: boolean;
    worker_hidden: boolean;
    stt_worker_state: string;
    stt_worker_pid: number;
    stt_model: string;
    stt_last_duration_ms: number;
    stt_last_audio_ms: number;
    stt_last_success: string;
    stt_last_error: string;

    static createFrom(source: any = {}) {
      return new AudioHealth(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.enabled = source["enabled"];
      this.overall = source["overall"];
      this.configured_tts = source["configured_tts"];
      this.actual_tts = source["actual_tts"];
      this.voice = source["voice"];
      this.stt_engine = source["stt_engine"];
      this.stt_ready = source["stt_ready"];
      this.worker_state = source["worker_state"];
      this.worker_pid = source["worker_pid"];
      this.last_success = source["last_success"];
      this.last_error = source["last_error"];
      this.speak_replies = source["speak_replies"];
      this.worker_hidden = source["worker_hidden"];
      this.stt_worker_state = source["stt_worker_state"];
      this.stt_worker_pid = source["stt_worker_pid"];
      this.stt_model = source["stt_model"];
      this.stt_last_duration_ms = source["stt_last_duration_ms"];
      this.stt_last_audio_ms = source["stt_last_audio_ms"];
      this.stt_last_success = source["stt_last_success"];
      this.stt_last_error = source["stt_last_error"];
    }
  }
  export class BenchmarkCase {
    name: string;
    status: string;
    summary: string;
    prompt_tokens?: number;
    completion_tokens?: number;
    iteration?: number;
    ttf_ms?: number;
    total_ms?: number;
    prompt_ms?: number;
    decode_ms?: number;
    prompt_tokens_per_second?: number;
    decode_tokens_per_second?: number;
    end_to_end_tokens_per_second?: number;
    timing_source?: string;
    tokens_per_second?: number;
    structured_tools?: number;
    repaired_tools?: number;
    inline_tool_markup?: boolean;
    output_leak?: boolean;
    valid_json?: boolean;
    expected_json?: boolean;
    response_chars?: number;
    error?: string;

    static createFrom(source: any = {}) {
      return new BenchmarkCase(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.name = source["name"];
      this.status = source["status"];
      this.summary = source["summary"];
      this.prompt_tokens = source["prompt_tokens"];
      this.completion_tokens = source["completion_tokens"];
      this.iteration = source["iteration"];
      this.ttf_ms = source["ttf_ms"];
      this.total_ms = source["total_ms"];
      this.prompt_ms = source["prompt_ms"];
      this.decode_ms = source["decode_ms"];
      this.prompt_tokens_per_second = source["prompt_tokens_per_second"];
      this.decode_tokens_per_second = source["decode_tokens_per_second"];
      this.end_to_end_tokens_per_second =
        source["end_to_end_tokens_per_second"];
      this.timing_source = source["timing_source"];
      this.tokens_per_second = source["tokens_per_second"];
      this.structured_tools = source["structured_tools"];
      this.repaired_tools = source["repaired_tools"];
      this.inline_tool_markup = source["inline_tool_markup"];
      this.output_leak = source["output_leak"];
      this.valid_json = source["valid_json"];
      this.expected_json = source["expected_json"];
      this.response_chars = source["response_chars"];
      this.error = source["error"];
    }
  }
  export class BenchmarkSpecInput {
    name: string;
    system: string;
    user: string;
    max_tokens: number;
    temperature: number;
    top_p: number;
    top_k: number;
    min_p: number;
    presence_penalty: number;
    seed: number;
    expect_json: boolean;
    tool_mode: string;

    static createFrom(source: any = {}) {
      return new BenchmarkSpecInput(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.name = source["name"];
      this.system = source["system"];
      this.user = source["user"];
      this.max_tokens = source["max_tokens"];
      this.temperature = source["temperature"];
      this.top_p = source["top_p"];
      this.top_k = source["top_k"];
      this.min_p = source["min_p"];
      this.presence_penalty = source["presence_penalty"];
      this.seed = source["seed"];
      this.expect_json = source["expect_json"];
      this.tool_mode = source["tool_mode"];
    }
  }
  export class ChatAttachment {
    id?: string;
    name: string;
    kind: string;
    mime?: string;
    content?: string;
    path?: string;
    size?: number;
    truncated?: boolean;

    static createFrom(source: any = {}) {
      return new ChatAttachment(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.id = source["id"];
      this.name = source["name"];
      this.kind = source["kind"];
      this.mime = source["mime"];
      this.content = source["content"];
      this.path = source["path"];
      this.size = source["size"];
      this.truncated = source["truncated"];
    }
  }
  export class ContextInspectionExclusion {
    path: string;
    display_path: string;
    source_bytes: number;
    reason: string;
    large: boolean;

    static createFrom(source: any = {}) {
      return new ContextInspectionExclusion(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.path = source["path"];
      this.display_path = source["display_path"];
      this.source_bytes = source["source_bytes"];
      this.reason = source["reason"];
      this.large = source["large"];
    }
  }
  export class ProjectInstructionRange {
    start_line: number;
    end_line: number;

    static createFrom(source: any = {}) {
      return new ProjectInstructionRange(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.start_line = source["start_line"];
      this.end_line = source["end_line"];
    }
  }
  export class ContextInspectionSource {
    path: string;
    display_path: string;
    sha256: string;
    reason: string;
    trust: string;
    source_bytes: number;
    prompt_bytes: number;
    estimated_tokens: number;
    partial: boolean;
    excerpt_ranges: ProjectInstructionRange[];

    static createFrom(source: any = {}) {
      return new ContextInspectionSource(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.path = source["path"];
      this.display_path = source["display_path"];
      this.sha256 = source["sha256"];
      this.reason = source["reason"];
      this.trust = source["trust"];
      this.source_bytes = source["source_bytes"];
      this.prompt_bytes = source["prompt_bytes"];
      this.estimated_tokens = source["estimated_tokens"];
      this.partial = source["partial"];
      this.excerpt_ranges = this.convertValues(
        source["excerpt_ranges"],
        ProjectInstructionRange,
      );
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a;
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs));
      } else if ("object" === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key]);
          }
          return a;
        }
        return new classs(a);
      }
      return a;
    }
  }
  export class ContextInspectionBudget {
    core_system_tokens: number;
    project_document_tokens: number;
    tool_schema_tokens: number;
    memory_progress_tokens: number;
    skill_tokens: number;
    user_profile_tokens: number;
    conversation_tokens: number;
    user_task_tokens: number;
    total_preflight_tokens: number;
    remaining_working_tokens: number;
    usage_percent: number;

    static createFrom(source: any = {}) {
      return new ContextInspectionBudget(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.core_system_tokens = source["core_system_tokens"];
      this.project_document_tokens = source["project_document_tokens"];
      this.tool_schema_tokens = source["tool_schema_tokens"];
      this.memory_progress_tokens = source["memory_progress_tokens"];
      this.skill_tokens = source["skill_tokens"];
      this.user_profile_tokens = source["user_profile_tokens"];
      this.conversation_tokens = source["conversation_tokens"];
      this.user_task_tokens = source["user_task_tokens"];
      this.total_preflight_tokens = source["total_preflight_tokens"];
      this.remaining_working_tokens = source["remaining_working_tokens"];
      this.usage_percent = source["usage_percent"];
    }
  }
  export class ContextInspection {
    generated_at: string;
    task_text: string;
    requested_class: string;
    effective_class: string;
    pinned_next_class?: string;
    policy: string;
    route_id?: string;
    profile_name: string;
    model_id: string;
    agent_mode: string;
    tool_choice: string;
    tool_count: number;
    tool_names: string[];
    tool_schema_sha256?: string;
    packet_sha256?: string;
    context_window_tokens: number;
    working_context_tokens: number;
    output_reserve_tokens: number;
    model_max_output_tokens: number;
    packet_limit_tokens: number;
    packet_limit_bytes: number;
    manifest_status: string;
    manifest_path?: string;
    manifest_sha256?: string;
    fallback_reason?: string;
    budget: ContextInspectionBudget;
    sources: ContextInspectionSource[];
    excluded_sources: ContextInspectionExclusion[];
    warnings: string[];
    synopsis: string;

    static createFrom(source: any = {}) {
      return new ContextInspection(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.generated_at = source["generated_at"];
      this.task_text = source["task_text"];
      this.requested_class = source["requested_class"];
      this.effective_class = source["effective_class"];
      this.pinned_next_class = source["pinned_next_class"];
      this.policy = source["policy"];
      this.route_id = source["route_id"];
      this.profile_name = source["profile_name"];
      this.model_id = source["model_id"];
      this.agent_mode = source["agent_mode"];
      this.tool_choice = source["tool_choice"];
      this.tool_count = source["tool_count"];
      this.tool_names = source["tool_names"];
      this.tool_schema_sha256 = source["tool_schema_sha256"];
      this.packet_sha256 = source["packet_sha256"];
      this.context_window_tokens = source["context_window_tokens"];
      this.working_context_tokens = source["working_context_tokens"];
      this.output_reserve_tokens = source["output_reserve_tokens"];
      this.model_max_output_tokens = source["model_max_output_tokens"];
      this.packet_limit_tokens = source["packet_limit_tokens"];
      this.packet_limit_bytes = source["packet_limit_bytes"];
      this.manifest_status = source["manifest_status"];
      this.manifest_path = source["manifest_path"];
      this.manifest_sha256 = source["manifest_sha256"];
      this.fallback_reason = source["fallback_reason"];
      this.budget = this.convertValues(
        source["budget"],
        ContextInspectionBudget,
      );
      this.sources = this.convertValues(
        source["sources"],
        ContextInspectionSource,
      );
      this.excluded_sources = this.convertValues(
        source["excluded_sources"],
        ContextInspectionExclusion,
      );
      this.warnings = source["warnings"];
      this.synopsis = source["synopsis"];
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a;
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs));
      } else if ("object" === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key]);
          }
          return a;
        }
        return new classs(a);
      }
      return a;
    }
  }

  export class ContextQualityVariantResult {
    prompt_index: number;
    pass: boolean;
    passed_repeats: number;
    repeats: number;
    stable: boolean;
    policy: string;
    effective_class: string;
    route_id?: string;
    agent_mode: string;
    project_tokens: number;
    tool_names: string[];
    packet_sha256?: string;
    tool_schema_sha256?: string;
    failures?: string[];

    static createFrom(source: any = {}) {
      return new ContextQualityVariantResult(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.prompt_index = source["prompt_index"];
      this.pass = source["pass"];
      this.passed_repeats = source["passed_repeats"];
      this.repeats = source["repeats"];
      this.stable = source["stable"];
      this.policy = source["policy"];
      this.effective_class = source["effective_class"];
      this.route_id = source["route_id"];
      this.agent_mode = source["agent_mode"];
      this.project_tokens = source["project_tokens"];
      this.tool_names = source["tool_names"];
      this.packet_sha256 = source["packet_sha256"];
      this.tool_schema_sha256 = source["tool_schema_sha256"];
      this.failures = source["failures"];
    }
  }
  export class ContextQualityFixtureResult {
    id: string;
    category: string;
    pass: boolean;
    pass_power: string;
    passed_repeats: number;
    repeats: number;
    variant_count: number;
    hostile_pass: boolean;
    policy_pass: boolean;
    tool_pass: boolean;
    source_pass: boolean;
    budget_pass: boolean;
    determinism_pass: boolean;
    variants: ContextQualityVariantResult[];
    failures?: string[];

    static createFrom(source: any = {}) {
      return new ContextQualityFixtureResult(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.id = source["id"];
      this.category = source["category"];
      this.pass = source["pass"];
      this.pass_power = source["pass_power"];
      this.passed_repeats = source["passed_repeats"];
      this.repeats = source["repeats"];
      this.variant_count = source["variant_count"];
      this.hostile_pass = source["hostile_pass"];
      this.policy_pass = source["policy_pass"];
      this.tool_pass = source["tool_pass"];
      this.source_pass = source["source_pass"];
      this.budget_pass = source["budget_pass"];
      this.determinism_pass = source["determinism_pass"];
      this.variants = this.convertValues(
        source["variants"],
        ContextQualityVariantResult,
      );
      this.failures = source["failures"];
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a;
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs));
      } else if ("object" === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key]);
          }
          return a;
        }
        return new classs(a);
      }
      return a;
    }
  }
  export class ContextQualityReport {
    id: string;
    created_at: string;
    profile: string;
    evaluation_envelope: string;
    repeats: number;
    pass_power: string;
    pass: boolean;
    pass_count: number;
    total: number;
    attempt_pass_count: number;
    attempt_total: number;
    hostile_pass: boolean;
    results: ContextQualityFixtureResult[];

    static createFrom(source: any = {}) {
      return new ContextQualityReport(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.id = source["id"];
      this.created_at = source["created_at"];
      this.profile = source["profile"];
      this.evaluation_envelope = source["evaluation_envelope"];
      this.repeats = source["repeats"];
      this.pass_power = source["pass_power"];
      this.pass = source["pass"];
      this.pass_count = source["pass_count"];
      this.total = source["total"];
      this.attempt_pass_count = source["attempt_pass_count"];
      this.attempt_total = source["attempt_total"];
      this.hostile_pass = source["hostile_pass"];
      this.results = this.convertValues(
        source["results"],
        ContextQualityFixtureResult,
      );
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a;
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs));
      } else if ("object" === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key]);
          }
          return a;
        }
        return new classs(a);
      }
      return a;
    }
  }

  export class DoctorCheck {
    name: string;
    status: string;
    message: string;
    detail?: string;

    static createFrom(source: any = {}) {
      return new DoctorCheck(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.name = source["name"];
      this.status = source["status"];
      this.message = source["message"];
      this.detail = source["detail"];
    }
  }
  export class DoctorResult {
    checks: DoctorCheck[];
    score: number;
    grade: string;

    static createFrom(source: any = {}) {
      return new DoctorResult(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.checks = this.convertValues(source["checks"], DoctorCheck);
      this.score = source["score"];
      this.grade = source["grade"];
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a;
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs));
      } else if ("object" === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key]);
          }
          return a;
        }
        return new classs(a);
      }
      return a;
    }
  }
  export class EngagementSetupArtifact {
    path: string;
    name: string;
    kind: string;
    size: number;
    modified_at: string;

    static createFrom(source: any = {}) {
      return new EngagementSetupArtifact(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.path = source["path"];
      this.name = source["name"];
      this.kind = source["kind"];
      this.size = source["size"];
      this.modified_at = source["modified_at"];
    }
  }
  export class EngagementSetupCheck {
    id: string;
    label: string;
    status: string;
    detail: string;
    blocking: boolean;

    static createFrom(source: any = {}) {
      return new EngagementSetupCheck(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.id = source["id"];
      this.label = source["label"];
      this.status = source["status"];
      this.detail = source["detail"];
      this.blocking = source["blocking"];
    }
  }
  export class EngagementSetupWorkflow {
    id: string;
    name: string;
    version: string;
    description: string;
    phase_count: number;
    checklist_name: string;
    checklist_version: string;
    check_count: number;

    static createFrom(source: any = {}) {
      return new EngagementSetupWorkflow(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.id = source["id"];
      this.name = source["name"];
      this.version = source["version"];
      this.description = source["description"];
      this.phase_count = source["phase_count"];
      this.checklist_name = source["checklist_name"];
      this.checklist_version = source["checklist_version"];
      this.check_count = source["check_count"];
    }
  }
  export class EngagementSetupPreview {
    project_name: string;
    project_id: string;
    workspace: string;
    target: string;
    hostname: string;
    scope: string[];
    scope_locked: boolean;
    active_profile: string;
    model_id: string;
    provider: string;
    provider_url: string;
    shell: string;
    vpn: string;
    workflows: EngagementSetupWorkflow[];
    candidate_artifacts: EngagementSetupArtifact[];
    checks: EngagementSetupCheck[];
    can_create: boolean;
    can_start: boolean;

    static createFrom(source: any = {}) {
      return new EngagementSetupPreview(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.project_name = source["project_name"];
      this.project_id = source["project_id"];
      this.workspace = source["workspace"];
      this.target = source["target"];
      this.hostname = source["hostname"];
      this.scope = source["scope"];
      this.scope_locked = source["scope_locked"];
      this.active_profile = source["active_profile"];
      this.model_id = source["model_id"];
      this.provider = source["provider"];
      this.provider_url = source["provider_url"];
      this.shell = source["shell"];
      this.vpn = source["vpn"];
      this.workflows = this.convertValues(
        source["workflows"],
        EngagementSetupWorkflow,
      );
      this.candidate_artifacts = this.convertValues(
        source["candidate_artifacts"],
        EngagementSetupArtifact,
      );
      this.checks = this.convertValues(source["checks"], EngagementSetupCheck);
      this.can_create = source["can_create"];
      this.can_start = source["can_start"];
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a;
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs));
      } else if ("object" === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key]);
          }
          return a;
        }
        return new classs(a);
      }
      return a;
    }
  }

  export class EngagementTargetProbe {
    target: string;
    status: string;
    detail: string;
    attempted?: string[];
    latency_ms?: number;
    http_status?: number;
    scope_match?: string;
    checked_at: string;

    static createFrom(source: any = {}) {
      return new EngagementTargetProbe(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.target = source["target"];
      this.status = source["status"];
      this.detail = source["detail"];
      this.attempted = source["attempted"];
      this.latency_ms = source["latency_ms"];
      this.http_status = source["http_status"];
      this.scope_match = source["scope_match"];
      this.checked_at = source["checked_at"];
    }
  }
  export class FileNode {
    name: string;
    path: string;
    isDir: boolean;
    children?: FileNode[];

    static createFrom(source: any = {}) {
      return new FileNode(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.name = source["name"];
      this.path = source["path"];
      this.isDir = source["isDir"];
      this.children = this.convertValues(source["children"], FileNode);
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a;
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs));
      } else if ("object" === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key]);
          }
          return a;
        }
        return new classs(a);
      }
      return a;
    }
  }
  export class GrammarToolArgsProbeResult {
    profile: string;
    backend: string;
    model_id: string;
    supported: boolean;
    structured_call: boolean;
    valid_arguments: boolean;
    tool_name?: string;
    arguments?: string;
    text?: string;
    error?: string;
    recommendation: string;

    static createFrom(source: any = {}) {
      return new GrammarToolArgsProbeResult(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.profile = source["profile"];
      this.backend = source["backend"];
      this.model_id = source["model_id"];
      this.supported = source["supported"];
      this.structured_call = source["structured_call"];
      this.valid_arguments = source["valid_arguments"];
      this.tool_name = source["tool_name"];
      this.arguments = source["arguments"];
      this.text = source["text"];
      this.error = source["error"];
      this.recommendation = source["recommendation"];
    }
  }
  export class HistoryStats {
    token_count: number;
    budget: number;
    fraction: number;
    rollback_len: number;
    window: number;
    reserve: number;
    configured_window?: number;

    static createFrom(source: any = {}) {
      return new HistoryStats(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.token_count = source["token_count"];
      this.budget = source["budget"];
      this.fraction = source["fraction"];
      this.rollback_len = source["rollback_len"];
      this.window = source["window"];
      this.reserve = source["reserve"];
      this.configured_window = source["configured_window"];
    }
  }
  export class JHUTBrowserReport {
    pass: boolean;
    url: string;
    desktop_screenshot: string;
    mobile_screenshot: string;
    canvas_width: number;
    canvas_height: number;
    pixel_variance: number;
    pixel_coverage: number;
    orbit_changed: boolean;
    responsive: boolean;
    console_errors: string[];
    runtime_errors: string[];
    failures: string[];
    verifier_version: string;

    static createFrom(source: any = {}) {
      return new JHUTBrowserReport(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.pass = source["pass"];
      this.url = source["url"];
      this.desktop_screenshot = source["desktop_screenshot"];
      this.mobile_screenshot = source["mobile_screenshot"];
      this.canvas_width = source["canvas_width"];
      this.canvas_height = source["canvas_height"];
      this.pixel_variance = source["pixel_variance"];
      this.pixel_coverage = source["pixel_coverage"];
      this.orbit_changed = source["orbit_changed"];
      this.responsive = source["responsive"];
      this.console_errors = source["console_errors"];
      this.runtime_errors = source["runtime_errors"];
      this.failures = source["failures"];
      this.verifier_version = source["verifier_version"];
    }
  }
  export class LabStatus {
    agent_root: string;
    lab_id: string;
    lab_name: string;
    shell_backend: string;
    shell_distro: string;
    shell_user: string;
    target: string;
    hostname: string;
    vpn_interface: string;
    vpn_ip: string;
    vpn_cidr: string;
    vpn_kind: string;
    latest_artifact: string;
    ops_profile: string;
    evidence_policy: string;
    access_preference: string;
    notes: string;
    listener_backend: string;
    listener_command: string;
    lhost_source: string;
    manual_lhost: string;
    open_folders: settings.WorkspaceFolder[];

    static createFrom(source: any = {}) {
      return new LabStatus(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.agent_root = source["agent_root"];
      this.lab_id = source["lab_id"];
      this.lab_name = source["lab_name"];
      this.shell_backend = source["shell_backend"];
      this.shell_distro = source["shell_distro"];
      this.shell_user = source["shell_user"];
      this.target = source["target"];
      this.hostname = source["hostname"];
      this.vpn_interface = source["vpn_interface"];
      this.vpn_ip = source["vpn_ip"];
      this.vpn_cidr = source["vpn_cidr"];
      this.vpn_kind = source["vpn_kind"];
      this.latest_artifact = source["latest_artifact"];
      this.ops_profile = source["ops_profile"];
      this.evidence_policy = source["evidence_policy"];
      this.access_preference = source["access_preference"];
      this.notes = source["notes"];
      this.listener_backend = source["listener_backend"];
      this.listener_command = source["listener_command"];
      this.lhost_source = source["lhost_source"];
      this.manual_lhost = source["manual_lhost"];
      this.open_folders = this.convertValues(
        source["open_folders"],
        settings.WorkspaceFolder,
      );
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a;
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs));
      } else if ("object" === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key]);
          }
          return a;
        }
        return new classs(a);
      }
      return a;
    }
  }
  export class LearningCandidate {
    id: string;
    run_id?: string;
    type: string;
    title: string;
    reason: string;
    content: string;
    kind: string;
    importance: number;
    tags: string[];
    evidence?: string[];
    template?: string;
    created_at: string;

    static createFrom(source: any = {}) {
      return new LearningCandidate(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.id = source["id"];
      this.run_id = source["run_id"];
      this.type = source["type"];
      this.title = source["title"];
      this.reason = source["reason"];
      this.content = source["content"];
      this.kind = source["kind"];
      this.importance = source["importance"];
      this.tags = source["tags"];
      this.evidence = source["evidence"];
      this.template = source["template"];
      this.created_at = source["created_at"];
    }
  }
  export class MaintenanceResult {
    summary: string;
    lines: string[];

    static createFrom(source: any = {}) {
      return new MaintenanceResult(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.summary = source["summary"];
      this.lines = source["lines"];
    }
  }
  export class MemoryEntry {
    id: string;
    scope: string;
    title: string;
    content: string;
    tags: string[];
    kind: string;
    confidence: string;
    source: string;
    importance: number;
    pinned: boolean;
    created_at: string;
    updated_at: string;
    last_used_at: string;

    static createFrom(source: any = {}) {
      return new MemoryEntry(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.id = source["id"];
      this.scope = source["scope"];
      this.title = source["title"];
      this.content = source["content"];
      this.tags = source["tags"];
      this.kind = source["kind"];
      this.confidence = source["confidence"];
      this.source = source["source"];
      this.importance = source["importance"];
      this.pinned = source["pinned"];
      this.created_at = source["created_at"];
      this.updated_at = source["updated_at"];
      this.last_used_at = source["last_used_at"];
    }
  }
  export class ModelProfileTemplateResult {
    matched: boolean;
    template_id?: string;
    family?: string;
    adapter?: string;
    tool_protocol?: string;
    chat_template?: string;
    requires_jinja?: boolean;
    huggingface_repo?: string;
    profile: settings.Profile;
    notes: string[];

    static createFrom(source: any = {}) {
      return new ModelProfileTemplateResult(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.matched = source["matched"];
      this.template_id = source["template_id"];
      this.family = source["family"];
      this.adapter = source["adapter"];
      this.tool_protocol = source["tool_protocol"];
      this.chat_template = source["chat_template"];
      this.requires_jinja = source["requires_jinja"];
      this.huggingface_repo = source["huggingface_repo"];
      this.profile = this.convertValues(source["profile"], settings.Profile);
      this.notes = source["notes"];
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a;
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs));
      } else if ("object" === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key]);
          }
          return a;
        }
        return new classs(a);
      }
      return a;
    }
  }
  export class ProfileBenchmarkResult {
    status: string;
    id?: string;
    created_at?: string;
    profile_name?: string;
    provider_name?: string;
    model_id?: string;
    ctx_tokens?: number;
    actual_ctx_tokens?: number;
    context_tier?: string;
    context_role?: string;
    score?: number;
    summary: string;
    notes: string[];
    recommended_profile: settings.Profile;
    scenarios: BenchmarkCase[];
    prompt_tokens?: number;
    completion_tokens?: number;
    ttf_ms?: number;
    total_ms?: number;
    load_ms?: number;
    warmup?: BenchmarkCase;
    measured_text_runs?: number;
    text_ttf_ms?: number;
    text_tokens_per_second?: number;
    decode_tokens_per_second?: number;
    end_to_end_tokens_per_second?: number;
    prompt_tokens_per_second?: number;
    prompt_ms?: number;
    decode_ms?: number;
    timing_source?: string;
    tokens_per_second?: number;

    static createFrom(source: any = {}) {
      return new ProfileBenchmarkResult(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.status = source["status"];
      this.id = source["id"];
      this.created_at = source["created_at"];
      this.profile_name = source["profile_name"];
      this.provider_name = source["provider_name"];
      this.model_id = source["model_id"];
      this.ctx_tokens = source["ctx_tokens"];
      this.actual_ctx_tokens = source["actual_ctx_tokens"];
      this.context_tier = source["context_tier"];
      this.context_role = source["context_role"];
      this.score = source["score"];
      this.summary = source["summary"];
      this.notes = source["notes"];
      this.recommended_profile = this.convertValues(
        source["recommended_profile"],
        settings.Profile,
      );
      this.scenarios = this.convertValues(source["scenarios"], BenchmarkCase);
      this.prompt_tokens = source["prompt_tokens"];
      this.completion_tokens = source["completion_tokens"];
      this.ttf_ms = source["ttf_ms"];
      this.total_ms = source["total_ms"];
      this.load_ms = source["load_ms"];
      this.warmup = this.convertValues(source["warmup"], BenchmarkCase);
      this.measured_text_runs = source["measured_text_runs"];
      this.text_ttf_ms = source["text_ttf_ms"];
      this.text_tokens_per_second = source["text_tokens_per_second"];
      this.decode_tokens_per_second = source["decode_tokens_per_second"];
      this.end_to_end_tokens_per_second =
        source["end_to_end_tokens_per_second"];
      this.prompt_tokens_per_second = source["prompt_tokens_per_second"];
      this.prompt_ms = source["prompt_ms"];
      this.decode_ms = source["decode_ms"];
      this.timing_source = source["timing_source"];
      this.tokens_per_second = source["tokens_per_second"];
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a;
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs));
      } else if ("object" === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key]);
          }
          return a;
        }
        return new classs(a);
      }
      return a;
    }
  }

  export class TaskRun {
    id: string;
    prompt: string;
    mode: string;
    profile: string;
    model?: string;
    context_packet_class?: string;
    claimant_id?: string;
    claimant_alias?: string;
    origin?: string;
    contract?: controlplane.TaskContract;
    control?: controlplane.MachineState;
    status: string;
    state?: string;
    stop_reason?: string;
    stop_detail?: string;
    started_at: string;
    ended_at?: string;
    duration_ms?: number;
    prompt_tokens?: number;
    completion_tokens?: number;
    total_tokens?: number;
    summary?: string;
    response?: string;
    tools?: TaskToolEvent[];
    events?: TaskRunEvent[];

    static createFrom(source: any = {}) {
      return new TaskRun(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.id = source["id"];
      this.prompt = source["prompt"];
      this.mode = source["mode"];
      this.profile = source["profile"];
      this.model = source["model"];
      this.context_packet_class = source["context_packet_class"];
      this.claimant_id = source["claimant_id"];
      this.claimant_alias = source["claimant_alias"];
      this.origin = source["origin"];
      this.contract = this.convertValues(
        source["contract"],
        controlplane.TaskContract,
      );
      this.control = this.convertValues(
        source["control"],
        controlplane.MachineState,
      );
      this.status = source["status"];
      this.state = source["state"];
      this.stop_reason = source["stop_reason"];
      this.stop_detail = source["stop_detail"];
      this.started_at = source["started_at"];
      this.ended_at = source["ended_at"];
      this.duration_ms = source["duration_ms"];
      this.prompt_tokens = source["prompt_tokens"];
      this.completion_tokens = source["completion_tokens"];
      this.total_tokens = source["total_tokens"];
      this.summary = source["summary"];
      this.response = source["response"];
      this.tools = this.convertValues(source["tools"], TaskToolEvent);
      this.events = this.convertValues(source["events"], TaskRunEvent);
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a;
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs));
      } else if ("object" === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key]);
          }
          return a;
        }
        return new classs(a);
      }
      return a;
    }
  }
  export class RunCheckpoint {
    run_id: string;
    prompt: string;
    mode: string;
    profile: string;
    messages: llm.Message[];
    run: TaskRun;
    saved_at: string;

    static createFrom(source: any = {}) {
      return new RunCheckpoint(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.run_id = source["run_id"];
      this.prompt = source["prompt"];
      this.mode = source["mode"];
      this.profile = source["profile"];
      this.messages = this.convertValues(source["messages"], llm.Message);
      this.run = this.convertValues(source["run"], TaskRun);
      this.saved_at = source["saved_at"];
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a;
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs));
      } else if ("object" === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key]);
          }
          return a;
        }
        return new classs(a);
      }
      return a;
    }
  }
  export class ServiceHealth {
    id: string;
    name: string;
    status: string;
    summary: string;
    detail?: string;
    updated_at: string;
    metadata?: Record<string, string>;

    static createFrom(source: any = {}) {
      return new ServiceHealth(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.id = source["id"];
      this.name = source["name"];
      this.status = source["status"];
      this.summary = source["summary"];
      this.detail = source["detail"];
      this.updated_at = source["updated_at"];
      this.metadata = source["metadata"];
    }
  }
  export class SessionChatMessage {
    role: string;
    content: string;
    images?: string[];
    attachments?: ChatAttachment[];

    static createFrom(source: any = {}) {
      return new SessionChatMessage(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.role = source["role"];
      this.content = source["content"];
      this.images = source["images"];
      this.attachments = this.convertValues(
        source["attachments"],
        ChatAttachment,
      );
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a;
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs));
      } else if ("object" === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key]);
          }
          return a;
        }
        return new classs(a);
      }
      return a;
    }
  }
  export class Skill {
    name: string;
    description: string;
    version: string;
    tags: string[];
    source_path: string;
    required_tools: string[];
    shell_backend: string;
    needs_network: boolean;
    needs_write: boolean;
    body: string;
    raw: string;
    created_at: string;
    updated_at: string;

    static createFrom(source: any = {}) {
      return new Skill(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.name = source["name"];
      this.description = source["description"];
      this.version = source["version"];
      this.tags = source["tags"];
      this.source_path = source["source_path"];
      this.required_tools = source["required_tools"];
      this.shell_backend = source["shell_backend"];
      this.needs_network = source["needs_network"];
      this.needs_write = source["needs_write"];
      this.body = source["body"];
      this.raw = source["raw"];
      this.created_at = source["created_at"];
      this.updated_at = source["updated_at"];
    }
  }
  export class SpecCalibrationSample {
    n: number;
    tok_per_sec: number;
    note?: string;

    static createFrom(source: any = {}) {
      return new SpecCalibrationSample(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.n = source["n"];
      this.tok_per_sec = source["tok_per_sec"];
      this.note = source["note"];
    }
  }
  export class SpecCalibration {
    key: string;
    model_id: string;
    best_n: number;
    tok_per_sec: number;
    baseline_tok_per_sec: number;
    speedup: number;
    ran_at: string;
    samples: SpecCalibrationSample[];

    static createFrom(source: any = {}) {
      return new SpecCalibration(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.key = source["key"];
      this.model_id = source["model_id"];
      this.best_n = source["best_n"];
      this.tok_per_sec = source["tok_per_sec"];
      this.baseline_tok_per_sec = source["baseline_tok_per_sec"];
      this.speedup = source["speedup"];
      this.ran_at = source["ran_at"];
      this.samples = this.convertValues(
        source["samples"],
        SpecCalibrationSample,
      );
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a;
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs));
      } else if ("object" === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key]);
          }
          return a;
        }
        return new classs(a);
      }
      return a;
    }
  }

  export class SpecPlan {
    enabled: boolean;
    spec_type: string;
    n_max: number;
    source: string;
    reason: string;
    locked: boolean;
    model_id: string;

    static createFrom(source: any = {}) {
      return new SpecPlan(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.enabled = source["enabled"];
      this.spec_type = source["spec_type"];
      this.n_max = source["n_max"];
      this.source = source["source"];
      this.reason = source["reason"];
      this.locked = source["locked"];
      this.model_id = source["model_id"];
    }
  }
  export class SpeechAudio {
    data_uri: string;
    engine: string;
    voice: string;

    static createFrom(source: any = {}) {
      return new SpeechAudio(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.data_uri = source["data_uri"];
      this.engine = source["engine"];
      this.voice = source["voice"];
    }
  }
  export class StorageItem {
    id: string;
    label: string;
    path: string;
    kind: string;
    bytes: number;
    size: string;
    clearable: boolean;
    description: string;

    static createFrom(source: any = {}) {
      return new StorageItem(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.id = source["id"];
      this.label = source["label"];
      this.path = source["path"];
      this.kind = source["kind"];
      this.bytes = source["bytes"];
      this.size = source["size"];
      this.clearable = source["clearable"];
      this.description = source["description"];
    }
  }

  export class TerminalRecoveryResult {
    status: string;
    summary: string;
    lines: string[];

    static createFrom(source: any = {}) {
      return new TerminalRecoveryResult(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.status = source["status"];
      this.summary = source["summary"];
      this.lines = source["lines"];
    }
  }
  export class TerminalStateSnapshot {
    session: string;
    state: string;
    summary: string;
    lines: string[];

    static createFrom(source: any = {}) {
      return new TerminalStateSnapshot(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.session = source["session"];
      this.state = source["state"];
      this.summary = source["summary"];
      this.lines = source["lines"];
    }
  }
  export class VPNInterfaceInfo {
    name: string;
    ip: string;
    cidr: string;
    kind: string;
    likely_vpn: boolean;
    label: string;

    static createFrom(source: any = {}) {
      return new VPNInterfaceInfo(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.name = source["name"];
      this.ip = source["ip"];
      this.cidr = source["cidr"];
      this.kind = source["kind"];
      this.likely_vpn = source["likely_vpn"];
      this.label = source["label"];
    }
  }
  export class VideoIngest {
    frames: string[];
    transcript: string;
    duration: number;
    frameCount: number;
    note: string;

    static createFrom(source: any = {}) {
      return new VideoIngest(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.frames = source["frames"];
      this.transcript = source["transcript"];
      this.duration = source["duration"];
      this.frameCount = source["frameCount"];
      this.note = source["note"];
    }
  }
}

export namespace channelbus {
  export class Attachment {
    kind: string;
    file_id?: string;
    file_name?: string;
    content_type?: string;
    path?: string;
    text?: string;

    static createFrom(source: any = {}) {
      return new Attachment(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.kind = source["kind"];
      this.file_id = source["file_id"];
      this.file_name = source["file_name"];
      this.content_type = source["content_type"];
      this.path = source["path"];
      this.text = source["text"];
    }
  }
  export class Envelope {
    id: string;
    source: string;
    session_id: string;
    user_id?: string;
    username?: string;
    text: string;
    attachments?: Attachment[];
    metadata?: Record<string, string>;
    created_at?: string;

    static createFrom(source: any = {}) {
      return new Envelope(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.id = source["id"];
      this.source = source["source"];
      this.session_id = source["session_id"];
      this.user_id = source["user_id"];
      this.username = source["username"];
      this.text = source["text"];
      this.attachments = this.convertValues(source["attachments"], Attachment);
      this.metadata = source["metadata"];
      this.created_at = source["created_at"];
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a;
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs));
      } else if ("object" === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key]);
          }
          return a;
        }
        return new classs(a);
      }
      return a;
    }
  }
  export class Response {
    lane: string;
    status: string;
    message: string;
    queued?: boolean;
    queue_id?: string;
    run_started?: boolean;
    data?: Record<string, string>;

    static createFrom(source: any = {}) {
      return new Response(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.lane = source["lane"];
      this.status = source["status"];
      this.message = source["message"];
      this.queued = source["queued"];
      this.queue_id = source["queue_id"];
      this.run_started = source["run_started"];
      this.data = source["data"];
    }
  }
  export class Route {
    lane: string;
    command?: string;
    argument?: string;
    policy?: string;
    read_only: boolean;
    reason?: string;
    project?: string;
    mode?: string;
    toolset?: string;
    from_voice?: boolean;

    static createFrom(source: any = {}) {
      return new Route(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.lane = source["lane"];
      this.command = source["command"];
      this.argument = source["argument"];
      this.policy = source["policy"];
      this.read_only = source["read_only"];
      this.reason = source["reason"];
      this.project = source["project"];
      this.mode = source["mode"];
      this.toolset = source["toolset"];
      this.from_voice = source["from_voice"];
    }
  }
  export class WorkItem {
    id: string;
    envelope: Envelope;
    route: Route;
    status: string;
    created_at: string;

    static createFrom(source: any = {}) {
      return new WorkItem(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.id = source["id"];
      this.envelope = this.convertValues(source["envelope"], Envelope);
      this.route = this.convertValues(source["route"], Route);
      this.status = source["status"];
      this.created_at = source["created_at"];
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a;
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs));
      } else if ("object" === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key]);
          }
          return a;
        }
        return new classs(a);
      }
      return a;
    }
  }
}

export namespace checkpacks {
  export class QualityIssue {
    check_id?: string;
    field: string;
    message: string;

    static createFrom(source: any = {}) {
      return new QualityIssue(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.check_id = source["check_id"];
      this.field = source["field"];
      this.message = source["message"];
    }
  }
}

export namespace controlplane {
  export class AcceptanceCheck {
    id: string;
    description: string;
    verifier: string;
    blocking: boolean;
    evidence_kinds?: string[];

    static createFrom(source: any = {}) {
      return new AcceptanceCheck(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.id = source["id"];
      this.description = source["description"];
      this.verifier = source["verifier"];
      this.blocking = source["blocking"];
      this.evidence_kinds = source["evidence_kinds"];
    }
  }
  export class Deliverable {
    id: string;
    description: string;
    kind?: string;

    static createFrom(source: any = {}) {
      return new Deliverable(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.id = source["id"];
      this.description = source["description"];
      this.kind = source["kind"];
    }
  }
  export class MachineState {
    version: number;
    contract_digest: string;
    contract_revision: number;
    phase: string;
    resume_phase?: string;
    revision: number;
    plan_required: boolean;
    plan_accepted: boolean;
    blocking_check_ids?: string[];
    satisfied_checks?: Record<string, Array<string>>;
    last_event?: string;
    last_detail?: string;
    updated_at: string;

    static createFrom(source: any = {}) {
      return new MachineState(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.version = source["version"];
      this.contract_digest = source["contract_digest"];
      this.contract_revision = source["contract_revision"];
      this.phase = source["phase"];
      this.resume_phase = source["resume_phase"];
      this.revision = source["revision"];
      this.plan_required = source["plan_required"];
      this.plan_accepted = source["plan_accepted"];
      this.blocking_check_ids = source["blocking_check_ids"];
      this.satisfied_checks = source["satisfied_checks"];
      this.last_event = source["last_event"];
      this.last_detail = source["last_detail"];
      this.updated_at = source["updated_at"];
    }
  }
  export class PathRule {
    root: string;
    access: string;

    static createFrom(source: any = {}) {
      return new PathRule(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.root = source["root"];
      this.access = source["access"];
    }
  }
  export class RunBudgets {
    max_tool_calls?: number;
    max_run_seconds?: number;

    static createFrom(source: any = {}) {
      return new RunBudgets(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.max_tool_calls = source["max_tool_calls"];
      this.max_run_seconds = source["max_run_seconds"];
    }
  }
  export class TaskContract {
    version: number;
    revision: number;
    parent_digest?: string;
    run_id: string;
    objective: string;
    workspace_root: string;
    deliverables?: Deliverable[];
    constraints?: string[];
    protected_resources?: string[];
    allowed_mutations?: PathRule[];
    acceptance_checks?: AcceptanceCheck[];
    required_evidence?: string[];
    risk: string;
    instruction_revision: number;
    plan_required: boolean;
    budgets: RunBudgets;
    approval_policy: string;
    completion_policy: string;
    created_at: string;
    digest: string;

    static createFrom(source: any = {}) {
      return new TaskContract(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.version = source["version"];
      this.revision = source["revision"];
      this.parent_digest = source["parent_digest"];
      this.run_id = source["run_id"];
      this.objective = source["objective"];
      this.workspace_root = source["workspace_root"];
      this.deliverables = this.convertValues(
        source["deliverables"],
        Deliverable,
      );
      this.constraints = source["constraints"];
      this.protected_resources = source["protected_resources"];
      this.allowed_mutations = this.convertValues(
        source["allowed_mutations"],
        PathRule,
      );
      this.acceptance_checks = this.convertValues(
        source["acceptance_checks"],
        AcceptanceCheck,
      );
      this.required_evidence = source["required_evidence"];
      this.risk = source["risk"];
      this.instruction_revision = source["instruction_revision"];
      this.plan_required = source["plan_required"];
      this.budgets = this.convertValues(source["budgets"], RunBudgets);
      this.approval_policy = source["approval_policy"];
      this.completion_policy = source["completion_policy"];
      this.created_at = source["created_at"];
      this.digest = source["digest"];
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a;
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs));
      } else if ("object" === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key]);
          }
          return a;
        }
        return new classs(a);
      }
      return a;
    }
  }
}

export namespace engagement {
  export class Claim {
    claimant: Claimant;
    // Go type: time
    claimed_at: any;
    // Go type: time
    heartbeat_at?: any;
    // Go type: time
    lease_until: any;

    static createFrom(source: any = {}) {
      return new Claim(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.claimant = this.convertValues(source["claimant"], Claimant);
      this.claimed_at = this.convertValues(source["claimed_at"], null);
      this.heartbeat_at = this.convertValues(source["heartbeat_at"], null);
      this.lease_until = this.convertValues(source["lease_until"], null);
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a;
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs));
      } else if ("object" === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key]);
          }
          return a;
        }
        return new classs(a);
      }
      return a;
    }
  }
  export class RunCount {
    Count: number;
    Indefinite: boolean;

    static createFrom(source: any = {}) {
      return new RunCount(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.Count = source["Count"];
      this.Indefinite = source["Indefinite"];
    }
  }
  export class Claimant {
    id: string;
    alias?: string;

    static createFrom(source: any = {}) {
      return new Claimant(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.id = source["id"];
      this.alias = source["alias"];
    }
  }
  export class Observation {
    run: number;
    status: string;
    text: string;
    claimant: Claimant;
    // Go type: time
    created_at: any;

    static createFrom(source: any = {}) {
      return new Observation(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.run = source["run"];
      this.status = source["status"];
      this.text = source["text"];
      this.claimant = this.convertValues(source["claimant"], Claimant);
      this.created_at = this.convertValues(source["created_at"], null);
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a;
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs));
      } else if ("object" === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key]);
          }
          return a;
        }
        return new classs(a);
      }
      return a;
    }
  }
  export class WorkRef {
    kind: string;
    phase_id?: string;
    endpoint_id?: string;
    id: string;

    static createFrom(source: any = {}) {
      return new WorkRef(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.kind = source["kind"];
      this.phase_id = source["phase_id"];
      this.endpoint_id = source["endpoint_id"];
      this.id = source["id"];
    }
  }
  export class WorkState {
    ref: WorkRef;
    title: string;
    status: string;
    observation?: string;
    observations?: Observation[];
    runs: RunCount;
    runs_completed: number;
    finished: boolean;
    claim?: Claim;
    revision: number;
    // Go type: time
    updated_at: any;

    static createFrom(source: any = {}) {
      return new WorkState(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.ref = this.convertValues(source["ref"], WorkRef);
      this.title = source["title"];
      this.status = source["status"];
      this.observation = source["observation"];
      this.observations = this.convertValues(
        source["observations"],
        Observation,
      );
      this.runs = this.convertValues(source["runs"], RunCount);
      this.runs_completed = source["runs_completed"];
      this.finished = source["finished"];
      this.claim = this.convertValues(source["claim"], Claim);
      this.revision = source["revision"];
      this.updated_at = this.convertValues(source["updated_at"], null);
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a;
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs));
      } else if ("object" === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key]);
          }
          return a;
        }
        return new classs(a);
      }
      return a;
    }
  }
  export class AvailableWork {
    phase_id: string;
    phase_name: string;
    parallel: boolean;
    items: WorkState[];
    blocker?: string;

    static createFrom(source: any = {}) {
      return new AvailableWork(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.phase_id = source["phase_id"];
      this.phase_name = source["phase_name"];
      this.parallel = source["parallel"];
      this.items = this.convertValues(source["items"], WorkState);
      this.blocker = source["blocker"];
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a;
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs));
      } else if ("object" === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key]);
          }
          return a;
        }
        return new classs(a);
      }
      return a;
    }
  }
  export class CheckApplicability {
    target_kinds?: string[];
    protocols?: string[];
    endpoint_kinds?: string[];
    technologies?: string[];
    any_features?: string[];
    requires_features?: string[];
    authentication?: string;

    static createFrom(source: any = {}) {
      return new CheckApplicability(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.target_kinds = source["target_kinds"];
      this.protocols = source["protocols"];
      this.endpoint_kinds = source["endpoint_kinds"];
      this.technologies = source["technologies"];
      this.any_features = source["any_features"];
      this.requires_features = source["requires_features"];
      this.authentication = source["authentication"];
    }
  }
  export class CheckAutomationReference {
    adapter: string;
    id: string;
    source?: string;

    static createFrom(source: any = {}) {
      return new CheckAutomationReference(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.adapter = source["adapter"];
      this.id = source["id"];
      this.source = source["source"];
    }
  }
  export class CheckEvidencePolicy {
    required_kinds?: string[];
    minimum?: number;
    reproduce?: boolean;
    negative_control?: boolean;

    static createFrom(source: any = {}) {
      return new CheckEvidencePolicy(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.required_kinds = source["required_kinds"];
      this.minimum = source["minimum"];
      this.reproduce = source["reproduce"];
      this.negative_control = source["negative_control"];
    }
  }
  export class CheckMapping {
    framework: string;
    id: string;
    url?: string;

    static createFrom(source: any = {}) {
      return new CheckMapping(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.framework = source["framework"];
      this.id = source["id"];
      this.url = source["url"];
    }
  }
  export class CheckDefinition {
    id: string;
    title: string;
    category?: string;
    category_name?: string;
    scope: string;
    description?: string;
    examples?: string;
    mappings?: CheckMapping[];
    applies_when?: CheckApplicability;
    prerequisites?: string[];
    procedure?: string[];
    expected_signals?: string[];
    negative_signals?: string[];
    false_positive_notes?: string[];
    safety?: string;
    evidence?: CheckEvidencePolicy;
    automation?: CheckAutomationReference[];
    tags?: string[];
    maturity?: string;
    verified?: boolean;
    deprecated?: boolean;
    superseded_by?: string;
    estimated_minutes?: number;
    repeatable?: boolean;
    runs?: RunCount;
    produces_endpoints?: boolean;

    static createFrom(source: any = {}) {
      return new CheckDefinition(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.id = source["id"];
      this.title = source["title"];
      this.category = source["category"];
      this.category_name = source["category_name"];
      this.scope = source["scope"];
      this.description = source["description"];
      this.examples = source["examples"];
      this.mappings = this.convertValues(source["mappings"], CheckMapping);
      this.applies_when = this.convertValues(
        source["applies_when"],
        CheckApplicability,
      );
      this.prerequisites = source["prerequisites"];
      this.procedure = source["procedure"];
      this.expected_signals = source["expected_signals"];
      this.negative_signals = source["negative_signals"];
      this.false_positive_notes = source["false_positive_notes"];
      this.safety = source["safety"];
      this.evidence = this.convertValues(
        source["evidence"],
        CheckEvidencePolicy,
      );
      this.automation = this.convertValues(
        source["automation"],
        CheckAutomationReference,
      );
      this.tags = source["tags"];
      this.maturity = source["maturity"];
      this.verified = source["verified"];
      this.deprecated = source["deprecated"];
      this.superseded_by = source["superseded_by"];
      this.estimated_minutes = source["estimated_minutes"];
      this.repeatable = source["repeatable"];
      this.runs = this.convertValues(source["runs"], RunCount);
      this.produces_endpoints = source["produces_endpoints"];
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a;
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs));
      } else if ("object" === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key]);
          }
          return a;
        }
        return new classs(a);
      }
      return a;
    }
  }

  export class DefinitionSource {
    id?: string;
    repository?: string;
    path?: string;
    ref?: string;
    commit?: string;
    license?: string;
    trust?: string;
    source_digest?: string;

    static createFrom(source: any = {}) {
      return new DefinitionSource(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.id = source["id"];
      this.repository = source["repository"];
      this.path = source["path"];
      this.ref = source["ref"];
      this.commit = source["commit"];
      this.license = source["license"];
      this.trust = source["trust"];
      this.source_digest = source["source_digest"];
    }
  }
  export class ChecklistDefinition {
    schema_version?: number;
    id: string;
    version?: string;
    name: string;
    description?: string;
    source?: DefinitionSource;
    items: CheckDefinition[];

    static createFrom(source: any = {}) {
      return new ChecklistDefinition(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.schema_version = source["schema_version"];
      this.id = source["id"];
      this.version = source["version"];
      this.name = source["name"];
      this.description = source["description"];
      this.source = this.convertValues(source["source"], DefinitionSource);
      this.items = this.convertValues(source["items"], CheckDefinition);
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a;
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs));
      } else if ("object" === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key]);
          }
          return a;
        }
        return new classs(a);
      }
      return a;
    }
  }

  export class Endpoint {
    id: string;
    method: string;
    url: string;
    name?: string;
    feature_group?: string;
    // Go type: time
    created_at: any;
    // Go type: time
    updated_at: any;

    static createFrom(source: any = {}) {
      return new Endpoint(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.id = source["id"];
      this.method = source["method"];
      this.url = source["url"];
      this.name = source["name"];
      this.feature_group = source["feature_group"];
      this.created_at = this.convertValues(source["created_at"], null);
      this.updated_at = this.convertValues(source["updated_at"], null);
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a;
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs));
      } else if ("object" === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key]);
          }
          return a;
        }
        return new classs(a);
      }
      return a;
    }
  }
  export class EndpointInput {
    id: string;
    method?: string;
    url: string;
    name?: string;
    feature_group?: string;

    static createFrom(source: any = {}) {
      return new EndpointInput(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.id = source["id"];
      this.method = source["method"];
      this.url = source["url"];
      this.name = source["name"];
      this.feature_group = source["feature_group"];
    }
  }
  export class Evidence {
    id: string;
    work: WorkRef;
    finding_id?: string;
    source_kind: string;
    ledger_event_id?: string;
    path?: string;
    sha256?: string;
    size?: number;
    agent_composed: boolean;
    description: string;
    run: number;
    created_by: Claimant;
    // Go type: time
    created_at: any;

    static createFrom(source: any = {}) {
      return new Evidence(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.id = source["id"];
      this.work = this.convertValues(source["work"], WorkRef);
      this.finding_id = source["finding_id"];
      this.source_kind = source["source_kind"];
      this.ledger_event_id = source["ledger_event_id"];
      this.path = source["path"];
      this.sha256 = source["sha256"];
      this.size = source["size"];
      this.agent_composed = source["agent_composed"];
      this.description = source["description"];
      this.run = source["run"];
      this.created_by = this.convertValues(source["created_by"], Claimant);
      this.created_at = this.convertValues(source["created_at"], null);
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a;
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs));
      } else if ("object" === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key]);
          }
          return a;
        }
        return new classs(a);
      }
      return a;
    }
  }
  export class EvidenceInput {
    id?: string;
    engagement_id: string;
    work: WorkRef;
    claimant: Claimant;
    source_kind: string;
    ledger_event_id?: string;
    path?: string;
    description: string;
    operator_trusted?: boolean;

    static createFrom(source: any = {}) {
      return new EvidenceInput(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.id = source["id"];
      this.engagement_id = source["engagement_id"];
      this.work = this.convertValues(source["work"], WorkRef);
      this.claimant = this.convertValues(source["claimant"], Claimant);
      this.source_kind = source["source_kind"];
      this.ledger_event_id = source["ledger_event_id"];
      this.path = source["path"];
      this.description = source["description"];
      this.operator_trusted = source["operator_trusted"];
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a;
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs));
      } else if ("object" === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key]);
          }
          return a;
        }
        return new classs(a);
      }
      return a;
    }
  }
  export class Finding {
    id: string;
    work: WorkRef;
    title: string;
    severity: string;
    state: string;
    description?: string;
    impact?: string;
    recommendation?: string;
    confidence?: string;
    reproduction?: string;
    evidence_ids: string[];
    operator_waiver?: string;
    created_by: Claimant;
    revision: number;
    // Go type: time
    created_at: any;
    // Go type: time
    updated_at: any;

    static createFrom(source: any = {}) {
      return new Finding(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.id = source["id"];
      this.work = this.convertValues(source["work"], WorkRef);
      this.title = source["title"];
      this.severity = source["severity"];
      this.state = source["state"];
      this.description = source["description"];
      this.impact = source["impact"];
      this.recommendation = source["recommendation"];
      this.confidence = source["confidence"];
      this.reproduction = source["reproduction"];
      this.evidence_ids = source["evidence_ids"];
      this.operator_waiver = source["operator_waiver"];
      this.created_by = this.convertValues(source["created_by"], Claimant);
      this.revision = source["revision"];
      this.created_at = this.convertValues(source["created_at"], null);
      this.updated_at = this.convertValues(source["updated_at"], null);
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a;
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs));
      } else if ("object" === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key]);
          }
          return a;
        }
        return new classs(a);
      }
      return a;
    }
  }
  export class FindingInput {
    id?: string;
    engagement_id: string;
    work: WorkRef;
    claimant: Claimant;
    title: string;
    severity: string;
    description?: string;
    impact?: string;
    recommendation?: string;
    confidence?: string;
    reproduction?: string;
    evidence_ids?: string[];
    expected_revision?: number;

    static createFrom(source: any = {}) {
      return new FindingInput(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.id = source["id"];
      this.engagement_id = source["engagement_id"];
      this.work = this.convertValues(source["work"], WorkRef);
      this.claimant = this.convertValues(source["claimant"], Claimant);
      this.title = source["title"];
      this.severity = source["severity"];
      this.description = source["description"];
      this.impact = source["impact"];
      this.recommendation = source["recommendation"];
      this.confidence = source["confidence"];
      this.reproduction = source["reproduction"];
      this.evidence_ids = source["evidence_ids"];
      this.expected_revision = source["expected_revision"];
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a;
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs));
      } else if ("object" === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key]);
          }
          return a;
        }
        return new classs(a);
      }
      return a;
    }
  }
  export class NextAction {
    action: string;
    phase_id?: string;
    phase_name?: string;
    work?: WorkState;
    phase_complete: boolean;
    workflow_done: boolean;

    static createFrom(source: any = {}) {
      return new NextAction(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.action = source["action"];
      this.phase_id = source["phase_id"];
      this.phase_name = source["phase_name"];
      this.work = this.convertValues(source["work"], WorkState);
      this.phase_complete = source["phase_complete"];
      this.workflow_done = source["workflow_done"];
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a;
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs));
      } else if ("object" === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key]);
          }
          return a;
        }
        return new classs(a);
      }
      return a;
    }
  }

  export class StepDefinition {
    id?: string;
    check?: string;
    title: string;
    description?: string;
    examples?: string;
    repeatable?: boolean;
    runs?: RunCount;

    static createFrom(source: any = {}) {
      return new StepDefinition(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.id = source["id"];
      this.check = source["check"];
      this.title = source["title"];
      this.description = source["description"];
      this.examples = source["examples"];
      this.repeatable = source["repeatable"];
      this.runs = this.convertValues(source["runs"], RunCount);
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a;
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs));
      } else if ("object" === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key]);
          }
          return a;
        }
        return new classs(a);
      }
      return a;
    }
  }
  export class PhaseDefinition {
    id: string;
    icon?: string;
    name: string;
    kind?: string;
    description?: string;
    optional?: boolean;
    free?: boolean;
    runs?: RunCount;
    steps: StepDefinition[];

    static createFrom(source: any = {}) {
      return new PhaseDefinition(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.id = source["id"];
      this.icon = source["icon"];
      this.name = source["name"];
      this.kind = source["kind"];
      this.description = source["description"];
      this.optional = source["optional"];
      this.free = source["free"];
      this.runs = this.convertValues(source["runs"], RunCount);
      this.steps = this.convertValues(source["steps"], StepDefinition);
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a;
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs));
      } else if ("object" === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key]);
          }
          return a;
        }
        return new classs(a);
      }
      return a;
    }
  }
  export class State {
    id: string;
    name: string;
    workflow_id: string;
    checklist_id: string;
    current_phase: string;
    scope: string[];
    scope_locked: boolean;
    notes?: string;
    notes_revision: number;
    // Go type: time
    notes_updated_at?: any;
    steps: Record<string, WorkState>;
    global_checks: Record<string, WorkState>;
    global_check_order: string[];
    endpoints: Record<string, Endpoint>;
    endpoint_order: string[];
    endpoint_checks: Record<string, WorkState>;
    endpoint_check_order: string[];
    evidence: Record<string, Evidence>;
    evidence_order: string[];
    findings: Record<string, Finding>;
    finding_order: string[];
    revision: number;
    // Go type: time
    created_at: any;
    // Go type: time
    updated_at: any;

    static createFrom(source: any = {}) {
      return new State(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.id = source["id"];
      this.name = source["name"];
      this.workflow_id = source["workflow_id"];
      this.checklist_id = source["checklist_id"];
      this.current_phase = source["current_phase"];
      this.scope = source["scope"];
      this.scope_locked = source["scope_locked"];
      this.notes = source["notes"];
      this.notes_revision = source["notes_revision"];
      this.notes_updated_at = this.convertValues(
        source["notes_updated_at"],
        null,
      );
      this.steps = this.convertValues(source["steps"], WorkState, true);
      this.global_checks = this.convertValues(
        source["global_checks"],
        WorkState,
        true,
      );
      this.global_check_order = source["global_check_order"];
      this.endpoints = this.convertValues(source["endpoints"], Endpoint, true);
      this.endpoint_order = source["endpoint_order"];
      this.endpoint_checks = this.convertValues(
        source["endpoint_checks"],
        WorkState,
        true,
      );
      this.endpoint_check_order = source["endpoint_check_order"];
      this.evidence = this.convertValues(source["evidence"], Evidence, true);
      this.evidence_order = source["evidence_order"];
      this.findings = this.convertValues(source["findings"], Finding, true);
      this.finding_order = source["finding_order"];
      this.revision = source["revision"];
      this.created_at = this.convertValues(source["created_at"], null);
      this.updated_at = this.convertValues(source["updated_at"], null);
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a;
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs));
      } else if ("object" === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key]);
          }
          return a;
        }
        return new classs(a);
      }
      return a;
    }
  }
  export class WorkflowDefinition {
    schema_version?: number;
    id: string;
    version?: string;
    name: string;
    description?: string;
    color?: string;
    checklist?: string;
    source?: DefinitionSource;
    phases: PhaseDefinition[];

    static createFrom(source: any = {}) {
      return new WorkflowDefinition(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.schema_version = source["schema_version"];
      this.id = source["id"];
      this.version = source["version"];
      this.name = source["name"];
      this.description = source["description"];
      this.color = source["color"];
      this.checklist = source["checklist"];
      this.source = this.convertValues(source["source"], DefinitionSource);
      this.phases = this.convertValues(source["phases"], PhaseDefinition);
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a;
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs));
      } else if ("object" === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key]);
          }
          return a;
        }
        return new classs(a);
      }
      return a;
    }
  }
  export class Record {
    workspace: string;
    workflow: WorkflowDefinition;
    checklist: ChecklistDefinition;
    state?: State;

    static createFrom(source: any = {}) {
      return new Record(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.workspace = source["workspace"];
      this.workflow = this.convertValues(
        source["workflow"],
        WorkflowDefinition,
      );
      this.checklist = this.convertValues(
        source["checklist"],
        ChecklistDefinition,
      );
      this.state = this.convertValues(source["state"], State);
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a;
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs));
      } else if ("object" === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key]);
          }
          return a;
        }
        return new classs(a);
      }
      return a;
    }
  }

  export class Summary {
    id: string;
    name: string;
    workspace: string;
    workflow_id: string;
    workflow_version?: string;
    checklist_id: string;
    checklist_version?: string;
    current_phase: string;
    revision: number;
    // Go type: time
    created_at: any;
    // Go type: time
    updated_at: any;

    static createFrom(source: any = {}) {
      return new Summary(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.id = source["id"];
      this.name = source["name"];
      this.workspace = source["workspace"];
      this.workflow_id = source["workflow_id"];
      this.workflow_version = source["workflow_version"];
      this.checklist_id = source["checklist_id"];
      this.checklist_version = source["checklist_version"];
      this.current_phase = source["current_phase"];
      this.revision = source["revision"];
      this.created_at = this.convertValues(source["created_at"], null);
      this.updated_at = this.convertValues(source["updated_at"], null);
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a;
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs));
      } else if ("object" === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key]);
          }
          return a;
        }
        return new classs(a);
      }
      return a;
    }
  }
}

export namespace ledger {
  export class Event {
    id: string;
    run_id?: string;
    kind: string;
    source?: string;
    tool?: string;
    status?: string;
    state?: string;
    message?: string;
    detail?: string;
    input?: string;
    output?: string;
    error?: string;
    duration_ms?: number;
    files?: string[];
    artifacts?: string[];
    metadata?: Record<string, string>;
    timestamp: string;

    static createFrom(source: any = {}) {
      return new Event(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.id = source["id"];
      this.run_id = source["run_id"];
      this.kind = source["kind"];
      this.source = source["source"];
      this.tool = source["tool"];
      this.status = source["status"];
      this.state = source["state"];
      this.message = source["message"];
      this.detail = source["detail"];
      this.input = source["input"];
      this.output = source["output"];
      this.error = source["error"];
      this.duration_ms = source["duration_ms"];
      this.files = source["files"];
      this.artifacts = source["artifacts"];
      this.metadata = source["metadata"];
      this.timestamp = source["timestamp"];
    }
  }
}

export namespace llm {
  export class FunctionCall {
    name: string;
    arguments: number[];

    static createFrom(source: any = {}) {
      return new FunctionCall(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.name = source["name"];
      this.arguments = source["arguments"];
    }
  }
  export class ToolCallDef {
    id: string;
    type: string;
    function: FunctionCall;

    static createFrom(source: any = {}) {
      return new ToolCallDef(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.id = source["id"];
      this.type = source["type"];
      this.function = this.convertValues(source["function"], FunctionCall);
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a;
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs));
      } else if ("object" === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key]);
          }
          return a;
        }
        return new classs(a);
      }
      return a;
    }
  }
  export class MessageAttachment {
    id?: string;
    name: string;
    kind: string;
    mime?: string;
    content?: string;
    path?: string;
    size?: number;
    truncated?: boolean;

    static createFrom(source: any = {}) {
      return new MessageAttachment(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.id = source["id"];
      this.name = source["name"];
      this.kind = source["kind"];
      this.mime = source["mime"];
      this.content = source["content"];
      this.path = source["path"];
      this.size = source["size"];
      this.truncated = source["truncated"];
    }
  }
  export class Message {
    role: string;
    content: any;
    display_content?: string;
    attachments?: MessageAttachment[];
    tool_call_id?: string;
    tool_calls?: ToolCallDef[];
    name?: string;

    static createFrom(source: any = {}) {
      return new Message(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.role = source["role"];
      this.content = source["content"];
      this.display_content = source["display_content"];
      this.attachments = this.convertValues(
        source["attachments"],
        MessageAttachment,
      );
      this.tool_call_id = source["tool_call_id"];
      this.tool_calls = this.convertValues(source["tool_calls"], ToolCallDef);
      this.name = source["name"];
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a;
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs));
      } else if ("object" === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key]);
          }
          return a;
        }
        return new classs(a);
      }
      return a;
    }
  }

  export class ModelMetadata {
    id: string;
    context_length?: number;
    max_completion_tokens?: number;
    supported_parameters?: string[];

    static createFrom(source: any = {}) {
      return new ModelMetadata(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.id = source["id"];
      this.context_length = source["context_length"];
      this.max_completion_tokens = source["max_completion_tokens"];
      this.supported_parameters = source["supported_parameters"];
    }
  }
}

export namespace packlibrary {
  export class CloneInput {
    source_key: string;
    scope: string;
    id: string;
    name: string;
    version?: string;

    static createFrom(source: any = {}) {
      return new CloneInput(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.source_key = source["source_key"];
      this.scope = source["scope"];
      this.id = source["id"];
      this.name = source["name"];
      this.version = source["version"];
    }
  }
  export class Summary {
    key: string;
    id: string;
    version: string;
    name: string;
    description?: string;
    scope: string;
    trust: string;
    license: string;
    path?: string;
    built_in: boolean;
    archived: boolean;
    active: boolean;
    valid: boolean;
    validation_error?: string;
    workflow_id: string;
    workflow_version: string;
    checklist_id: string;
    checklist_version: string;
    workflow_digest: string;
    checklist_digest: string;
    phase_count: number;
    check_count: number;
    global_checks: number;
    endpoint_checks: number;
    automated_checks: number;
    quality_score: number;
    quality_ready: boolean;
    quality_issues?: checkpacks.QualityIssue[];

    static createFrom(source: any = {}) {
      return new Summary(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.key = source["key"];
      this.id = source["id"];
      this.version = source["version"];
      this.name = source["name"];
      this.description = source["description"];
      this.scope = source["scope"];
      this.trust = source["trust"];
      this.license = source["license"];
      this.path = source["path"];
      this.built_in = source["built_in"];
      this.archived = source["archived"];
      this.active = source["active"];
      this.valid = source["valid"];
      this.validation_error = source["validation_error"];
      this.workflow_id = source["workflow_id"];
      this.workflow_version = source["workflow_version"];
      this.checklist_id = source["checklist_id"];
      this.checklist_version = source["checklist_version"];
      this.workflow_digest = source["workflow_digest"];
      this.checklist_digest = source["checklist_digest"];
      this.phase_count = source["phase_count"];
      this.check_count = source["check_count"];
      this.global_checks = source["global_checks"];
      this.endpoint_checks = source["endpoint_checks"];
      this.automated_checks = source["automated_checks"];
      this.quality_score = source["quality_score"];
      this.quality_ready = source["quality_ready"];
      this.quality_issues = this.convertValues(
        source["quality_issues"],
        checkpacks.QualityIssue,
      );
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a;
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs));
      } else if ("object" === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key]);
          }
          return a;
        }
        return new classs(a);
      }
      return a;
    }
  }
  export class Snapshot {
    packs: Summary[];
    personal_root: string;
    project_root?: string;
    built_in_count: number;
    active_count: number;
    archived_count: number;
    invalid_count: number;

    static createFrom(source: any = {}) {
      return new Snapshot(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.packs = this.convertValues(source["packs"], Summary);
      this.personal_root = source["personal_root"];
      this.project_root = source["project_root"];
      this.built_in_count = source["built_in_count"];
      this.active_count = source["active_count"];
      this.archived_count = source["archived_count"];
      this.invalid_count = source["invalid_count"];
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a;
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs));
      } else if ("object" === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key]);
          }
          return a;
        }
        return new classs(a);
      }
      return a;
    }
  }
}

export namespace sessionstore {
  export class SearchResult {
    session_id: string;
    session_name: string;
    message_id: number;
    role: string;
    content: string;
    tool_name?: string;
    rank?: string;
    updated_at?: string;

    static createFrom(source: any = {}) {
      return new SearchResult(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.session_id = source["session_id"];
      this.session_name = source["session_name"];
      this.message_id = source["message_id"];
      this.role = source["role"];
      this.content = source["content"];
      this.tool_name = source["tool_name"];
      this.rank = source["rank"];
      this.updated_at = source["updated_at"];
    }
  }
}

export namespace settings {
  export class AgentModePreset {
    enabled: boolean;
    profile: string;
    context_budget: number;
    autonomy: string;
    toolset: string;
    instructions: string;
    tool_permissions: Record<string, boolean>;

    static createFrom(source: any = {}) {
      return new AgentModePreset(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.enabled = source["enabled"];
      this.profile = source["profile"];
      this.context_budget = source["context_budget"];
      this.autonomy = source["autonomy"];
      this.toolset = source["toolset"];
      this.instructions = source["instructions"];
      this.tool_permissions = source["tool_permissions"];
    }
  }
  export class ReviewLoopConfig {
    enabled: boolean;
    only_autonomous: boolean;
    max_review_cycles: number;
    verify_gate: boolean;
    verify_commands: string[];
    verify_timeout_sec: number;
    completion_rails: boolean;
    completion_blocking: boolean;
    reviewer_pass: boolean;
    reviewer_max_tools: number;

    static createFrom(source: any = {}) {
      return new ReviewLoopConfig(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.enabled = source["enabled"];
      this.only_autonomous = source["only_autonomous"];
      this.max_review_cycles = source["max_review_cycles"];
      this.verify_gate = source["verify_gate"];
      this.verify_commands = source["verify_commands"];
      this.verify_timeout_sec = source["verify_timeout_sec"];
      this.completion_rails = source["completion_rails"];
      this.completion_blocking = source["completion_blocking"];
      this.reviewer_pass = source["reviewer_pass"];
      this.reviewer_max_tools = source["reviewer_max_tools"];
    }
  }
  export class AgentsConfig {
    mode_override: string;
    default_autonomy: string;
    offline_only: boolean;
    max_tool_calls: number;
    max_run_seconds: number;
    escalation_profile: string;
    require_plan: boolean;
    no_think_after_tool_calls: number;
    reasoning_effort: string;
    review_loop: ReviewLoopConfig;
    presets: Record<string, AgentModePreset>;

    static createFrom(source: any = {}) {
      return new AgentsConfig(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.mode_override = source["mode_override"];
      this.default_autonomy = source["default_autonomy"];
      this.offline_only = source["offline_only"];
      this.max_tool_calls = source["max_tool_calls"];
      this.max_run_seconds = source["max_run_seconds"];
      this.escalation_profile = source["escalation_profile"];
      this.require_plan = source["require_plan"];
      this.no_think_after_tool_calls = source["no_think_after_tool_calls"];
      this.reasoning_effort = source["reasoning_effort"];
      this.review_loop = this.convertValues(
        source["review_loop"],
        ReviewLoopConfig,
      );
      this.presets = this.convertValues(
        source["presets"],
        AgentModePreset,
        true,
      );
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a;
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs));
      } else if ("object" === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key]);
          }
          return a;
        }
        return new classs(a);
      }
      return a;
    }
  }
  export class AudioConfig {
    enabled: boolean;
    mode: string;
    stt_engine: string;
    tts_engine: string;
    voice: string;
    speed: number;
    input_device: string;
    vad_threshold: number;
    barge_in: boolean;
    speak_replies: boolean;
    speak_tool_notes: boolean;
    clause_min_chars: number;

    static createFrom(source: any = {}) {
      return new AudioConfig(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.enabled = source["enabled"];
      this.mode = source["mode"];
      this.stt_engine = source["stt_engine"];
      this.tts_engine = source["tts_engine"];
      this.voice = source["voice"];
      this.speed = source["speed"];
      this.input_device = source["input_device"];
      this.vad_threshold = source["vad_threshold"];
      this.barge_in = source["barge_in"];
      this.speak_replies = source["speak_replies"];
      this.speak_tool_notes = source["speak_tool_notes"];
      this.clause_min_chars = source["clause_min_chars"];
    }
  }
  export class LabProfile {
    id: string;
    name: string;
    workspace_dir: string;
    target: string;
    hostname: string;
    vpn_interface: string;
    latest_artifact: string;
    ops_profile: string;
    evidence_policy: string;
    access_preference: string;
    notes: string;

    static createFrom(source: any = {}) {
      return new LabProfile(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.id = source["id"];
      this.name = source["name"];
      this.workspace_dir = source["workspace_dir"];
      this.target = source["target"];
      this.hostname = source["hostname"];
      this.vpn_interface = source["vpn_interface"];
      this.latest_artifact = source["latest_artifact"];
      this.ops_profile = source["ops_profile"];
      this.evidence_policy = source["evidence_policy"];
      this.access_preference = source["access_preference"];
      this.notes = source["notes"];
    }
  }
  export class LabContext {
    id: string;
    name: string;
    target: string;
    hostname: string;
    vpn_interface: string;
    latest_artifact: string;
    ops_profile: string;
    evidence_policy: string;
    access_preference: string;
    notes: string;

    static createFrom(source: any = {}) {
      return new LabContext(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.id = source["id"];
      this.name = source["name"];
      this.target = source["target"];
      this.hostname = source["hostname"];
      this.vpn_interface = source["vpn_interface"];
      this.latest_artifact = source["latest_artifact"];
      this.ops_profile = source["ops_profile"];
      this.evidence_policy = source["evidence_policy"];
      this.access_preference = source["access_preference"];
      this.notes = source["notes"];
    }
  }
  export class WorkspacePreference {
    path: string;
    agent_mode: string;

    static createFrom(source: any = {}) {
      return new WorkspacePreference(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.path = source["path"];
      this.agent_mode = source["agent_mode"];
    }
  }
  export class WorkspaceFolder {
    path: string;
    name: string;
    role: string;

    static createFrom(source: any = {}) {
      return new WorkspaceFolder(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.path = source["path"];
      this.name = source["name"];
      this.role = source["role"];
    }
  }
  export class ContextConfig {
    auto_inject_file: boolean;
    auto_inject_cursor: boolean;
    compaction_at: number;
    show_compaction: boolean;
    mauler_md_path: string;
    project_doc_max_bytes: number;
    project_doc_fallback_filenames: string[];
    workspace_dir: string;
    open_folders: WorkspaceFolder[];
    workspace_preferences: WorkspacePreference[];
    lab: LabContext;
    active_lab_profile: string;
    lab_profiles: LabProfile[];

    static createFrom(source: any = {}) {
      return new ContextConfig(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.auto_inject_file = source["auto_inject_file"];
      this.auto_inject_cursor = source["auto_inject_cursor"];
      this.compaction_at = source["compaction_at"];
      this.show_compaction = source["show_compaction"];
      this.mauler_md_path = source["mauler_md_path"];
      this.project_doc_max_bytes = source["project_doc_max_bytes"];
      this.project_doc_fallback_filenames =
        source["project_doc_fallback_filenames"];
      this.workspace_dir = source["workspace_dir"];
      this.open_folders = this.convertValues(
        source["open_folders"],
        WorkspaceFolder,
      );
      this.workspace_preferences = this.convertValues(
        source["workspace_preferences"],
        WorkspacePreference,
      );
      this.lab = this.convertValues(source["lab"], LabContext);
      this.active_lab_profile = source["active_lab_profile"];
      this.lab_profiles = this.convertValues(
        source["lab_profiles"],
        LabProfile,
      );
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a;
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs));
      } else if ("object" === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key]);
          }
          return a;
        }
        return new classs(a);
      }
      return a;
    }
  }
  export class EnvironmentConfig {
    main_os: string;
    ai_shell_backend: string;
    ai_shell_distro: string;
    ai_shell_user: string;
    target_work_backend: string;
    listener_backend: string;
    listener_command: string;
    lhost_source: string;
    manual_lhost: string;
    prefer_terminal_tools: boolean;
    reverse_shell_guidance: string;
    user_correction_policy: string;

    static createFrom(source: any = {}) {
      return new EnvironmentConfig(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.main_os = source["main_os"];
      this.ai_shell_backend = source["ai_shell_backend"];
      this.ai_shell_distro = source["ai_shell_distro"];
      this.ai_shell_user = source["ai_shell_user"];
      this.target_work_backend = source["target_work_backend"];
      this.listener_backend = source["listener_backend"];
      this.listener_command = source["listener_command"];
      this.lhost_source = source["lhost_source"];
      this.manual_lhost = source["manual_lhost"];
      this.prefer_terminal_tools = source["prefer_terminal_tools"];
      this.reverse_shell_guidance = source["reverse_shell_guidance"];
      this.user_correction_policy = source["user_correction_policy"];
    }
  }
  export class GenerationParams {
    temperature: number;
    top_p: number;
    top_k: number;
    min_p: number;
    presence_penalty: number;
    repeat_penalty: number;
    max_tokens: number;
    seed: number;

    static createFrom(source: any = {}) {
      return new GenerationParams(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.temperature = source["temperature"];
      this.top_p = source["top_p"];
      this.top_k = source["top_k"];
      this.min_p = source["min_p"];
      this.presence_penalty = source["presence_penalty"];
      this.repeat_penalty = source["repeat_penalty"];
      this.max_tokens = source["max_tokens"];
      this.seed = source["seed"];
    }
  }
  export class ImageConfig {
    vision_enabled: boolean;
    clipboard_method: string;
    display_method: string;
    max_display_width: number;
    wsl_path_translate: boolean;
    video_enabled: boolean;
    video_max_frames: number;
    video_frame_width: number;
    video_transcribe: boolean;

    static createFrom(source: any = {}) {
      return new ImageConfig(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.vision_enabled = source["vision_enabled"];
      this.clipboard_method = source["clipboard_method"];
      this.display_method = source["display_method"];
      this.max_display_width = source["max_display_width"];
      this.wsl_path_translate = source["wsl_path_translate"];
      this.video_enabled = source["video_enabled"];
      this.video_max_frames = source["video_max_frames"];
      this.video_frame_width = source["video_frame_width"];
      this.video_transcribe = source["video_transcribe"];
    }
  }

  export class LoggingConfig {
    enabled: boolean;
    log_tool_inputs: boolean;
    log_tool_results: boolean;
    log_responses: boolean;
    max_runs: number;

    static createFrom(source: any = {}) {
      return new LoggingConfig(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.enabled = source["enabled"];
      this.log_tool_inputs = source["log_tool_inputs"];
      this.log_tool_results = source["log_tool_results"];
      this.log_responses = source["log_responses"];
      this.max_runs = source["max_runs"];
    }
  }
  export class MemoryConfig {
    enabled: boolean;
    auto_inject: boolean;
    disable_auto_distill: boolean;
    max_entries: number;
    max_inject: number;
    max_entry_chars: number;

    static createFrom(source: any = {}) {
      return new MemoryConfig(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.enabled = source["enabled"];
      this.auto_inject = source["auto_inject"];
      this.disable_auto_distill = source["disable_auto_distill"];
      this.max_entries = source["max_entries"];
      this.max_inject = source["max_inject"];
      this.max_entry_chars = source["max_entry_chars"];
    }
  }
  export class Profile {
    name: string;
    provider: string;
    model_id: string;
    ctx_tokens: number;
    thinking: boolean;
    preserve_thinking: boolean;
    mmproj: string;
    thinking_general: GenerationParams;
    thinking_coding: GenerationParams;
    nothinking: GenerationParams;
    spec_type: string;
    spec_draft_n_max: number;
    spec_draft_model: string;
    backend?: string;
    base_url?: string;
    api_key_env?: string;

    static createFrom(source: any = {}) {
      return new Profile(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.name = source["name"];
      this.provider = source["provider"];
      this.model_id = source["model_id"];
      this.ctx_tokens = source["ctx_tokens"];
      this.thinking = source["thinking"];
      this.preserve_thinking = source["preserve_thinking"];
      this.mmproj = source["mmproj"];
      this.thinking_general = this.convertValues(
        source["thinking_general"],
        GenerationParams,
      );
      this.thinking_coding = this.convertValues(
        source["thinking_coding"],
        GenerationParams,
      );
      this.nothinking = this.convertValues(
        source["nothinking"],
        GenerationParams,
      );
      this.spec_type = source["spec_type"];
      this.spec_draft_n_max = source["spec_draft_n_max"];
      this.spec_draft_model = source["spec_draft_model"];
      this.backend = source["backend"];
      this.base_url = source["base_url"];
      this.api_key_env = source["api_key_env"];
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a;
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs));
      } else if ("object" === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key]);
          }
          return a;
        }
        return new classs(a);
      }
      return a;
    }
  }
  export class Provider {
    name: string;
    backend: string;
    base_url: string;
    api_key_env: string;

    static createFrom(source: any = {}) {
      return new Provider(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.name = source["name"];
      this.backend = source["backend"];
      this.base_url = source["base_url"];
      this.api_key_env = source["api_key_env"];
    }
  }
  export class ProfilesFile {
    providers: Record<string, Provider>;
    profiles: Record<string, Profile>;

    static createFrom(source: any = {}) {
      return new ProfilesFile(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.providers = this.convertValues(source["providers"], Provider, true);
      this.profiles = this.convertValues(source["profiles"], Profile, true);
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a;
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs));
      } else if ("object" === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key]);
          }
          return a;
        }
        return new classs(a);
      }
      return a;
    }
  }

  export class ProviderAPIKeyStatus {
    provider: string;
    environment_configured: boolean;
    stored_configured: boolean;
    effective_configured: boolean;

    static createFrom(source: any = {}) {
      return new ProviderAPIKeyStatus(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.provider = source["provider"];
      this.environment_configured = source["environment_configured"];
      this.stored_configured = source["stored_configured"];
      this.effective_configured = source["effective_configured"];
    }
  }

  export class UIConfig {
    theme: string;
    accent_color: string;
    primary_color: string;
    status_bar: boolean;
    token_counter: boolean;
    think_indicator: boolean;
    syntax_highlight: boolean;
    diff_colours: boolean;
    chat_timestamps: boolean;
    tool_countdown: boolean;
    terminal_default_open: boolean;
    terminal_height: number;
    tree_width: number;
    chat_width: number;
    artifact_width: number;

    static createFrom(source: any = {}) {
      return new UIConfig(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.theme = source["theme"];
      this.accent_color = source["accent_color"];
      this.primary_color = source["primary_color"];
      this.status_bar = source["status_bar"];
      this.token_counter = source["token_counter"];
      this.think_indicator = source["think_indicator"];
      this.syntax_highlight = source["syntax_highlight"];
      this.diff_colours = source["diff_colours"];
      this.chat_timestamps = source["chat_timestamps"];
      this.tool_countdown = source["tool_countdown"];
      this.terminal_default_open = source["terminal_default_open"];
      this.terminal_height = source["terminal_height"];
      this.tree_width = source["tree_width"];
      this.chat_width = source["chat_width"];
      this.artifact_width = source["artifact_width"];
    }
  }
  export class TelegramConfig {
    enabled: boolean;
    token: string;
    bot_username: string;
    require_mention: boolean;
    allow_from: string[];
    default_project: string;
    default_profile: string;
    default_mode: string;
    default_toolset: string;
    send_progress: boolean;
    progress_interval_s: number;
    voice_replies: string;
    transcription_mode: string;
    transcription_url: string;

    static createFrom(source: any = {}) {
      return new TelegramConfig(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.enabled = source["enabled"];
      this.token = source["token"];
      this.bot_username = source["bot_username"];
      this.require_mention = source["require_mention"];
      this.allow_from = source["allow_from"];
      this.default_project = source["default_project"];
      this.default_profile = source["default_profile"];
      this.default_mode = source["default_mode"];
      this.default_toolset = source["default_toolset"];
      this.send_progress = source["send_progress"];
      this.progress_interval_s = source["progress_interval_s"];
      this.voice_replies = source["voice_replies"];
      this.transcription_mode = source["transcription_mode"];
      this.transcription_url = source["transcription_url"];
    }
  }
  export class SkillsConfig {
    enabled: boolean;
    auto_inject: boolean;
    max_inject: number;
    skills_dir: string;

    static createFrom(source: any = {}) {
      return new SkillsConfig(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.enabled = source["enabled"];
      this.auto_inject = source["auto_inject"];
      this.max_inject = source["max_inject"];
      this.skills_dir = source["skills_dir"];
    }
  }
  export class ToolSafeRule {
    id: string;
    tool: string;
    input_hash: string;
    label: string;
    created_at: string;

    static createFrom(source: any = {}) {
      return new ToolSafeRule(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.id = source["id"];
      this.tool = source["tool"];
      this.input_hash = source["input_hash"];
      this.label = source["label"];
      this.created_at = source["created_at"];
    }
  }
  export class ToolsConfig {
    enabled: boolean;
    confirm_reads: boolean;
    confirm_writes: boolean;
    confirm_exec: boolean;
    bash_timeout: number;
    shell_backend: string;
    shell_mode: string;
    shell_distro: string;
    shell_user: string;
    artifact_timeout: number;
    web_engine: string;
    web_base_url: string;
    web_api_key_env: string;
    brave_api_key: string;
    max_searches: number;
    max_fetches: number;
    max_failed_fetches: number;
    max_browser_actions: number;
    max_tool_result_chars: number;
    tool_result_preview_chars: number;
    tool_result_aggregate_chars: number;
    protected_paths: string[];
    redact_secrets: boolean;
    active_toolset: string;
    toolsets: Record<string, Array<string>>;
    enabled_tools: Record<string, boolean>;
    safe_rules: ToolSafeRule[];
    tool_grammar_constraint: boolean;

    static createFrom(source: any = {}) {
      return new ToolsConfig(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.enabled = source["enabled"];
      this.confirm_reads = source["confirm_reads"];
      this.confirm_writes = source["confirm_writes"];
      this.confirm_exec = source["confirm_exec"];
      this.bash_timeout = source["bash_timeout"];
      this.shell_backend = source["shell_backend"];
      this.shell_mode = source["shell_mode"];
      this.shell_distro = source["shell_distro"];
      this.shell_user = source["shell_user"];
      this.artifact_timeout = source["artifact_timeout"];
      this.web_engine = source["web_engine"];
      this.web_base_url = source["web_base_url"];
      this.web_api_key_env = source["web_api_key_env"];
      this.brave_api_key = source["brave_api_key"];
      this.max_searches = source["max_searches"];
      this.max_fetches = source["max_fetches"];
      this.max_failed_fetches = source["max_failed_fetches"];
      this.max_browser_actions = source["max_browser_actions"];
      this.max_tool_result_chars = source["max_tool_result_chars"];
      this.tool_result_preview_chars = source["tool_result_preview_chars"];
      this.tool_result_aggregate_chars = source["tool_result_aggregate_chars"];
      this.protected_paths = source["protected_paths"];
      this.redact_secrets = source["redact_secrets"];
      this.active_toolset = source["active_toolset"];
      this.toolsets = source["toolsets"];
      this.enabled_tools = source["enabled_tools"];
      this.safe_rules = this.convertValues(source["safe_rules"], ToolSafeRule);
      this.tool_grammar_constraint = source["tool_grammar_constraint"];
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a;
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs));
      } else if ("object" === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key]);
          }
          return a;
        }
        return new classs(a);
      }
      return a;
    }
  }
  export class Settings {
    active_profile: string;
    tools: ToolsConfig;
    agents: AgentsConfig;
    environment: EnvironmentConfig;
    context: ContextConfig;
    memory: MemoryConfig;
    skills: SkillsConfig;
    image: ImageConfig;
    telegram: TelegramConfig;
    audio: AudioConfig;
    ui: UIConfig;
    logging: LoggingConfig;
    log_level: string;

    static createFrom(source: any = {}) {
      return new Settings(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.active_profile = source["active_profile"];
      this.tools = this.convertValues(source["tools"], ToolsConfig);
      this.agents = this.convertValues(source["agents"], AgentsConfig);
      this.environment = this.convertValues(
        source["environment"],
        EnvironmentConfig,
      );
      this.context = this.convertValues(source["context"], ContextConfig);
      this.memory = this.convertValues(source["memory"], MemoryConfig);
      this.skills = this.convertValues(source["skills"], SkillsConfig);
      this.image = this.convertValues(source["image"], ImageConfig);
      this.telegram = this.convertValues(source["telegram"], TelegramConfig);
      this.audio = this.convertValues(source["audio"], AudioConfig);
      this.ui = this.convertValues(source["ui"], UIConfig);
      this.logging = this.convertValues(source["logging"], LoggingConfig);
      this.log_level = source["log_level"];
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a;
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs));
      } else if ("object" === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key]);
          }
          return a;
        }
        return new classs(a);
      }
      return a;
    }
  }
}

export namespace tools {
  export class TodoItem {
    id: string;
    text: string;
    status: string;
    detail?: string;
    created_at: string;
    updated_at: string;

    static createFrom(source: any = {}) {
      return new TodoItem(source);
    }

    constructor(source: any = {}) {
      if ("string" === typeof source) source = JSON.parse(source);
      this.id = source["id"];
      this.text = source["text"];
      this.status = source["status"];
      this.detail = source["detail"];
      this.created_at = source["created_at"];
      this.updated_at = source["updated_at"];
    }
  }
}
