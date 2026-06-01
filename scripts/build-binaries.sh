#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DIST_DIR="${DIST_DIR:-${ROOT_DIR}/dist/binaries}"
VERSION="${VERSION:-dev}"

mkdir -p "${DIST_DIR}"

echo "==> Building frontend"
(
  cd "${ROOT_DIR}/frontend"
  npm ci --legacy-peer-deps
  npm run build
)

compress_binary() {
  local path="$1"
  local goos="$2"
  if [[ "${goos}" == "darwin" ]]; then
    return 0
  fi
  case "${UPX_ENABLED:-auto}" in
    0|false|FALSE|off|OFF|no|NO)
      return 0
      ;;
  esac
  if command -v upx >/dev/null 2>&1; then
    echo "==> UPX compressing $(basename "${path}")"
    upx -9 --lzma "${path}" >/dev/null || echo "WARN: UPX failed for ${path}; keeping stripped binary"
  elif [[ "${UPX_ENABLED:-auto}" == "true" || "${UPX_ENABLED:-auto}" == "1" ]]; then
    echo "WARN: UPX_ENABLED=${UPX_ENABLED} but upx is not installed; keeping stripped binary" >&2
  fi
}

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
      go build -trimpath -ldflags "-s -w -buildid= -X main.version=${VERSION}" \
        -o "${out}/novelbuilder${ext}" ./cmd/server
  )
  compress_binary "${out}/novelbuilder${ext}" "${goos}"

  cp -R "${ROOT_DIR}/frontend/dist" "${out}/frontend/dist"
  rsync -a \
    --exclude '__pycache__' \
    --exclude '*.pyc' \
    --exclude '.pytest_cache' \
    --exclude '.mypy_cache' \
    --exclude '.ruff_cache' \
    --exclude '.venv' \
    --exclude 'node_modules' \
    --exclude 'novel-downloader/.git' \
    --exclude 'novel-downloader/docs' \
    --exclude 'novel-downloader/tests' \
    --exclude 'novel-downloader/.pytest_cache' \
    --exclude 'novel-downloader/.mypy_cache' \
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
