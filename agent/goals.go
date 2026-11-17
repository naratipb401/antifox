package agent

import (
	"context"
	"encoding/json"
	"strings"

	"antifox/engine/agentcore"
	acperm "antifox/engine/permission"
	actool "antifox/engine/tool"
	"antifox/db"
)

// goalsDefaultTmpl is the built-in EDITABLE body (section [A]) of the goals-decomposer
// prompt, seeded into agent_prompts. No template vars are used today.
const goalsDefaultTmpl = `You are the pentest goal decomposer. Your job is to identify the **final outcome to achieve** from the user's input, not to plan attack steps.

**A goal = the final deliverable / verifiable result**

**What is NOT a goal (never list as subgoals)**:
- Information gathering, reconnaissance, endpoint scanning
- Vulnerability analysis and verification work
- Attack steps, exploitation techniques
- Result-validation steps

**Decomposition rules**:
- One single final goal described by the user → output one goal
- Multiple **mutually independent** final deliverables → list them separately
- Tag vulnclass when a clear vulnerability class applies; leave empty for recon/business-logic goals
- Never invent goals the user did not mention

Call set_goals to submit the result.`

// GoalSpec is one decomposed objective.
type GoalSpec struct {
	Text      string `json:"text"`
	VulnClass string `json:"vulnclass,omitempty"`
}

// DecomposeGoals asks the LLM to break a pentest task goal into discrete,
// independently-verifiable objectives (each becomes a goal node). Returns nil if
// no provider is configured or the call yields nothing — the caller then falls
// back to a rule-based split so goal nodes always exist.
//
// desc is the task's free-text description (background: target scope / flag count /
// engagement notes, etc.).
// It is fed alongside the goal so the decomposer no longer splits blind — the
// prompt still forbids inventing anything the two texts don't state.
//
// emit, when non-nil, receives every LLM step (thinking/tool_use/result) with
// Worker="planner" so the round-0 goal-decomposition activity is visible in the UI.
func DecomposeGoals(ctx context.Context, c Config, goalText, desc string, emit func(db.Activity)) []GoalSpec {
	prov, err := c.NewProvider()
	if err != nil {
		return nil
	}
	var out struct {
		Goals []GoalSpec `json:"goals"`
	}
	captured := false
	submitTool := actool.Build(actool.Spec{
		Name:        "set_goals",
		Description: "Submit the decomposed goal list.",
		Schema: map[string]any{"type": "object", "properties": map[string]any{
			"goals": map[string]any{"type": "array", "items": map[string]any{
				"type": "object", "properties": map[string]any{
					"text":      map[string]any{"type": "string", "description": "One independent, verifiable goal"},
					"vulnclass": map[string]any{"type": "string", "description": "Matching vulnerability class (if clear), e.g. SQLi/IDOR; business-logic goals may be empty"},
				}, "required": []string{"text"},
			}},
		}, "required": []string{"goals"}},
		Permissions: func(context.Context, json.RawMessage, acperm.Context) acperm.Decision { return acperm.Allowed() },
		Run: func(_ context.Context, in json.RawMessage, _ *actool.ToolContext) (actool.Result, error) {
			_ = json.Unmarshal(in, &out)
			captured = true
			return actool.Text("ok"), nil
		},
	})
	// Description rides in the user message (same channel as the goal), NOT via the
	// {{.EngagementDescription}} template var — else a prompt that references the var
	// would inject the description twice. System prompt stays pure static instructions.
	sys := renderSystem("goals", goalsDefaultTmpl, GoalsVars{})
	userMsg := "Task goal:\n" + goalText
	if d := strings.TrimSpace(desc); d != "" {
		userMsg += "\n\nTask description (background info; may contain target scope / flag count / engagement notes; for reference only — do not invent anything it does not mention):\n" + d
	}
	// Use captureRun so every LLM step is emitted as an activity record (visible in
	// the plan tab under the round-0 marker). Falls back gracefully when emit is nil.
	captureEmit := func(r db.Activity) {
		if emit != nil {
			r.Worker = "planner"
			emit(r)
		}
	}
	captureRun(ctx, agentcore.Options{
		Provider:               prov,
		SystemPrompt:           []string{sys},
		Tools:                  []actool.CoreTool{submitTool},
		PermissionMode:         acperm.ModeBypass,
		DisableBackgroundTasks: true,
		MaxTurns:               4,
	}, userMsg, captureEmit)
	if !captured {
		return nil
	}
	return out.Goals
}
