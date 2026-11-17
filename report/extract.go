package report

// Retroactive finding extraction: pull confirmed vulnerabilities out of a task's
// agent transcript (synthesis + worker conclusions) that were described but never
// recorded via report_finding — so they can be back-filled as finding nodes.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"antifox/db"
)

// ExtractedFinding is one vuln pulled from the transcript.
type ExtractedFinding struct {
	VulnClass string `json:"vulnclass"`
	Severity  string `json:"severity"` // high|medium|low
	Summary   string `json:"summary"`
	Target    string `json:"target"`
	Evidence  string `json:"evidence"`
}

const sysExtract = "You are a senior penetration testing expert. Extract [confirmed vulnerabilities] from the penetration testing execution/summary records. Output strictly JSON only, no markdown code fences. Evidence must preserve the verbatim original text from the records; do not fabricate."

const userExtract = `From the penetration testing execution and summary records below, extract all [confirmed vulnerabilities] and [proven exposures/information-disclosure risks] (excluding unverified speculation and negative conclusions).

Already recorded vulnerabilities (do not repeat these):
%s
Execution/summary records:
%s

Requirements:
- One confirmed vulnerability per entry: vulnclass (vulnerability type/name, e.g. "Stored XSS", "Hardcoded Credential", "Unauthorized Access"), severity (critical|high|medium|low; critical = can directly compromise the system/data entirely), summary (one sentence, including the affected object), target (affected host or URL), evidence (verbatim key PoC/request/command/response, preserving the real content).
- Only extract vulnerabilities/exposures **explicitly verified** in the records; do not repeat items semantically identical to those in the "already recorded" list above.
- **Exposure/information-disclosure findings must be extracted too**: sensitive/admin/debug endpoints actually reached without authorization and backed by response evidence, exposed Swagger/API docs, publicly readable configs/backups/source code, leaked sensitive data, etc.; set severity by sensitivity (plain info disclosure = low, unauthorized access to sensitive/admin endpoints = medium, account takeover or credential/secret disclosure = high). Still exclude pure speculation (version/CVE matches, "looks injectable"); an endpoint that merely exists without unauthorized exposure does not count.
- If there are no new vulnerabilities/exposures, output an empty findings array.

Output JSON only: {"findings":[{"vulnclass":"...","severity":"high","summary":"...","target":"...","evidence":"..."}]}`

// ExtractFindings asks the active LLM to extract confirmed vulns from the
// transcript, excluding ones already recorded (passed as short labels).
func ExtractFindings(ctx context.Context, pg *db.DB, transcript string, existing []string) ([]ExtractedFinding, error) {
	if strings.TrimSpace(transcript) == "" {
		return nil, fmt.Errorf("no session records to analyze")
	}
	var ex strings.Builder
	for _, e := range existing {
		ex.WriteString("- ")
		ex.WriteString(e)
		ex.WriteString("\n")
	}
	if ex.Len() == 0 {
		ex.WriteString("(none)\n")
	}
	raw, err := complete(ctx, pg, sysExtract, fmt.Sprintf(userExtract, ex.String(), transcript), 4000)
	if err != nil {
		return nil, err
	}
	var out struct {
		Findings []ExtractedFinding `json:"findings"`
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, fmt.Errorf("failed to parse AI output")
	}
	return out.Findings, nil
}
