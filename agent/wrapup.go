package agent

import (
	"strings"

	"antifox/engine/harness"
)

// Wrap-up (settlement) prompt: when an agent is terminated for [step exhaustion (MaxTurns)] or
// [timeout (run_seconds/MaxDuration)], the SDK settlement phase injects this prompt so the agent
// first persists identified-but-unwritten results, then outputs one summary -- no dead-end finishes.
//
// Each agent's wrap-up prompt can be overridden in the admin UI (stored in agents.wrapup_prompt); empty falls back
// to the built-in default here. Only the [prompt body] is editable; which tools are hidden and the wrap-up's own turn
// budget are code-fixed policy.

// WrapupOverride, if set, returns the stored wrap-up prompt for an agent key and
// whether a non-empty one exists. Wired by the server to the agents table (like
// PromptOverride for system prompts). nil / empty → the built-in default is used.
var WrapupOverride func(agentKey string) (string, bool)

// WrapupMaxTurnsOverride, if set, returns the admin-configured turn budget for the
// wrap-up phase of an agent and whether a positive one exists. Wired to the agents
// table. nil / ≤0 → the built-in per-agent default (wrapupTurnDefaults) is used.
var WrapupMaxTurnsOverride func(agentKey string) (int, bool)

// Built-in default wrap-up prompts, keyed by agent. worker reuses the historically hardcoded settleWrapUpPrompt
// (defined in worker.go); planner/mainagent have their own; unmatched (custom agents) take the generic fallback.
var wrapupDefaults = map[string]string{
	"worker":    settleWrapUpPrompt,
	"planner":   plannerWrapUpDefault,
	"mainagent": mainAgentWrapUpDefault,
}

// wrapupTurnDefaults: built-in defaults for each agent's [own] wrap-up turn budget (admin override >0 wins).
// worker gets 4 since write-backs may take several turns (record_fact/insert_assets); others 5. Unmatched -> genericWrapupTurns.
var wrapupTurnDefaults = map[string]int{
	"worker":    4,
	"planner":   5,
	"mainagent": 5,
}

const genericWrapupTurns = 5

const plannerWrapUpDefault = "Your planning steps for this round are nearly exhausted -- note this only ends [this round]; the system will wake you again to continue planning as the situation changes; the task is not terminating, so do not wind down the whole plan here. Land the conclusions you have already reached so the round is not wasted, but do [not pad intents just to wrap up] (0 intents this round remains perfectly normal): (1) if you have judged there are exploration directions that [should be dispatched now], submit them in one add_intent batch (do not hold ready ones back); (2) for goals already proven attained by a finding/fact, call prove_goal to mark met (do not miss any); (3) serial exploit chains needing multiple steps: record them with TodoWrite so the next wake can continue dispatching. Then simply end the round; no summary text needed."

const mainAgentWrapUpDefault = "Your steps are nearly exhausted and this interaction is ending. Do not start new exploration/actions. In **a single plain-text sentence**, summarize for the user the current progress, key conclusions, and the recommended next step."

const genericWrapUpDefault = "You are about to be stopped, budget exhausted. First write back completed-but-unpersisted results, then in **a single plain-text sentence** summarize what you did and which key conclusions you reached (shown as the result of this run)."

// WrapupDefault returns the built-in default wrap-up prompt for an agent key —
// used by the admin UI as the "restore default" value and empty-field placeholder.
func WrapupDefault(agentKey string) string {
	if d, ok := wrapupDefaults[agentKey]; ok {
		return d
	}
	return genericWrapUpDefault
}

// WrapupTurnsDefault returns the built-in wrap-up turn budget for an agent key —
// used by the admin UI as the "0 = default N" hint.
func WrapupTurnsDefault(agentKey string) int {
	if n, ok := wrapupTurnDefaults[agentKey]; ok {
		return n
	}
	return genericWrapupTurns
}

// resolveWrapup returns the effective wrap-up prompt: the DB override (if set and
// non-empty) over the built-in default.
func resolveWrapup(agentKey string) string {
	if WrapupOverride != nil {
		if t, ok := WrapupOverride(agentKey); ok && strings.TrimSpace(t) != "" {
			return t
		}
	}
	return WrapupDefault(agentKey)
}

// resolveWrapupTurns returns the effective wrap-up turn budget: a positive DB
// override over the built-in per-agent default.
func resolveWrapupTurns(agentKey string) int {
	if WrapupMaxTurnsOverride != nil {
		if v, ok := WrapupMaxTurnsOverride(agentKey); ok && v > 0 {
			return v
		}
	}
	return WrapupTurnsDefault(agentKey)
}

