// Package db is the PostgreSQL data source for AntiFox (replaces the legacy single-file graph SQLite store).
// It opens the connection, applies the schema, and seeds the built-in agents and variable catalog.
package db

import (
	"database/sql"
	_ "embed"
	"fmt"
	"log"
	"net/url"
	"strings"
	"time"

	"antifox/config"
	_ "github.com/jackc/pgx/v5/stdlib" // pgx database/sql driver ("pgx")
)

//go:embed schema.sql
var schemaSQL string

// Connection pool caps (see Open). Conservative: enough for a single-instance deployment
// while leaving headroom for other PG clients.
const (
	maxOpenConns    = 25
	maxIdleConns    = 10
	connMaxLifetime = 55 * time.Minute
	connMaxIdleTime = 5 * time.Minute
)

// DSN resolves the PostgreSQL connection string and reports where it came from.
// Precedence: env ANTIFOX_PG_DSN > config file (config.json). There is no
// built-in default — it errors if neither source is configured.
func DSN() (dsn, source string, err error) {
	return config.PostgresDSN()
}

// DB wraps the shared *sql.DB. PG handles its own connection pool + concurrency
// (MVCC), so unlike the old SQLite store there is no process-wide write mutex.
type DB struct{ *sql.DB }

// ensureDatabase connects to the postgres system database and creates the target
// database if it does not exist. dsn must be a postgres:// URL.
func ensureDatabase(dsn string) error {
	u, err := url.Parse(dsn)
	if err != nil {
		return nil // unparseable DSN — let the normal Open fail with a clear error
	}
	dbName := strings.TrimPrefix(u.Path, "/")
	if dbName == "" || dbName == "postgres" {
		return nil
	}
	// connect to the postgres maintenance database instead
	adminDSN := *u
	adminDSN.Path = "/postgres"
	admin, err := sql.Open("pgx", adminDSN.String())
	if err != nil {
		return nil // best-effort; let Open surface the real error
	}
	defer admin.Close()
	if err := admin.Ping(); err != nil {
		return nil
	}
	var exists bool
	_ = admin.QueryRow(`SELECT true FROM pg_database WHERE datname=$1`, dbName).Scan(&exists)
	if !exists {
		if _, err := admin.Exec(`CREATE DATABASE "` + dbName + `"`); err != nil {
			return fmt.Errorf("create database %q: %w", dbName, err)
		}
	}
	return nil
}

// Open connects, applies the schema (idempotent), and seeds builtin rows.
func Open(dsn string) (*DB, error) {
	if err := ensureDatabase(dsn); err != nil {
		return nil, err
	}
	sqlDB, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}
	if err := sqlDB.Ping(); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("ping postgres (%s): %w", config.Redact(dsn), err)
	}
	// Connection pool caps: bound concurrently held DB connections so request storms cannot
	// exhaust them and break queries. PG defaults to max_connections=100; leave ample headroom
	// for other clients (psql/pg_dump/backups). Idle reaping avoids long-lived conns; the
	// lifetime cap prevents half-dead network connections from piling up.
	sqlDB.SetMaxOpenConns(maxOpenConns)
	sqlDB.SetMaxIdleConns(maxIdleConns)
	sqlDB.SetConnMaxLifetime(connMaxLifetime)
	sqlDB.SetConnMaxIdleTime(connMaxIdleTime)
	// Serialize schema migration + seeding across concurrent openers (e.g. parallel
	// test packages) using a session-level advisory lock. The lock is released
	// automatically when the connection is returned to the pool or closed.
	if _, err := sqlDB.Exec(`SELECT pg_advisory_lock(7337741001)`); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("advisory lock: %w", err)
	}
	// pgx runs multi-statement Exec via the simple protocol when there are no args.
	if _, err := sqlDB.Exec(schemaSQL); err != nil {
		sqlDB.Exec(`SELECT pg_advisory_unlock(7337741001)`) //nolint:errcheck
		sqlDB.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	d := &DB{sqlDB}
	if err := d.seedBuiltins(); err != nil {
		sqlDB.Exec(`SELECT pg_advisory_unlock(7337741001)`) //nolint:errcheck
		sqlDB.Close()
		return nil, fmt.Errorf("seed builtins: %w", err)
	}
	if _, err := sqlDB.Exec(`SELECT pg_advisory_unlock(7337741001)`); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("advisory unlock: %w", err)
	}
	return d, nil
}

