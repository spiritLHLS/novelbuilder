#!/bin/bash
# Wait for PostgreSQL to be ready before starting the application.
# Usage: wait-for-pg.sh <command> [args...]

HOST="${DB_HOST:-127.0.0.1}"
PORT="${DB_PORT:-5432}"
USER="${DB_USER:-novelbuilder}"
RETRIES="${PG_WAIT_RETRIES:-60}"
SLEEP="${PG_WAIT_SLEEP:-2}"

echo "==> Waiting for PostgreSQL at ${HOST}:${PORT}..."
for i in $(seq 1 "${RETRIES}"); do
    if pg_isready -h "${HOST}" -p "${PORT}" -U "${USER}" -q 2>/dev/null; then
        echo "==> PostgreSQL is ready (attempt ${i})."
        exec "$@"
    fi
    sleep "${SLEEP}"
done

echo "ERROR: PostgreSQL did not become ready after $((RETRIES * SLEEP))s" >&2
exit 1
