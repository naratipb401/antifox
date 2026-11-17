package report

// AI narrative layer: turns thin findings ({vulnclass, severity, summary, poc})
// into professional report prose (description / test process / risk /
// remediation + CVSS) via the platform's active LLM profile. Report-level and
// per-finding calls run concurrently. All AI output is JSON; parse failures fall
// back to the raw finding fields (assembly in build.go), so generation never
// hard-fails on a bad LLM response.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"antifox/agent"
	"antifox/db"
	"antifox/engine/agentcore"
	acperm "antifox/engine/permission"
	actool "antifox/engine/tool"
)

// complete runs one system+user → text completion on the active LLM profile.
// Returns an error when no profile is configured (caller falls back to skeleton).
func complete(ctx context.Context, pg *db.DB, system, user string, maxTokens int) (string, error) {
	// The report/enrichment role can bind a dedicated LLM profile (role separation);
	// when unbound the globally active profile is used. This lets enrichment run on a
	// stronger model (e.g. Claude) without touching the pentest agent's model.
	p, err := pg.ReportRoleProfile()
	if err != nil || p == nil || strings.TrimSpace(p.APIKey) == "" {
		return "", fmt.Errorf("no active LLM profile")
	}
	cfg := agent.ConfigFrom(p.Format, p.Model, p.BaseURL, p.APIKey, p.Proxy)
	cfg.RatePerSecond, cfg.RatePerMinute = p.RatePerSecond, p.RatePerMinute
	cfg.ReasoningEffort = p.ReasoningEffort
	prov, err := cfg.NewProvider()
	if err != nil {
		return "", err
	}
	cctx, cancel := context.WithTimeout(ctx, 6*time.Minute)
	defer cancel()
	out, err := agentcore.Run(cctx, agentcore.Options{
		Provider:       prov,
		SystemPrompt:   []string{system},
		Tools:          []actool.CoreTool{}, // non-nil empty → no default tools offered
		PermissionMode: acperm.ModeBypass,
		MaxTurns:       1,
		MaxTokens:      maxTokens,
	}, user)
	if err != nil {
		return "", err
	}
	return cleanJSON(out), nil
}

// ---------------------------------------------------------------------------
// Report-level narrative (background / service result / remediation / checklist)
// ---------------------------------------------------------------------------

type reportLevelOut struct {
	Background            []string `json:"background"`
	ServiceResult         string   `json:"service_result"`
	RemediationConclusion string   `json:"remediation_conclusion"`
	ChecklistHits         []string `json:"checklist_hits"`
}

const sysReportLevel = "You are a senior penetration-testing report author for %s, skilled at writing professional, client-readable, technically rigorous penetration test reports. Output strictly JSON only, no markdown code fences."

const userReportLevel = `Write the report-level summary content based on the following project and findings list.

Project name: %s
Client name: %s
Findings:
%s
Requirements (professional delivery tone; avoid phrasing like "the test was performed today"):
- background: string array, 2 paragraphs. Paragraph 1: the client engaged %s to perform a penetration test, and the test objectives; paragraph 2: the test scope and the combination of manual and tool-driven testing. 60-130 words each.
- service_result: 1 paragraph summarizing which systems were tested and the number and severity distribution of the findings.
- remediation_conclusion: 1 remediation conclusion paragraph (first-round penetration: all findings pending remediation by default; recommend prompt remediation and retesting).
- checklist_hits: string array; infer the standard checklist item names hit, based on the finding types (e.g. "SQL Injection", "Unauthorized Access", "Authentication Bypass", "Sensitive Information Disclosure", "Command Injection", "Cross-Site Scripting (XSS)", "Vertical Privilege Escalation" — use the standard names).

Output JSON only: {"background":["...","..."],"service_result":"...","remediation_conclusion":"...","checklist_hits":["...","..."]}`

// reportLevelPromptData feeds the editable report-level prompt template.
type reportLevelPromptData struct {
	Company, Project, Client, FindingsList, LangDirective string
}

// vulnPromptData feeds the editable per-vuln prompt template.
type vulnPromptData struct {
	Company, VulnClass, Severity, Summary, PoC, LangDirective string
}

