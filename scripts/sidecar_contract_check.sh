#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT_DIR}"

node <<'NODE'
const fs = require('fs')
const path = require('path')

const read = (file) => fs.readFileSync(file, 'utf8')

function normalize(route) {
  return route
    .replace(/\{[^}]+\}/g, ':param')
    .replace(/\+[A-Za-z0-9_]+/g, ':param')
    .replace(/:[A-Za-z_][A-Za-z0-9_]*/g, ':param')
}

function walk(dir, out = []) {
  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    if (entry.name === 'novel-downloader' || entry.name === '__pycache__') {
      continue
    }
    const full = path.join(dir, entry.name)
    if (entry.isDirectory()) {
      walk(full, out)
    } else if (entry.name.endsWith('.py')) {
      out.push(full)
    }
  }
  return out
}

const pythonRoutes = new Set()
for (const file of walk('python-sidecar')) {
  const src = read(file)
  const prefixMatch = src.match(/APIRouter\(\s*prefix="([^"]+)"/)
  const routerPrefix = prefixMatch ? prefixMatch[1] : ''
  let match
  const re = /@(app|router)\.(get|post|put|delete)\("([^"]+)"/g
  while ((match = re.exec(src))) {
    const route = match[1] === 'router'
      ? `${routerPrefix}${match[3]}`
      : match[3]
    pythonRoutes.add(`${match[2].toUpperCase()} ${normalize(route)}`)
  }
}

const goFiles = [
  ...fs.readdirSync('backend/internal/services')
    .filter((name) => name.endsWith('.go') && !name.endsWith('_test.go'))
    .map((name) => path.join('backend/internal/services', name)),
  'backend/cmd/server/main.go',
  'backend/internal/handlers/handler_fanqie.go',
  'backend/internal/handlers/handler_references.go',
]

const goCalls = new Set()
for (const file of goFiles) {
  const src = read(file)
  let match
  const serviceCall = /s\.(get|post)\(ctx,\s*"([^"]+)"\s*(\+)?/g
  while ((match = serviceCall.exec(src))) {
    const route = match[3] ? `${match[2]}:param` : match[2]
    goCalls.add(`${match[1].toUpperCase()} ${normalize(route)}`)
  }
  const directHTTP = /http\.NewRequestWithContext\([^,]+,\s*http\.Method(Get|Post|Put|Delete),\s*[^,\n]*\+\s*"([^"]+)"\s*(\+)?/g
  while ((match = directHTTP.exec(src))) {
    const route = match[3] ? `${match[2]}:param` : match[2]
    goCalls.add(`${match[1].toUpperCase()} ${normalize(route)}`)
  }
}

const missing = [...goCalls]
  .filter((call) => !pythonRoutes.has(call))
  .sort()

if (missing.length > 0) {
  console.error('Missing Python sidecar routes for Go calls:')
  for (const route of missing) {
    console.error(`  ${route}`)
  }
  process.exit(1)
}

console.log(`Sidecar contract OK (${goCalls.size} Go calls, ${pythonRoutes.size} Python routes)`)
NODE
