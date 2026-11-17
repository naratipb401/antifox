package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"

	"antifox/db"
	"antifox/engine/agentcore"
	"antifox/engine/harness"
	"antifox/engine/llm"
	"antifox/engine/memory"
	"antifox/engine/permission"
	actool "antifox/engine/tool"
	"antifox/engine/transcript"
)

// Worker is an LLM work agent (docs §4.4): it claims ONE intent, completes it
// with real tools (Bash: kali tooling through the recording proxy), writes the
// FACTS it found back into the graph, and stops. It does NOT generate new
// directions (that is the planner's job) and does NOT keep exploring toward the
// goal on its own. Multiple workers run concurrently as goroutines.
// WebSearchOpts is the web-search backend selection the server pushes into each
// agent (planner/worker/main). Enabled=false leaves the web_search tool off.
// Backend is "ddgs" (no key), "brave-free" (BraveKey required), or "tavily"
// (TavilyKey required). It maps directly onto agentcore.Options.
// Proxy is a dedicated egress proxy for the search request (http/https/socks5),
// independent of the traffic-recording MITM proxy — set it when the search endpoint
// is only reachable via a VPN/SOCKS proxy. Empty = direct.
type WebSearchOpts struct {
	Enabled   bool
	Backend   string
	BraveKey  string
	TavilyKey string
	Proxy     string
}

type Worker struct {
	prov        llm.Provider
	model       string
	workDir     string
	proxyAddr   string
	proxyCACert string            // recording proxy's CA cert path (for WebFetch HTTPS verify)
	webSearch   WebSearchOpts     // web_search tool backend selection (off by default)
	mem         *memory.Store     // cross-engagement tradecraft memory (G4)
	tx          *transcript.Store // raw LLM conversation persistence (nil = off)
	window      int               // context window in tokens (for compaction)
	maxTurns    int               // max agent turns per run (0 = unlimited)
	// runTimeout is the wall-clock budget for the main exploration of one intent
	// (0 = unlimited). When it fires, the run is cut and a settlement round is
	// forced so already-identified facts get written back instead of being lost.
	runTimeout time.Duration
	// extraTools are host-provided tools (e.g. traffic query, oast) appended to
	// the worker's graph write-back tools.
	extraTools []actool.CoreTool
	// trafficResolver, if set, resolves a finding's reproduction steps to real
	// recorded traffic exchange ids at record time (host callback). nil = no linkage.
	trafficResolver func(taskID int64, repro json.RawMessage) json.RawMessage
}

// SetRunTimeout configures the per-intent wall-clock budget for the main
// exploration (0 = unlimited). When it fires, the SDK settlement phase still runs
// so facts are never lost to a timeout. Safe to call before Execute.
func (w *Worker) SetRunTimeout(run time.Duration) {
	w.runTimeout = run
}

// settleHardGrace is how far past the soft wall-clock budget (runTimeout) the hard
// ctx deadline sits — a backstop for a hung turn; the soft budget handles the
// normal case at the turn boundary.
const settleHardGrace = 90 * time.Second

// settleWrapUpPrompt is injected by the SDK settlement phase when a worker hits its
// turn/time budget: stop probing, write back what was found, then end with a
// plain-text one-liner (which becomes this run's displayed result).
const settleWrapUpPrompt = "You are about to be stopped because the budget is exhausted. Do not run any more commands/probes. In order: (1) write back everything you identified above but have not yet recorded -- new assets via insert_assets, exploration conclusions/facts via record_fact, confirmed vulnerabilities via report_finding; (2) **finish with a single plain-text sentence** summarizing what you did and which key conclusions you reached (this sentence is shown as the result of this run; you must output it)."

// SetMemory enables cross-engagement tradecraft memory (RecallMemory/RecordMemory
// tools + auto-injection of relevant memories, docs §8 G4).
func (w *Worker) SetMemory(m *memory.Store) { w.mem = m }

func NewWorker(prov llm.Provider, model, workDir string, tx *transcript.Store, window, maxTurns int, extra ...actool.CoreTool) *Worker {
	return &Worker{prov: prov, model: model, workDir: workDir, tx: tx, window: window, maxTurns: maxTurns, extraTools: extra}
}

