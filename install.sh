#!/usr/bin/env bash
# AntiFox install script: (1) all-Docker  (2) local build & run
set -euo pipefail
cd "$(cd "$(dirname "$0")" && pwd)"

info(){ printf '\033[36m[*]\033[0m %s\n' "$*"; }
ok(){   printf '\033[32m[+]\033[0m %s\n' "$*"; }
warn(){ printf '\033[33m[!]\033[0m %s\n' "$*"; }
die(){  printf '\033[31m[x]\033[0m %s\n' "$*" >&2; exit 1; }
ask(){  local p="$1" d="${2:-}" a; read -rp "$p${d:+ [$d]}: " a; echo "${a:-$d}"; }
rand(){ head -c 18 /dev/urandom | base64 | tr -dc 'A-Za-z0-9' | head -c 24; }

# -- docker environment check / auto-install --------------------
ensure_docker(){
  if command -v docker >/dev/null 2>&1 && docker compose version >/dev/null 2>&1; then
    ok "docker and docker compose detected"; return
  fi
  warn "docker / docker compose not found"
  case "$(uname -s)" in
    Linux)
      if [ "$(ask 'Auto-install Docker? (y/n)' y)" = y ]; then
        curl -fsSL https://get.docker.com | sh
        sudo usermod -aG docker "$USER" || true
        ok "Docker installed (log out and back in for group changes to take effect, no sudo needed)"
      else
        die "Please install docker yourself, then retry"
      fi ;;
    Darwin) die "On macOS, install Docker Desktop: https://www.docker.com/products/docker-desktop/" ;;
    *)      die "Please install docker yourself, then retry" ;;
  esac
}

# -- (1) all-Docker ---------------------------------------------
install_docker(){
  ensure_docker
  if [ ! -f .env ]; then
    cp .env.example .env 2>/dev/null || true
    local pw key
    pw="$(ask 'Postgres password (Enter to generate randomly)' "$(rand)")"
    key="$(ask 'ANTHROPIC_API_KEY (may be empty; configure later in the UI)' '')"
    sed -i.bak "s|^POSTGRES_PASSWORD=.*|POSTGRES_PASSWORD=${pw}|" .env
    sed -i.bak "s|^ANTHROPIC_API_KEY=.*|ANTHROPIC_API_KEY=${key}|" .env
    rm -f .env.bak
    ok ".env generated (POSTGRES_PASSWORD set)"
  else
    info "Keeping the existing .env"
  fi
  info "Pulling images and starting..."
  docker compose pull || true
  docker compose up -d
  ok "Started → http://localhost:8787"
  info "Logs: docker compose logs -f antifox"
}

# -- (2) local build & run --------------------------------------
install_local(){
  echo "Database setup:"
  echo "  1) Connect to an existing PostgreSQL"
  echo "  2) Start a PostgreSQL via Docker (requires docker)"
  case "$(ask 'Choice' 1)" in
    2)
      ensure_docker
      local pw; pw="$(ask 'Postgres password (Enter for random)' "$(rand)")"
      docker run -d --name antifox-pg -p 5432:5432 \
        -e POSTGRES_USER=antifox -e POSTGRES_PASSWORD="$pw" -e POSTGRES_DB=antifox \
        -v antifox-pg:/var/lib/postgresql/data postgres:16-alpine
      DB_HOST=127.0.0.1 DB_PORT=5432 DB_USER=antifox DB_PASS="$pw" DB_NAME=antifox DB_SSL=disable ;;
    *)
      DB_HOST="$(ask 'Database host' 127.0.0.1)"
      DB_PORT="$(ask 'Port' 5432)"
      DB_USER="$(ask 'User' antifox)"
      DB_PASS="$(ask 'Password' '')"
      DB_NAME="$(ask 'Database name' antifox)"
      DB_SSL="$(ask 'sslmode (disable/require)' disable)" ;;
  esac

  # generate config.json
  cat > config.json <<JSON
{
  "database": {
    "host": "${DB_HOST}",
    "port": ${DB_PORT},
    "user": "${DB_USER}",
    "password": "${DB_PASS}",
    "dbname": "${DB_NAME}",
    "sslmode": "${DB_SSL}"
  }
}
JSON
  ok "config.json generated"

  # Go toolchain check
  command -v go >/dev/null 2>&1 || die "Go not found; install Go (>=1.26) first: https://go.dev/dl/"
  ok "Go: $(go version)"

  # the embedded frontend needs node to produce static assets
  if command -v npm >/dev/null 2>&1; then
    info "Building frontend static assets..."
    ( cd web && npm ci && npm run build:static )
    rm -rf server/webui/dist && cp -r web/out server/webui/dist
    info "Building embedded single binary..."
    CGO_ENABLED=0 go build -tags embedui -trimpath -o antifox ./cmd/antifox
  else
    warn "npm not found: building the backend **without the embedded frontend** (run npm run dev for the frontend separately)"
    CGO_ENABLED=0 go build -o antifox ./cmd/antifox
  fi
  ok "Build complete → ./antifox"

  info "Starting... (Ctrl-C to exit)"
  ./antifox
}

echo "=============================="
echo "  AntiFox install"
echo "  1) All-Docker install"
echo "  2) Local run (go build)"
echo "=============================="
case "$(ask 'Choice' 1)" in
  1) install_docker ;;
  2) install_local ;;
  *) die "Invalid choice" ;;
esac
