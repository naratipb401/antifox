// AntiFox domain model — types used across the UI.
// Derived from the functional spec (section 7: key data shapes).

export type TaskStatus = "created" | "running" | "paused" | "done" | "failed" | "timeout";
export type EngineMode = "exploring" | "paused" | "stalled" | "idle";

export interface Task {
  id: string;
  name?: string; // short task name/goal; empty = fall back to #id·description
  description: string;
  goal: string;
  status: TaskStatus;
  created_at: string;
  created_unix?: number; // created_at as unix seconds (run-duration calc)
  completed_at?: string; // RFC3339 finish time (done/failed); "" if unfinished
  completed_unix?: number; // completed_at as unix seconds (0/undef if unfinished)
  last_activity_unix?: number; // unix seconds of the last activity (0/undef if none)
  paused?: boolean;
  active?: boolean;
  in_flight?: number;
  last_activity?: string;
  stalled?: boolean;
  goals_total?: number;
  goals_met?: number;
  engine_mode?: EngineMode;
  tokens?: TokenTotal; // whole-task token consumption
  llm_profile_id?: number; // LLM profile used; absent = default profile
  timeout_seconds?: number; // per-task timeout (seconds); 0/absent = unlimited (edit prefill)
  egress?: string; // egress proxy: ''=follow global | 'direct'=direct | 'proxy'=via global proxy
}

// ---- Asset graph (global, shared across tasks) ----
export type AssetType =
  | "company"
  | "domain"
  | "ip"
  | "port"
  | "service"
  | "site"
  | "endpoint"
  | "parameter"
  | "tech"
  | "credential"
  | "data";

export type NodeState = "observed" | "confirmed" | "tombstoned";

export interface AssetNode {
  id: string;
  type: AssetType;
  name: string;
  key: string; // nkey
  value?: string;
  company_id?: string; // owning company asset id; empty = unassigned
  state: NodeState;
  confidence: number; // 0..1
  attrs?: Record<string, unknown>;
  first_seen: string;
  last_seen: string;
}

export type AssetRel =
  | "owns"
  | "resolves"
  | "exposes"
  | "runs"
  | "serves"
  | "has_endpoint"
  | "has_param"
  | "fingerprinted"
  | "authenticates_as"
  | "reachable"
  | "has_subdomain";

export interface Edge {
  src: string;
  dst: string;
  rel: AssetRel | ExploreRel;
}

// Task asset view — server-side enriched, paginated.
export interface TaskAssetRef {
  id: string;
  name?: string;
  key: string;
  attrs?: Record<string, unknown>;
}

export interface TaskAssetItem extends AssetNode {
  techs?: TaskAssetRef[];
  auth?: TaskAssetRef[];
  params?: TaskAssetRef[];
}

export interface TaskAssetView {
  counts: Record<string, number>;
  total: number;
  items: TaskAssetItem[];
}

// ---- New unified asset model (new backend) ----
export type NewAssetType = "root_domain" | "ip" | "subdomain" | "app" | "service" | "endpoint";

export interface Asset {
  id: number;
  type: NewAssetType;
  company_id?: number;
  task_ids: number[];
  domain?: string;
  root_domain?: string;
  ip?: string;
  c_segment?: string;
  port?: number;
  icp?: string;
  bound_domains?: string[];
  open_ports?: { port: number; service?: string }[];
  record_type?: string;
  record_value?: string[] | string;
  bundle_id?: string;
  app_name?: string;
  category?: string;
  app_description?: string;
  app_icp?: string;
  url?: string;
  service_type?: string;
  service_name?: string;
  favicon_mmh3?: string;
  status_code?: number;
  content_length?: number;
  page_title?: string;
  technologies?: string[];
  auth?: Record<string, unknown>[];
  method?: string;
  params?: Record<string, unknown>[];
  extra?: Record<string, unknown>;
  last_seen: string;
}

// One company asset-scope rule (single source of truth for ownership).
export interface ScopeRow {
  id: number;
  company_id: number;
  kind: "domain" | "ip" | "cidr";
  domain?: string;  // set when kind=domain
  net?: string;     // set when kind=ip|cidr
  raw: string;      // raw user input, for display and prefill
  reason?: string;
}