func aiReportLevel(ctx context.Context, pg *db.DB, in *GenInput, sk *skillContent) reportLevelOut {
	var out reportLevelOut
	loc := normLocale(in.Locale)
	var sb strings.Builder
	// Feed the full test scope / target systems to the narrative as context (via the
	// FindingsList channel, on both the skill and fallback prompt paths) so the summary
	// can name the concrete systems — the cover project name (ProjectName) stays short.
	if sc := strings.TrimSpace(in.Scope); sc != "" {
		fmt.Fprintf(&sb, "[Test scope / target systems] %s\n\n[Findings]\n", truncateRune(sc, 1500))
	}
	for _, f := range in.Findings {
		fmt.Fprintf(&sb, "- [%s] %s: %s\n", severityName(loc, f.Severity), nz(f.VulnClass, "-"), truncateRune(f.Summary, 80))
	}
	dir := outputLangDirective(loc)
	data := reportLevelPromptData{Company: in.CompanyName, Project: in.ProjectName, Client: in.ClientName, FindingsList: sb.String(), LangDirective: dir}
	sys := renderPrompt(sk.reportLevelSys, data)
	if sys == "" {
		sys = fmt.Sprintf(sysReportLevel, in.CompanyName)
	}
	user := renderPrompt(sk.reportLevelUser, data)
	if user == "" {
		user = fmt.Sprintf(userReportLevel, in.ProjectName, in.ClientName, sb.String(), in.CompanyName) +
			"\n\nOutput language requirement: " + dir
	}
	raw, err := complete(ctx, pg, sys, user, 4000)
	if err != nil {
		return out
	}
	_ = json.Unmarshal([]byte(raw), &out)
	return out
}

// ---------------------------------------------------------------------------
// Per-finding narrative (description / test process / risk / remediation / CVSS)
// ---------------------------------------------------------------------------

type Enrichment struct {
	Title        string            `json:"title"`  // concise professional vuln name
	Target       string            `json:"target"` // affected host / URL, extracted from evidence
	Description  []string          `json:"description"`
	Module       string            `json:"module"`
	TestProcess  []procBlock       `json:"test_process"`
	RiskAnalysis []string          `json:"risk_analysis"`
	Remediation  []string          `json:"remediation"`
	CVSSScore    float64           `json:"cvss_score"`
	CVSSVector   string            `json:"cvss_vector"`
	CVSSMetrics  map[string]string `json:"cvss_metrics"`
}

const sysVuln = "You are a senior penetration-testing report author for %s. Rewrite the raw finding record into delivery-grade report prose — as if written personally for the client by a senior consultant: concise, professional, strictly to the point. Output strictly JSON only, no markdown code fences, no pleasantries, disclaimers, or boilerplate. Verbatim HTTP requests/commands/responses must be preserved as-is; never fabricate."

const userVuln = `Rewrite the finding below into report prose. Core requirements: [concise, specific, restrained] — prefer quality over quantity: no padding, no filler, no technically-true-but-empty statements; every sentence must carry real information about this finding on this target.

Finding type / original name: %s
Severity: %s
Original summary:
%s
Original reproduction / evidence:
%s

Field requirements:
- title: a canonical, professional finding name (10-26 characters). If the original name is already canonical (CVE ids etc.), keep it or adjust lightly; do not copy the summary verbatim.
- target: the affected host or URL, extracted from the summary/evidence; empty string when undeterminable.
- description: 1 paragraph, 2-3 sentences. State only what the vulnerability is, where it occurs, and its root cause. No OWASP taxonomy dumping, no restating the fix, no vague padding.
- module: the affected feature module (short, e.g. "external account login / unauthenticated endpoint").
- risk_analysis: 2-4 items, one sentence each, each a concrete consequence for THIS finding on THIS target (who can do what, and what data or access they gain). No boilerplate.
- remediation: 2-4 items, one sentence each, concrete and actionable (what to change, how, and what to verify). No empty phrases.
- CVSS 3.1: cvss_score (number), cvss_vector (e.g. "AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H"), cvss_metrics (the eight English values).

Do not output test_process — the platform generates the reproduction from real evidence; just return "test_process":[].

Output JSON only: {"title":"...","target":"...","description":["..."],"module":"...","test_process":[],"risk_analysis":["..."],"remediation":["..."],"cvss_score":0,"cvss_vector":"...","cvss_metrics":{...}}`