// builtinAgent describes one of the fixed agents and its prompt-variable catalog.
type builtinAgent struct {
	key, name, role, desc string
	vars                  []promptVar
	interactiveShell      bool // default interactive-shell flag at row-creation time; ON CONFLICT never overrides a later manual toggle
	runSeconds            *int // per-run wall-clock cap in seconds at row creation; nil = table default (600), 0 = unlimited
}

type promptVar struct{ name, desc, example, source string }

// intp returns a pointer to v, for explicitly setting optional builtinAgent fields (e.g. runSeconds).
func intp(v int) *int { return &v }

// builtinAgents mirrors docs §5(a). Built-in tools are not persisted; only the agents and
// their variable catalog are seeded here.
// Note: interactive shell for planner/worker/mainagent/auto is defaulted to true by the
// interactive_shell_default_v1 block below (later toggles respected); interactiveShell here
// only covers new agents that must be enabled from row creation.
var builtinAgents = []builtinAgent{
	{"goals", "Goal Decomposition", "goals", "Decompose a pentest task's goal into independent, verifiable sub-goals.", []promptVar{
		{"EngagementDescription", "Task description (target/background)", "Test the example.com site", "exploration"},
		{"Now", "Server-side current time (RFC3339)", "2026-06-25T00:00:00Z", "runtime"},
	}, false, nil},
	{"planner", "Planning", "planner", "Reads the situation and decides goals; adds exploration intents only for genuinely uncovered new directions (one planning loop per task).", []promptVar{
		{"Goal", "Overall task goal", "Take admin access on example.com", "exploration"},
		{"AssetSummary", "Asset count/type summary (optional)", "domain:3 ip:5 site:2", "distilled"},
	}, false, nil},
	{"mainagent", "Main", "main", "Human interface: watches progress and turns operator intent into a hint or a high-priority intent.", []promptVar{
		{"Goal", "Current task goal", "Take admin access on example.com", "exploration"},
		{"AssetSummary", "Opening situation summary (optional)", "domain:3 ip:5", "distilled"},
		{"FindingsSummary", "Confirmed vulnerability summary (optional)", "high:1 medium:2", "distilled"},
	}, false, nil},
	{"worker", "Executor", "worker", "Claims one intent, executes it, writes discovered facts/vulnerabilities back to the knowledge graph, then stops.", []promptVar{
		{"ProxyAddr", "Recording proxy address (drives the if copy)", "127.0.0.1:8080", "runtime"},
		{"WorkerName", "Worker self-identifier (optional)", "worker-1", "runtime"},
	}, false, intp(1200)}, // worker per-round wall clock defaults to 1200s (hard targets/slow networks often exceed 600s; see AntiFox 0.3.3)
	// Auto: built-in "platform operations" agent. Not part of the pentest orchestration loop;
	// driven from the chat page, operates the platform via tools.
	{"auto", "Auto", "assistant", "Platform operations assistant: manages tasks (create/view/pause/hint) and assets via tools, and can create/edit skills, custom tools, and MCP.", nil, false, nil},
	// Pentest: built-in "standalone pentest" agent. Driven from the chat page; one operator runs
	// the full chain from recon to wrap-up, planning/executing/verifying solo. Interactive shell on by default.
	{"pentest", "Penetration Test", "assistant", "Standalone pentest agent: one operator runs recon → attack surface → deep exploitation → verification → wrap-up, planning, executing, and adversarially verifying solo.", nil, true, intp(0)},
}