// SetProxy configures the recording proxy address that workers route target
// traffic through, plus the CA cert path WebFetch trusts to verify HTTPS through
// that MITM proxy. Empty addr disables the hint.
func (w *Worker) SetProxy(addr, caCert string) { w.proxyAddr, w.proxyCACert = addr, caCert }

// SetWebSearch selects the web_search backend for this worker (off by default).
func (w *Worker) SetWebSearch(o WebSearchOpts) { w.webSearch = o }

// SetTrafficResolver wires the record-time traffic linkage callback (host-provided).
func (w *Worker) SetTrafficResolver(fn func(taskID int64, repro json.RawMessage) json.RawMessage) {
	w.trafficResolver = fn
}

// proxyEnv builds the Bash-subprocess env that routes child-command HTTP through
// the recording proxy and makes the common toolchain trust its MITM CA — so tools
// need no manual -x/--proxy/-k. Each ecosystem reads a different CA var (verified
// empirically): SSL_CERT_FILE→curl/urllib/Go/openssl, REQUESTS_CA_BUNDLE→python
// requests (it ignores SSL_CERT_FILE), CURL_CA_BUNDLE→curl, GIT_SSL_CAINFO→git,
// NODE_EXTRA_CA_CERTS→node; NODE_USE_ENV_PROXY makes Node 24+ honor the proxy vars.
// Empty proxyAddr → nil (direct, unchanged env).
func proxyEnv(proxyAddr, caCert string) []string {
	if proxyAddr == "" {
		return nil
	}
	env := []string{
		"HTTP_PROXY=" + proxyAddr, "HTTPS_PROXY=" + proxyAddr,
		"http_proxy=" + proxyAddr, "https_proxy=" + proxyAddr,
		"NODE_USE_ENV_PROXY=1", // Node 24+: honor HTTP(S)_PROXY in built-in fetch/http
	}
	if caCert != "" {
		env = append(env,
			"SSL_CERT_FILE="+caCert,
			"CURL_CA_BUNDLE="+caCert,
			"REQUESTS_CA_BUNDLE="+caCert,
			"GIT_SSL_CAINFO="+caCert,
			"NODE_EXTRA_CA_CERTS="+caCert,
		)
	}
	return env
}

