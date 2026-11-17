package agent

import (
	"context"
	"fmt"

	"antifox/engine/agentcore"
	"antifox/engine/llm"
	"antifox/engine/permission"
	actool "antifox/engine/tool"
	"antifox/engine/transcript"
	"antifox/db"
)

// MainAgent is the thin human-interface orchestrator (docs §4.2 / §7). The human
// chats with it; it observes (read tools), and steers by injecting hints
// (→planner) or direct high-priority intents (→frontier). It does NOT run the
// autonomous intent-generation loop (that is the planner's job).
type MainAgent struct {
	prov        llm.Provider
	model       string
	tx          *transcript.Store // raw LLM conversation persistence (nil = off)
	window      int               // context window in tokens (for compaction)
	maxTurns    int               // max agent turns per run (0 = unlimited)
	proxyAddr   string            // recording proxy for WebFetch (empty = direct)
	proxyCACert string            // recording proxy's CA cert path (HTTPS verify)
	webSearch   WebSearchOpts     // web_search tool backend selection (off by default)
	workDir     string            // shared work dir (surfaced in prompt as artifact-output target)
}

func NewMainAgent(prov llm.Provider, model, workDir string, tx *transcript.Store, window, maxTurns int) *MainAgent {
	return &MainAgent{prov: prov, model: model, workDir: workDir, tx: tx, window: window, maxTurns: maxTurns}
}

// SetProxy points the main agent's WebFetch at the recording proxy plus the CA
// cert it trusts to verify HTTPS through it (empty addr = direct).
func (m *MainAgent) SetProxy(addr, caCert string) { m.proxyAddr, m.proxyCACert = addr, caCert }

// SetWebSearch selects the web_search backend for the main agent (off by default).
func (m *MainAgent) SetWebSearch(o WebSearchOpts) { m.webSearch = o }

// mainAgentDefaultTmpl is the built-in EDITABLE body (section [A]) of the main agent
// prompt, seeded into agent_prompts. Goal is a {{.Goal}} template var; the artifact
// output-policy tail is code-owned (artifactSpec), appended after rendering.
const mainAgentDefaultTmpl = `You are the "main agent" of an authorized pentest system, the interface for the human operator. You do not explore yourself and do not autonomously spawn intents in a loop (that is the planner's job). Your duties:

1. Observe: answer the human's questions about current progress with graph_overview / list_findings / list_facts / list_assets / get_worker_output.
2. Record findings (important, never miss): any vulnerability confirmed by a worker's verification, or confirmed by your own review, must be **persisted immediately with one report_finding call per vulnerability** — fill vulnclass (vulnerability name/type), severity (critical|high|medium|low; critical = can directly compromise the whole system/dataset, e.g. RCE, unauthorized arbitrary file read yielding keys/credentials, account-takeover auth bypass), summary (including affected target), evidence (verbatim reproducible request/command/response PoC), and asset_ids when the affected assets have ids; **also fill the reproduction structured repro whenever possible** (entry_point the entry interface / prerequisites incl. how to register test accounts / steps, each with the real request+expectation+proof / provenance the source) — take requests verbatim from the worker transcript/real execution, never rewrite them, so the report can be reproduced from scratch by a human. **Confirm means record: do not ask for permission and do not merely list vulnerabilities in your reply** — only vulnerabilities recorded via report_finding reach the findings page / exploration graph / reports; listing them in a reply without recording = wasted work. Check list_findings first to avoid duplicates. Never report speculation ("looks exploitable", version/CVE match alone) as a confirmed vulnerability. **Exposure / information-disclosure findings count too**: sensitive surfaces a worker has proven reachable without authorization (admin/debug interfaces, Swagger/API docs, exposed config/backup/source files, sensitive-data endpoints etc., with real response evidence) must each be report_finding-ed as well (severity by sensitivity: ordinary info leak=low, unauthorized access to sensitive/admin interface=medium, direct takeover or credential/key disclosure=high) — do not leave them as mere assets/facts. **When the user asks you to "sort out current findings", or on task wrap-up/pause**: persist every qualifying vulnerability and proven exposure the workers have written back (check list_findings/list_facts first, dedupe) — even when a hardened target resisted exploitation, hand over these proven exposures/risks instead of only listing them in chat.
3. Steer (turn human intent into system actions):
   - Human wants to "change direction / emphasize a vulnerability class / focus an area" → write a hint with add_hint (the planner reads it next wake-up).
   - Human wants a specific target tested right now → inject one high-priority intent directly with add_intent (priority 8-10).
4. Reply concisely in plain language: what you did and which findings you recorded.

Current task goal: {{.Goal}}

Never fabricate findings; record and answer only from real tool output and verified evidence.`