// seedBuiltins inserts the fixed built-in agents and their variable catalog (idempotent).
func (d *DB) seedBuiltins() error {
	for _, a := range builtinAgents {
		var agentID int64
		err := d.QueryRow(`
INSERT INTO agents(key, name, description, role, builtin, enabled, interactive_shell, run_seconds)
VALUES ($1, $2, NULLIF($3,''), $4, true, true, $5, COALESCE($6, 600))
ON CONFLICT (key) DO UPDATE SET name = EXCLUDED.name, description = EXCLUDED.description
RETURNING id`, a.key, a.name, a.desc, a.role, a.interactiveShell, a.runSeconds).Scan(&agentID)
		if err != nil {
			return fmt.Errorf("agent %s: %w", a.key, err)
		}
		for _, v := range a.vars {
			if _, err := d.Exec(`
INSERT INTO agent_prompt_vars(agent_id, var_name, description, example, source)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (agent_id, var_name) DO UPDATE
  SET description = EXCLUDED.description, example = EXCLUDED.example, source = EXCLUDED.source`,
				agentID, v.name, v.desc, v.example, v.source); err != nil {
				return fmt.Errorf("agent %s var %s: %w", a.key, v.name, err)
			}
		}
	}
	// Drop catalog entries for variables that were renamed, so the white-list no
	// longer advertises a name templates can't resolve (EngagementTitle→Description).
	if _, err := d.Exec(`DELETE FROM agent_prompt_vars WHERE var_name IN ('EngagementTitle', 'CoverageGaps')`); err != nil {
		return fmt.Errorf("cleanup renamed vars: %w", err)
	}
	// Default-on interactive_shell for the runtime agents (planner/worker/mainagent/auto)
	// ONCE — respects a later user toggle-off (guarded by a settings flag). goals(one-shot
	// decomposer) stays off. Runs after the column exists (schema applied before seed).
	if v, _, _ := d.GetSetting("interactive_shell_default_v1"); v != "true" {
		if _, err := d.Exec(`UPDATE agents SET interactive_shell=true WHERE key IN ('planner','worker','mainagent','auto')`); err != nil {
			return fmt.Errorf("seed interactive_shell defaults: %w", err)
		}
		_ = d.SetSetting("interactive_shell_default_v1", "true")
	}
	// Seed built-in MCP servers — insert-if-absent so user edits survive restart.
	// browser (Playwright): headless browser automation, MITM recording proxy auto-
	// injected when traffic capture is on (server.Manager.syncBrowserMCPProxy).
	if _, err := d.Exec(`
INSERT INTO mcp_servers(name, transport, command, args, env, enabled)
VALUES ('browser', 'stdio', 'npx', $1, '{}', true)
ON CONFLICT (name) DO NOTHING`,
		`["@playwright/mcp","--headless"]`); err != nil {
		return fmt.Errorf("seed browser mcp: %w", err)
	}
	// filesystem: read/write files during pentest (payloads, output, configs).
	if _, err := d.Exec(`
INSERT INTO mcp_servers(name, transport, command, args, env, enabled, description)
VALUES ('filesystem', 'stdio', 'npx', $1, '{}', true, 'Read/write the local filesystem — store payloads, script output, discovered config files, etc. during pentests')
ON CONFLICT (name) DO NOTHING`,
		`["@modelcontextprotocol/server-filesystem","/tmp/antifox-fs"]`); err != nil {
		return fmt.Errorf("seed filesystem mcp: %w", err)
	}
	// github: recon via GitHub API — search repos, find exposed secrets, analyze code.
	// Seeded DISABLED: it needs a real GITHUB_PERSONAL_ACCESS_TOKEN, so leaving it
	// enabled with the placeholder guarantees "connection closed" error spam on every
	// deploy. Operator enables it after setting a token on the MCP page.
	if _, err := d.Exec(`
INSERT INTO mcp_servers(name, transport, command, args, env, enabled, description)
VALUES ('github', 'stdio', 'npx', $1, $2, false, 'GitHub repository recon — search public repos for leaked credentials, secrets, and sensitive config, or analyze the target organization open-source projects (enable after setting GITHUB_PERSONAL_ACCESS_TOKEN on the MCP page)')
ON CONFLICT (name) DO NOTHING`,
		`["@modelcontextprotocol/server-github"]`, `{"GITHUB_PERSONAL_ACCESS_TOKEN":"<your-token>"}`); err != nil {
		return fmt.Errorf("seed github mcp: %w", err)
	}
	if err := d.seedBuiltinMCPVisibility(); err != nil {
		return fmt.Errorf("seed mcp visibility: %w", err)
	}
	// NOTE: the placeholder ScopeSentry data-source MCP (empty URL + empty X-API-Key,
	// disabled) is seeded directly in schema.sql §F so a raw `psql < schema.sql` init
	// also gets it. schema.sql is Exec'd on every startup, so it stays idempotent.
	if err := d.seedBuiltinSkillVisibility(); err != nil {
		return fmt.Errorf("seed skill visibility: %w", err)
	}
	if err := d.seedDefaultInterceptRules(); err != nil {
		return fmt.Errorf("seed intercept rules: %w", err)
	}
	d.ensureKnowledgeVectorSchema() // best-effort; degrades to keyword search if pgvector absent
	return nil
}

