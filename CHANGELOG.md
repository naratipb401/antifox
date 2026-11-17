# Changelog

Notable changes per version. Format follows [Keep a Changelog](https://keepachangelog.com/); versioning follows [SemVer](https://semver.org/).

## v2.0.3

- **Per-session LLM header**: each LLM config can set one HTTP header whose value is automatically the current session id — fits gateways such as opencode that do per-session prompt caching / sticky routing.
- **Global egress proxy**: target traffic of all agents can leave the network through a single egress proxy (http/https/socks5) to hide the source IP / route via a jump host; includes a "test availability" check (echoes the egress IP through the proxy).
- **Per-task egress**: when creating a task, choose whether it uses the global proxy or connects directly.
- **Confirm-to-enrich**: manually confirmed findings are enriched into structured data automatically — confirmation makes them deliverable.
- **Report management**: new reports list only confirmed findings by default (with status / CVSS); the body reuses the enriched data.
- **Auto-filled evidence captions**: evidence screenshot captions are generated from the reproduction steps and remain editable.
- **Finding detail polish**: status dropdown, section headings, one-click copy.
- **Traffic task filter fix**: filtering by task now intersects with the task's activity window, so traffic no longer leaks across repeated tests of the same target.
- The system settings page now uses a responsive two-column layout.

## v2.0.2

- **One-click update**: check for and install new GitHub releases from the system settings page (SHA256 verification / atomic swap / automatic rollback on failure); data is unaffected.
- Release artifacts now bundle a `start.sh` supervisor script and a systemd unit template.

## v2.0.1

- Reproduction steps are linked to the real traffic capture on save.

## v2.0.0

- Evidence-driven delivery: reproduction steps persisted in place, findings page organized by exploit chain with per-step traffic captures; human-grade trilingual reports.
