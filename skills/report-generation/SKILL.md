---
name: report-generation
description: Editable content for penetration testing report generation—AI rewriting prompts and the fixed skeleton (methodology/testing checklist/tools/reference standards/testers), stored per language in separate files. The report engine reads from disk on every generation, so edits take effect immediately.
---

# Report Generation (Editable Content)

This skill holds the **editable content of penetration testing reports**, read by the generation pipeline of "Report Management". Changes take effect on the **next generation**—no restart or rebuild needed; if any file is missing or corrupted, the built-in defaults are used as fallback.

## Layout

- `prompts/report-level.tmpl` — AI prompt for report-level narrative (background / service results / remediation conclusion / matched checklist items).
- `prompts/vuln-narrative.tmpl` — AI prompt for rewriting a single vulnerability (description / reproduction steps / risk / remediation / CVSS).
- `skeleton/<language>/methodology.json` — 2.2 Testing methodology: `{intro, phases:[{name,desc}]}`.
- `skeleton/<language>/checklist.json` — 2.3 Testing checklist: `[{category,name,desc}]`. Items the AI flags as hit are rendered as "failed".
- `skeleton/<language>/tools.json` — 2.4 Testing tools: `[{name,desc}]`.
- `skeleton/<language>/standards.json` — 2.5 Reference standards: `["..."]`.
- `skeleton/<language>/personnel.json` — 2.1 Tester defaults: `[{name,role,contact}]`(values the user enters in report settings take precedence).

Language directories: `zh-CN` (Simplified) / `zh-TW` (Traditional) / `en-US` (English).

## Prompt Template Variables

The prompts are Go `text/template` files, with `===USER===` separating the system and user sections:

- Report level: `{{.Company}}` `{{.Project}}` `{{.Client}}` `{{.FindingsList}}` `{{.LangDirective}}`
- Vulnerability level: `{{.Company}}` `{{.VulnClass}}` `{{.Severity}}` `{{.Summary}}` `{{.PoC}}` `{{.LangDirective}}`

`{{.LangDirective}}` injects the target-language instruction (Simplified/Traditional/English), so the prompts themselves are language-neutral and shared across all three languages.
