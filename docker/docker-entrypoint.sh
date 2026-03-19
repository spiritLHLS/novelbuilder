#!/bin/bash
set -e

# ============================================================
# NovelBuilder docker-entrypoint.sh
# Initialises: PostgreSQL, Neo4j, Qdrant, Redis, then hands
# off to supervisord which starts all processes.
# ============================================================

# ── Defaults ─────────────────────────────────────────────
export DB_HOST="${DB_HOST:-127.0.0.1}"
export DB_PORT="${DB_PORT:-5432}"
export DB_USER="${DB_USER:-novelbuilder}"
export DB_PASSWORD="${DB_PASSWORD:-novelbuilder}"
export DB_NAME="${DB_NAME:-novelbuilder}"
export REDIS_ADDR="${REDIS_ADDR:-127.0.0.1:6379}"
export NEO4J_USER="${NEO4J_USER:-neo4j}"
export NEO4J_PASSWORD="${NEO4J_PASSWORD:-novelbuilder}"

# ── PostgreSQL init ──────────────────────────────────────
PGDATA="/var/lib/postgresql/data"
PG_BIN="/usr/lib/postgresql/16/bin"

if [ ! -d "$PGDATA/base" ]; then
    echo "==> Initialising PostgreSQL 16..."
    mkdir -p "$PGDATA"
    chown -R postgres:postgres "$PGDATA"
    chmod 700 "$PGDATA"
    su - postgres -c "$PG_BIN/initdb -D $PGDATA"

    # Trust local connections
    cat >> "$PGDATA/pg_hba.conf" <<'HBA'
host  all  all  127.0.0.1/32  trust
local all  all               trust
HBA

    # Bring up temporarily for initial setup
    su - postgres -c "$PG_BIN/pg_ctl -D $PGDATA -l /tmp/pg_init.log start -w"

    su - postgres -c "psql -c \"CREATE USER $DB_USER WITH PASSWORD '$DB_PASSWORD';\""
    su - postgres -c "psql -c \"CREATE DATABASE $DB_NAME OWNER $DB_USER;\""
    su - postgres -c "psql -c \"ALTER USER $DB_USER CREATEDB;\""

    su - postgres -c "psql -d $DB_NAME -c 'CREATE EXTENSION IF NOT EXISTS vector;'"
    su - postgres -c "psql -d $DB_NAME -c 'CREATE EXTENSION IF NOT EXISTS \"uuid-ossp\";'"

    echo "==> Running SQL migrations..."
    for f in /app/migrations/*.sql; do
        echo "   applying $f"
        su - postgres -c "psql -d $DB_NAME -f $f"
    done

    su - postgres -c "$PG_BIN/pg_ctl -D $PGDATA stop -w"
    echo "==> PostgreSQL ready."
fi

# ── Neo4j init ───────────────────────────────────────────
NEO4J_DATA="${NEO4J_HOME}/data"

if [ ! -d "$NEO4J_DATA/databases/neo4j" ]; then
    echo "==> Initialising Neo4j 5..."
    mkdir -p "${NEO4J_HOME}/data" "${NEO4J_HOME}/logs" "${NEO4J_HOME}/run" "${NEO4J_HOME}/import"
    chown -R neo4j:neo4j "${NEO4J_HOME}"

    # Set initial password via neo4j-admin (Neo4j 5.x syntax)
    su - neo4j -s /bin/bash -c "/opt/neo4j/bin/neo4j-admin dbms set-initial-password '${NEO4J_PASSWORD}'" 2>/dev/null || \
        gosu neo4j /opt/neo4j/bin/neo4j-admin dbms set-initial-password "${NEO4J_PASSWORD}" 2>/dev/null || true

    echo "==> Neo4j password set."
fi

# ── Qdrant init ──────────────────────────────────────────
QDRANT_STORAGE="/var/lib/qdrant/storage"
if [ ! -d "$QDRANT_STORAGE" ]; then
    echo "==> Creating Qdrant storage directory..."
    mkdir -p "$QDRANT_STORAGE"
    chmod 755 /var/lib/qdrant "$QDRANT_STORAGE"
fi

# ── Redis data dir ────────────────────────────────────────
mkdir -p /var/lib/redis
chown redis:redis /var/lib/redis 2>/dev/null || chmod 777 /var/lib/redis

# ── Docker helper scripts dir ───────────────────────────
mkdir -p /app/docker

# ── Wait helper (used post-startup if needed) ─────────────
wait_for_port() {
    local host="$1" port="$2" name="$3" retries="${4:-30}"
    echo "==> Waiting for $name on $host:$port..."
    for i in $(seq 1 $retries); do
        if nc -z "$host" "$port" 2>/dev/null; then
            echo "   $name is up."
            return 0
        fi
        sleep 2
    done
    echo "WARNING: $name did not become available in time." >&2
    return 0   # non-fatal — supervisord will restart the process
}

# ── Start all services via supervisord ────────────────────
echo "==> Starting all services via supervisord..."
exec /usr/bin/supervisord -c /etc/supervisor/conf.d/supervisord.conf