// wrapupSettlement builds the settlement config for an agent's run. Prompt and the
// turn budget are admin-editable per agent; disabled tools are code-owned policy so
// a user can't edit away the "stop probing" guardrail. Resolved fresh each run
// (reads DB live), so edits apply on the next run without a restart.
func wrapupSettlement(agentKey string, disabledTools []string) *harness.Settlement {
	return &harness.Settlement{
		Prompt:        resolveWrapup(agentKey),
		DisabledTools: disabledTools,
		MaxTurns:      resolveWrapupTurns(agentKey),
	}
}

// ---------- Task-level timeout wrap-up words (see docs/task-timeout-and-wrapup.md) ----------
//
// A [separate set] from the per-run wrap-up words: per-run means "this run's budget is used up"; task timeout means
// "the whole task has hit its deadline and is ending". The semantics are often opposite (planner especially: per-run
// says "keep planning", task timeout says "stop planning, make the final call"). Configured only for worker/planner.

// WrapupTaskTimeoutOverride / ...TurnsOverride: DB overrides of the task-timeout wrap-up words and turns
// (wired to agents.task_timeout_wrapup_prompt / _max_turns; worker/planner only).
var (
	WrapupTaskTimeoutOverride      func(agentKey string) (string, bool)
	WrapupTaskTimeoutTurnsOverride func(agentKey string) (int, bool)
)

var taskTimeoutWrapupDefaults = map[string]string{
	"worker":  workerTaskTimeoutDefault,
	"planner": plannerTaskTimeoutDefault,
}

const workerTaskTimeoutDefault = "**The whole task has hit its timeout and is about to end** (not this run's budget -- the entire exploration is out of time). Last chance: (1) persist [everything] identified but not yet written back -- new assets insert_assets, conclusions/facts record_fact, confirmed vulnerabilities report_finding; (2) start no new commands/probes; (3) **finally, in a single plain-text sentence**, summarize your key conclusions on this intent."

const plannerTaskTimeoutDefault = "**The whole task has hit its timeout and is about to end** (not this round -- the entire task terminates). Based on [all] current facts and findings, make one final goal judgement: for goals proven attained by evidence, call prove_goal to mark met (miss none). **Generate no new intents** (dispatched ones will no longer be executed). Conclude immediately after; no summary text needed."

// TaskTimeoutWrapupDefault returns an agent's built-in default task-timeout wrap-up words (for admin placeholder/reset).
func TaskTimeoutWrapupDefault(agentKey string) string {
	return taskTimeoutWrapupDefaults[agentKey] // empty for unconfigured (mainagent/chat)
}

// resolveTaskTimeoutWrapup: DB override (non-empty) > built-in default. Empty string means the agent has no task-timeout
// words (not worker/planner); callers should then fall back to the per-run words.
func resolveTaskTimeoutWrapup(agentKey string) string {
	if WrapupTaskTimeoutOverride != nil {
		if t, ok := WrapupTaskTimeoutOverride(agentKey); ok && strings.TrimSpace(t) != "" {
			return t
		}
	}
	return TaskTimeoutWrapupDefault(agentKey)
}

func resolveTaskTimeoutTurns(agentKey string) int {
	if WrapupTaskTimeoutTurnsOverride != nil {
		if v, ok := WrapupTaskTimeoutTurnsOverride(agentKey); ok && v > 0 {
			return v
		}
	}
	return resolveWrapupTurns(agentKey) // defaults to the per-run turn count
}

// wrapupSettlementForTask builds settlement for a worker/planner run that is aware
// of the task deadline. See §5 of the design doc:
//   - clamped=true  -> this run was clamped by the task deadline: Timeout wrap-up = task expiry -> task-timeout words;
//     MaxTurns wrap-up = steps exhausted inside the clamped window with minutes of task left -> per-run words.
//   - clamped=false -> plenty of task time left: both reasons use per-run words (i.e. degenerates to wrapupSettlement).
//
// PromptByReason picks by the [actual] reason at wrap-up time; no build-time mismatch.
func wrapupSettlementForTask(agentKey string, disabledTools []string, clamped bool) *harness.Settlement {
	perRun := resolveWrapup(agentKey)
	st := &harness.Settlement{
		Prompt:        perRun, // fallback (and both reasons when not clamped)
		DisabledTools: disabledTools,
		MaxTurns:      resolveWrapupTurns(agentKey),
	}
	if clamped {
		if tt := resolveTaskTimeoutWrapup(agentKey); tt != "" {
			st.PromptByReason = map[harness.TerminalReason]string{
				harness.ReasonTimeout:  tt,     // task time is up
				harness.ReasonMaxTurns: perRun, // steps ran out first, task time remains
			}
			st.MaxTurns = resolveTaskTimeoutTurns(agentKey)
		}
	}
	return st
}