func aiVulnNarrative(ctx context.Context, pg *db.DB, locale string, f FindingInput, companyName string, sk *skillContent) Enrichment {
	var out Enrichment
	loc := normLocale(locale)
	dir := outputLangDirective(loc)
	data := vulnPromptData{Company: companyName, VulnClass: nz(f.VulnClass, "-"), Severity: severityName(loc, f.Severity), Summary: nz(f.Summary, "-"), PoC: nz(f.PoC, "-"), LangDirective: dir}
	sys := renderPrompt(sk.vulnSys, data)
	if sys == "" {
		sys = fmt.Sprintf(sysVuln, companyName)
	}
	user := renderPrompt(sk.vulnUser, data)
	if user == "" {
		user = fmt.Sprintf(userVuln,
			nz(f.VulnClass, "-"), severityName(loc, f.Severity), nz(f.Summary, "-"), nz(f.PoC, "-")) +
			"\n\nOutput language requirement: " + dir
	}
	// Ground the PROSE (description / risk / remediation) in the ACTUAL work transcript
	// (real tool calls / requests / responses / analysis recorded when the finding was
	// made) so it reflects what actually happened, not an expansion of the one-liner.
	if tr := strings.TrimSpace(f.Transcript); tr != "" {
		user += "\n\n=== VERBATIM WORK TRANSCRIPT FROM WHEN THIS FINDING WAS MADE (tool calls / requests / responses / analysis) ===\n" +
			"base description/risk_analysis on the real observations in the transcript; never fabricate or invent anything the transcript does not show.\n\n" + tr
	}
	// PROSE-ONLY, ALWAYS. We never ask the model to reconstruct a verbatim test_process:
	// that large generation is slow and unreliable (many models time out / return nothing
	// on it) AND risks fabricating a PoC (the user's key pain). Instead test_process is
	// filled DETERMINISTICALLY below from REAL evidence — the agent's in-situ reproduction
	// if present, else the raw PoC. The model only writes prose (title/desc/module/risk/
	// remediation/CVSS), which is small, fast, and reliable on any model.
	if len(f.Reproduction) > 2 { // ""/"{}" carry nothing
		user += "\n\n=== Reproduction already captured in-situ by the test engine ===\n" +
			"This finding's entry point / prerequisites / steps / provenance were captured in-situ during testing and will be used directly for the report's 'Testing Process' section. You need NOT and should NOT rebuild test_process — just return \"test_process\":[]."
	} else {
		user += "\n\n=== test_process is provided directly from real evidence ===\n" +
			"The 'Testing Process' section will be generated by the platform verbatim from the real PoC/evidence; you need NOT and should NOT rebuild or fabricate test_process — just return \"test_process\":[]"
	}
	user += ". Focus on making title / description / module / risk_analysis / remediation and CVSS solid and professional, grounded in the actual observations in the summary and transcript above; never fabricate content absent from them."
	raw, err := complete(ctx, pg, sys, user, 4000)
	if err != nil {
		return out
	}
	_ = json.Unmarshal([]byte(raw), &out)
	// Fill test_process from REAL evidence, never the model's reconstruction: the agent's
	// in-situ reproduction wins; else the raw PoC (deterministic). This makes every consumer
	// (persisted enrichment → findings page UI, and the report) show real, non-fabricated
	// steps — and keeps the generation prose-only (small, reliable).
	if b := reproToBlocks(f.Reproduction, loc, nil); len(b) > 0 {
		out.TestProcess = b
	} else if len(out.TestProcess) == 0 && strings.TrimSpace(f.PoC) != "" {
		out.TestProcess = proofToBlocks(f.PoC)
	}
	return out
}

// EnrichFinding is the exported per-finding AI enrichment, shared by the report
// pipeline and the findings-management "AI analysis" endpoint. locale controls the
// output language (zh-CN | zh-TW | en-US); skillDir enables the editable
// report-generation prompt (falls back to the built-in prompt when unset/missing).
// Returns a zero Enrichment (not an error) when no LLM profile is configured —
// callers keep whatever raw fields the finding already has.
func EnrichFinding(ctx context.Context, pg *db.DB, skillDir, locale string, f FindingInput, companyName string) Enrichment {
	if strings.TrimSpace(companyName) == "" {
		companyName = pick(locale, "Security Services Team", "Security Services Team", "Security Services Team")
	}
	sk := loadSkillContent(skillDir, locale)
	return aiVulnNarrative(ctx, pg, locale, f, companyName, sk)
}

// cleanJSON strips markdown fences and surrounding prose from an LLM response.
func cleanJSON(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	s = strings.TrimSpace(s)
	if i := strings.Index(s, "{"); i >= 0 {
		if j := strings.LastIndex(s, "}"); j > i {
			s = s[i : j+1] // strip leading AND trailing prose around the JSON object
		}
	}
	return s
}