// ensureKnowledgeVectorSchema creates the pgvector extension + knowledge_chunks
// table (the semantic layer). It is best-effort and NEVER fails boot: CREATE
// EXTENSION vector needs a superuser to have pre-installed it, so on a deployment
// where that hasn't happened the knowledge base degrades to keyword search.
// Records the outcome in the settings flag "knowledge.vector_available".
func (d *DB) ensureKnowledgeVectorSchema() {
	if _, err := d.Exec(`CREATE EXTENSION IF NOT EXISTS vector`); err != nil {
		log.Printf("[knowledge] pgvector unavailable (%v) — semantic search disabled, falling back to keyword search. "+
			"Enabling it requires a one-time CREATE EXTENSION vector by a superuser", err)
		_ = d.SetBool("knowledge.vector_available", false)
		return
	}
	// embedding uses a dimension-less vector column plus per-chunk embed_model/dim; search filters
	// by the current model, so switching embedders never mixes embeddings (old-model rows are
	// ignored and unified after a rebuild). Exact scan is fine at this data scale.
	if _, err := d.Exec(`
CREATE TABLE IF NOT EXISTS knowledge_chunks (
    id          BIGSERIAL PRIMARY KEY,
    doc_id      BIGINT NOT NULL REFERENCES knowledge_docs(id) ON DELETE CASCADE,
    chunk_index INT NOT NULL DEFAULT 0,
    content     TEXT NOT NULL DEFAULT '',
    embedding   vector,
    embed_model TEXT NOT NULL DEFAULT '',
    embed_dim   INT NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_knowledge_chunks_doc ON knowledge_chunks(doc_id);
CREATE INDEX IF NOT EXISTS idx_knowledge_chunks_model ON knowledge_chunks(embed_model);`); err != nil {
		log.Printf("[knowledge] failed to create knowledge_chunks (%v) — semantic search disabled", err)
		_ = d.SetBool("knowledge.vector_available", false)
		return
	}
	_ = d.SetBool("knowledge.vector_available", true)
}