// Company: a type=company asset node + logo + asset count + scope rules.
export interface Company {
  id: number;
  name: string;
  logo?: string; // remote logo URL; empty = UI falls back to the initial letter
  asset_count: number;
  scope: ScopeRow[];
}

// ---- Exploration graph (per task) ----
export type ExploreKind = "task" | "begin" | "goal" | "intent" | "fact" | "finding" | "hint";
export type GoalState = "open" | "met" | "abandoned";
export type IntentState = "open" | "running" | "done" | "blocked" | "exhausted" | "stopped";
export type FindingState = "confirmed" | "dismissed";
export type HintState = "active" | "consumed";
export type ExploreRel = "spawns" | "derived_from" | "yields" | "proves";

export interface TaskNode {
  id: string;
  type: ExploreKind;
  payload?: string;
  priority: number; // 0..10
  state: string; // GoalState | IntentState | FindingState | HintState
  origin: string;
  ts: string;
}

// ---- Findings ----
export type Severity = "critical" | "high" | "medium" | "low";

export interface Finding {
  id: string;
  node_id?: string;
  vulnclass: string;
  severity: Severity;
  summary: string;
  evidence: string;
  intent_id?: string;
  param_id?: string;
  task_id?: string;
  task_description?: string;
  ts: string;
  // enrichment summary (merged in the list)
  enr_name?: string;
  enr_target?: string;
  status?: FindingStatus;
  enriched?: boolean;
  cvss_score?: number;
  evidence_count?: number;
  traffic_count?: number;
}

export type FindingStatus = "open" | "confirmed" | "fixed" | "false_positive" | "ignored";

// Full structured enrichment (AI analysis + human edits) for one finding.
export interface Enrichment {
  node_id: string;
  name: string;
  target: string;
  module: string;
  severity: string;
  status: FindingStatus;
  cvss_score: number;
  cvss_vector: string;
  cvss_metrics: Record<string, string>;
  description: string[];
  test_process: { type: string; content: string }[];
  risk_analysis: string[];
  remediation: string[];
  source: string;
  updated_at?: string;
}

// Structured reproduction (recorded in place by the agent). Rendered as explicit steps; evidence attaches per step.
export interface ReproStep {
  request: string;
  response: string;
  expected: string;
  proves: string;
  // Exchange id of the real traffic matched verbatim from the request (powers each step's "jump to traffic/screenshot"); empty if unmatched.
  exchange_id?: string;
}
export interface Reproduction {
  entry_point?: string;
  prerequisites?: string[];
  steps?: ReproStep[];
  provenance?: string;
}

export interface FindingDetailResp {
  finding: Finding;
  enrichment: Enrichment | null;
  reproduction?: Reproduction | null;
}

// Finding evidence screenshots (shared by findings page/report/MD export). step_index maps to the reproduction step (0=overview).
export interface FindingEvidence {
  id: number;
  node_id: number;
  filename: string;
  caption: string;
  step_index: number;
  sort_order: number;
  created_at: string;
}
export interface FindingEvidenceItem {
  evidence: FindingEvidence;
  url: string;
}

// Async "AI analysis" job state (POST enrich / GET enrich/status).
export interface EnrichJob {
  status: "analyzing" | "done" | "failed" | "idle";
  error?: string;
  started_at?: string;
  enrichment?: Enrichment;
}

// ---- Activity / sessions ----
export type ActivityKind =
  | "tool_use"
  | "tool_result"
  | "text"
  | "thinking"
  | "result"
  | "user"
  | "intent"            // LLM-generated exploration objective leading a worker session (UI-synthesized)
  | "round"             // planner round boundary marker (engine-emitted)
  | "usage"             // live cumulative token usage (per model turn); not rendered
  | "intercept_request"; // user-approval request from the intercept layer

