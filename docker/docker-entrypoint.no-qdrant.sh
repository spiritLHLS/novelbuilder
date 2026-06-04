#!/bin/bash
set -euo pipefail

export APP_PROFILE="${APP_PROFILE:-no-qdrant}"
export DB_DRIVER="${DB_DRIVER:-postgres}"
export DB_HOST="${DB_HOST:-127.0.0.1}"
export DB_PORT="${DB_PORT:-5432}"
export DB_USER="${DB_USER:-novelbuilder}"
export DB_PASSWORD="${DB_PASSWORD:-}"
export DB_NAME="${DB_NAME:-novelbuilder}"
export REDIS_ADDR="${REDIS_ADDR:-127.0.0.1:6379}"
export NEO4J_USER="${NEO4J_USER:-neo4j}"
export NEO4J_PASSWORD="${NEO4J_PASSWORD:-}"
export QDRANT_URL="${QDRANT_URL:-}"

if [ -z "${DB_PASSWORD:-}" ]; then
    echo "ERROR: DB_PASSWORD must be set to a strong value before starting this Docker profile." >&2
    exit 64
fi
if [ -z "${NEO4J_PASSWORD:-}" ]; then
    echo "ERROR: NEO4J_PASSWORD must be set to a strong value before starting this Docker profile." >&2
    exit 64
fi

for d in "${NEO4J_HOME}/data" "${NEO4J_HOME}/logs" "${NEO4J_HOME}/run" "${NEO4J_HOME}/import"; do
    if [ -L "$d" ] || { [ -e "$d" ] && [ ! -d "$d" ]; }; then
        rm -rf "$d"
    fi
    mkdir -p "$d"
done
chown -R neo4j:neo4j "${NEO4J_HOME}/data" "${NEO4J_HOME}/logs" "${NEO4J_HOME}/run" "${NEO4J_HOME}/import" 2>/dev/null || true

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

NEO4J_DATA="${NEO4J_HOME}/data"
if [ ! -d "$NEO4J_DATA/databases/neo4j" ]; then
    echo "==> Initialising Neo4j 5..."
    chown -R neo4j:neo4j "${NEO4J_HOME}" 2>/dev/null || true
    if ! gosu neo4j /opt/neo4j/bin/neo4j-admin dbms set-initial-password "${NEO4J_PASSWORD}" 2>/dev/null; then
        echo "ERROR: failed to set initial Neo4j password." >&2
        exit 1
    fi
    echo "==> Neo4j password set."
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

echo "==> Starting no-qdrant services via supervisord..."
exec /usr/bin/supervisord -c /etc/supervisor/conf.d/supervisord.conf
