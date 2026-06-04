#!/usr/bin/env bash
set -uo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT_DIR}"

ERRORS=0

pass() { printf '[PASS] %s\n' "$*"; }
fail() { printf '[FAIL] %s\n' "$*" >&2; ERRORS=$((ERRORS + 1)); }
info() { printf '[INFO] %s\n' "$*"; }

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    fail "missing command: $1"
    return 1
  fi
}

check_rg() {
  if command -v rg >/dev/null 2>&1; then
    rg "$@"
  else
    grep -R "$@"
  fi
}

echo "==> Workflow"
if check_rg -n 'FORCE_JAVASCRIPT_ACTIONS_TO_NODE24:\s*true' .github/workflows -g '*.yml' >/dev/null; then
  pass "Node 24 action compatibility flag is set"
else
  fail "FORCE_JAVASCRIPT_ACTIONS_TO_NODE24 is missing"
fi

if check_rg -n 'actions/checkout@v[45]|actions/setup-go@v5|actions/setup-node@v4|actions/upload-artifact@v4|docker/(setup-qemu-action|setup-buildx-action|login-action)@v3|docker/build-push-action@v[56]' .github/workflows -g '*.yml' >/tmp/nb_forbidden_actions.$$ 2>/dev/null; then
  cat /tmp/nb_forbidden_actions.$$ >&2
  fail "workflow still references deprecated action versions"
else
  pass "workflow action versions are current"
fi
rm -f /tmp/nb_forbidden_actions.$$

job_count=$(check_rg -n 'runs-on:' .github/workflows -g '*.yml' 2>/dev/null | wc -l | tr -d ' ')
timeout_count=$(check_rg -n 'timeout-minutes:' .github/workflows -g '*.yml' 2>/dev/null | wc -l | tr -d ' ')
if [[ "${timeout_count}" -ge "${job_count}" && "${job_count}" -gt 0 ]]; then
  pass "all workflow jobs have timeout-minutes"
else
  fail "workflow timeout-minutes count ${timeout_count} is lower than job count ${job_count}"
fi

echo
echo "==> Docker"
for script in docker/docker-entrypoint.sh docker/docker-entrypoint.standard.sh docker/docker-entrypoint.no-neo4j.sh docker/docker-entrypoint.no-qdrant.sh; do
  if bash -n "${script}"; then
    pass "${script} syntax"
  else
    fail "${script} syntax"
  fi
done

for file in Dockerfile.no-redis Dockerfile.no-neo4j Dockerfile.no-qdrant Dockerfile.no-graph-vector; do
  profile="$(awk -F= '/APP_PROFILE=/{gsub(/"/, "", $2); print $2; exit}' "${file}")"
  conf="docker/supervisord.${profile}.conf"
  if [[ -n "${profile}" && -f "${conf}" ]]; then
    pass "${file} -> ${conf}"
  else
    fail "${file} APP_PROFILE does not map to a supervisord config"
  fi
done

if check_rg -n 'DB_PASSWORD=.*:-novelbuilder|NEO4J_PASSWORD=.*:-novelbuilder|POSTGRES_PASSWORD=.*:-novelbuilder|NEO4J_AUTH:\s*neo4j/\$\{NEO4J_PASSWORD:-' docker docker-compose*.yml >/tmp/nb_docker_password_defaults.$$ 2>/dev/null; then
  cat /tmp/nb_docker_password_defaults.$$ >&2
  fail "Docker profiles must not define fixed database/Neo4j password defaults"
else
  pass "Docker profiles do not use fixed database/Neo4j password defaults"
fi
rm -f /tmp/nb_docker_password_defaults.$$

echo
echo "==> Dependency Audit Hooks"
if [[ -x scripts/python_dependency_audit.sh ]] && bash -n scripts/python_dependency_audit.sh; then
  pass "Python dependency audit script is executable and syntactically valid"
else
  fail "scripts/python_dependency_audit.sh is missing, not executable, or invalid"
fi

if [[ -x scripts/secret_history_scan.sh ]] && bash -n scripts/secret_history_scan.sh; then
  pass "secret history scan script is executable and syntactically valid"
else
  fail "scripts/secret_history_scan.sh is missing, not executable, or invalid"
fi

echo
echo "==> Database Pool Contract"
if grep -Fq 'envIntMin("DB_MAX_OPEN_CONNS", 25, 20)' backend/internal/config/config.go && \
  grep -Fq 'envIntMin("DB_MAX_IDLE_CONNS", 5, 5)' backend/internal/config/config.go && \
  grep -Fq 'envIntRange("DB_CONN_MAX_LIFETIME_MINUTES", 60, 1, 60)' backend/internal/config/config.go && \
  grep -Fq 'SetConnMaxLifetime(time.Duration(lifetimeMinutes) * time.Minute)' backend/internal/database/gorm.go && \
  grep -Fq '_env_int_min("SIDECAR_DB_MIN_CONNS", 5, 5)' python-sidecar/main.py && \
  grep -Fq '_env_int_min("SIDECAR_DB_MAX_CONNS", 20, 20)' python-sidecar/main.py; then
  pass "database pool defaults enforce open>=20 idle>=5 lifetime<=60m"
