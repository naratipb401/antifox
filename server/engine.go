package server

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"antifox/engine/harness"
	"antifox/engine/llm"
	"antifox/agent"
	"antifox/db"
	"antifox/intercept"
)

// model_error (provider/API failure: the LLM layer exhausted its transient retries,
// or the stream broke mid-run after starting). The wrapped-up work neither "tried
// but ran out of turns" nor truly failed -- it was external flakiness. The default
// of leaving it permanently blocked would waste an intent, so this terminal state
// gets a few extra re-runs with backoff in between to give the provider time to
// recover; a pause/kill/cancel during the retry yields to the matching branch.
const (
	modelErrorRetries      = 2               // extra retries after a model_error wrap-up
	modelErrorRetryBackoff = 3 * time.Second // backoff before each retry
)

// Engine drives the event-driven exploration loop with real LLM agents
// (docs §4.3/§4.4): on asset/exploration-graph change (debounced) it wakes the
// planner, which reads the route, queries assets, judges goals and emits intents;
// N concurrent work agents claim intents and execute them. There is no
// simulation mode: an LLM provider is required. The planner/worker can be
// (re)installed at runtime (LLM configured from the UI); the loops always run
// but idle until an LLM is set.
type Engine struct {
	m        *Manager
	debounce time.Duration

	bc *Broadcaster // live activity pub/sub (SSE)

	mu      sync.RWMutex
	planner *agent.Planner
	worker  *agent.Worker
	started sync.Map // taskID -> bool, so Run is idempotent per task
	lastAct sync.Map // taskID -> int64 unix, last planner/worker activity (heartbeat)
	paused  sync.Map // taskID -> bool, user-paused (planner + workers idle but loops alive)

	// per-task execution context: each planner.Plan / worker.Execute runs under it,
	// so pausing can CANCEL an in-flight run (not just skip the next one). Recreated
	// on resume since cancelling is one-shot.
	execMu     sync.Mutex
	execCancel map[string]context.CancelFunc
	execCtx    map[string]context.Context

	// per-work (per running intent) cancel, so the planner's kill_work can stop a
	// single worker without pausing the whole task. Keyed by globally-unique intent id.
	workMu     sync.Mutex
	workCancel map[int64]context.CancelFunc

	// steerBox queues planner course-corrections for a running work (keyed by intent
	// id). The worker's PreToolUse hook drains it before its next tool call and hands
	// the message to the model (blocking that call) so it re-plans; no kill needed.
	steerMu  sync.Mutex
	steerBox map[int64][]string

	plannerRound sync.Map // taskID -> int, planner round counter (for UI round separators)

	// per-task timeout (see docs/task-timeout-and-wrapup.md):
	settling     sync.Map // taskID -> bool, task entered the wrap-up sequence (stop dispatching/claiming new intents)
	deadline     sync.Map // taskID -> int64 unix, absolute deadline (stamped on first run; 0/absent = no limit)
	stamped      sync.Map // taskID -> bool, whether first_run_at was stamped (once per process)
	inflight     sync.Map // taskID -> *int64, count of running planner.Plan + worker.Execute (for drain)
	coordStarted sync.Map // taskID -> bool, whether the deadline coordinator is running (deduped across Run/reload)

	// resolve, if set, returns a task's dedicated planner/worker when it pins a
	// specific LLM profile (nil,nil = use the global active pair). Set once at startup.
	resolve func(t *Task) (*agent.Planner, *agent.Worker)
}

// nextPlannerRound returns the next planner round number for a task (1-based).
func (e *Engine) nextPlannerRound(taskID string) int {
	v, _ := e.plannerRound.LoadOrStore(taskID, 0)
	n := v.(int) + 1
	e.plannerRound.Store(taskID, n)
	return n
}

// Pause stops a task: marks it paused AND cancels any in-flight planner/worker run
// for it (a long worker.Execute would otherwise keep going until it finishes).
func (e *Engine) Pause(taskID string) {
	e.paused.Store(taskID, true)
	e.cancelExec(taskID)
}

// cancelExec cancels a task's current per-task exec context (any in-flight
// planner.Plan / worker.Execute), if present. Shared by Pause and the settle
// sequence's hard-drain backstop.
func (e *Engine) cancelExec(taskID string) {
	e.execMu.Lock()
	if cancel := e.execCancel[taskID]; cancel != nil {
		cancel()
	}
	e.execMu.Unlock()
}

