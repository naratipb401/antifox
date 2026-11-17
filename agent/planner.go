package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"antifox/engine/agentcore"
	"antifox/engine/llm"
	"antifox/engine/permission"
	actool "antifox/engine/tool"
	"antifox/engine/transcript"
	"antifox/db"
)

// Planner is the event-driven LLM planner (docs §4.3): each time the asset or
// exploration graph changes (debounced), it reads the exploration route, queries
// assets, judges whether the task goal is met, and emits 0..N exploration intents
// into the frontier. It is the sole intent generator.
type Planner struct {
	prov        llm.Provider
	model       string
	tx          *transcript.Store                      // raw LLM conversation persistence (nil = off)
	window      int                                    // context window in tokens (for compaction)
	maxTurns    int                                    // max agent turns per run (0 = unlimited)
	killWork    func(intentID int64) error             // engine callback to terminate a running work (nil = off)
	steerWork   func(intentID int64, msg string) error // engine callback to steer a running work mid-run (nil = off)
	proxyAddr   string                                 // recording proxy for WebFetch (empty = direct)
	proxyCACert string                                 // recording proxy's CA cert path (HTTPS verify)
	webSearch   WebSearchOpts                          // web_search tool backend selection (off by default)
	workDir     string                                 // shared work dir (surfaced in prompt as artifact-output target)

	// todos keeps ONE plan-scratchpad per task (keyed by exploration id) so the
	// planner's multi-step plan survives across wake-ups — each Plan() is a fresh
	// session, but the shared store lets it record a serial exploit chain once and
	// dispatch it step-by-step over rounds instead of front-loading it in parallel.
	todoMu sync.Mutex
	todos  map[int64]*actool.TodoStore
}

func NewPlanner(prov llm.Provider, model, workDir string, tx *transcript.Store, window, maxTurns int) *Planner {
	return &Planner{prov: prov, model: model, workDir: workDir, tx: tx, window: window, maxTurns: maxTurns, todos: map[int64]*actool.TodoStore{}}
}

// SetProxy points the planner's WebFetch at the recording proxy plus the CA cert
// it trusts to verify HTTPS through it (empty addr = direct).
func (p *Planner) SetProxy(addr, caCert string) { p.proxyAddr, p.proxyCACert = addr, caCert }

// SetWebSearch selects the web_search backend for the planner (off by default).
func (p *Planner) SetWebSearch(o WebSearchOpts) { p.webSearch = o }

// todoFor returns the task's persistent planning todo store, creating it on first
// use. Shared across all of this task's planner wake-ups.
func (p *Planner) todoFor(expID int64) *actool.TodoStore {
	p.todoMu.Lock()
	defer p.todoMu.Unlock()
	s := p.todos[expID]
	if s == nil {
		s = actool.NewTodoStore()
		p.todos[expID] = s
	}
	return s
}

// SetKillWork wires the engine's per-work terminate callback so the planner's
// kill_work tool can stop a single running worker.
func (p *Planner) SetKillWork(fn func(intentID int64) error) { p.killWork = fn }

// SetSteerWork wires the engine's per-work steering callback so the planner's
// steer_work tool can inject a mid-run course-correction into a running worker.
func (p *Planner) SetSteerWork(fn func(intentID int64, msg string) error) { p.steerWork = fn }

// renderPlannerTodos formats the persistent planning todo for injection into the
// wake-up prompt (empty when there are no todos yet — first wake-up).
func renderPlannerTodos(items []actool.Todo) string {
	if len(items) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\n[Your planning todos (kept across wakes; written by you last round)]:\n")
	for _, it := range items {
		mark := map[actool.TodoStatus]string{actool.TodoPending: "☐", actool.TodoInProgress: "▶", actool.TodoCompleted: "✔"}[it.Status]
		if mark == "" {
			mark = "☐"
		}
		b.WriteString(fmt.Sprintf("  %s %s\n", mark, it.Content))
	}
	b.WriteString("Proceed accordingly: dispatch intents only for next steps whose [prerequisites are done / their fact already exists]; update the list with TodoWrite (mark steps satisfied by a fact as completed). Do not re-dispatch steps already pending/in_progress in the list.")
	return b.String()
}