else
  fail "database pool contract is missing or weakened"
fi

echo
echo "==> Secret Hygiene"
if check_rg -n 'spiritlhl136|sk-[A-Za-z0-9_-]{20,}|AIza[A-Za-z0-9_-]{20}|admin123|password:\s*password|POSTGRES_PASSWORD:\s*password|NEO4J_AUTH:\s*neo4j/password|Access-Control-Allow-Origin:\s*\*' . \
  -g '!frontend/package-lock.json' \
  -g '!backend/go.sum' \
  -g '!scripts/ci_check.sh' \
  -g '!scripts/secret_history_scan.sh' \
  -g '!python-sidecar/novel-downloader/**' \
  -g '!node_modules/**' >/tmp/nb_secret_hits.$$ 2>/dev/null; then
  cat /tmp/nb_secret_hits.$$ >&2
  fail "potential hardcoded secret or wildcard CORS hit"
else
  pass "no high-confidence hardcoded secret patterns found"
fi
rm -f /tmp/nb_secret_hits.$$

if check_rg -n 'DB_PASSWORD.*novelbuilder|NEO4J_PASSWORD.*novelbuilder|POSTGRES_PASSWORD.*novelbuilder|NEO4J_AUTH.*novelbuilder|os\.getenv\("DB_PASSWORD", "novelbuilder"\)|os\.getenv\("NEO4J_PASSWORD", "novelbuilder"\)|envStr\("DB_PASSWORD", "novelbuilder"\)' backend python-sidecar docker scripts docker-compose*.yml \
  -g '!scripts/ci_check.sh' \
  -g '!python-sidecar/novel-downloader/**' >/tmp/nb_password_default_hits.$$ 2>/dev/null; then
  cat /tmp/nb_password_default_hits.$$ >&2
  fail "database/Neo4j passwords must not fall back to the fixed value novelbuilder"
else
  pass "database/Neo4j passwords do not use fixed novelbuilder fallbacks"
fi
rm -f /tmp/nb_password_default_hits.$$

for pattern in '*.env' '*.db' '*.csv' 'result.json' 'screenshots/' '.cache/' '__pycache__/'; do
  if grep -Fxq "${pattern}" .gitignore; then
    pass ".gitignore includes ${pattern}"
  else
    fail ".gitignore missing ${pattern}"
  fi
done

echo
echo "==> API Contracts"
require_cmd node
if [[ -x scripts/api_route_contract_check.sh ]]; then
  if scripts/api_route_contract_check.sh; then
    pass "frontend/backend API contract"
  else
    fail "frontend/backend API contract"
  fi
else
  fail "scripts/api_route_contract_check.sh is not executable"
fi

if [[ -x scripts/sidecar_contract_check.sh ]]; then
  if scripts/sidecar_contract_check.sh; then
    pass "Go/Python sidecar contract"
  else
    fail "Go/Python sidecar contract"
  fi
else
  fail "scripts/sidecar_contract_check.sh is not executable"
fi

if grep -Fq 'handlers.RegisterDocsRoutes(r, authMiddleware, version)' backend/cmd/server/main.go && \
  grep -Fq 'docs.Use(authMiddleware)' backend/internal/handlers/handler_docs.go && \
  [[ -f backend/internal/handlers/handler_docs_test.go ]]; then
  pass "authenticated OpenAPI docs route is registered and tested"
else
  fail "authenticated OpenAPI docs route is missing or untested"
fi

echo
echo "==> Frontend Build Contract"
if grep -q 'rolldownOptions:' frontend/vite.config.ts && \
  grep -q "name: 'vendor-element'" frontend/vite.config.ts && \
  grep -q "name: 'vendor-graph'" frontend/vite.config.ts; then
  pass "Vite keeps explicit Rolldown vendor chunks"
else
  fail "frontend/vite.config.ts is missing explicit Rolldown vendor chunks"
fi

echo
echo "==> Documentation"
en_headers=$(grep -c '^##' README.md 2>/dev/null || echo 0)
zh_headers=$(grep -c '^##' README.zh-CN.md 2>/dev/null || echo 0)
if [[ "${en_headers}" -eq "${zh_headers}" ]]; then
  pass "README section counts match (${en_headers})"
else
  fail "README section count mismatch: EN=${en_headers} ZH=${zh_headers}"
fi

echo
if [[ "${ERRORS}" -eq 0 ]]; then
  echo "All static checks passed."
else
  echo "${ERRORS} static check(s) failed." >&2
  exit 1
fi