// Resume un-pauses a task and nudges a fresh planning round. The next exec under
// it gets a fresh (uncancelled) context.
func (e *Engine) Resume(t *Task) {
	e.paused.Delete(t.ID)
	t.Notify()
}

// Rerun continues a stopped/finished task (resume): the caller has already reset
// the task to a non-terminal status + cleared its run clock (Manager.ReactivateTask).
// Here we drop the stale deadline, ensure the planner/worker loops are alive, re-arm
// the timeout coordinator, and nudge a planning round. The exploration graph is kept.
func (e *Engine) Rerun(ctx context.Context, t *Task) {
	e.deadline.Delete(t.ID)     // clear stale absolute deadline (re-stamped on next run)
	e.coordStarted.Delete(t.ID) // allow the deadline coordinator to re-arm below
	e.paused.Delete(t.ID)
	e.Run(ctx, t)                      // start loops if not yet running this session; else nudge
	e.startDeadlineCoordinator(ctx, t) // re-arm timeout timer (dedup-safe via coordStarted)
	t.Notify()
}

// execContextFor returns a live per-task context derived from parent, recreating
// it if a prior pause cancelled it.
func (e *Engine) execContextFor(parent context.Context, taskID string) context.Context {
	e.execMu.Lock()
	defer e.execMu.Unlock()
	if e.IsPaused(taskID) {
		// never hand out a live context while paused (guards the claim→Execute race)
		c, cancel := context.WithCancel(parent)
		cancel()
		return c
	}
	if c := e.execCtx[taskID]; c != nil && c.Err() == nil {
		return c
	}
	c, cancel := context.WithCancel(parent)
	e.execCtx[taskID] = c
	e.execCancel[taskID] = cancel
	return c
}

// IsPaused reports whether a task is user-paused.
func (e *Engine) IsPaused(taskID string) bool {
	v, ok := e.paused.Load(taskID)
	return ok && v.(bool)
}

// Started reports whether the engine loops are running for a task.
func (e *Engine) Started(taskID string) bool {
	_, ok := e.started.Load(taskID)
	return ok
}

// LastActivity returns the unix time of the last planner/worker activity for a
// task (0 if none yet).
func (e *Engine) LastActivity(taskID string) int64 {
	if v, ok := e.lastAct.Load(taskID); ok {
		return v.(int64)
	}
	return 0
}

func (e *Engine) touch(taskID string) { e.lastAct.Store(taskID, time.Now().Unix()) }

func NewEngine(m *Manager) *Engine {
	return &Engine{m: m, debounce: 800 * time.Millisecond, bc: NewBroadcaster(),
		execCancel: map[string]context.CancelFunc{}, execCtx: map[string]context.Context{},
		workCancel: map[int64]context.CancelFunc{}, steerBox: map[int64][]string{}}
}

// registerWork records the cancel for the work currently running intentID.
func (e *Engine) registerWork(intentID int64, cancel context.CancelFunc) {
	e.workMu.Lock()
	e.workCancel[intentID] = cancel
	e.workMu.Unlock()
}

// unregisterWork removes and releases the work's cancel (called when Execute returns).
func (e *Engine) unregisterWork(intentID int64) {
	e.workMu.Lock()
	if c := e.workCancel[intentID]; c != nil {
		delete(e.workCancel, intentID)
		c() // release context resources (no-op if already cancelled)
	}
	e.workMu.Unlock()
	e.steerMu.Lock()
	delete(e.steerBox, intentID) // drop any undelivered steering for a finished work
	e.steerMu.Unlock()
}

// SteerWork queues a mid-run course-correction for the work running intentID (the
// planner's steer_work tool). The worker delivers it before its next tool call and
// re-plans; no kill. Errors if no work is currently running that intent.
func (e *Engine) SteerWork(intentID int64, msg string) error {
	if strings.TrimSpace(msg) == "" {
		return fmt.Errorf("steer message must not be empty")
	}
	e.workMu.Lock()
	running := e.workCancel[intentID] != nil
	e.workMu.Unlock()
	if !running {
		return fmt.Errorf("intent %d has no running work (it may have finished or not been claimed yet)", intentID)
	}
	e.steerMu.Lock()
	e.steerBox[intentID] = append(e.steerBox[intentID], msg)
	e.steerMu.Unlock()
	return nil
}