// builtinSkillVisibility maps a shipped skill's directory name → the built-in
// agent keys that should see it by default. The skill FILES themselves live on the
// filesystem (SkillDir, loaded by the engine at runtime); DB only carries this visibility
// binding. Skills omitted here (e.g. playwright-cli, scopesentry) ship invisible by
// default — the user turns them on per-agent when needed. scopesentry additionally
// declares `mcps: ScopeSentry`, which only takes effect once it's made visible and
// that MCP is enabled/configured.
var builtinSkillVisibility = map[string][]string{
	"api-recon": {"auto", "pentest", "worker"},
	// Migrated from legacy AntiFox's pentest methodology library (tool-agnostic, driven through
	// Bash with public CLI tools).
	// Visible by default to the testing agents (worker/auto/pentest); planner/mainagent stay clean.
	// incident-response (blue-team) and security-awareness-training (training) are intentionally
	// not visible by default — installed; users enable them per-agent on the Skills page when needed.
	"api-security-testing":        {"worker", "auto", "pentest"},
	"business-logic-testing":      {"worker", "auto", "pentest"},
	"cloud-security-audit":        {"worker", "auto", "pentest"},
	"command-injection-testing":   {"worker", "auto", "pentest"},
	"container-security-testing":  {"worker", "auto", "pentest"},
	"csrf-testing":                {"worker", "auto", "pentest"},
	"deserialization-testing":     {"worker", "auto", "pentest"},
	"file-upload-testing":         {"worker", "auto", "pentest"},
	"idor-testing":                {"worker", "auto", "pentest"},
	"ldap-injection-testing":      {"worker", "auto", "pentest"},
	"mobile-app-security-testing": {"worker", "auto", "pentest"},
	"network-penetration-testing": {"worker", "auto", "pentest"},
	"secure-code-review":          {"worker", "auto", "pentest"},
	"security-automation":         {"worker", "auto", "pentest"},
	"sql-injection-testing":       {"worker", "auto", "pentest"},
	"ssrf-testing":                {"worker", "auto", "pentest"},
	"vulnerability-assessment":    {"worker", "auto", "pentest"},
	"xpath-injection-testing":     {"worker", "auto", "pentest"},
	"xss-testing":                 {"worker", "auto", "pentest"},
	"xxe-testing":                 {"worker", "auto", "pentest"},
}

// seedBuiltinSkillVisibility binds the shipped built-in skills to their default
// agents. Insert-if-absent (ON CONFLICT DO NOTHING) so a user's later toggle-off is
// never resurrected on restart — matches the browser-MCP / intercept-rule seed policy.
func (d *DB) seedBuiltinSkillVisibility() error {
	for skillName, agentKeys := range builtinSkillVisibility {
		for _, key := range agentKeys {
			if _, err := d.Exec(`
INSERT INTO agent_skill_visibility(agent_id, skill_name, enabled)
SELECT id, $2, true FROM agents WHERE key=$1
ON CONFLICT (agent_id, skill_name) DO NOTHING`, key, skillName); err != nil {
				return fmt.Errorf("skill %s → agent %s: %w", skillName, key, err)
			}
		}
	}
	return nil
}

// builtinMCPVisibility maps a shipped MCP server name → the built-in agent keys
// that should see it by default. browser/filesystem/github are useful for pentest
// agents (worker/auto/pentest); planner/mainagent stay clean.
var builtinMCPVisibility = map[string][]string{
	"browser":    {"worker", "auto", "pentest"},
	"filesystem": {"worker", "auto", "pentest"},
	"github":     {"worker", "auto", "pentest"},
}

// seedBuiltinMCPVisibility binds the shipped built-in MCP servers to their default
// agents via agent_visibility. Insert-if-absent (ON CONFLICT DO NOTHING) so a user's
// later toggle-off is never resurrected on restart.
func (d *DB) seedBuiltinMCPVisibility() error {
	for serverName, agentKeys := range builtinMCPVisibility {
		for _, key := range agentKeys {
			if _, err := d.Exec(`
INSERT INTO agent_visibility(agent_id, resource_kind, resource_id, enabled)
SELECT a.id, 'mcp', m.id, true
FROM agents a, mcp_servers m
WHERE a.key=$1 AND m.name=$2
ON CONFLICT (agent_id, resource_kind, resource_id, mcp_tool_name) DO NOTHING`, key, serverName); err != nil {
				return fmt.Errorf("mcp %s → agent %s: %w", serverName, key, err)
			}
		}
	}
	return nil
}