// renderDoneIntents surfaces the intent(s) whose completion triggered this round
// so the planner looks first at their fresh yields (in the situation below) before
// deciding whether a new direction opened up. Empty for non-completion wakes.
func renderDoneIntents(ids []int64) string {
	if len(ids) == 0 {
		return ""
	}
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = fmt.Sprintf("#%d", id)
	}
	return "\n\n[Just-completed intent(s) that triggered this round]: " + strings.Join(parts, ", ")
}

// renderGraphOverview folds the pre-computed graph_overview snapshot into the
// wake-up prompt so the planner starts each round with the full situation in
// hand — saving the round-trip it would otherwise spend calling the tool. It is
// the exact same JSON graph_overview would return; deeper detail is still one
// tool call away (node_detail / list_facts / …).
func renderGraphOverview(data map[string]any) string {
	b, err := json.Marshal(data)
	if err != nil {
		return "" // fall back to the model calling graph_overview itself
	}
	return "\n\n[Round situation (pre-fetched graph_overview; identical to calling the tool; call node_detail/list_facts etc. on demand for details)]:\n" + string(b)
}

// plannerDefaultTmpl is the built-in EDITABLE body (section [A]) of the planner prompt,
// tail is code-owned (artifactSpec) and appended by plannerSystem after rendering.
const plannerDefaultTmpl = `You are the "planner" of an authorized penetration-testing system. You are woken frequently (every graph change). Your job: read the situation, judge goal attainment, and add exploration intents **only for genuinely uncovered new directions**.

Task goal: {{.Goal}}

Warning: **emitting 0 intents is a legal, normal outcome -- but it needs a [ concrete legitimate reason], not a "fewer dispatches are safer" default.** Emit 0 intents only when:
- **Already covered**: every direction you can think of is covered by open / running / recent_done intents -- regenerating existing intents verbatim or reworded is a serious mistake; or
- **Waiting on dependencies**: the next step depends on output of a [currently running work] that has not landed yet -- in that case [wait for the next wake after those works finish and the graph updates]; dispatching now makes downstream workers spin without prerequisites that do not exist yet.
Conversely: if there genuinely are [new directions uncovered by any intent and independent of running works], or [the goal is unattained and untested surfaces remain in scope], do [not] hold back just because "0 intents is common" -- dispatch when warranted. Never treat 0 intents as a lazy default.

Decision procedure (every wake):
1. **The full situation is attached directly below in this prompt (it IS the graph_overview return; no need to call it)**: task (**original task title + goal**, the root node), asset counts, goals and their states, open/running/recent_done intents, sites_without_endpoints, findings (**confirmed vulnerability** count), facts (**exploration fact/conclusion** count, a different class from vulnerabilities), recent_facts (recent facts {id, summary, confidence?}, incl. **negative results** like "port closed/not injectable" -- use them to avoid re-intents for already-proven dead ends; but a confidence=inferred negative is only an [inference] with weak evidence, not hard fact -- if that direction is critical to the goal, a recheck intent is worthwhile). **Exploration nodes here (goals/intents/facts/findings) cover only this task** (never another task's goals); **the asset graph is global and shared** (one copy across tasks; asset counts are global in-scope figures, not per-task). Dig deeper only on demand: list_facts lists all facts, list_findings all vulnerabilities, **node_detail(id)** full evidence/detail for one entry (lists/recent_facts give summary only).
   - open_intents / running_intents / recent_done_intents -- "which directions already have an intent covering/attempting them". Each intent also carries **parents (upstream: which facts/intents it derives from) and yields (downstream: which facts/findings it produced)** -- the exploration graph\'s **lineage**, use it to see which facts came from which direction and whether they combine into a new one. Each fact in recent_facts carries **from_intent** (producing intent).
   - sites_without_endpoints / findings -- directions that [may] need exploration.
   - Only when you need detail on one slice, call on demand: list_assets (pull mode: q keyword search, plus type/company_id/task_id filters, limit/offset paging; or fetch by id/ids), asset_neighbors, list_findings. The asset graph is global and shared; do not pull it in full by default. It may contain assets unrelated to this task -- ignore such assets when they show up.
2. Judge goals (core duty): the goals field of graph_overview already has goals and states; for an unattained goal already proven by some finding/fact, call prove_goal(goal_id,evidence_id,reason) to mark it met. **When the goal you just marked with prove_goal happens to be the last unattained one, the system auto-declares the whole task complete -- no need to call goal_met**; wrap-up should be driven by per-goal prove_goal.
   Warning: **quantified acceptance check (never stamp early)**: if a goal has [quantified acceptance criteria] (e.g. "obtain N flags / N vulnerabilities", "obtain a credential/sensitive data", "cover a set of assets/endpoints with conclusions"), then BEFORE prove_goal (and any form of wrap-up) you [must] verify the measured situation in the graph_overview above (findings count, relevant facts, whether the corresponding assets all have conclusions): if measurement shows the bar [not met], you are forbidden from marking the goal attained -- dispatch intents to close the gap instead. Do [not] mark met early on grounds of "mostly done/broadly attained/core goal secured", and do not bypass this check with goal_met. E.g. goal demands "3 flags" but the graph proves only 1: the goal is [unattained]; keep dispatching supplementary intents, no completion mark.
2.5. **(Optional, opening only, very lightweight) probe to understand**: you have Bash and similar execution ability; its [sole legitimate use] is -- when the [graph has almost no facts yet] (recent_facts basically empty, task just started) and the situation alone cannot make the initial intents concrete -- a [tiny, read-only] probe of the target (e.g. 1-2 curls of the homepage/fingerprint) to produce a sharper **initial intent**.
   Warning: **remember your role boundary: you are the "planner", not the "executor". Everything here serves [generating/articulating intents], never [doing the work inside the plan].** The [only legitimate product of probing is a sharper intent sentence] -- never vulnerability discovery/validation/exploitation, nor endpoint/directory/param enumeration results. Any "I tested/confirmed this in passing" is scope creep: that is the worker's job in the work phase; you only [write it up as an intent and dispatch it].
   **Three hard boundaries; hold them:**
   - **Once the graph has any worker-produced fact (facts>0 / recent_facts non-empty), probing yourself is [forbidden].** All judgement then rests on existing facts; your round's only outputs are "dispatch new intents" or "end the round". Want to dig into a lead? The right move is an intent for a worker, never your own curl.
   - Even at the opening, probe at most a few times (<=3) and stop, solely to make the initial intents clear; never slide into enumerating endpoints/directories one by one, trying ids, base64 decode chains, re-probing the same endpoint, or any injection/auth-bypass/vuln testing -- all of that is the worker's heavy lifting, the content of intents you generate, not your own work. The moment you notice "deep verification" instead of "quick direction-setting", stop and write the rest as intents.
   - Probing only helps you "think it through"; it [produces no intent by itself]; if existing facts/the situation suffice, do not probe at all.
3. **Decide whether to add exploration directions (with restraint)**: an intent is an [open exploration direction], not a fixed type/menu -- weighing known facts, assets and the task goal, judge for yourself which valuable, not-yet-covered directions bring the goal closer. Compare each against open_intents + running_intents + recent_done_intents:
   - Already covered by an open or running intent -> do [not] regenerate (being handled).
   - Already attempted per recent_done_intents (even without output) -> do [not] retry as-is. Treat it as a [blocked route]: re-dispatch only on a [materially new mechanism] -- new fact, new asset, new parameter, or a clearly different approach/construct -- and say in the new intent's summary "how this differs from last time". A mere rewording or "maybe it works this time" is not a new mechanism; no retry. Conversely, if a negative conclusion is weak (confidence=inferred) and the direction is goal-critical, one [recheck intent] to confirm or refute it is legitimate (that counts as a new reason).
   - Generate only for [brand-new directions covered by no intent at all].
   - All known directions covered by existing intents -> [emit no intents, end the round].
   - **Keep routes diverse; do not converge early onto one**: while the goal is unattained, if every existing intent crowds the [same attack route/entry class] while [essentially different] uncovered directions remain (another entry surface, another asset class, another exploit chain), prefer adding one such divergent route over stacking synonyms on the same line. The ideal is 2-3 mutually independent, mechanically distinct routes alive at once (e.g. "via the upload chain" vs "via auth bypass"); concentrate resources on a route only once it shows evidence of [goal progress]. Judge diversity by substantive difference of direction, not wording. (No conflict with "0 intents is normal": add only genuinely different, uncovered directions; if the blank routes are already covered, still 0 intents.)
3.5. **Serial exploit chains: dispatch stepwise, not all-parallel at once.** Many exploits are a [hard-ordered serial chain] (e.g. (1) -> (2) -> (3)) where each step needs the previous step's [actual output]. Then:
   - Do [not] dispatch (1)(2)(3) simultaneously as parallel intents (downstream workers lack prerequisites that do not exist yet and will duplicate/spin);
   - [Record the whole chain as todos with TodoWrite] (one per step), and this round dispatch only the step whose [prerequisites are satisfied] (usually the first);
   - Once that step yields its fact, the next wake (your todo list is included in the prompt) dispatches the next step and marks satisfied steps completed;
   - Do not split "the same thing" into two intents (e.g. "confirm the trigger" and "trigger the trigger" are one step). Only [parallel exploration dimensions] (e.g. enumerating several unrelated endpoints simultaneously) justify multiple parallel intents.
4. Submit the directions screened in step 3 with [one] add_intent call (intents array, at most the 4 highest-value; never call it repeatedly item by item):
   - summary: one free-form sentence for the direction (what + why), natural language, [no fixed taxonomy]. The direction must be clear in the summary; dedup relies mainly on comparing with existing intents.
   - asset_ids: [target asset ids] this direction will test/attack (**pass when possible**, 0/1/many; asset ids from list_assets). Whenever the direction concerns concrete assets (a site/endpoint/param/host) [always pass them] -- it marks "which targets this exploration hits", used for coverage dedup and linking the intent into the asset graph; pass all when spanning several assets. Leave empty only for purely global recon with no concrete target asset.
   - parent_ids: which [upstream nodes] this direction derives from (optional, 0/1/many). **When several facts combine into one intent, pass all their ids**; derived from an upstream intent/finding, pass its id too; brand-new top-level directions leave it empty.


Better to emit nothing than to duplicate or pad. Concise, restrained, efficient.`

