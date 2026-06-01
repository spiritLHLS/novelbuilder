#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
if [[ -d "${SCRIPT_DIR}/python-sidecar" || -d "${SCRIPT_DIR}/frontend" ]]; then
  ROOT_DIR="${SCRIPT_DIR}"
else
  ROOT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
fi
cd "${ROOT_DIR}"

PYTHON_BIN="${PYTHON_BIN:-python3}"
SIDECAR_HOST="${SIDECAR_HOST:-127.0.0.1}"
SIDECAR_PORT="${SIDECAR_PORT:-8081}"
SERVER_HOST="${SERVER_HOST:-0.0.0.0}"
SERVER_PORT="${SERVER_PORT:-8080}"

export APP_PROFILE="${APP_PROFILE:-binary}"
export SERVER_HOST SERVER_PORT
export SIDECAR_URL="${SIDECAR_URL:-http://${SIDECAR_HOST}:${SIDECAR_PORT}}"
export DB_DRIVER="${DB_DRIVER:-sqlite}"
export DB_HOST="${DB_HOST:-127.0.0.1}"
export DB_PORT="${DB_PORT:-5432}"
export DB_USER="${DB_USER:-novelbuilder}"
export DB_PASSWORD="${DB_PASSWORD:-novelbuilder}"
export DB_NAME="${DB_NAME:-novelbuilder}"
export DB_SSLMODE="${DB_SSLMODE:-disable}"
export SQLITE_PATH="${SQLITE_PATH:-${ROOT_DIR}/data/novelbuilder.db}"
export REDIS_ENABLED="${REDIS_ENABLED:-false}"
export REDIS_URL="${REDIS_URL:-}"
export NEO4J_URI="${NEO4J_URI:-}"
export QDRANT_URL="${QDRANT_URL:-}"

need() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing command: $1" >&2
    exit 1
  fi
}

need "${PYTHON_BIN}"

if [[ "${DB_DRIVER}" == "sqlite" || "${DB_DRIVER}" == "sqlite3" ]]; then
  mkdir -p "$(dirname "${SQLITE_PATH}")" "${ROOT_DIR}/data/uploads"
elif [[ "${DB_DRIVER}" == "postgres" && "${SKIP_DB_CHECK:-0}" != "1" ]]; then
  if ! timeout 3 bash -c "cat < /dev/null > /dev/tcp/${DB_HOST}/${DB_PORT}" 2>/dev/null; then
    echo "PostgreSQL is not reachable at ${DB_HOST}:${DB_PORT}." >&2
    echo "Set DB_HOST/DB_PORT/DB_USER/DB_PASSWORD/DB_NAME, or start docker compose first." >&2
    exit 65
  fi
elif [[ "${DB_DRIVER}" != "postgres" ]]; then
  echo "Unsupported DB_DRIVER=${DB_DRIVER}; use sqlite or postgres." >&2
  exit 64
fi

if [[ ! -d "python-sidecar/.venv" ]]; then
  "${PYTHON_BIN}" -m venv python-sidecar/.venv
fi

# shellcheck disable=SC1091
source python-sidecar/.venv/bin/activate
pip install -q --upgrade pip
pip install -q -r python-sidecar/requirements.txt

if [[ -f "python-sidecar/runtime_capabilities.py" ]]; then
  (
    cd python-sidecar
    python - <<'PY'
from runtime_capabilities import detect_accelerators
caps = detect_accelerators()
print(f"accelerator={caps.get('selected_accelerator', 'cpu')}")
PY
  )
fi

BACKEND_BIN="${BACKEND_BIN:-${ROOT_DIR}/novelbuilder}"
if [[ ! -x "${BACKEND_BIN}" ]]; then
  if [[ -x "${ROOT_DIR}/novelbuilder.exe" ]]; then
    BACKEND_BIN="${ROOT_DIR}/novelbuilder.exe"
  elif [[ -d "${ROOT_DIR}/backend" ]]; then
    need go
    echo "building backend binary..."
    (cd backend && go build -o "${ROOT_DIR}/novelbuilder" ./cmd/server)
    BACKEND_BIN="${ROOT_DIR}/novelbuilder"
  else
    echo "backend binary not found: ${BACKEND_BIN}" >&2
    exit 66
  fi
fi

if [[ ! -d "frontend/dist" ]]; then
  if [[ -d "frontend" ]]; then
    need npm
    (cd frontend && npm install --legacy-peer-deps && npm run build)
  else
    echo "frontend/dist not found" >&2
    exit 67
  fi
fi

sidecar_pid=""
cleanup() {
  if [[ -n "${sidecar_pid}" ]]; then
    kill "${sidecar_pid}" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT INT TERM

(
  cd python-sidecar
  uvicorn main:app --host "${SIDECAR_HOST}" --port "${SIDECAR_PORT}"
) &
sidecar_pid="$!"

echo "NovelBuilder setup page: http://127.0.0.1:${SERVER_PORT}/setup"
exec "${BACKEND_BIN}"