// drainSteer pops the oldest queued steering message for intentID (FIFO), if any.
func (e *Engine) drainSteer(intentID int64) (string, bool) {
	e.steerMu.Lock()
	defer e.steerMu.Unlock()
	q := e.steerBox[intentID]
	if len(q) == 0 {
		return "", false
	}
	msg := q[0]
	if len(q) == 1 {
		delete(e.steerBox, intentID)
	} else {
		e.steerBox[intentID] = q[1:]
	}
	return msg, true
}

// steerHooks wraps the guard's hook runner so the planner can steer a running work:
// before each tool call it drains a queued course-correction (if any) and blocks the
// call, handing the message back to the model, which re-plans its next step instead
// of running the tool. No queued message → the guard behaves exactly as before.
type steerHooks struct {
	inner harness.HookRunner
	drain func() (string, bool)
}

func (h steerHooks) PreToolUse(ctx context.Context, name string, input []byte) (bool, string, []byte) {
	if msg, ok := h.drain(); ok {
		return true, "[planner live steer] " + msg +
			"\n(This is the planner's immediate instruction for this intent; the current tool call was not executed. Adjust your next step accordingly. If this conflicts with your current plan, it takes precedence.)", nil
	}
	if h.inner != nil {
		return h.inner.PreToolUse(ctx, name, input)
	}
	return false, "", nil
}

func (h steerHooks) PostToolUse(ctx context.Context, name string, input, result []byte, isErr bool) {
	if h.inner != nil {
		h.inner.PostToolUse(ctx, name, input, result, isErr)
	}
}

func (h steerHooks) Stop(ctx context.Context, messages []llm.Message) (bool, []string, string) {
	if h.inner != nil {
		return h.inner.Stop(ctx, messages)
	}
	return false, nil, ""
}

// KillWork cancels the in-flight work running intentID (planner's kill_work tool).
// The work's agent-core session honors ctx cancellation and aborts promptly.
func (e *Engine) KillWork(intentID int64) error {
	e.workMu.Lock()
	c := e.workCancel[intentID]
	e.workMu.Unlock()
	if c == nil {
		return fmt.Errorf("intent %d has no running work (it may have finished or not been claimed yet)", intentID)
	}
	c()
	return nil
}

// Broadcaster exposes the engine's live activity pub/sub (used by the SSE handler).
func (e *Engine) Broadcaster() *Broadcaster { return e.bc }

// emitActivity persists one captured step AND fans it out to live subscribers,
// from a single point so storage and the SSE stream never diverge.
func (e *Engine) emitActivity(t *Task, r db.Activity) {
	id, err := e.appendActivity(t, r)
	if err != nil {
		// NO LONGER SILENT: dropping a record breaks command↔result pairing in the
		// trace: a tool_use whose tool_result was lost would show as "running" forever.
		log.Printf("[activity] task %s still failing after retries, dropping the record (kind=%s tool=%s tuid=%s): %v",
			t.ID, r.Kind, r.Tool, r.ToolUseID, err)
		e.touch(t.ID)
		return
	}
	r.ID = id
	if r.CreatedAt.IsZero() {
		r.CreatedAt = time.Now()
	}
	e.bc.Publish(t.ID, r)
	e.touch(t.ID)
}

// appendActivity persists one activity row, retrying briefly on write failure.
// Concurrent planner + worker inserts into the same exploration's activity log
// occasionally fail; a couple of quick retries recover most. Crucially, every
// failure is now LOGGED (it used to be swallowed by an `if err == nil`), so the
// underlying DB error is finally visible for diagnosis.
func (e *Engine) appendActivity(t *Task, r db.Activity) (int64, error) {
	var id int64
	var err error
	for attempt := 1; attempt <= 3; attempt++ {
		if id, err = t.Store.AppendActivity(r); err == nil {
			if attempt > 1 {
				log.Printf("[activity] task %s write succeeded on retry %d (worker=%s kind=%s tool=%s)",
					t.ID, attempt, r.Worker, r.Kind, r.Tool)
			}
			return id, nil
		}
		log.Printf("[activity] task %s write failed (attempt %d/3, worker=%s kind=%s tool=%s): %v",
			t.ID, attempt, r.Worker, r.Kind, r.Tool, err)
		time.Sleep(time.Duration(attempt) * 25 * time.Millisecond)
	}
	return 0, err
}

