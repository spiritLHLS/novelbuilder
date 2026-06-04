#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT_DIR}"

PORT="${SMOKE_PORT:-18080}"
ADMIN_USERNAME="${SMOKE_ADMIN_USERNAME:-admin}"
ADMIN_PASSWORD="${SMOKE_ADMIN_PASSWORD:-novelbuilder-smoke-password}"
TMP_DIR="$(mktemp -d)"
BIN="${TMP_DIR}/novelbuilder-smoke"
LOG_FILE="${TMP_DIR}/server.log"
SERVER_PID=""

cleanup() {
  if [[ -n "${SERVER_PID}" ]]; then
    kill "${SERVER_PID}" >/dev/null 2>&1 || true
    wait "${SERVER_PID}" >/dev/null 2>&1 || true
  fi
  rm -rf "${TMP_DIR}"
}
trap cleanup EXIT INT TERM

need() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing command: $1" >&2
    exit 1
  fi
}

json_get() {
  python3 -c "import json,sys; data=json.load(sys.stdin); cur=data; [cur := cur[p] for p in sys.argv[1].split('.') if p]; print(cur)" "$1"
}

need curl
need go
need node
need npm
need python3

if [[ "${SMOKE_SKIP_BUILD:-0}" != "1" ]]; then
  if [[ ! -d frontend/node_modules ]]; then
    (cd frontend && npm ci --legacy-peer-deps)
  fi
  (cd frontend && npm run build)
  (cd backend && go build -trimpath -ldflags "-s -w -buildid= -X main.version=smoke" -o "${BIN}" ./cmd/server)
elif [[ -n "${SMOKE_BACKEND_BIN:-}" ]]; then
  BIN="${SMOKE_BACKEND_BIN}"
else
  echo "SMOKE_SKIP_BUILD=1 requires SMOKE_BACKEND_BIN" >&2
  exit 64
fi

SQLITE_PATH="${TMP_DIR}/novelbuilder.db" \
APP_PROFILE=smoke \
SERVER_HOST=127.0.0.1 \
SERVER_PORT="${PORT}" \
SERVER_MODE=release \
DB_DRIVER=sqlite \
REDIS_ENABLED=false \
REDIS_URL="" \
NEO4J_URI="" \
QDRANT_URL="" \
SIDECAR_URL=http://127.0.0.1:65535 \
ADMIN_USERNAME="${ADMIN_USERNAME}" \
ADMIN_PASSWORD="${ADMIN_PASSWORD}" \
"${BIN}" >"${LOG_FILE}" 2>&1 &
SERVER_PID="$!"

base_url="http://127.0.0.1:${PORT}"
for _ in $(seq 1 60); do
  if curl -fsS "${base_url}/api/health" >/dev/null 2>&1; then
    break
  fi
  if ! kill -0 "${SERVER_PID}" >/dev/null 2>&1; then
    echo "server exited during startup" >&2
    cat "${LOG_FILE}" >&2 || true
    exit 1
  fi
  sleep 1
done
curl -fsS "${base_url}/api/health" >/dev/null

login_json="$(curl -fsS \
  -H 'Content-Type: application/json' \
  -d "{\"username\":\"${ADMIN_USERNAME}\",\"password\":\"${ADMIN_PASSWORD}\"}" \
  "${base_url}/api/auth/login")"
token="$(printf '%s' "${login_json}" | json_get token)"

auth_header=(-H "Authorization: Bearer ${token}" -H "Content-Type: application/json")

project_json="$(curl -fsS "${auth_header[@]}" \
  -d '{"title":"Smoke Story","genre":"test","description":"non-interactive smoke test","target_words":10000,"chapter_words":2000}' \
  "${base_url}/api/projects")"
project_id="$(printf '%s' "${project_json}" | json_get data.id)"

curl -fsS "${auth_header[@]}" \
  -d '{"name":"Smoke Local","provider":"openai_compatible","base_url":"http://127.0.0.1:11434/v1","api_key":"smoke-key","model_name":"smoke-model","max_tokens":1024,"temperature":0.7,"is_default":true}' \
  "${base_url}/api/llm-profiles" >/dev/null

curl -fsS "${auth_header[@]}" "${base_url}/api/projects/${project_id}" >/dev/null
curl -fsS "${auth_header[@]}" "${base_url}/api/tasks/stats" >/dev/null
curl -fsS "${auth_header[@]}" "${base_url}/api/setup/status" >/dev/null

echo "SQLite smoke test passed: project_id=${project_id}"