export interface Activity {
  seq: number;
  intent_id?: string;
  worker: string; // session owner: planner | mainagent | work#1 ...
  ts: string;
  kind: ActivityKind;
  tool?: string;
  tool_use_id?: string;
  is_error?: boolean;
  summary: string;
  detail?: string;
  // token usage (present only on kind='result')
  input_tokens?: number;
  output_tokens?: number;
  cache_read_tokens?: number;
  cache_write_tokens?: number;
}

// ---- Agent triggers (P3 scheduling, custom agents only) ----
export interface AgentTrigger {
  id: number;
  agent_key: string;
  enabled: boolean;
  interval_sec: number; // interval: every N seconds (0=off)
  on_finding: boolean; // fires when any task yields a finding
  on_goal_met: boolean; // fires when any task meets its goal
  on_task_timeout: boolean; // fires when any task times out
  interval_message: string; // dedicated user message per trigger
  finding_message: string;
  goal_message: string;
  task_timeout_message: string;
  last_fire?: string;
}

// ---- Conversations (chat page) ----
export interface Conversation {
  id: number;
  agent_key: string;
  title: string;
  llm_profile_id?: number;
  created_at: string;
  updated_at: string;
}

// ---- Backend logs (/logs page) ----
export interface LogLine {
  seq: number;
  db_id?: number; // server_logs.id; present for DB-persisted lines
  ts: string;
  level: "info" | "warn" | "error";
  tag: string;
  text: string;
}

export type SessionRole = "mainagent" | "planner" | "worker";
export type SessionStatus = "running" | "done" | "blocked" | "exhausted" | "pending" | "stopped";

// Daily token aggregate bucket (GET /api/tokens/daily).
export interface DailyTokenBucket {
  date: string; // "YYYY-MM-DD"
  input_tokens: number;
  output_tokens: number;
  cache_read_tokens: number;
  cache_write_tokens: number;
}

// Per-worker token usage (GET /api/exploration/tokens).
export interface TokenUsage {
  worker: string;
  input_tokens: number;
  output_tokens: number;
  cache_read_tokens: number;
  cache_write_tokens: number;
}

// Whole-task (all agents) token aggregate.
export interface TokenTotal {
  input_tokens: number;
  output_tokens: number;
  cache_read_tokens: number;
  cache_write_tokens: number;
}

export interface Session {
  id: string;
  role: SessionRole;
  title: string;
  status: SessionStatus;
  live: boolean;
  last_activity: string;
  intent_id?: string;
}

// ---- Security ----
export interface AuditEntry {
  ts: string;
  tool: string;
  action: "allow" | "block";
  reason?: string;
  command?: string;
}

export interface Audit {
  entries?: AuditEntry[];
  attributions?: Record<string, number>;
}

// ---- Traffic ----
export interface TrafficExchange {
  id: string;
  ts: string;
  host: string;
  method: string;
  url: string;
  status: number;
  content_type: string;
  resp_len: number;
  // Highlight snippet for body-search hits (\x01/\x02 wrap the match); set only when filtering by body keyword.
  match_snippet?: string;
}

export interface TrafficResp {
  enabled: boolean;
  proxy?: string;
  count?: number; // global total (unfiltered)
  total?: number; // rows matching the current filter (for pagination)
  page?: number;
  size?: number;
  exchanges?: TrafficExchange[];
  status_buckets?: Record<string, number>; // {"2":N,"3":N,"4":N,"5":N} full status-code distribution
}

// Full raw request/response of one exchange (lazy-loaded on row select).
export interface TrafficDetail {
  req: string;
  resp: string;
}

// ---- App settings (runtime toggles) ----
export interface Settings {
  traffic_capture: boolean;
  // Web search. brave_key_set / tavily_key_set reflect whether a key is stored
  // (the values are never returned). On PUT, send the corresponding field to set/clear.
  web_search_enabled: boolean;
  web_search_backend: string; // "ddgs" | "brave-free" | "tavily"
  brave_key_set: boolean;
  tavily_key_set: boolean;
  // write-only: only sent on PUT to store/clear the key.
  brave_search_api_key?: string;
  tavily_search_api_key?: string;
  // Dedicated egress proxy (http/https/socks5) for reaching search endpoints; unrelated to the traffic-capture MITM proxy. empty = direct.
  web_search_proxy?: string;
  // Global egress proxy (http/https/socks5): all agent target traffic leaves through it (as the MITM upstream), hiding the source IP. empty = direct.
  global_proxy?: string;
  python_interpreter?: string; // python interpreter for custom script tools (empty = detected at runtime)
  workers?: number; // concurrent worker agents (default 3); applies to tasks started afterwards
}

