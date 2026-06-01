# Modernization TODO

This checklist is intentionally kept in the repository so future changes can be audited against the current architecture rather than reintroducing old dual-track designs.

- [x] Replace manual schema bootstrap with GORM AutoMigrate for fresh databases.
- [x] Synchronize runtime defaults and runtime snapshots into `system_settings`.
- [x] Add deployment profiles for full, standard, app-only, no-Neo4j, no-Qdrant, no-graph-vector, no-Redis, and SQLite.
- [x] Add multi-container compose deployment.
- [x] Add Linux/macOS/Windows binary packaging scripts and local run scripts.
- [x] Add `/setup` initialization page and `/api/setup/status`.
- [x] Add CPU/GPU/NPU capability detection in the Python sidecar.
- [x] Add explicit Redis-disabled session fallback.
- [x] Add Qdrant/Neo4j disabled guards in sidecar routes and agent retrieval nodes.
- [x] Add task lifecycle logs and panic recovery.
- [x] Split the first schema file that crossed 1k lines without changing behavior.
- [x] Remove application-level database pool coupling from service constructors and route all database access through the GORM-backed runtime database.
- [x] Enable SQLite runtime for the minimal Docker and binary profiles.
- [ ] Continue replacing legacy SQL compatibility calls with small GORM repository methods where it reduces maintenance cost.
- [ ] Split remaining near-1k files by feature module while preserving behavior.
- [ ] Add integration tests for every Docker profile with disabled dependencies.
