package report

// Seeds the editable report-generation skill on startup by materializing the
// built-in Go defaults (defaults.go) into files under <skillDir>/report-generation/.
// Only writes files that are MISSING, so user edits in the Skills page are never
// overwritten. This makes the built-in skeleton + prompts editable without ever
// requiring a hand-maintained copy: the Go defaults remain the single source, and
// this projects them to disk once so they can be tuned and hot-reloaded.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

var seedLocales = []string{"zh-CN", "zh-TW", "en-US"}

// SeedSkillFiles writes any missing report-generation skill files. Best-effort:
// returns the first write error but always attempts every file.
func SeedSkillFiles(skillDir string) error {
	if strings.TrimSpace(skillDir) == "" {
		return nil
	}
	base := filepath.Join(skillDir, reportSkillName)
	var firstErr error
	put := func(rel, content string) {
		if err := writeIfMissing(filepath.Join(base, rel), content); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	putJSON := func(rel string, v any) {
		b, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			return
		}
		put(rel, string(b)+"\n")
	}

	put("SKILL.md", skillMD)
	put(filepath.Join("prompts", "report-level.tmpl"), promptReportLevelTmpl)
	put(filepath.Join("prompts", "vuln-narrative.tmpl"), promptVulnTmpl)

	for _, loc := range seedLocales {
		d := filepath.Join("skeleton", loc)
		putJSON(filepath.Join(d, "methodology.json"), map[string]any{
			"intro": methodologyIntro(loc), "phases": methodology(loc),
		})
		putJSON(filepath.Join(d, "tools.json"), tools(loc))
		putJSON(filepath.Join(d, "standards.json"), standards(loc))
		putJSON(filepath.Join(d, "checklist.json"), checklist(loc))
		putJSON(filepath.Join(d, "personnel.json"), defaultPersonnel(loc))
	}
	return firstErr
}