// UseLLM installs (or replaces) the LLM planner + work agent at runtime.
func (e *Engine) UseLLM(p *agent.Planner, w *agent.Worker) {
	e.mu.Lock()
	e.planner, e.worker = p, w
	e.mu.Unlock()
}

func (e *Engine) snapshot() (*agent.Planner, *agent.Worker) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.planner, e.worker
}

// SetAgentResolver installs a per-task planner/worker resolver (wired by the server).
// Called once at startup before any task loop runs, so no lock is needed on reads.
func (e *Engine) SetAgentResolver(fn func(t *Task) (*agent.Planner, *agent.Worker)) {
	e.resolve = fn
}

// snapshotFor returns the planner/worker a task should run on: the task's dedicated
// pair when it pins a specific LLM profile (via the resolver), else the global active
// pair. The resolver returning nil means "no override" (fall back to global).
func (e *Engine) snapshotFor(t *Task) (*agent.Planner, *agent.Worker) {
	if e.resolve != nil {
		if p, w := e.resolve(t); p != nil && w != nil {
			return p, w
		}
	}
	return e.snapshot()
}

// Ready reports whether an LLM provider is configured.
func (e *Engine) Ready() bool {
	p, w := e.snapshot()
	return p != nil && w != nil
}

// Mode reports "llm" when ready, else "idle".
func (e *Engine) Mode() string {
	if e.Ready() {
		return "llm"
	}
	return "idle"
}

// Run starts the planner loop + N worker loops for a task. The loops always run
// but no-op until an LLM is configured (so a task created while idle picks up
// automatically once LLM is set from the UI).
func (e *Engine) Run(ctx context.Context, t *Task) {
	if _, loaded := e.started.LoadOrStore(t.ID, true); loaded {
		t.Notify() // already running — just nudge a planning round
		return
	}
	e.touch(t.ID)
	go e.plannerLoop(ctx, t)
	workers := e.m.Workers()
	for i := 0; i < workers; i++ {
		go e.workerLoop(ctx, t, fmt.Sprintf("work#%d", i+1))
	}
	e.startDeadlineCoordinator(ctx, t) // task-level timeout timer (only when timeout>0; deduped)
	t.Notify()                         // kick the first planning round (acted on once LLM is ready)
}

func (e *Engine) plannerLoop(ctx context.Context, t *Task) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.notify:
			// debounce: coalesce a burst of changes into one planning round
			timer := time.NewTimer(e.debounce)
		drain:
			for {
				select {
				case <-t.notify:
				case <-timer.C:
					break drain
				}
			}
			planner, _ := e.snapshotFor(t)
			if planner == nil {
				continue // idle until LLM configured
			}
			if e.IsPaused(t.ID) {
				continue // user-paused: don't plan
			}
			// terminal task (goals all met → done, or failed): the run is over. A
			// resume/nudge — e.g. auto-resume of the active task on restart — must NOT
			// re-plan (it would burn an LLM round and re-confirm a settled result).
			if isTerminalStatus(t.Status) {
				continue
			}
			// task-level timeout settling in progress: drop ordinary wakeups — neither the
			// workers' final write-back nor Resume's Notify may trigger a regular planning
			// round; the final round is driven directly by the coordinator (settleTask).
			if e.isSettling(t.ID) {
				continue
			}
			e.stampFirstRun(t) // first real planning → stamps first_run_at + computes the deadline (timed tasks only)
			e.touch(t.ID)
			emit := func(r db.Activity) { e.emitActivity(t, r) }
			ectx := e.clockCtx(e.execContextFor(ctx, t.ID), t, false) // cancellable by Pause; carries the task deadline
			log.Printf("[planner] task %s planning round…", t.ID)
			// round marker: each Plan() is one planner round; emit a boundary so the
			// UI can separate rounds in the transcript (kind='round').
			e.emitActivity(t, db.Activity{Worker: "planner", Kind: "round",
				Summary: fmt.Sprintf("planning round %d", e.nextPlannerRound(t.ID))})
			// which intents' completion triggered this round (may be several — debounce
			// coalesces a burst; empty for non-completion wakes like a new hint).
			doneIntents := t.drainDone()
			e.incInflight(t.ID)
			taskIDInt, _ := strconv.ParseInt(t.ID, 10, 64)
			met, reason, err := planner.Plan(ectx, taskIDInt, e.m.assets, t.Store, t.Goal, doneIntents, emit)
			e.decInflight(t.ID)
			switch {
			case err != nil && ectx.Err() == nil:
				log.Printf("[planner] task %s planning error: %v", t.ID, err)
			case met:
				log.Printf("[planner] task %s goal met: %s", t.ID, reason)
				// All goals met → persist the task status as done (the frontend DTO shows this
				// terminal state with priority).
				if err := e.m.SetTaskStatus(t.ID, "done"); err != nil {
					log.Printf("[planner] task %s failed to persist done status: %v", t.ID, err)
				}
				// Task judged done → cancel running workers right away: their in-flight intents
				// no longer matter. On the next worker loop iteration they hit the terminal-state
				// gate and pick up nothing new; the cancelled batch falls into the "task done"
				// branch below as stopped (not blocked).
				e.cancelExec(t.ID)
			default:
				log.Printf("[planner] task %s planning finished", t.ID)
			}
			e.touch(t.ID)
		}
	}
}