func mainAgentSystem(goal, workDir string) string {
	body := renderSystem("mainagent", mainAgentDefaultTmpl, MainVars{Goal: goal})
	return body + artifactSpec(workDir)
}

// Chat handles one human message and returns the assistant reply. emit, if
// non-nil, receives each execution step (thinking / tool_use / tool_result /
// text / result) so the main-agent session shows its work — exactly like the
// worker/planner sessions — not just the final answer.
func (m *MainAgent) Chat(ctx context.Context, taskID int64, as *db.AssetStore, ts *db.ExplorationStore, goal, message string, emit func(db.Activity), notify func()) (string, error) {
	tsx := NewToolSet(ts, "human")
	if as != nil {
		tsx.SetAssetStore(as, as.Companies())
	}
	tsx.SetTaskID(taskID)
	tsx.SetNotify(notify) // add_hint wakes this task's planner (debounced)
	// Domain tools + base default toolset (Read/Write/Edit/MultiEdit/LS/Glob/Grep/Bash)
	base := append(tsx.MainAgentTools(), actool.DefaultTools()...)
	tools, def, cleanup := AugmentTools(ctx, "mainagent", base)
	defer cleanup()
	system, boundary := deferredSystem(mainAgentSystem(goal, m.workDir), def)
	opts := agentcore.Options{
		Provider:        m.prov,
		SystemPrompt:    system,
		DynamicBoundary: boundary,
		Tools:           tools,
		DeferredTools:   def.Deferred,
		UnlockSet:       def.Unlock,
		PermissionMode:  permission.ModeBypass,
		EnableWebFetch:  true, // route through the recording proxy for audit; trust the CA that MITM-re-signs HTTPS
		WebFetchProxy:   m.proxyAddr,
		WebFetchCACert:  m.proxyCACert,
		// Web search (optional). ddgs needs no key; brave-free needs BraveKey; tavily needs TavilyKey.
		// WebSearchProxy is a separate egress proxy (http/https/socks5), unrelated to the traffic-recording MITM proxy; empty = direct.
		EnableWebSearch:    m.webSearch.Enabled,
		WebSearchBackend:   m.webSearch.Backend,
		BraveSearchAPIKey:  m.webSearch.BraveKey,
		TavilySearchAPIKey: m.webSearch.TavilyKey,
		WebSearchProxy:     m.webSearch.Proxy,
		BashEnv:           proxyEnv(m.proxyAddr, m.proxyCACert), // Bash subcommands default to proxy + trust CA
		MaxTurns:          m.maxTurns,                           // 0 = unlimited (configurable in agent management)
		Compaction:        compactionConfig(m.window),           // long chats stay within the window
		Todos:             actool.NewTodoStore(),                // session-scoped scratch todos (TodoWrite), planning only, dropped on exit
		// On hitting the turn budget -> SDK runs wrap-up: emit one progress summary to the user. Prompt and wrap-up turns are editable in the admin UI (default 5).
		Settlement: wrapupSettlement("mainagent", nil),
	}
	if m.tx != nil { // persist raw human↔AI conversation; one accumulating file per task
		opts.Transcript = m.tx
		opts.SessionID = fmt.Sprintf("exp%d-main", ts.ID())
	}
	s := agentcore.NewSession(opts)
	defer s.Close()
	// reload the prior conversation from the transcript so the agent has context
	// across turns (each Chat is a fresh session; without this it can't see earlier
	// messages). First turn: no file yet → Resume loads nothing and proceeds.
	if m.tx != nil {
		_ = s.Resume(opts.SessionID)
	}
	// C2: this session is fresh each turn; re-unlock skill-gated MCPs from prior
	// Skill() calls in the reloaded history so revealed tools stay callable.
	seedUnlockFromHistory(s.Messages(), def.UnlockSkill)
	text, _, err := captureRunSession(ctx, s, message, func(r db.Activity) {
		if emit != nil {
			r.Worker = "mainagent"
			emit(r)
		}
	})
	return text, err
}
