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

# ── Stage 5: Runtime ─────────────────────────────────────
FROM python:3.11-slim

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
       postgresql-16 postgresql-16-pgvector \
       redis-server \
       supervisor \
       # OpenJDK 21 for Neo4j
       openjdk-17-jre-headless \
    && apt-get clean && rm -rf /var/lib/apt/lists/*

# ---- Neo4j (copy from neo4j-source image) ----
ENV NEO4J_HOME=/opt/neo4j
COPY --from=neo4j-source /var/lib/neo4j ${NEO4J_HOME}
# Neo4j needs its bin dir on PATH
ENV PATH="${NEO4J_HOME}/bin:${PATH}"

# Create neo4j user
RUN groupadd -r neo4j && useradd -r -g neo4j -d ${NEO4J_HOME} neo4j \
    && chown -R neo4j:neo4j ${NEO4J_HOME}

# ---- Qdrant (copy binary + config) ----
COPY --from=qdrant-source /qdrant/qdrant /usr/local/bin/qdrant
RUN chmod +x /usr/local/bin/qdrant
# Qdrant data directory
RUN mkdir -p /var/lib/qdrant && chmod 755 /var/lib/qdrant

# ---- Python sidecar ----
WORKDIR /app/python-sidecar
COPY python-sidecar/requirements.txt ./
# Install CPU-only torch first (keeps image smaller)
RUN pip install --no-cache-dir \
    torch==2.5.1+cpu \
    --index-url https://download.pytorch.org/whl/cpu
RUN pip install --no-cache-dir -r requirements.txt
COPY python-sidecar/ ./

# ---- Go backend ----
COPY --from=go-builder /build/server /app/server

# ---- Vue frontend ----
COPY --from=vue-builder /build/frontend/dist /app/frontend/dist

# ---- Migrations only (configs/config.yaml is no longer needed) ----
COPY migrations/ /app/migrations/

# ---- Supervisord ----
COPY docker/supervisord.conf /etc/supervisor/conf.d/supervisord.conf

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