// ---- one-click update ----
export interface UpdateCheck {
  current: string; // running version ("dev" = dev build, one-click update disabled)
  latest?: string; // latest stable GitHub release tag
  has_update?: boolean;
  comparable?: boolean; // whether the versions are comparable (dev/suffixed = false)
  reason?: string; // why they are not comparable
  mode: "binary" | "docker";
  os: string;
  arch: string;
  asset?: string; // release asset for this platform
  asset_available?: boolean; // whether this release ships a build for this platform
  size?: number;
  notes?: string; // release notes (changelog)
  html_url?: string;
  published_at?: string;
  has_backup?: boolean; // whether a rollback-able previous version exists
  repo?: string;
  boot_notice?: string; // last upgrade result (rollback/verification failure etc.)
  rolled_back?: boolean;
  error?: string; // why the check failed (e.g. GitHub unreachable/rate-limited)
}

export interface UpdateProgress {
  phase: "idle" | "downloading" | "verifying" | "extracting" | "staged" | "failed";
  percent: number; // 0-100 while downloading, -1 otherwise
  message: string;
  version?: string;
  error?: string;
}

// ---- LLM config ----
export interface LLMProfile {
  id: string;
  name: string;
  format: "openai" | "anthropic";
  base_url?: string;
  proxy?: string;
  model: string;
  api_key_hint?: string;
  rate_per_second: number;
  rate_per_minute: number;
  context_window_k?: number;
  // Thinking mode: ""=default (not sent) | "off"=disabled | "low"/"medium"/"high"/"max"=on with effort
  reasoning_effort?: string;
  is_default: boolean;
  priority?: number; // LLM failover chain order weight (higher = earlier; active config always first)
  pool_exclude?: boolean; // never used as a failover fallback
  session_header_key?: string; // non-empty = send this header on every request, value = current session id (for opencode-style gateways)
}

// One member of the LLM round-robin (failover) chain + circuit-breaker state.
export interface LLMPoolMember {
  profile_id: string;
  name: string;
  model: string;
  format: string;
  priority: number;
  active: boolean; // globally active config (always first)
  excluded: boolean; // pool_exclude, no fallback duty
  state: "ok" | "degraded" | "tripped";
  fails: number;
  trips: number;
  cooldown_secs: number; // circuit-breaker cooldown remaining; 0 = none
  last_error?: string;
  last_at?: string;
}

export interface LLMPoolStatus {
  enabled: boolean;
  chain: LLMPoolMember[];
}

// ---- Agents ----
export interface Agent {
  id: string;
  key: string; // builtin: goals/planner/mainagent/worker; custom: user-defined key
  name: string;
  description?: string;
  role: string;
  builtin: boolean;
  enabled: boolean;
  max_turns?: number; // 0 = unlimited
  run_seconds?: number; // worker per-run wall-clock cap (seconds); 0 = unlimited
  web_search?: boolean; // enable web search (gated by the global system switch)
  interactive_shell?: boolean; // enable interactive shell (persistent PTY session tool family)
  llm_profile_id?: number; // per-tier LLM profile id; 0/absent = use the global active config
  // Binding counts (list endpoints only): visible MCPs / visible Skills / bound tools
  mcp_count?: number;
  skill_count?: number;
  tool_count?: number;
}

export interface PromptVar {
  name: string;
  description: string;
  example: string;
  source: "exploration" | "runtime" | "distilled";
}

export interface PromptVersion {
  version: number;
  ts: string;
  note: string;
  template_text: string;
}

