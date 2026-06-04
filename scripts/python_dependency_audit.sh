#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT_DIR}"

if ! command -v pip-audit >/dev/null 2>&1; then
  cat >&2 <<'EOF'
pip-audit is not installed.

Install it in an isolated environment, then rerun:
  python -m venv /tmp/novelbuilder-pip-audit
  /tmp/novelbuilder-pip-audit/bin/python -m pip install pip-audit
  PATH="/tmp/novelbuilder-pip-audit/bin:$PATH" scripts/python_dependency_audit.sh
EOF
  exit 127
fi

tmp_requirements="$(mktemp)"
trap 'rm -f "${tmp_requirements}"' EXIT

awk '
  /^[[:space:]]*#/ { next }
  /^[[:space:]]*$/ { next }
  /==/ { print }
' \
  python-sidecar/requirements-base.txt \
  python-sidecar/requirements-graph.txt \
  python-sidecar/requirements-vector.txt \
  python-sidecar/requirements-browser.txt \
  > "${tmp_requirements}"

echo "Auditing directly pinned Python requirements:"
cat "${tmp_requirements}"
echo

# --disable-pip keeps the audit independent from the host interpreter and avoids
# resolving heavyweight optional stacks such as torch on unsupported local Python
# versions.  Full transitive audits should run inside the target Python 3.11 image.
pip-audit --disable-pip --no-deps -r "${tmp_requirements}"
