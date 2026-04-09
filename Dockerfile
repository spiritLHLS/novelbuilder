# ===========================================================
# NovelBuilder — all-in-one Docker image
#
# Services inside the container
#   PostgreSQL 16  :5432   — relational + pgvector
#   Redis 7        :6379   — short-term RecurrentGPT memory
#   Neo4j 5 CE     :7687   — knowledge graph (Graphiti/LangGraph)
#   Qdrant 1.12    :6333   — vector store (hybrid retrieval)
#   Python sidecar :8081   — LangGraph agent + FastAPI
#   Go backend     :8080   — Gin API gateway + Vue static
# ===========================================================

# ── Stage 1: Go backend ─────────────────────────────────
FROM golang:1.22-alpine AS go-builder

RUN apk add --no-cache git gcc musl-dev

WORKDIR /build/backend
COPY backend/go.mod backend/go.sum ./
RUN go mod download

COPY backend/ ./
RUN CGO_ENABLED=0 GOOS=linux go build -o /build/server ./cmd/server

# ── Stage 2: Vue frontend ────────────────────────────────
FROM node:20-alpine AS vue-builder

WORKDIR /build/frontend
COPY frontend/package.json frontend/package-lock.json* ./
RUN npm install --legacy-peer-deps

COPY frontend/ ./
RUN npm run build

# ── Stage 3: Pull Qdrant binary ──────────────────────────
FROM qdrant/qdrant:v1.12.0 AS qdrant-source

# ── Stage 4: Pull Neo4j distribution ────────────────────
FROM neo4j:5.24-community AS neo4j-source

# ── Stage 5: Pull Java 21 JRE ─────────────────────────────
# eclipse-temurin:21-jre is glibc-based (Ubuntu Jammy) and compatible with
# Debian bookworm.  Copying the JRE avoids depending on openjdk-21 being
# present in the Debian apt repos of the runtime base image.
FROM eclipse-temurin:21-jre AS jre-source

# ── Stage 6: Runtime ─────────────────────────────────────
# novel-downloader>=3.1.0 requires Python>=3.11.  Python 3.11 narrowed
# importlib.resources MultiplexedPath.joinpath() from variadic (*descendants)
# to a single positional arg, which breaks novel-downloader at import time.
# The runtime patch in python-sidecar/main.py restores the variadic form
# before novel-downloader is first imported.
FROM python:3.11.11-slim