func plannerSystem(goal, workDir string) string {
	body := renderSystem("planner", plannerDefaultTmpl, PlannerVars{Goal: goal, Now: time.Now().Format("2006-01-02 15:04:05 MST")})
	return body + artifactSpec(workDir)
}

// Plan runs one planning round. emit, if non-nil, receives the planner's execution
// steps (so users can see how it reads the situation and judges goals — the
// planner is the intent generator and was previously a black box). Returns whether
// the planner judged the goal met.
// doneIntents carries the ids of the intent(s) whose completion triggered this
// round (may be several — the engine debounces a burst; empty for non-completion
// wakes). They are surfaced in the prompt so the planner focuses on the fresh
// yields of what just finished.
func (p *Planner) Plan(ctx context.Context, taskID int64, as *db.AssetStore, ts *db.ExplorationStore, goal string, doneIntents []int64, emit func(db.Activity)) (met bool, reason string, err error) {
	tsx := NewToolSet(ts, "planner")
	if as != nil {
		tsx.SetAssetStore(as, as.Companies())
	}
	tsx.SetTaskID(taskID)
	tsx.killWork = p.killWork   // enable kill_work tool (nil = unavailable)
	tsx.steerWork = p.steerWork // enable steer_work tool (nil = unavailable)
	if origin, _ := ts.OriginFactID(); origin > 0 {
		tsx.SetOwnerNode(origin) // planner-side anchors default to the task root (origin fact)
	}
	// Domain tools + base default toolset (Read/Write/Edit/MultiEdit/LS/Glob/Grep/Bash)
	base := append(tsx.PlannerTools(), actool.DefaultTools()...)
	tools, def, cleanup := AugmentTools(ctx, "planner", base)
	defer cleanup()
	// Key situation (just-finished intents + pre-fetched full graph) goes into the system prompt, not the user input:
	// the system section is never compacted away, so the situation stays present in long rounds; fixed within the round,
	// it also hits the system cache. It sits after the instructions, before the deferred-tools block (deferredSystem keeps that last).
	situational := renderDoneIntents(doneIntents) + renderGraphOverview(tsx.graphOverviewData())
	// Task-level deadline / endgame mode (injected via ctx, see taskclock.go). On the endgame round the task-timeout
	// planner wrap-up words are injected into system as [this round's operating instructions]: final goal judgement only, no new intents.
	tc := taskClockFrom(ctx)
	if tc.Final {
		situational += "\n\n[Task endgame wrap-up (special instructions for this round; override the regular planning procedure above)]: " + resolveTaskTimeoutWrapup("planner")
	}
	system, boundary := deferredSystem(plannerSystem(goal, p.workDir)+situational, def)
	// The planner has no wall-clock budget of its own; with a deadline, clamp MaxDuration to the remaining time so a
	// running planning round enters wrap-up when the task expires (timeout -> task-timeout words; step budget -> per-run words).
	maxDur, clamped := clampMaxDuration(tc.DeadlineUnix, 0)
	settle := wrapupSettlement("planner", nil)
	if tc.DeadlineUnix > 0 {
		settle = wrapupSettlementForTask("planner", nil, clamped)
	}
	opts := agentcore.Options{
		Provider:        p.prov,
		SystemPrompt:    system,
		DynamicBoundary: boundary,
		Tools:           tools,
		DeferredTools:   def.Deferred,
		UnlockSet:       def.Unlock,
		PermissionMode:  permission.ModeBypass,
		EnableWebFetch:  true, // route through the recording proxy for audit; trust the CA that MITM-re-signs HTTPS
		WebFetchProxy:   p.proxyAddr,
		WebFetchCACert:  p.proxyCACert,
		// Web search (optional). ddgs needs no key; brave-free needs BraveKey; tavily needs TavilyKey.
		// WebSearchProxy is a separate egress proxy (http/https/socks5), unrelated to the traffic-recording MITM proxy; empty = direct.
		EnableWebSearch:    p.webSearch.Enabled,
		WebSearchBackend:   p.webSearch.Backend,
		BraveSearchAPIKey:  p.webSearch.BraveKey,
		TavilySearchAPIKey: p.webSearch.TavilyKey,
		WebSearchProxy:     p.webSearch.Proxy,
		BashEnv:           proxyEnv(p.proxyAddr, p.proxyCACert), // Bash subcommands default to proxy + trust CA
		MaxTurns:          p.maxTurns,                           // 0 = unlimited (configurable in agent management)
		MaxDuration:       maxDur,                               // 0=unlimited; with a deadline, time remaining until it
		Compaction:        compactionConfig(p.window),
		// Planning todos shared across wakes: serial chains survive across rounds (the session is new, the store is not).
		Todos: p.todoFor(ts.ID()),
		// On hitting [this round's] step budget -> SDK runs wrap-up: land the conclusions already reached (add_intent
		// dispatches, prove_goal markings, serial chain into TodoWrite) rather than halting planning -- the planner keeps
		// being woken afterwards. When clamped (by the task deadline) use PromptByReason instead (see wrapupSettlementForTask).
		Settlement: settle,
	}
	if p.tx != nil { // persist raw LLM conversation; one accumulating file per task's planner
		opts.Transcript = p.tx
		opts.SessionID = fmt.Sprintf("exp%d-planner", ts.ID())
	}
	// The situation (just-finished intents + full graph) is already folded into the system prompt above (compaction-proof,
	// present all round). The user input here carries only the instruction + cross-wake todos (todos are the model's own
	// planning scratchpad, regenerable, fine in user).
	input := "The graph changed; plan the next step: read the situation in the system prompt above and judge the goals. **If this round has no uncovered new direction, simply end it (0 intents is normal and common, especially right after dispatching while waiting on worker output). Never call goal_met just to end the round -- goal_met [ends the entire task immediately]; call it only once you are sure the goals are [truly attained] (goal deliverables in hand / goal vulnerabilities confirmed); while unattained, mark goals one by one with prove_goal, or end the round calling nothing.**" +
		renderPlannerTodos(opts.Todos.List())
	// With a deadline clamp, add a hard ctx backstop (soft budget + grace) so a stuck round cannot ride past the round-boundary soft timeout.
	runCtx := ctx
	if maxDur > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, maxDur+settleHardGrace)
		defer cancel()
	}
	_, _, err = captureRun(runCtx, opts, input,
		func(r db.Activity) {
			if emit != nil {
				r.Worker = "planner" // planner activity has no intent_id (it generates them)
				emit(r)
			}
		})
	return tsx.GoalMet, tsx.Reason, err
}
