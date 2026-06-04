# ===========================================================
# NovelBuilder — app-only Docker image
#
# For multi-container deployments. Contains Go backend, Python sidecar and Vue
# assets only; PostgreSQL/Redis/Qdrant/Neo4j are external compose services.
# ===========================================================

FROM golang:1.22-alpine AS go-builder

RUN apk add --no-cache git gcc musl-dev
ARG SERVER_VERSION=dev
ARG UPX_ENABLED=false
WORKDIR /build/backend
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath \
      -ldflags "-s -w -buildid= -X main.version=${SERVER_VERSION}" \
      -o /build/server ./cmd/server \
    && if [ "${UPX_ENABLED}" = "true" ]; then \
      (apk add --no-cache upx && upx -9 /build/server) || true; \
    fi

FROM node:24-alpine AS vue-builder

WORKDIR /build/frontend
COPY frontend/package.json frontend/package-lock.json* ./
RUN npm ci --legacy-peer-deps
COPY frontend/ ./
RUN npm run build

FROM python:3.11.11-slim
ENV PIP_NO_CACHE_DIR=1 \
    PIP_DISABLE_PIP_VERSION_CHECK=1 \
    PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1

RUN apt-get update && apt-get install -y --no-install-recommends \
    curl netcat-openbsd supervisor \
    && apt-get clean && rm -rf /var/lib/apt/lists/*

WORKDIR /app/python-sidecar
COPY python-sidecar/requirements*.txt ./
COPY python-sidecar/novel-downloader ./novel-downloader
ARG TARGETARCH
ARG NB_ACCELERATOR=cpu
ARG TORCH_VERSION=2.12.0
ENV NB_ACCELERATOR=${NB_ACCELERATOR}
RUN case "${NB_ACCELERATOR}" in \
      cpu|auto) \
        if [ "$TARGETARCH" = "amd64" ]; then \
          pip install --no-cache-dir --no-compile "torch==${TORCH_VERSION}" --index-url https://download.pytorch.org/whl/cpu; \
        else \
          pip install --no-cache-dir --no-compile "torch==${TORCH_VERSION}"; \
        fi ;; \
      cuda|gpu) \
        if [ "$TARGETARCH" != "amd64" ]; then \
          echo "CUDA torch wheels are only supported for linux/amd64 in this image" >&2; exit 64; \
        fi; \
        pip install --no-cache-dir --no-compile "torch==${TORCH_VERSION}" ;; \
      *) echo "Unsupported NB_ACCELERATOR=${NB_ACCELERATOR}; use cpu or cuda" >&2; exit 64 ;; \
    esac
RUN pip install --no-cache-dir --no-compile \
        -r requirements-base.txt \
        -r requirements-browser.txt \
        -r requirements-graph.txt \
        -r requirements-vector.txt
RUN python -m playwright install --with-deps chromium \
    && (python -m camoufox fetch || echo "WARNING: Camoufox fetch failed; Playwright Chromium fallback remains available") \
    && rm -rf /var/lib/apt/lists/*
RUN if [ -f "./novel-downloader/pyproject.toml" ]; then \
        pip install --no-cache-dir --no-compile ./novel-downloader; \
    fi
COPY python-sidecar/ ./

COPY --from=go-builder /build/server /app/server
COPY --from=vue-builder /build/frontend/dist /app/frontend/dist
COPY docker/supervisord.app.conf /etc/supervisor/conf.d/supervisord.conf
COPY docker/wait-for-port.sh /app/docker/wait-for-port.sh
RUN chmod +x /app/docker/wait-for-port.sh

ENV APP_PROFILE=multi
ENV DB_DRIVER=postgres

HEALTHCHECK --interval=30s --timeout=10s --start-period=90s --retries=5 \
    CMD curl -f http://localhost:8080/api/health || exit 1

EXPOSE 8080
WORKDIR /app
CMD ["/usr/bin/supervisord", "-c", "/etc/supervisor/conf.d/supervisord.conf"]