export interface AgentDetail {
  agent: Agent;
  prompt: string;
  variables: PromptVar[];
  versions: PromptVersion[];
  visibility: { mcp: number[]; skill: string[] };
  wrapup_prompt?: string; // saved wrap-up prompt (empty = use builtin default)
  wrapup_default?: string; // builtin default wrap-up prompt (placeholder / reset target)
  wrapup_max_turns?: number; // saved wrap-up turn limit (0 = use builtin default)
  wrapup_max_turns_default?: number; // builtin default wrap-up turns (for the "0 = default N" hint)
  // Task-level timeout wrap-up (worker/planner only; section shown when task_timeout_wrapup_supported=true)
  task_timeout_wrapup_supported?: boolean;
  task_timeout_wrapup_prompt?: string;
  task_timeout_wrapup_default?: string;
  task_timeout_wrapup_max_turns?: number;
  task_timeout_wrapup_max_turns_default?: number;
}

// ---- MCP ----
export interface MCPServer {
  id: number;
  name: string;
  transport: "stdio" | "http";
  command?: string;
  args: string[];
  env: Record<string, string>;
  headers?: Record<string, string>; // http transport request headers
  url?: string;
  description?: string;
  timeout_seconds?: number;
  enabled: boolean;
  status?: string;       // '' | 'ok' | 'error'
  status_at?: string;    // ISO timestamp
  status_error?: string; // error message
  tools?: string[];      // mcp_tools_cache (names only, for the count)
}

// MCP call stats (reuses observability, mirrors SkillStat)
export interface MCPStat {
  server_name: string;
  calls: number;
  last_at?: string;
  by_tool: Record<string, number>;
  by_agent: Record<string, number>;
}
export interface MCPStatsResp {
  servers: MCPStat[];
  total_calls: number;
  enabled_count: number;
  total_count: number;
}

export interface MCPTool {
  name: string;
  description: string;
}

// ---- Skills ----
// Fields align with the agentskills.io open specification.
// description covers both "what the skill does" and "when to use it".
export interface SkillItem {
  name: string;           // unique key = directory name
  description?: string;   // required per spec; covers what + when to use
  license?: string;       // optional: SPDX identifier or free text
  compatibility?: string; // optional: environment requirements
  mcps?: string[];        // MCP server names this skill unlocks on load
  files: string[];        // files in the skill directory
}

// Skill call stats (reuses observability)
export interface SkillStat {
  skill_name: string;
  calls: number;
  last_at?: string;
  by_agent: Record<string, number>; // agent_key → calls
}
export interface SkillStatsResp {
  skills: SkillStat[];
  total_calls: number;
  installed_count: number;
  invoked_count: number;
}

// ---- Tools (builtin tool catalog) ----
// key + handler live in Go; only these fields are page-editable. system tools lock
// the key and the parameter *structure* (name/type/required) — the per-param
// description/default and the agent binding are what move.
export interface Tool {
  key: string;
  system: boolean;
  description: string;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  schema: Record<string, any>; // full JSON-Schema (object with properties)
  agents: string[];            // bound agent keys
  enabled: boolean;
  kind?: "builtin" | "shell" | "command" | "script" | "http"; // custom tool kind
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  exec?: Record<string, any>;  // custom tool exec spec (kind != builtin)
  deferred?: boolean;          // deferred schema (SearchExtraTools/ExecuteExtraTool)
}

// ---- Stats ----
export interface Stats {
  assets: number;
  engine_mode: EngineMode;
  llm_configured: boolean;
  roe_enabled: boolean;
  findings_confirmed: number;
}

// ---- Intercept Rules ----
export type InterceptAction = "allow" | "deny" | "ask";
export type InterceptMatchTarget = "tool_name" | "tool_input";
export type InterceptMatchType = "string" | "regex";

export interface InterceptRule {
  id: number;
  name: string;
  enabled: boolean;
  priority: number;
  match_target: InterceptMatchTarget;
  match_type: InterceptMatchType;
  pattern: string;
  action: InterceptAction;
  message: string;
  timeout_enabled: boolean;
  timeout_seconds: number;
  timeout_action: "deny" | "allow";
  created_at: string;
  updated_at: string;
}