// workerDefaultTmpl is the built-in EDITABLE body (section [A]) of the worker system
// prompt, seeded into agent_prompts. The trafficTool block and the intermediate-artifact output spec
// are NOT here — they are code-owned and appended by workerSystem after rendering
// (sections [B]/[C]), so editing the DB body can never drop them.
const workerDefaultTmpl = `You are the "executor" (work agent) of an authorized penetration-testing system. You are assigned [one intent] (a one-sentence exploration direction); your sole duty: **complete exactly this intent, write the facts you find back into the knowledge graph, then stop and return.**

Iron rules (follow them strictly):
1. **Do only this one intent.** The intent boundary is your red line. A fingerprinting intent means fingerprinting only -- do not casually enumerate endpoints, brute-force directories, scrape JS for APIs, or test vulnerabilities; those belong to [other intents], dispatched by the planner to other workers.
2. **You do not own "exploration directions".** Found a lead worth pursuing? Do not keep going yourself; write it into the graph -- the planner reads it and generates new intents. Creating exploration directions is the planner's job, not yours.
3. **Exhaust, then return; never give up at the first obstacle.** "Intent attained" means [you have genuinely probed this direction through]: an initial block (a payload filtered, an endpoint 404, an injection point with no reflection) does not mean dead end -- first vary encoding/method/params/path to exhaust the reasonable means within this intent, then conclude. **The boundary stands**: exhausting applies only to means [inside this one intent], never casually doing other intents (enumerating other endpoints, testing other vulns); those remain for workers dispatched by the planner. Once truly exhausted, or the dead end is confirmed, write back and return at once -- do not continue just because "the overall task goal is not yet met", and do not spin on exhausted directions to burn steps.
4. **Write back as you discover, and to the right place.** Record each result the moment you get it (never hoard until the end, or the step budget eats it all). Only what is in the graph counts; results living in your head/notes do not. **Keep the two graphs apart**:
   - **New asset/resource found** -> insert_assets writes the [asset graph] (register the asset itself: endpoint / parameter / tech fingerprint / service / credential / subdomain etc.). Structured attributes live on the asset: site/endpoint status code/title/body length/content_type in props.http, tech stack as a type=tech node. Register multiple assets in one batch via insert_assets' assets array.
   - **Exploration conclusion/fact reached** (positive results like fingerprints/enumeration, and negative ones like "port closed", "param not injectable", "no login page found") -> record_fact into the [exploration graph] (pass intent_id). **Consolidate the multiple observations of one exploration into [ONE] fact**: summary is the one summarizing sentence, detail holds the specifics (stack, status codes, response traits all go in that one detail) -- **never one fact per attribute**; one intent usually yields one fact, over-splitting bloats the graph. Use the facts array in one call only for genuinely [distinct] conclusions. [New] **write increments only**: before writing, glance at recent_facts in the [global exploration situation] above -- record only conclusions [newly obtained] this run, do not re-record existing facts reworded (duplicates bloat the graph and mislead the planner into seeing progress). If your observation merely confirms an existing fact with nothing added, skip recording.
   - **Vulnerability confirmed** -> report_finding into the [exploration graph] (with PoC, pass intent_id).
   - **Exposure/info leak is a finding, not just an asset**: sensitive surfaces reachable without authentication -- admin/debug endpoints, Swagger/API docs, config/backup/source files, sensitive-data leak endpoints etc. -- [as soon as you actually access them and hold a response proving the exposure], that is a [confirmed] finding (reachable + response = triggered, not speculation) -> record with report_finding (severity by sensitivity: generic info leak=low, unauth-reachable sensitive/admin endpoint=medium, direct takeover or credentials/keys=high). **The distinction**: "this endpoint exists" is an asset (insert_assets); "reachable unauthenticated and exposing content it should not" is a finding (report_finding) -- do not file the latter only as asset/fact. When the hard-goal exploit did not break through, such proven exposures are the findings you should deliver -- do not record none.

Available tools:
- insert_assets: register new assets (asset graph). **You pass raw info only; keys and parent/child links are computed by code**: new endpoint -> full url+method (code auto-builds domain→site→endpoint and extracts query params from the URL; body/header params go in params); fingerprint -> type=tech,name=<tech>,on_url=<site url>,props={version,category}. Batch multiple assets via the assets array in one call. Attributes go on the asset's own props; exploration conclusions do not belong here.
- record_fact: write an exploration [fact/conclusion] into the exploration graph, linked to the intent (pass intent_id). For positive/negative conclusions, observations, judgements; **consolidate one exploration's observations into one fact** (summary sentence + details in detail), never one fact per attribute. Use the facts array only for genuinely distinct conclusions. **Record only what you actually saw**: give evidence (one line: command + key output, terse, no dumps -- specifics in detail) and confidence (observed = seen directly / inferred = deduced); **evidence bar for negative conclusions** (not injectable/port closed/no login page -- conclusions that [may make the planner drop a whole direction]): before concluding, confirm you have [exhausted the reasonable means within this intent] (encoding/params/path/method variants); if the means are not exhausted, or the evidence is merely "looks like/probably", always mark confidence=inferred. **Better inferred (planner rechecks) than a hasty observed negative welding an entire route shut** -- early in the task especially, one wrong observed negative derails the whole task and is hard to walk back.
- report_finding: vulnerability confirmed -> record it (with PoC; intent_id = the intent you were assigned). **Use it only for vulnerabilities you actually triggered in this run with reproducible evidence (request/response or command output).** Never report the following as confirmed vulnerabilities: a CVE matched by version/fingerprint alone, "the param looks injectable", inference from external vuln DBs/changelogs/code diffs. **Never substitute CVE lookup or "patch version comparison" for actually triggering it.** Cannot trigger but genuinely suspicious? record_fact with confidence=inferred (describe the suspicion + why it would not trigger) and let the planner dispatch follow-ups; do not force it in as a finding.
  **[Always fill the structured reproduction]** (you just ran it; nobody can reconstruct it accurately later): entry_point (key endpoint triggering the vuln, method+URL) - prerequisites (accounts/roles/prior steps needed; for a test account state [how to register] -- endpoint + key request details, reproducible from zero) - steps (ordered; per step request=your actual request verbatim [do not rewrite/invent], response=the real response [status code + key headers + key body fragments, verbatim; if long keep the fragments that prove it], expected=one-line expectation/conclusion, proves=what it proves) - provenance (attack-chain lineage/dependencies). Give real raw request/response wherever possible -- this reproduction decides whether a human can repeat it; fill it carefully.
- list_assets (query assets, not exploration nodes) / asset_neighbors / list_facts (exploration facts) / list_findings (vulnerabilities) / node_detail (exploration node id, not asset id): query context on demand.
- [Use only when needed; most context is in this session] search_all_worker_traces / list_worker_traces / get_worker_trace: **reuse information across works** (things that appeared in other works' execution but were never written as facts). You cannot see the exploration graph, but you can: search_all_worker_traces(q) keyword-search every work's process (returns intent_id); list_worker_traces to see which works ran; get_worker_trace(intent_id) for a work's step summaries and get_worker_trace(intent_id, step_ids=[...]) for those steps in full (max 5 per call). **For reusing others' observations and avoiding duplicate effort only -- it does not widen your scope (still only your one intent).**

Operate only within the authorized scope. After finishing this intent, summarize in one sentence what you did and which facts you wrote back. Pragmatic, restrained, focused on this one intent.`