# ---- System packages ----
RUN apt-get update && apt-get install -y --no-install-recommends \
    # PostgreSQL 16
    gnupg curl lsb-release ca-certificates \
    && install -d /usr/share/postgresql-common/pgdg \
    && curl -fsSL https://www.postgresql.org/media/keys/ACCC4CF8.asc \
       -o /usr/share/postgresql-common/pgdg/apt.postgresql.org.asc \
    && echo "deb [signed-by=/usr/share/postgresql-common/pgdg/apt.postgresql.org.asc] \
       https://apt.postgresql.org/pub/repos/apt $(lsb_release -cs)-pgdg main" \
       > /etc/apt/sources.list.d/pgdg.list \
    && apt-get update && apt-get install -y --no-install-recommends \
       postgresql-16 postgresql-16-pgvector postgresql-client-16 \
       redis-server \
       supervisor \
       # Qdrant runtime deps (binary is glibc-based)
       libunwind8 libgcc-s1 \
       # libgomp needed by Qdrant HNSW index builds
       libgomp1 \
       # netcat for wait-for-port.sh TCP readiness checks
       netcat-openbsd \
       # gosu for privilege dropping
       gosu \
    && apt-get clean && rm -rf /var/lib/apt/lists/*

# ---- Java 21 (for Neo4j) ----
# Copied from eclipse-temurin:21-jre to avoid relying on Debian apt repos
# that may not carry openjdk-21 for every bookworm mirror.
COPY --from=jre-source /opt/java/openjdk /opt/java/openjdk
ENV JAVA_HOME=/opt/java/openjdk
ENV PATH="${JAVA_HOME}/bin:${PATH}"

# ---- Neo4j (copy from neo4j-source image) ----
ENV NEO4J_HOME=/opt/neo4j
COPY --from=neo4j-source /var/lib/neo4j ${NEO4J_HOME}
# Neo4j needs its bin dir on PATH
ENV PATH="${NEO4J_HOME}/bin:${PATH}"

# Create neo4j user and fix permissions.
# The neo4j source image ships data/logs/import as symlinks that would
# break mkdir-p; remove them unconditionally and recreate as real dirs.
RUN if ! getent group neo4j >/dev/null; then groupadd -r neo4j; fi \
    && if ! id -u neo4j >/dev/null 2>&1; then useradd -r -g neo4j -d ${NEO4J_HOME} neo4j; fi \
    && rm -rf ${NEO4J_HOME}/data ${NEO4J_HOME}/logs ${NEO4J_HOME}/run ${NEO4J_HOME}/import \
    && mkdir -p ${NEO4J_HOME}/data ${NEO4J_HOME}/logs ${NEO4J_HOME}/run ${NEO4J_HOME}/import \
    && chown -R neo4j:neo4j ${NEO4J_HOME}

# ---- Qdrant (copy binary) ----
# In the official qdrant image the executable is /qdrant/qdrant (inside a /qdrant dir)
COPY --from=qdrant-source /qdrant/qdrant /usr/local/bin/qdrant
RUN chmod +x /usr/local/bin/qdrant
# Qdrant data directory
RUN mkdir -p /var/lib/qdrant && chmod 755 /var/lib/qdrant

# ---- Python sidecar ----
WORKDIR /app/python-sidecar
COPY python-sidecar/requirements.txt ./
# Copy novel-downloader submodule source (populated when cloned with --recurse-submodules)
COPY python-sidecar/novel-downloader ./novel-downloader
# Install CPU-only torch first (keeps image smaller)
RUN pip install --no-cache-dir \
    torch==2.5.1+cpu \
    --index-url https://download.pytorch.org/whl/cpu
RUN pip install --no-cache-dir -r requirements.txt
# Install Playwright and Chromium browser for Fanqie auto-upload
RUN pip install --no-cache-dir playwright>=1.40.0 \
    && playwright install --with-deps chromium
# Install novel-downloader: prefer local submodule copy; fall back to GitHub if submodule
# was not initialised (i.e. the directory is empty after a shallow clone).
RUN if [ -f "./novel-downloader/pyproject.toml" ]; then \
        pip install --no-cache-dir ./novel-downloader; \
    else \
        echo "WARNING: novel-downloader submodule is empty; downloading from GitHub..." \
        && curl -fsSL https://github.com/spiritLHLS/novel-downloader/archive/refs/heads/main.tar.gz \
           | tar -xz -C /tmp \
        && pip install --no-cache-dir /tmp/novel-downloader-main \
        && rm -rf /tmp/novel-downloader-main; \
    fi
COPY python-sidecar/ ./

# ---- Go backend ----
COPY --from=go-builder /build/server /app/server

# ---- Vue frontend ----
COPY --from=vue-builder /build/frontend/dist /app/frontend/dist

# ---- Migrations only (configs/config.yaml is no longer needed) ----
COPY migrations/ /app/migrations/

# ---- Supervisord ----
COPY docker/supervisord.conf /etc/supervisor/conf.d/supervisord.conf

# ---- Helper scripts ----
COPY docker/wait-for-pg.sh /app/docker/wait-for-pg.sh
RUN chmod +x /app/docker/wait-for-pg.sh

COPY docker/wait-for-port.sh /app/docker/wait-for-port.sh
RUN chmod +x /app/docker/wait-for-port.sh

# ---- Entrypoint ----
COPY docker/docker-entrypoint.sh /app/docker-entrypoint.sh
RUN chmod +x /app/docker-entrypoint.sh

# Healthcheck (Go gateway)
HEALTHCHECK --interval=30s --timeout=10s --start-period=120s --retries=5 \
    CMD curl -f http://localhost:8080/api/health || exit 1

EXPOSE 8080

VOLUME ["/var/lib/postgresql/data", "/var/lib/qdrant", "/opt/neo4j/data"]

WORKDIR /app
ENTRYPOINT ["/app/docker-entrypoint.sh"]