func (e *Engine) workerLoop(ctx context.Context, t *Task, name string) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		_, worker := e.snapshotFor(t)
		if worker == nil {
			if sleepCtx(ctx, 1500*time.Millisecond) {
				return
			}
			continue
		}
		if e.IsPaused(t.ID) {
			if sleepCtx(ctx, 1000*time.Millisecond) {
				return
			}
			continue // user-paused: don't claim/execute intents
		}
		if e.isSettling(t.ID) {
			if sleepCtx(ctx, 1000*time.Millisecond) {
				return
			}
			continue // task timeout settling: no new intents (running ones wrap up; the coordinator waits for the drain)
		}
		if isTerminalStatus(e.m.TaskStatus(t.ID)) {
			if sleepCtx(ctx, 1000*time.Millisecond) {
				return
			}
			continue // task already terminal (done/failed/timeout): stop claiming leftover intents, don't spin the frontier after completion
		}
		intent := e.claimNext(t, name)
		if intent == nil {
			if sleepCtx(ctx, 800*time.Millisecond) {
				return
			}
			continue
		}
		log.Printf("[worker %s] task %s claimed intent #%d", name, t.ID, intent.ID)
		e.stampFirstRun(t) // first real execution → stamps first_run_at + computes the deadline (timed tasks only)
		e.touch(t.ID)
		emit := func(r db.Activity) { e.emitActivity(t, r) }
		ectx := e.clockCtx(e.execContextFor(ctx, t.ID), t, false) // cancellable by Pause; carries the task deadline
		// per-work child context so the planner's kill_work can stop just this work.
		workCtx, workCancel := context.WithCancel(ectx)
		e.registerWork(intent.ID, workCancel)
		// wrap the guard hooks so steer_work can inject a mid-run course-correction
		// for THIS intent (drained before the worker's next tool call).
		iid := intent.ID
		taskEmit := func(a db.Activity) {
			nid := iid
			a.NodeID, a.Worker = &nid, name
			emit(a)
		}
		workCtx = intercept.WithTaskContext(workCtx, t.ID, fmt.Sprintf("%s · #%d", name, iid), taskEmit)
		hooks := steerHooks{inner: t.Guard.Hooks(), drain: func() (string, bool) { return e.drainSteer(iid) }}
		wTaskID, _ := strconv.ParseInt(t.ID, 10, 64)
		e.incInflight(t.ID) // counted as running, for the settlement drain to wait on
		reason, wrote, err := worker.Execute(workCtx, name, wTaskID, e.m.assets, t.Store, intent, hooks, emit, e.m.enrich)
		// model_error outcome → rerun a few extra times (after backoff). Only while the intent
		// still belongs to this work and the task is not paused/terminated/cancelled (and not
		// settling); otherwise the matching branch below handles it (no retries during
		// settlement, so backoff doesn't eat into other workers' graceful wrap-up window).
		for attempt := 1; attempt <= modelErrorRetries &&
			reason == harness.ReasonModelError &&
			workCtx.Err() == nil && ectx.Err() == nil && !e.IsPaused(t.ID) && !e.isSettling(t.ID); attempt++ {
			log.Printf("[worker %s] task %s intent #%d ended model_error, retrying in %v (%d/%d)",
				name, t.ID, intent.ID, modelErrorRetryBackoff, attempt, modelErrorRetries)
			if sleepCtx(workCtx, modelErrorRetryBackoff) {
				break // cancelled during backoff (terminate/pause) → handled by the branch below
			}
			reason, wrote, err = worker.Execute(workCtx, name, wTaskID, e.m.assets, t.Store, intent, hooks, emit, e.m.enrich)
		}
		e.decInflight(t.ID)
		// CAPTURE kill state BEFORE unregisterWork cancels workCtx. kill = this work's
		// ctx was cancelled (planner kill_work) while the TASK ctx kept running; a
		// pause cancels the task ctx (ectx) instead. Checking workCtx.Err() AFTER
		// unregister would always be true (unregister cancels it) → every completed
		// work would be wrongly marked stopped.
		killed := workCtx.Err() != nil && ectx.Err() == nil
		e.unregisterWork(intent.ID)
		// if a pause cancelled this run mid-flight, return the intent to the frontier
		// so it is re-claimed (and re-run from scratch) on resume — not marked done.
		if ectx.Err() != nil && e.IsPaused(t.ID) {
			_ = t.Store.SetIntentState(intent.ID, "open")
			continue
		}
		// The task-timeout settlement's hard fallback cancel (not pause, not kill) cancelled
		// this run → classify as exhausted (already settled), don't mislabel blocked. The
		// worker has usually written results back during the settlement phase.
		if ectx.Err() != nil && e.isSettling(t.ID) {
			_ = t.Store.SetIntentState(intent.ID, "exhausted")
			log.Printf("[worker %s] task %s intent #%d ended by task-timeout settlement (exhausted), wrote back %s", name, t.ID, intent.ID, wrote)
			e.touch(t.ID)
			continue
		}
		// Task already done (via the regular path) → the cancelExec above cancelled this run.
		// The intent result no longer matters: mark stopped (not blocked) so it doesn't pollute
		// the intent states of a finished task.
		if ectx.Err() != nil && isTerminalStatus(e.m.TaskStatus(t.ID)) {
			_ = t.Store.SetIntentState(intent.ID, "stopped")
			log.Printf("[worker %s] task %s intent #%d cancelled because the task is done (stopped)", name, t.ID, intent.ID)
			e.touch(t.ID)
			continue
		}
		// killed by the planner: mark stopped (don't write back results, don't auto-reclaim).
		if killed {
			_ = t.Store.SetIntentState(intent.ID, "stopped")
			log.Printf("[worker %s] task %s intent #%d terminated (stopped)", name, t.ID, intent.ID)
			e.touch(t.ID)
			t.Notify()
			continue
		}
		if err != nil {
			log.Printf("[worker %s] intent %d: %v", name, intent.ID, err)
		}
		// terminal triage: hitting the turn cap ≠ done. max_turns→exhausted (the planner thus
		// knows this direction was tried but not finished and needs another angle, instead of
		// being treated as covered and skipped forever); error→blocked; normal→done.
		state := "done"
		switch {
		case err != nil:
			state = "blocked"
		case reason == harness.ReasonMaxTurns:
			state = "exhausted"
			log.Printf("[worker %s] intent %d hit the turn cap (exhausted), wrote back %s", name, intent.ID, wrote)
		case reason == harness.ReasonTimeout:
			state = "exhausted"
			log.Printf("[worker %s] intent %d run timed out (exhausted), wrote back %s after settle", name, intent.ID, wrote)
		}
		_ = t.Store.SetIntentState(intent.ID, state)
		log.Printf("[worker %s] task %s intent #%d finished: %s (wrote back %s)", name, t.ID, intent.ID, state, wrote)
		e.touch(t.ID)
		t.NotifyDone(intent.ID) // results changed the graph -> wake the planner (with the just-finished intent id)
	}
}

func sleepCtx(ctx context.Context, d time.Duration) (done bool) {
	select {
	case <-ctx.Done():
		return true
	case <-time.After(d):
		return false
	}
}

func (e *Engine) claimNext(t *Task, name string) *db.Node {
	fr, _ := t.Store.Frontier(20)
	for _, in := range fr {
		if ok, _ := t.Store.ClaimIntent(in.ID, name); ok {
			return in
		}
	}
	return nil
}
