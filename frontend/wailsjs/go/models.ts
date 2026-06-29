export namespace app {
	
	export class AgentEvalResult {
	    name: string;
	    pass: boolean;
	    status: string;
	    tool_calls: number;
	    auto_continues: number;
	    truncations: number;
	    tool_errors: number;
	    duration_ms: number;
	    fail_reason?: string;
	
	    static createFrom(source: any = {}) {
	        return new AgentEvalResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.pass = source["pass"];
	        this.status = source["status"];
	        this.tool_calls = source["tool_calls"];
	        this.auto_continues = source["auto_continues"];
	        this.truncations = source["truncations"];
	        this.tool_errors = source["tool_errors"];
	        this.duration_ms = source["duration_ms"];
	        this.fail_reason = source["fail_reason"];
	    }
	}
	export class AgentEvalReport {
	    results: AgentEvalResult[];
	    pass_count: number;
	    total: number;
	    profile: string;
	
	    static createFrom(source: any = {}) {
	        return new AgentEvalReport(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.results = this.convertValues(source["results"], AgentEvalResult);
	        this.pass_count = source["pass_count"];
	        this.total = source["total"];
	        this.profile = source["profile"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
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
	
	export class BenchmarkCase {
	    name: string;
	    status: string;
	    summary: string;
	    prompt_tokens?: number;
	    completion_tokens?: number;
	    ttf_ms?: number;
	    total_ms?: number;
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
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.status = source["status"];
	        this.summary = source["summary"];
	        this.prompt_tokens = source["prompt_tokens"];
	        this.completion_tokens = source["completion_tokens"];
	        this.ttf_ms = source["ttf_ms"];
	        this.total_ms = source["total_ms"];
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
	        if ('string' === typeof source) source = JSON.parse(source);
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
	        if ('string' === typeof source) source = JSON.parse(source);
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
	export class DoctorCheck {
	    name: string;
	    status: string;
	    message: string;
	    detail?: string;
	
	    static createFrom(source: any = {}) {
	        return new DoctorCheck(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
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
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.checks = this.convertValues(source["checks"], DoctorCheck);
	        this.score = source["score"];
	        this.grade = source["grade"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
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
	export class FileNode {
	    name: string;
	    path: string;
	    isDir: boolean;
	    children?: FileNode[];
	
	    static createFrom(source: any = {}) {
	        return new FileNode(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
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
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
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
	        if ('string' === typeof source) source = JSON.parse(source);
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
	
	    static createFrom(source: any = {}) {
	        return new HistoryStats(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.token_count = source["token_count"];
	        this.budget = source["budget"];
	        this.fraction = source["fraction"];
	        this.rollback_len = source["rollback_len"];
	        this.window = source["window"];
	        this.reserve = source["reserve"];
	    }
	}
	export class LabStatus {
	    agent_root: string;
	    shell_backend: string;
	    shell_distro: string;
	    shell_user: string;
	    target: string;
	    vpn_interface: string;
	    vpn_ip: string;
	    vpn_cidr: string;
	    vpn_kind: string;
	    latest_artifact: string;
	    ops_profile: string;
	    open_folders: settings.WorkspaceFolder[];
	
	    static createFrom(source: any = {}) {
	        return new LabStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.agent_root = source["agent_root"];
	        this.shell_backend = source["shell_backend"];
	        this.shell_distro = source["shell_distro"];
	        this.shell_user = source["shell_user"];
	        this.target = source["target"];
	        this.vpn_interface = source["vpn_interface"];
	        this.vpn_ip = source["vpn_ip"];
	        this.vpn_cidr = source["vpn_cidr"];
	        this.vpn_kind = source["vpn_kind"];
	        this.latest_artifact = source["latest_artifact"];
	        this.ops_profile = source["ops_profile"];
	        this.open_folders = this.convertValues(source["open_folders"], settings.WorkspaceFolder);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
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
	        if ('string' === typeof source) source = JSON.parse(source);
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
	        if ('string' === typeof source) source = JSON.parse(source);
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
	        if ('string' === typeof source) source = JSON.parse(source);
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
	    tokens_per_second?: number;
	
	    static createFrom(source: any = {}) {
	        return new ProfileBenchmarkResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
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
	        this.recommended_profile = this.convertValues(source["recommended_profile"], settings.Profile);
	        this.scenarios = this.convertValues(source["scenarios"], BenchmarkCase);
	        this.prompt_tokens = source["prompt_tokens"];
	        this.completion_tokens = source["completion_tokens"];
	        this.ttf_ms = source["ttf_ms"];
	        this.total_ms = source["total_ms"];
	        this.tokens_per_second = source["tokens_per_second"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
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
	export class TaskRunEvent {
	    kind: string;
	    message: string;
	    timestamp: string;
	    detail?: string;
	
	    static createFrom(source: any = {}) {
	        return new TaskRunEvent(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
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
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.input = source["input"];
	        this.result = source["result"];
	        this.status = source["status"];
	        this.timestamp = source["timestamp"];
	        this.duration_ms = source["duration_ms"];
	    }
	}
	export class TaskRun {
	    id: string;
	    prompt: string;
	    mode: string;
	    profile: string;
	    model?: string;
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
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.prompt = source["prompt"];
	        this.mode = source["mode"];
	        this.profile = source["profile"];
	        this.model = source["model"];
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
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
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
	        if ('string' === typeof source) source = JSON.parse(source);
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
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
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
	export class SessionChatMessage {
	    role: string;
	    content: string;
	    images?: string[];
	
	    static createFrom(source: any = {}) {
	        return new SessionChatMessage(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.role = source["role"];
	        this.content = source["content"];
	        this.images = source["images"];
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
	        if ('string' === typeof source) source = JSON.parse(source);
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
	        if ('string' === typeof source) source = JSON.parse(source);
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
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.model_id = source["model_id"];
	        this.best_n = source["best_n"];
	        this.tok_per_sec = source["tok_per_sec"];
	        this.baseline_tok_per_sec = source["baseline_tok_per_sec"];
	        this.speedup = source["speedup"];
	        this.ran_at = source["ran_at"];
	        this.samples = this.convertValues(source["samples"], SpecCalibrationSample);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
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
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.spec_type = source["spec_type"];
	        this.n_max = source["n_max"];
	        this.source = source["source"];
	        this.reason = source["reason"];
	        this.locked = source["locked"];
	        this.model_id = source["model_id"];
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
	        if ('string' === typeof source) source = JSON.parse(source);
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
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.ip = source["ip"];
	        this.cidr = source["cidr"];
	        this.kind = source["kind"];
	        this.likely_vpn = source["likely_vpn"];
	        this.label = source["label"];
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
	        if ('string' === typeof source) source = JSON.parse(source);
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
	        if ('string' === typeof source) source = JSON.parse(source);
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
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.type = source["type"];
	        this.function = this.convertValues(source["function"], FunctionCall);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
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
	export class Message {
	    role: string;
	    content: any;
	    tool_call_id?: string;
	    tool_calls?: ToolCallDef[];
	    name?: string;
	
	    static createFrom(source: any = {}) {
	        return new Message(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.role = source["role"];
	        this.content = source["content"];
	        this.tool_call_id = source["tool_call_id"];
	        this.tool_calls = this.convertValues(source["tool_calls"], ToolCallDef);
	        this.name = source["name"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
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
	        if ('string' === typeof source) source = JSON.parse(source);
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
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.profile = source["profile"];
	        this.context_budget = source["context_budget"];
	        this.autonomy = source["autonomy"];
	        this.toolset = source["toolset"];
	        this.instructions = source["instructions"];
	        this.tool_permissions = source["tool_permissions"];
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
	    presets: Record<string, AgentModePreset>;
	
	    static createFrom(source: any = {}) {
	        return new AgentsConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.mode_override = source["mode_override"];
	        this.default_autonomy = source["default_autonomy"];
	        this.offline_only = source["offline_only"];
	        this.max_tool_calls = source["max_tool_calls"];
	        this.max_run_seconds = source["max_run_seconds"];
	        this.escalation_profile = source["escalation_profile"];
	        this.require_plan = source["require_plan"];
	        this.no_think_after_tool_calls = source["no_think_after_tool_calls"];
	        this.reasoning_effort = source["reasoning_effort"];
	        this.presets = this.convertValues(source["presets"], AgentModePreset, true);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
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
	export class LabContext {
	    target: string;
	    vpn_interface: string;
	    latest_artifact: string;
	    ops_profile: string;
	
	    static createFrom(source: any = {}) {
	        return new LabContext(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.target = source["target"];
	        this.vpn_interface = source["vpn_interface"];
	        this.latest_artifact = source["latest_artifact"];
	        this.ops_profile = source["ops_profile"];
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
	        if ('string' === typeof source) source = JSON.parse(source);
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
	    lab: LabContext;
	
	    static createFrom(source: any = {}) {
	        return new ContextConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.auto_inject_file = source["auto_inject_file"];
	        this.auto_inject_cursor = source["auto_inject_cursor"];
	        this.compaction_at = source["compaction_at"];
	        this.show_compaction = source["show_compaction"];
	        this.mauler_md_path = source["mauler_md_path"];
	        this.project_doc_max_bytes = source["project_doc_max_bytes"];
	        this.project_doc_fallback_filenames = source["project_doc_fallback_filenames"];
	        this.workspace_dir = source["workspace_dir"];
	        this.open_folders = this.convertValues(source["open_folders"], WorkspaceFolder);
	        this.lab = this.convertValues(source["lab"], LabContext);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
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
	export class GenerationParams {
	    temperature: number;
	    top_p: number;
	    top_k: number;
	    min_p: number;
	    presence_penalty: number;
	    max_tokens: number;
	    seed: number;
	
	    static createFrom(source: any = {}) {
	        return new GenerationParams(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.temperature = source["temperature"];
	        this.top_p = source["top_p"];
	        this.top_k = source["top_k"];
	        this.min_p = source["min_p"];
	        this.presence_penalty = source["presence_penalty"];
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
	
	    static createFrom(source: any = {}) {
	        return new ImageConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.vision_enabled = source["vision_enabled"];
	        this.clipboard_method = source["clipboard_method"];
	        this.display_method = source["display_method"];
	        this.max_display_width = source["max_display_width"];
	        this.wsl_path_translate = source["wsl_path_translate"];
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
	        if ('string' === typeof source) source = JSON.parse(source);
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
	        if ('string' === typeof source) source = JSON.parse(source);
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
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.provider = source["provider"];
	        this.model_id = source["model_id"];
	        this.ctx_tokens = source["ctx_tokens"];
	        this.thinking = source["thinking"];
	        this.preserve_thinking = source["preserve_thinking"];
	        this.mmproj = source["mmproj"];
	        this.thinking_general = this.convertValues(source["thinking_general"], GenerationParams);
	        this.thinking_coding = this.convertValues(source["thinking_coding"], GenerationParams);
	        this.nothinking = this.convertValues(source["nothinking"], GenerationParams);
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
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
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
	        if ('string' === typeof source) source = JSON.parse(source);
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
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.providers = this.convertValues(source["providers"], Provider, true);
	        this.profiles = this.convertValues(source["profiles"], Profile, true);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
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
	        if ('string' === typeof source) source = JSON.parse(source);
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
	export class SkillsConfig {
	    enabled: boolean;
	    auto_inject: boolean;
	    max_inject: number;
	    skills_dir: string;
	
	    static createFrom(source: any = {}) {
	        return new SkillsConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
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
	        if ('string' === typeof source) source = JSON.parse(source);
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
	
	    static createFrom(source: any = {}) {
	        return new ToolsConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
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
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
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
	    context: ContextConfig;
	    memory: MemoryConfig;
	    skills: SkillsConfig;
	    image: ImageConfig;
	    ui: UIConfig;
	    logging: LoggingConfig;
	    log_level: string;
	
	    static createFrom(source: any = {}) {
	        return new Settings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.active_profile = source["active_profile"];
	        this.tools = this.convertValues(source["tools"], ToolsConfig);
	        this.agents = this.convertValues(source["agents"], AgentsConfig);
	        this.context = this.convertValues(source["context"], ContextConfig);
	        this.memory = this.convertValues(source["memory"], MemoryConfig);
	        this.skills = this.convertValues(source["skills"], SkillsConfig);
	        this.image = this.convertValues(source["image"], ImageConfig);
	        this.ui = this.convertValues(source["ui"], UIConfig);
	        this.logging = this.convertValues(source["logging"], LoggingConfig);
	        this.log_level = source["log_level"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
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
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.text = source["text"];
	        this.status = source["status"];
	        this.detail = source["detail"];
	        this.created_at = source["created_at"];
	        this.updated_at = source["updated_at"];
	    }
	}

}

