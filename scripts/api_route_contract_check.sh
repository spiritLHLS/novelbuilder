#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT_DIR}"

node <<'NODE'
const fs = require('fs')

const read = (path) => fs.readFileSync(path, 'utf8')

function normalize(path) {
  return path
    .replace(/\?.*$/, '')
    .replace(/\$\{[^}]+\}/g, ':param')
    .replace(/:[A-Za-z_][A-Za-z0-9_]*/g, ':param')
    .replace(/\/+/g, '/')
}

function addRoute(routes, method, path) {
  routes.add(`${method.toUpperCase()} ${normalize(path)}`)
}

const backendRoutes = new Set()
const frontendCalls = []

for (const file of ['backend/internal/handlers/handlers.go', 'backend/cmd/server/main.go']) {
  const src = read(file)
  let match
  const groupRoute = /api\.(GET|POST|PUT|DELETE|PATCH)\("([^"]+)"/g
  while ((match = groupRoute.exec(src))) {
    addRoute(backendRoutes, match[1], `/api${match[2]}`)
  }
  const rootRoute = /r\.(GET|POST|PUT|DELETE|PATCH)\("([^"]+)"/g
  while ((match = rootRoute.exec(src))) {
    if (match[2].startsWith('/api/')) {
      addRoute(backendRoutes, match[1], match[2])
    }
  }
}

const frontend = read('frontend/src/api/index.ts')
let match
const callRe = /api\.(get|post|put|delete|patch)(?:<[^>]+>)?\(\s*([`'"])(.*?)\2/gs
while ((match = callRe.exec(frontend))) {
  const method = match[1].toUpperCase()
  const raw = match[3]
  if (!raw.startsWith('/')) {
    continue
  }
  if (/\$\{[^}]*\?/.test(raw)) {
    frontendCalls.push(`${method} ${normalize(`/api${raw.split('${')[0]}`)}`)
    continue
  }
  frontendCalls.push(`${method} ${normalize(`/api${raw}`)}`)
}

const conditionalCallRe = /api\.(get|post|put|delete|patch)(?:<[^>]+>)?\(\s*[^?,()]+\?\s*([`'"])(.*?)\2\s*:\s*([`'"])(.*?)\4/gs
while ((match = conditionalCallRe.exec(frontend))) {
  const method = match[1].toUpperCase()
  for (const raw of [match[3], match[5]]) {
    if (raw.startsWith('/')) {
      frontendCalls.push(`${method} ${normalize(`/api${raw}`)}`)
    }
  }
}

const ignored = new Set([
  // Dynamic helper accepts both project-scoped and global forms; the project
  // branch is statically checked and the global branch is registered too.
])

const missing = [...new Set(frontendCalls)]
  .filter((call) => !backendRoutes.has(call) && !ignored.has(call))
  .sort()

if (missing.length > 0) {
  console.error('Missing backend routes for frontend API calls:')
  for (const route of missing) {
    console.error(`  ${route}`)
  }
  process.exit(1)
}

console.log(`API route contract OK (${frontendCalls.length} frontend calls, ${backendRoutes.size} backend routes)`)
NODE