export interface InterceptPending {
  id: number;
  rule_id?: number;
  conversation_id?: number;
  task_id?: string;
  agent_name: string;
  tool_name: string;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  tool_input: Record<string, any>;
  status: "pending" | "allowed" | "denied" | "timeout";
  decided_at?: string;
  created_at: string;
}

// InterceptApprovalRow enriches InterceptPending with conversation/task and rule context.
export interface InterceptApprovalRow extends InterceptPending {
  conv_title: string;      // "" if no linked conversation
  conv_agent_key: string;  // "" if no linked conversation
  rule_name: string;       // "" if rule was deleted
}

// ── Asset sync (ScopeSentry source) ───────────────────────────────────────────
export interface SSProject {
  id: string;        // MongoDB ObjectID — used as filter.project
  name: string;
  logo?: string;
  AssetCount?: number;
  tag?: string;
}

export interface SSTask {
  id: string;
  name: string;      // used as filter.task
  status?: number;
  progress?: number;
  creatTime?: string;
  endTime?: string;
}

// ConvTokenSummary — one conversation's token total (+ profile/date) for merging
// chat usage into the dashboard token stats. GET /api/tokens/conversations.
export interface ConvTokenSummary {
  llm_profile_id: number | null;
  created_at: string;
  input_tokens: number;
  output_tokens: number;
  cache_read_tokens: number;
  cache_write_tokens: number;
}

// ---- Platform RBAC ----
export interface RBACUser {
  id: string;
  username: string;
  display_name: string;
  enabled: boolean;
  is_builtin: boolean;
  created_at: string;
  updated_at: string;
  role_ids?: string[];
}

export interface RBACRole {
  id: string;
  name: string;
  description: string;
  scope: string;
  is_system: boolean;
  created_at: string;
  updated_at: string;
  permissions?: string[];
}

export interface RBACMe {
  user: { id: string; username: string; display_name: string };
  roles: RBACRole[];
  permissions: string[];
  scope: string;
}

export interface AuditLog {
  id: number;
  created_at: string;
  actor: string;
  category: string;
  action: string;
  result: string;
  ip: string;
  message: string;
  detail: Record<string, unknown>;
}

// Async .docx report generation job (AI narrative + python-docx pipeline).
export interface ReportJob {
  status: "idle" | "generating" | "done" | "failed";
  filename?: string;
  error?: string;
  started_at?: string;
}

// Report management: org-level settings + generated-report record.
export interface Personnel {
  name: string;
  role: string;
  contact: string;
}
export interface ReportSettings {
  company_name: string;
  company_name_en: string;
  logo_path: string;
  personnel: Personnel[];
  cvss_version: string;
  updated_at?: string;
}
export interface ReportRecord {
  id: number;
  task_id?: number;
  task_desc: string;
  filename: string;
  size: number;
  company: string;
  created_at: string;
}

export interface ReportTemplate {
  id: number;
  name: string;
  company_name: string;
  company_name_en: string;
  logo_path: string;
  personnel: Personnel[];
  cvss_version: string;
  locale: string;
  created_at?: string;
  updated_at?: string;
}

// ---- Knowledge base (two-tier RAG: global + per-task) ----
export interface KnowledgeDoc {
  id: number;
  scope: string; // global | task
  task_id?: number;
  category: string;
  title: string;
  content?: string;
  source: string; // manual | upload | migrated
  file_name?: string;
  bytes: number;
  indexed: boolean;
  chunks: number;
  created_at: string;
  updated_at: string;
}

export interface KnowledgeHit {
  doc_id: number;
  title: string;
  category: string;
  scope: string;
  content: string;
  score: number;
}

export interface KnowledgeConfig {
  enabled: boolean;
  base_url: string;
  model: string;
  dim: number;
  has_key: boolean;
  vector_available: boolean;
  semantic_ready: boolean;
  docs_global: number;
  docs_task: number;
  api_key?: string; // write-only
}

export interface KnowledgeLog {
  id: number;
  task_id?: number;
  agent_key: string;
  query: string;
  scope: string;
  mode: string;
  top_k: number;
  hits: number;
  created_at: string;
}
