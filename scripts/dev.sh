#!/bin/sh
# `make dev`: the backend on HTTP_PORT, then the Vite dev server on :5173
# once the backend answers. If the backend cannot start, this stops with its
# error rather than leaving Vite up with nothing to proxy to.
set -eu
cd "$(dirname "$0")/.."
./scripts/dev-env.sh

port=$(sed -n 's/^HTTP_PORT=//p' backend/.env | tr -d '[:space:]')
port=${port:-3000}

echo "Building the backend..."
(cd backend && go build -o bin/sql-mcp-dev ./cmd/server)

(cd backend && exec ./bin/sql-mcp-dev) &
backend_pid=$!
trap 'kill "$backend_pid" 2>/dev/null || true' EXIT INT TERM

i=0
until curl -sf "http://localhost:$port/healthz" >/dev/null 2>&1; do
  if ! kill -0 "$backend_pid" 2>/dev/null; then
    echo "The backend exited; its error is above. Check backend/.env." >&2
    exit 1
  fi
  i=$((i + 1))
  if [ "$i" -gt 60 ]; then
    echo "The backend did not answer on :$port within 30s." >&2
    exit 1
  fi
  sleep 0.5
done
echo "Backend up on :$port; starting the web app on http://localhost:5173"

cd frontend
npm install --no-audit --no-fund
SQLMCP_DEV_API_TARGET="http://localhost:$port" npm run dev
