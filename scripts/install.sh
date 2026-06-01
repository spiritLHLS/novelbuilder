#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT_DIR}"

PYTHON_BIN="${PYTHON_BIN:-python3}"
VERSION="${VERSION:-local}"

need() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing command: $1" >&2
    exit 1
  fi
}

need go
need npm
need "${PYTHON_BIN}"

echo "==> Preparing Python sidecar"
if [[ ! -d "python-sidecar/.venv" ]]; then
  "${PYTHON_BIN}" -m venv python-sidecar/.venv
fi
# shellcheck disable=SC1091
source python-sidecar/.venv/bin/activate
pip install -q --upgrade pip
pip install -q -r python-sidecar/requirements.txt
if [[ -f "python-sidecar/novel-downloader/pyproject.toml" ]]; then
  pip install -q ./python-sidecar/novel-downloader
fi

echo "==> Building frontend"
(cd frontend && npm ci --legacy-peer-deps && npm run build)

echo "==> Building backend"
(cd backend && go build -trimpath -ldflags "-s -w -buildid= -X main.version=${VERSION}" -o "${ROOT_DIR}/novelbuilder" ./cmd/server)

chmod +x scripts/run-local.sh novelbuilder

echo
echo "Install complete."
echo "Default run mode uses local SQLite at ./data/novelbuilder.db."
echo "Set DB_DRIVER=postgres plus DB_HOST/DB_PORT/DB_USER/DB_PASSWORD/DB_NAME for PostgreSQL."
echo "Start: ./scripts/run-local.sh"
