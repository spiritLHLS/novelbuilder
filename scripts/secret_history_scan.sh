#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT_DIR}"

if command -v gitleaks >/dev/null 2>&1; then
  exec gitleaks detect --source . --no-banner
fi

pattern='spiritlhl136|sk-[A-Za-z0-9_-]{20,}|AIza[A-Za-z0-9_-]{20}|admin123|password:[[:space:]]*password|POSTGRES_PASSWORD:[[:space:]]*password|NEO4J_AUTH:[[:space:]]*neo4j/password|Access-Control-Allow-Origin:[[:space:]]*\*'
tmp_hits="$(mktemp)"
trap 'rm -f "${tmp_hits}"' EXIT

scan_rev() {
  local rev="$1"
  git grep -nE "${pattern}" "${rev}" -- \
    . \
    ':!frontend/package-lock.json' \
    ':!backend/go.sum' \
    ':!scripts/ci_check.sh' \
    ':!scripts/secret_history_scan.sh' \
    ':!python-sidecar/novel-downloader/**' \
    ':!node_modules/**' \
    >> "${tmp_hits}" 2>/dev/null || true
}

scan_rev HEAD

while IFS= read -r rev; do
  scan_rev "${rev}"
done < <(git rev-list --all)

if [[ -s "${tmp_hits}" ]]; then
  sort -u "${tmp_hits}" >&2
  echo "Potential secret patterns found in git history." >&2
  exit 1
fi

echo "No high-confidence secret patterns found in git history fallback scan."
