#!/bin/bash
set -euo pipefail

export APP_PROFILE="${APP_PROFILE:-no-neo4j}"
export DB_DRIVER="${DB_DRIVER:-postgres}"
export DB_HOST="${DB_HOST:-127.0.0.1}"
export DB_PORT="${DB_PORT:-5432}"
export DB_USER="${DB_USER:-novelbuilder}"
export DB_PASSWORD="${DB_PASSWORD:-}"
export DB_NAME="${DB_NAME:-novelbuilder}"
export REDIS_ADDR="${REDIS_ADDR:-127.0.0.1:6379}"
export NEO4J_URI="${NEO4J_URI:-}"

if [ -z "${DB_PASSWORD:-}" ]; then
    echo "ERROR: DB_PASSWORD must be set to a strong value before starting this Docker profile." >&2
    exit 64
fi

PGDATA="/var/lib/postgresql/data"
PG_BIN="/usr/lib/postgresql/16/bin"

if [ ! -d "$PGDATA/base" ]; then
    echo "==> Initialising PostgreSQL 16..."
    mkdir -p "$PGDATA"
    chown -R postgres:postgres "$PGDATA"
    chmod 700 "$PGDATA"
    gosu postgres "$PG_BIN/initdb" -D "$PGDATA"

    cat >> "$PGDATA/pg_hba.conf" <<'HBA'
host  all  all  127.0.0.1/32  trust
local all  all               trust
HBA

    gosu postgres "$PG_BIN/pg_ctl" -D "$PGDATA" -l /tmp/pg_init.log start -w
    gosu postgres psql -v ON_ERROR_STOP=1 \
        -v db_user="$DB_USER" \
        -v db_password="$DB_PASSWORD" \
        -v db_name="$DB_NAME" <<'SQL'
CREATE USER :"db_user" WITH PASSWORD :'db_password';
CREATE DATABASE :"db_name" OWNER :"db_user";
ALTER USER :"db_user" CREATEDB;
SQL
    gosu postgres psql -v ON_ERROR_STOP=1 -d "$DB_NAME" -c 'CREATE EXTENSION IF NOT EXISTS vector;'
    gosu postgres psql -v ON_ERROR_STOP=1 -d "$DB_NAME" -c 'CREATE EXTENSION IF NOT EXISTS "uuid-ossp";'
    gosu postgres "$PG_BIN/pg_ctl" -D "$PGDATA" stop -w
    echo "==> PostgreSQL ready. Go backend will run GORM AutoMigrate."
else
    echo "==> Existing PostgreSQL data found. Ensuring PostgreSQL extensions..."
    gosu postgres "$PG_BIN/pg_ctl" -D "$PGDATA" -l /tmp/pg_extensions.log start -w
    gosu postgres psql -v ON_ERROR_STOP=1 -d "$DB_NAME" -c 'CREATE EXTENSION IF NOT EXISTS vector;' 2>/dev/null || true
    gosu postgres psql -v ON_ERROR_STOP=1 -d "$DB_NAME" -c 'CREATE EXTENSION IF NOT EXISTS "uuid-ossp";' 2>/dev/null || true
    gosu postgres "$PG_BIN/pg_ctl" -D "$PGDATA" stop -w
fi

QDRANT_STORAGE="/var/lib/qdrant/storage"
if [ ! -d "$QDRANT_STORAGE" ]; then
    echo "==> Creating Qdrant storage directory..."
    mkdir -p "$QDRANT_STORAGE"
    chmod 755 /var/lib/qdrant "$QDRANT_STORAGE"
fi

if [ ! -x /usr/local/bin/qdrant ] || [ -d /usr/local/bin/qdrant ]; then
    echo "ERROR: /usr/local/bin/qdrant is missing or not executable. Rebuild the image." >&2
    exit 1
elif ! /usr/local/bin/qdrant --version >/dev/null 2>&1; then
    echo "WARNING: qdrant binary exists but failed --version (runtime dependency missing?)." >&2
fi

mkdir -p /var/lib/redis
chown redis:redis /var/lib/redis 2>/dev/null || chmod 777 /var/lib/redis

for helper in /app/docker/wait-for-port.sh /app/docker/wait-for-pg.sh; do
    if [ ! -f "$helper" ]; then
        echo "ERROR: required helper script missing: $helper" >&2
        exit 1
    fi
    chmod +x "$helper" 2>/dev/null || true
done

echo "==> Starting no-neo4j services via supervisord..."
exec /usr/bin/supervisord -c /etc/supervisor/conf.d/supervisord.conf
