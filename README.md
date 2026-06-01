# NovelBuilder

[中文说明](README.zh-CN.md)

NovelBuilder is an AI long-form fiction workbench.
## Architecture

```text
Vue SPA
  -> Go API Gateway
     -> GORM schema bootstrap and PostgreSQL/SQLite data path
     -> task queue, auth, LLM profile routing, quality gates
  -> Python Sidecar
     -> reference analysis, LangGraph agents, audit, humanization, upload automation
  -> PostgreSQL/SQLite, Redis, Qdrant, Neo4j
```

## Deployment Profiles

| Tag | Dockerfile | Dependencies | Suggested resources | Use case |
| --- | --- | --- | --- | --- |
| `latest`, `full`, `YYYYMMDD` | `Dockerfile` | Go, Python, Vue, PostgreSQL, Redis, Qdrant, Neo4j, Playwright | 4 CPU, 8 GB RAM, 20 GB disk | Complete local all-in-one deployment |
| `standard`, `YYYYMMDD-standard` | `Dockerfile.standard` | Go, Python, Vue, PostgreSQL, Redis | 2 CPU, 4 GB RAM, 10 GB disk | Daily writing and review without graph/vector services |
| `app`, `YYYYMMDD-app` | `Dockerfile.app` | Go, Python, Vue only | 2 CPU, 2 GB RAM plus external services | Multi-container compose or managed databases |
| `no-neo4j` | `Dockerfile.no-neo4j` | Full minus Neo4j | 3 CPU, 6 GB RAM | Keep vector search, disable graph memory |
| `no-qdrant` | `Dockerfile.no-qdrant` | Full minus Qdrant | 3 CPU, 6 GB RAM | Keep graph memory, disable vector search |
| `no-graph-vector` | `Dockerfile.no-graph-vector` | PostgreSQL and Redis only | 2 CPU, 4 GB RAM | Text generation with deterministic context only |
| `no-redis` | `Dockerfile.no-redis` | PostgreSQL only | 2 CPU, 3 GB RAM | Single-user degraded mode with in-process sessions |
| `sqlite` | `Dockerfile.sqlite` | Go, Python, Vue, SQLite | 1 CPU, 2 GB RAM, 5 GB disk | Minimal local/binary mode without external services |

## Quick Start

All-in-one full profile:

```bash
docker compose up -d
open http://127.0.0.1:8080/setup
```

Standard profile:

```bash
docker compose -f docker-compose.standard.yml up -d
```

Minimal SQLite profile:

```bash
docker compose -f docker-compose.sqlite.yml up -d
```

Multi-container profile:

```bash
docker compose -f docker-compose.multi.yml --profile full up -d
```

Bare-metal source install:

```bash
./scripts/install.sh
./scripts/run-local.sh
```

Windows source install:

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\install.ps1
powershell -ExecutionPolicy Bypass -File .\scripts\run-local.ps1
```

Binary packages are produced by:

```bash
VERSION=dev ./scripts/build-binaries.sh
```

Each package contains the Go backend binary, Vue `frontend/dist`, Python sidecar source, and `run-local` scripts. Binary mode defaults to SQLite at `./data/novelbuilder.db`; set `DB_DRIVER=postgres` plus `DB_HOST`, `DB_PORT`, `DB_USER`, `DB_PASSWORD`, and `DB_NAME` to use PostgreSQL. Redis, Qdrant, and Neo4j are optional.

## Configuration

Infrastructure settings come from environment variables:

| Variable | Default | Notes |
| --- | --- | --- |
| `APP_PROFILE` | `full` | Runtime profile name displayed in diagnostics |
| `DB_DRIVER` | `postgres` in containers, `sqlite` in binary scripts | `sqlite`/`sqlite3` or `postgres` |
| `SQLITE_PATH` | `/data/novelbuilder.db` or `./data/novelbuilder.db` | Used when `DB_DRIVER=sqlite` |
| `DB_HOST`, `DB_PORT`, `DB_USER`, `DB_PASSWORD`, `DB_NAME` | local PostgreSQL defaults | Used when `DB_DRIVER=postgres` |
| `REDIS_ENABLED` | `true` | Set `false` for in-process sessions and degraded cache behavior |
| `REDIS_ADDR`, `REDIS_URL` | local Redis defaults | Go uses `REDIS_ADDR`; Python uses `REDIS_URL` |
| `NEO4J_URI` | profile-specific | Empty disables graph services |
| `QDRANT_URL` | profile-specific | Empty disables vector services |
| `NB_ACCELERATOR` | `auto` | `auto`, `cpu`, `cuda`, `rocm`, or `npu` |

Application settings, LLM profiles, prompt presets, and runtime snapshots live in `system_settings` and related database tables.

## Development Checks

```bash
cd backend && go test ./...
cd python-sidecar && python3 -m py_compile main.py routes_audit.py routes_analysis.py runtime_capabilities.py
cd frontend && npm run build
```
