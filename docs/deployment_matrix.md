# Deployment Matrix

This file records the supported build tags, dependency mode, and runtime behavior.

| Profile | Tag | Dockerfile | PostgreSQL | Redis | Qdrant | Neo4j | Runtime behavior |
| --- | --- | --- | --- | --- | --- | --- | --- |
| full | `latest`, `full`, `YYYYMMDD` | `Dockerfile` | bundled | bundled | bundled | bundled | All features enabled |
| standard | `standard` | `Dockerfile.standard` | bundled | bundled | disabled | disabled | Graph/vector endpoints degrade with 503 |
| app | `app` | `Dockerfile.app` | external | external optional | external optional | external optional | Multi-container or managed services |
| no-neo4j | `no-neo4j` | `Dockerfile.no-neo4j` | bundled | bundled | bundled | disabled | Vector retrieval enabled, graph memory disabled |
| no-qdrant | `no-qdrant` | `Dockerfile.no-qdrant` | bundled | bundled | disabled | bundled | Graph memory enabled, vector retrieval disabled |
| no-graph-vector | `no-graph-vector` | `Dockerfile.no-graph-vector` | bundled | bundled | disabled | disabled | Deterministic context only |
| no-redis | `no-redis` | `Dockerfile.no-redis` | bundled | disabled | disabled | disabled | In-process session store, single-instance only |
| sqlite | `sqlite` | `Dockerfile.sqlite` | disabled | disabled | disabled | disabled | Minimal local state, SQLite file at `/data/novelbuilder.db` |

## Compose Modes

- `docker-compose.yml`: full single-container image with named volumes.
- `docker-compose.standard.yml`: standard single-container image without Neo4j/Qdrant.
- `docker-compose.sqlite.yml`: minimal SQLite image with only Go, Python, Vue, and local file storage.
- `docker-compose.multi.yml`: app container plus external PostgreSQL, Redis, Qdrant, and Neo4j services. Enable graph/vector services with compose profiles:

```bash
docker compose -f docker-compose.multi.yml --profile full up -d
docker compose -f docker-compose.multi.yml --profile vector up -d
docker compose -f docker-compose.multi.yml --profile graph up -d
```

## Binary Mode

The binary package contains:

- `novelbuilder` or `novelbuilder.exe`
- `frontend/dist`
- `python-sidecar`
- `run-local.sh` and `run-local.ps1`

Runtime scripts start the Python sidecar, report detected CPU/GPU/NPU capability, then start the Go API gateway. SQLite is the default binary database (`./data/novelbuilder.db`). Set `DB_DRIVER=postgres` and the `DB_*` connection variables when using PostgreSQL. Redis/Qdrant/Neo4j can be disabled by leaving their environment variables empty.
