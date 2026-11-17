#!/usr/bin/env bash
# Dev mode: backend(:8787) + traffic proxy(:8788) + frontend next dev(:5173) together.
# The frontend proxies /api to the backend; Ctrl-C stops everything.
#
# For the single-binary build (embedded frontend) see the README "single binary"
# section; this script is not part of that flow.
set -euo pipefail
cd "$(dirname "$0")"

# On exit, kill every subprocess in this process group (backend + frontend).
cleanup() { kill 0 2>/dev/null || true; }
trap cleanup EXIT INT TERM

# Backend (plain go run, no embedded frontend); concurrent worker count is set in System Settings.
go run ./cmd/antifox -addr :8787 -proxy :8788 &

# Frontend hot reload (Vite/Next dev server, /api proxied to :8787).
( cd web && npm run dev ) &

echo "[dev] backend :8787 / proxy :8788 / frontend http://localhost:5173  (Ctrl-C to exit)"
wait
