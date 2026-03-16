# ==== Stage 1: Build Go Backend ====
FROM golang:1.22-alpine AS go-builder

RUN apk add --no-cache git gcc musl-dev

WORKDIR /build/backend
COPY backend/go.mod backend/go.sum ./
RUN go mod download

COPY backend/ ./
RUN CGO_ENABLED=0 GOOS=linux go build -o /build/server ./cmd/server

# ==== Stage 2: Build Vue Frontend ====
FROM node:20-alpine AS vue-builder

WORKDIR /build/frontend
COPY frontend/package.json frontend/package-lock.json* ./
RUN npm install --legacy-peer-deps

COPY frontend/ ./
RUN npm run build

# ==== Stage 3: Runtime ====
FROM python:3.11-slim

# Install PostgreSQL, Redis, supervisord
RUN apt-get update && apt-get install -y --no-install-recommends \
    postgresql postgresql-contrib \
    redis-server \
    supervisor \
    curl \
    && rm -rf /var/lib/apt/lists/*

# Install pgvector extension
RUN apt-get update && apt-get install -y --no-install-recommends \
    build-essential postgresql-server-dev-all git \
    && git clone --branch v0.7.4 https://github.com/pgvector/pgvector.git /tmp/pgvector \
    && cd /tmp/pgvector && make && make install \
    && apt-get purge -y build-essential postgresql-server-dev-all git \
    && apt-get autoremove -y \
    && rm -rf /var/lib/apt/lists/* /tmp/pgvector

# Python sidecar
WORKDIR /app/python-sidecar
COPY python-sidecar/requirements.txt ./
RUN pip install --no-cache-dir -r requirements.txt
COPY python-sidecar/ ./

# Go backend binary
COPY --from=go-builder /build/server /app/server

# Vue frontend dist
COPY --from=vue-builder /build/frontend/dist /app/frontend/dist

# Config and migrations
COPY configs/ /app/configs/
COPY migrations/ /app/migrations/

# Supervisor config
COPY docker/supervisord.conf /etc/supervisor/conf.d/supervisord.conf

# Entrypoint
COPY docker/docker-entrypoint.sh /app/docker-entrypoint.sh
RUN chmod +x /app/docker-entrypoint.sh

# Healthcheck
HEALTHCHECK --interval=30s --timeout=10s --start-period=60s --retries=3 \
    CMD curl -f http://localhost:8080/api/health || exit 1

EXPOSE 8080

WORKDIR /app
ENTRYPOINT ["/app/docker-entrypoint.sh"]