// workerTrafficBlock is section [B]: the traffic-tool note, code-injected only when
// traffic capture is on (proxyAddr set). Not stored, not editable.
func workerTrafficBlock(proxyAddr string) string {
	if proxyAddr == "" {
		return ""
	}
	return "\n\n**Traffic tools**:\n- traffic_search / traffic_get: the recording proxy has persisted all target traffic (local tree data/traffic/<host>/<url path>/). To review responses or find already-fetched resources, **check recorded traffic first; never re-curl the same URL**. traffic_search **requires host** and by default returns only 3 ultra-light index rows (id/method/url/status/resp_len, no body); raise limit explicitly for more; traffic_get(id) for a raw exchange."
}

// artifactSpec is section [C]: the code-owned, non-editable tail appended to every
// pentest agent's prompt — intermediate artifacts must land in the shared work
// dir, never /tmp. Guaranteed present regardless of how the DB body is edited.
func artifactSpec(workDir string) string {
	return "\n\n**Intermediate-artifact output spec**: scripts, payloads, captured response bodies, temp data -- every intermediate artifact goes **into the shared work directory " + workDir + "** (the CWD shared by all agents; relative paths land here, or use that absolute path) -- **never /tmp, never other absolute paths**."
}

// workerArtifactSubdir is the worker-only addendum to artifactSpec: put a run's
// artifacts under an i<intentID>/ subdir to avoid concurrent name collisions.
const workerArtifactSubdir = "To avoid filename clashes with other works, put this run's artifacts in subdirectory i<intent id>/ (e.g. i123/exploit.py)."

func workerSystem(proxyAddr, workDir string) string {
	body := renderSystem("worker", workerDefaultTmpl, WorkerVars{ProxyAddr: proxyAddr})
	return body + workerTrafficBlock(proxyAddr) + artifactSpec(workDir) + workerArtifactSubdir
}

// renderIntentTask formats the claimed intent for the worker's SYSTEM prompt: the
// intent is the worker's whole job, so — like the planner's situational block — it
// belongs in system, where compaction can never drop it during a long run.
func renderIntentTask(intent *db.Node) string {
	return fmt.Sprintf("\n\n[Your assigned intent (this run's only task: do just this one, produce facts, stop when done)]:\n%s\nintent id: %d (pass it when writing record_fact / report_finding)", string(intent.Payload), intent.ID)
}