// seedDefaultInterceptRules inserts built-in safety intercept rules once on
// first startup. The seed is gated by a settings flag so user edits (disable,
// delete, re-order) are never overwritten on subsequent restarts.
func (d *DB) seedDefaultInterceptRules() error {
	if v, _, _ := d.GetSetting("intercept_default_rules_v1"); v == "done" {
		return nil
	}
	type rule struct {
		name     string
		target   string // tool_name | tool_input
		typ      string // string | regex
		pattern  string
		action   string
		message  string
		priority int
	}
	rules := []rule{
		// ── destructive system commands (priority 100) ──────────────────────
		{
			name:     "[builtin] recursive force delete rm -rf",
			target:   "tool_input",
			typ:      "regex",
			pattern:  `(?i)\brm\b.{0,80}(?:-[a-z]*r[a-z]*f[a-z]*|-[a-z]*f[a-z]*r[a-z]*|--recursive|--no-preserve-root)`,
			action:   "deny",
			message:  "Recursive force delete (rm -rf / rm --recursive) is denied; it can permanently damage the system or the target environment",
			priority: 100,
		},
		{
			name:     "[builtin] delete critical system directories",
			target:   "tool_input",
			typ:      "regex",
			pattern:  `\brm\b[^"'\n]{0,60}["'\s](/|/etc|/bin|/usr|/boot|/var|/lib|/sys|/proc|/dev|/sbin|/root)`,
			action:   "deny",
			message:  "Deleting critical system paths is denied",
			priority: 100,
		},
		{
			name:     "[builtin] disk format mkfs",
			target:   "tool_input",
			typ:      "regex",
			pattern:  `\bmkfs\b`,
			action:   "deny",
			message:  "Formatting disks (mkfs) is denied",
			priority: 100,
		},
		{
			name:     "[builtin] overwrite disk device dd",
			target:   "tool_input",
			typ:      "regex",
			pattern:  `\bdd\b[^|\n]{0,100}\bof=\s*/dev/[a-zA-Z]`,
			action:   "deny",
			message:  "Overwriting disk devices with dd is denied",
			priority: 100,
		},
		{
			name:     "[builtin] fork bomb",
			target:   "tool_input",
			typ:      "regex",
			pattern:  `:\(\)\s*\{[^}]*:\|:`,
			action:   "deny",
			message:  "Fork bombs are denied",
			priority: 100,
		},
		{
			name:     "[builtin] shutdown / reboot",
			target:   "tool_input",
			typ:      "regex",
			pattern:  `\b(?:shutdown|reboot|halt|poweroff|init\s+[06])\b`,
			action:   "deny",
			message:  "Shutdown or reboot commands are denied",
			priority: 100,
		},
		{
			name:     "[builtin] kill all processes",
			target:   "tool_input",
			typ:      "regex",
			pattern:  `\bkill\s+-9\s+-1\b|\bkillall\s+-9\b`,
			action:   "deny",
			message:  "kill -9 -1 or killall -9 (killing every process) is denied",
			priority: 100,
		},
		{
			name:     "[builtin] disk erase shred / wipe",
			target:   "tool_input",
			typ:      "regex",
			pattern:  `\b(?:shred|wipe)\b[^|\n]{0,80}/dev/[a-zA-Z]`,
			action:   "deny",
			message:  "Running shred/wipe erase against disk devices is denied",
			priority: 100,
		},
		{
			name:     "[builtin] flush firewall rules",
			target:   "tool_input",
			typ:      "regex",
			pattern:  `\biptables\s+(?:-F|--flush)\b|\bnft\s+flush\s+ruleset\b`,
			action:   "deny",
			message:  "Flushing firewall rules (iptables -F / nft flush) is denied",
			priority: 100,
		},
		// ── destructive database operations (priority 90) ───────────────────
		{
			name:     "[builtin] SQL DROP DATABASE / TABLE / SCHEMA",
			target:   "tool_input",
			typ:      "regex",
			pattern:  `(?i)\bDROP\s+(?:DATABASE|TABLE|SCHEMA|INDEX|VIEW|TABLESPACE|USER|ROLE)\b`,
			action:   "deny",
			message:  "DROP operations are denied; they can irreversibly destroy database objects",
			priority: 90,
		},
		{
			name:     "[builtin] SQL TRUNCATE",
			target:   "tool_input",
			typ:      "regex",
			pattern:  `(?i)\bTRUNCATE\s+(?:TABLE\s+)?\w`,
			action:   "deny",
			message:  "TRUNCATE is denied; it can wipe all data in a table",
			priority: 90,
		},
		{
			name:     "[builtin] MongoDB drop / dropDatabase",
			target:   "tool_input",
			typ:      "regex",
			pattern:  `(?i)\.(?:dropDatabase|dropCollection|drop)\s*\(`,
			action:   "deny",
			message:  "MongoDB drop operations are denied",
			priority: 90,
		},
		{
			name:     "[builtin] Redis FLUSHALL / FLUSHDB",
			target:   "tool_input",
			typ:      "regex",
			pattern:  `(?i)\b(?:FLUSHALL|FLUSHDB)\b`,
			action:   "deny",
			message:  "Redis FLUSHALL / FLUSHDB is denied; it can wipe the entire cache",
			priority: 90,
		},
		// ── destructive HTTP requests (priority 80) ─────────────────────────
		// Three common ways an agent sends DELETE requests:
		//   1. curl -X DELETE / --request DELETE (run directly by the Bash tool or written into a script)
		//   2. the .delete() method of a Python HTTP client
		//   3. method: 'DELETE' / method="DELETE" in JS/generic scripts
		{
			name:    "[builtin] DELETE request via curl / wget",
			target:  "tool_input",
			typ:     "regex",
			pattern: `(?i)\bcurl\b[^|\n&;"]{0,300}(?:-X\s*DELETE|--request\s+DELETE|-XDELETE)|\bwget\b[^|\n&;"]{0,300}--method[=\s]+DELETE`,
			action:  "deny",
			message: "Sending HTTP DELETE via curl/wget is denied; it can delete data on the target system",
			priority: 80,
		},
		{
			name:    "[builtin] Python HTTP client DELETE (requests/httpx/aiohttp)",
			target:  "tool_input",
			typ:     "regex",
			pattern: `(?i)\b(?:requests|httpx|aiohttp|urllib\.request)\.delete\s*\(|session\.delete\s*\(|client\.delete\s*\(`,
			action:  "deny",
			message: "Sending DELETE requests via a Python HTTP client is denied",
			priority: 80,
		},
		{
			name:    "[builtin] HTTP DELETE declared in scripts (JS/generic)",
			target:  "tool_input",
			typ:     "regex",
			pattern: `(?i)axios\.delete\s*\(|method\s*[:=]\s*['"]DELETE['"]`,
			action:  "deny",
			message: "Declaring and sending HTTP DELETE requests from scripts is denied",
			priority: 80,
		},
		{
			name:     "[builtin] bulk clear / purge endpoints",
			target:   "tool_input",
			typ:      "regex",
			pattern:  `(?i)/(?:clear|wipe|flush|purge|truncate|drop|destroy|factory[-_]reset|reset[-_]all)(?:[/?#"'\s]|$)`,
			action:   "deny",
			message:  "Calling bulk clear/destroy-style endpoints (/clear /wipe /flush /purge etc.) is denied",
			priority: 80,
		},
	}
	for _, r := range rules {
		if _, err := d.Exec(`
INSERT INTO intercept_rules(name, enabled, priority, match_target, match_type, pattern, action, message, timeout_enabled, timeout_seconds, timeout_action)
VALUES ($1, true, $2, $3, $4, $5, $6, $7, false, 60, 'deny')
ON CONFLICT DO NOTHING`,
			r.name, r.priority, r.target, r.typ, r.pattern, r.action, r.message,
		); err != nil {
			return fmt.Errorf("rule %q: %w", r.name, err)
		}
	}
	return d.SetSetting("intercept_default_rules_v1", "done")
}
