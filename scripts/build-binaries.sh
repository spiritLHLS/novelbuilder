#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DIST_DIR="${DIST_DIR:-${ROOT_DIR}/dist/binaries}"
VERSION="${VERSION:-dev}"

mkdir -p "${DIST_DIR}"

echo "==> Building frontend"
(
  cd "${ROOT_DIR}/frontend"
  npm install --legacy-peer-deps
  npm run build
)

build_one() {
  local goos="$1"
  local goarch="$2"
  local ext=""
  if [[ "${goos}" == "windows" ]]; then
    ext=".exe"
  fi

  local name="novelbuilder-${VERSION}-${goos}-${goarch}"
  local out="${DIST_DIR}/${name}"
  rm -rf "${out}"
  mkdir -p "${out}/frontend" "${out}/python-sidecar"

  echo "==> Building ${name}"
  (
    cd "${ROOT_DIR}/backend"
    GOSUMDB="${GOSUMDB:-off}" GOPROXY="${GOPROXY:-https://proxy.golang.org,direct}" \
      CGO_ENABLED=0 GOOS="${goos}" GOARCH="${goarch}" \
      go build -ldflags "-s -w -X main.version=${VERSION}" -o "${out}/novelbuilder${ext}" ./cmd/server
  )

  cp -R "${ROOT_DIR}/frontend/dist" "${out}/frontend/dist"
  rsync -a --exclude '__pycache__' --exclude '.venv' --exclude 'novel-downloader/.git' \
    "${ROOT_DIR}/python-sidecar/" "${out}/python-sidecar/"
  cp "${ROOT_DIR}/scripts/run-local.sh" "${out}/run-local.sh"
  cp "${ROOT_DIR}/scripts/run-local.ps1" "${out}/run-local.ps1"
  chmod +x "${out}/run-local.sh" || true

  (
    cd "${DIST_DIR}"
    if command -v tar >/dev/null 2>&1; then
      tar -czf "${name}.tar.gz" "${name}"
    fi
    if command -v zip >/dev/null 2>&1; then
      zip -qr "${name}.zip" "${name}"
    fi
  )
}

if [[ -n "${TARGETS:-}" ]]; then
  IFS=',' read -r -a targets <<< "${TARGETS}"
else
  targets=(
    "linux amd64"
    "linux arm64"
    "darwin amd64"
    "darwin arm64"
    "windows amd64"
  )
fi

for target in "${targets[@]}"; do
  build_one ${target}
done

echo "==> Artifacts written to ${DIST_DIR}"