// renderWorkerGraphOverview folds the global situational snapshot into the worker's
// SYSTEM prompt for AWARENESS ONLY. The framing is deliberately strong: the overview
// must NOT widen the worker's job — it still does only its assigned intent. Its sole
// purpose is letting the worker read context (existing facts/assets/hints)
// so it avoids redundant work and doesn't re-derive what others already found.
func renderWorkerGraphOverview(data map[string]any) string {
	b, err := json.Marshal(data)
	if err != nil {
		return "" // fall back silently: the worker just won't have the global context
	}
	return "\n\n[Global exploration situation (for your awareness only, not your task list)]:\n" +
		"Below is the task-wide exploration snapshot. Its [sole purpose] is to let you see the big picture, reuse existing conclusions, and avoid repeating what others already did.\n" +
		"**It never widens your scope**: you still do only the one intent assigned above. Other open intents / uncovered spots / attackable directions you spot here -- [never act on them yourself]; they belong to other workers, scheduled by the planner. If you notice a relevant new lead, at most record it as a fact for the planner; do not chase it.\n" +
		string(b)
}

// Execute runs one intent. hooks (the per-task Guard) gates every tool call; may
// be nil. emit, if non-nil, receives one ActivityRecord per execution step.
// Returns the terminal reason (so the engine can distinguish completed vs
// max_turns) and a per-kind breakdown of what was written back (so an intent that
// explored but persisted nothing isn't mistaken for done, and the engine can log
// facts/assets/findings separately instead of lumping them under "facts").
func (w *Worker) Execute(ctx context.Context, name string, taskID int64, as *db.AssetStore, ts *db.ExplorationStore, intent *db.Node, hooks harness.HookRunner, emit func(db.Activity), enr EnrichTrigger) (harness.TerminalReason, WriteCounts, error) {
	tsx := NewToolSet(ts, name)
	tsx.SetTaskID(taskID)
	tsx.SetTrafficResolver(w.trafficResolver)
	if as != nil {
		tsx.SetAssetStore(as, as.Companies())
	}
	tsx.SetOwnerNode(intent.ID) // assets this worker discovers anchor to its intent → visible to the task
	tsx.SetEnrich(enr)          // async DNS/HTTP auto-completion for assets this worker writes
	// base = built-in worker tools ∪ host tools (traffic) ∪ default tools (incl. Bash);
	// then augment with the agent's visible skills/MCP. During the SDK settlement
	// phase, Bash is hidden via Settlement.DisabledTools (no local gating needed).
	base := append(tsx.WorkerTools(), w.extraTools...)
	base = append(base, actool.DefaultTools()...)
	tools, def, cleanup := AugmentTools(ctx, "worker", base)
	defer cleanup()

	// The intent is the worker's entire task: put it in the system prompt (compaction-proof, present the whole round),
	// not the user input -- mirroring the planner keeping key situation in system. After the instructions, before the
	// deferred block. The global situation (graphOverviewData) is included too, awareness only; intent goes last, most prominent.
	overview := renderWorkerGraphOverview(tsx.graphOverviewData())
	system, boundary := deferredSystem(workerSystem(w.proxyAddr, w.workDir)+overview+renderIntentTask(intent), def)
	// The task-level deadline (injected via ctx) clamps this run's wall-clock budget and picks the wrap-up words (see taskclock.go).
	tc := taskClockFrom(ctx)
	maxDur, clamped := clampMaxDuration(tc.DeadlineUnix, w.runTimeout)
	settle := wrapupSettlement("worker", []string{"Bash"})
	if tc.DeadlineUnix > 0 {
		settle = wrapupSettlementForTask("worker", []string{"Bash"}, clamped)
	}
	opts := agentcore.Options{
		Provider:        w.prov,
		SystemPrompt:    system,
		DynamicBoundary: boundary,
		Tools:           tools,
		DeferredTools:   def.Deferred,
		UnlockSet:       def.Unlock,
		PermissionMode:  permission.ModeBypass,
		// WebFetch goes through the recording proxy, so its HTTP is logged like curl; loading the proxy CA lets MITM-re-signed
		// HTTPS certificates [verify normally] (instead of disabling verification). Empty proxy = direct.
		EnableWebFetch: true,
		WebFetchProxy:  w.proxyAddr,
		WebFetchCACert: w.proxyCACert,
		// Web search (optional). ddgs needs no key; brave-free needs BraveKey; tavily needs TavilyKey.
		// WebSearchProxy is a separate egress proxy (http/https/socks5), unrelated to the traffic-recording MITM proxy; empty = direct.
		EnableWebSearch:    w.webSearch.Enabled,
		WebSearchBackend:   w.webSearch.Backend,
		BraveSearchAPIKey:  w.webSearch.BraveKey,
		TavilySearchAPIKey: w.webSearch.TavilyKey,
		WebSearchProxy:     w.webSearch.Proxy,
		// HTTP from Bash subcommands defaults through the recording proxy + trusts its CA (tools need no -x/-k).
		BashEnv:    proxyEnv(w.proxyAddr, w.proxyCACert),
		WorkingDir: w.workDir,
		MaxTurns:   w.maxTurns, // 0 = unlimited (configurable in agent management)
		// Wall-clock budget, judged at round boundaries, never mid-round; 0 = unlimited. With a task deadline, clamp to
		// min(own budget, time remaining) so this run enters wrap-up naturally when the task expires (see taskclock.go).
		MaxDuration: maxDur,
		// On hitting the budget (turn OR duration) -> SDK runs one wrap-up round (Bash hidden) to record what was found, avoiding a dead-end finish.
		// When clamped (by the task deadline) use PromptByReason: timeout = task expiry -> task-timeout words;
		// steps = budget exhausted inside the clamped window -> fall back to per-run words. Unclamped stays pure per-run.
		Settlement: settle,
		// large tool output spills to cmd-output/ with a head + pointer (SDK tool.Capture);
		// full output preserved on disk. Truncation cap is the SDK default (30000 chars).
		ToolOutputDir: filepath.Join(w.workDir, "cmd-output"),
		Compaction:    compactionConfig(w.window), // long tool-heavy runs stay within the window
		Todos:         actool.NewTodoStore(),      // session-scoped scratch todos (TodoWrite), planning only, dropped on exit
	}
	if hooks != nil { // typed-nil guard: only set when concrete (avoids harness panic)
		opts.Hooks = hooks
	}
	if w.mem != nil {
		opts.Memory = &agentcore.MemoryOptions{Store: w.mem, AutoInject: true, MaxInject: 3}
	}
	if w.tx != nil { // persist raw LLM conversation; one file per worked intent
		opts.Transcript = w.tx
		opts.SessionID = fmt.Sprintf("exp%d-worker-i%d", ts.ID(), intent.ID)
	}
	intentID := intent.ID
	emitWrap := func(r db.Activity) {
		if emit != nil {
			r.NodeID, r.Worker = &intentID, name
			emit(r)
		}
	}
	// The intent is already in system. The user input carries only the kick-off instruction.
	input := "Start executing the intent in the system prompt: do only it, produce facts only, stop when done."

	s := agentcore.NewSession(opts)
	defer s.Close() // release the session's background-task manager (temp dir + processes)

	// Budgets + settlement are owned by the SDK (MaxTurns/MaxDuration + Settlement):
	// on hit it runs a wrap-up turn and finishes with ReasonMaxTurns/ReasonTimeout.
	// We only add a HARD ctx backstop past the soft wall-clock budget, for the rare
	// case a single turn hangs so the turn-boundary check never runs — the soft
	// budget handles the normal case gracefully (no mid-stream abort). pause /
	// planner kill cancel ctx; the engine distinguishes those and re-queues/stops.
	// backstop uses the EFFECTIVE (deadline-clamped) budget, not the raw runTimeout,
	// so a task-timeout run's hard deadline sits just past its clamped soft budget.
	runCtx := ctx
	if maxDur > 0 {
		var runCancel context.CancelFunc
		runCtx, runCancel = context.WithTimeout(ctx, maxDur+settleHardGrace)
		defer runCancel()
	}
	_, reason, err := captureRunSession(runCtx, s, input, emitWrap)
	return reason, tsx.Writes(), err
}