// writeIfMissing creates parent dirs and writes content only when path is absent.
func writeIfMissing(path, content string) error {
	if _, err := os.Stat(path); err == nil {
		return nil // exists → keep user edits
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

const skillMD = `---
name: report-generation
description: Editable content for penetration-test report generation — the AI-rewrite prompts and the fixed skeleton (methodology / checklist / tools / standards / personnel), one file set per locale. The report engine reads from disk on every generation, so edits take effect immediately.
---

# Report generation (editable content)

This skill holds the **editable content of the penetration test report**, read by the report-management generation pipeline. Changes take effect on the **next generation** — no restart or recompile needed; any missing or corrupted file falls back to the built-in defaults.

## Layout

- ` + "`prompts/report-level.tmpl`" + ` — AI prompt for the report-level narrative (background / service result / remediation conclusion / hit checklist items).
- ` + "`prompts/vuln-narrative.tmpl`" + ` — AI prompt for per-finding rewriting (description / test process / risk / remediation / CVSS).
- ` + "`skeleton/<locale>/methodology.json`" + ` — 2.2 test process: ` + "`{intro, phases:[{name,desc}]}`" + `.
- ` + "`skeleton/<locale>/checklist.json`" + ` — 2.3 test checklist: ` + "`[{category,name,desc}]`" + `. Items the AI marks as hit render as "fail".
- ` + "`skeleton/<locale>/tools.json`" + ` — 2.4 testing tools: ` + "`[{name,desc}]`" + `.
- ` + "`skeleton/<locale>/standards.json`" + ` — 2.5 referenced standards: ` + "`[\"...\"]`" + `.
- ` + "`skeleton/<locale>/personnel.json`" + ` — 2.1 default personnel: ` + "`[{name,role,contact}]`" + ` (values the user fills in under report settings take precedence).

Locale dirs: ` + "`zh-CN`" + ` / ` + "`zh-TW`" + ` / ` + "`en-US`" + `.

## Prompt template variables

Prompts are Go ` + "`text/template`" + ` bodies, split into a system and a user part by ` + "`===USER===`" + `:

- Report level: ` + "`{{.Company}}` `{{.Project}}` `{{.Client}}` `{{.FindingsList}}` `{{.LangDirective}}`" + `
- Finding level: ` + "`{{.Company}}` `{{.VulnClass}}` `{{.Severity}}` `{{.Summary}}` `{{.PoC}}` `{{.LangDirective}}`" + `

` + "`{{.LangDirective}}`" + ` injects the target-language directive, so the prompt body itself is language-agnostic and shared by all locales.
`

const promptReportLevelTmpl = `You are a senior penetration-testing report author for {{.Company}}, skilled at writing professional, client-readable, technically rigorous penetration test reports. Output strictly JSON only, no markdown code fences.
===USER===
Write the report-level summary content based on the following project and findings list.

Project name: {{.Project}}
Client name: {{.Client}}
Findings:
{{.FindingsList}}
Requirements (professional delivery tone; avoid phrasing like "the test was performed today"):
- background: string array, 2 paragraphs. Paragraph 1: the client engaged {{.Company}} to perform a penetration test, and the test objectives; paragraph 2: the test scope and the combination of manual and tool-driven testing. 60-130 words each.
- service_result: 1 paragraph summarizing which systems were tested and the number and severity distribution of the findings.
- remediation_conclusion: 1 remediation conclusion paragraph (first-round penetration: all findings pending remediation by default; recommend prompt remediation and retesting).
- checklist_hits: string array; infer the standard checklist item names hit, based on the finding types (e.g. "SQL Injection", "Unauthorized Access", "Authentication Bypass", "Sensitive Information Disclosure", "Command Injection", "Cross-Site Scripting (XSS)", "Vertical Privilege Escalation" — use the standard names).

Output JSON only: {"background":["...","..."],"service_result":"...","remediation_conclusion":"...","checklist_hits":["...","..."]}

Output language requirement: {{.LangDirective}}
`

const promptVulnTmpl = `You are a senior penetration-testing report author for {{.Company}}. Rewrite the raw finding record into delivery-grade report prose — as if written personally for the client by a senior consultant: concise, professional, strictly to the point. Output strictly JSON only, no markdown code fences, no pleasantries, disclaimers, or universally-applicable boilerplate. Verbatim HTTP requests/commands/responses must be preserved as-is; never fabricate.
===USER===
Rewrite the finding below into report prose. Core requirements: [concise, specific, restrained] — prefer quality over quantity: no padding, no filler, no technically-true-but-empty statements; every sentence must carry real information about this finding on this target.

Finding type / original name: {{.VulnClass}}
Severity: {{.Severity}}
Original summary:
{{.Summary}}
Original reproduction / evidence:
{{.PoC}}

Field requirements:
- title: a canonical, professional finding name (10-26 characters, e.g. "unauthenticated reset lets attacker take over admin account"). If the original name is already canonical (CVE ids etc.), keep it or adjust lightly; do not copy the summary verbatim.
- target: the affected host or URL, extracted from the summary/evidence; empty string when undeterminable.
- description: 1 paragraph, 2-3 sentences. State only three things — what the vulnerability is, where it occurs, and its root cause. No OWASP taxonomy dumping, no restating the fix, no vague padding.
- module: the affected feature module (short, e.g. "external account login / unauthenticated endpoint").
- risk_analysis: 2-4 items, one sentence each, each a concrete consequence THIS finding can cause on THIS target (who can do what, and what data or privileges they gain). Fewer items when there is no concrete consequence; no "may impact system security" boilerplate.
- remediation: 2-4 items, one sentence each, concrete and actionable (what to change, how, and what to verify). No empty phrases like "raise security awareness" or "audit regularly".
- CVSS 3.1: cvss_score (number), cvss_vector (e.g. "AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H"), cvss_metrics (the eight English values, e.g. {"AV":"Network","AC":"Low","PR":"None","UI":"None","S":"Unchanged","C":"High","I":"High","A":"High"}).

Do not output test_process — the platform generates the reproduction from real evidence; just return "test_process":[].

Output JSON only: {"title":"...","target":"...","description":["..."],"module":"...","test_process":[],"risk_analysis":["..."],"remediation":["..."],"cvss_score":0,"cvss_vector":"...","cvss_metrics":{...}}

Output language requirement: {{.LangDirective}}
`
