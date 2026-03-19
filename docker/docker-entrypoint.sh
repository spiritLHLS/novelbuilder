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

# Ensure Neo4j runtime directories exist as real directories.
# Symlinks (or files) from the image layer are removed first so mkdir never fails.
for d in "${NEO4J_HOME}/data" "${NEO4J_HOME}/logs" "${NEO4J_HOME}/run" "${NEO4J_HOME}/import"; do
    if [ -L "$d" ] || { [ -e "$d" ] && [ ! -d "$d" ]; }; then
        rm -rf "$d"
    fi
    mkdir -p "$d"
done
chown -R neo4j:neo4j "${NEO4J_HOME}/data" "${NEO4J_HOME}/logs" "${NEO4J_HOME}/run" "${NEO4J_HOME}/import" 2>/dev/null || true

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

    # Grant the app user full access to every table/sequence created by migrations.
    # Migrations run as postgres (owner), so novelbuilder has no privileges by default.
    su - postgres -c "psql -d $DB_NAME -c \"
        GRANT ALL PRIVILEGES ON ALL TABLES    IN SCHEMA public TO $DB_USER;
        GRANT ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA public TO $DB_USER;
        ALTER DEFAULT PRIVILEGES IN SCHEMA public
            GRANT ALL ON TABLES    TO $DB_USER;
        ALTER DEFAULT PRIVILEGES IN SCHEMA public
            GRANT ALL ON SEQUENCES TO $DB_USER;
    \""

    su - postgres -c "$PG_BIN/pg_ctl -D $PGDATA stop -w"
    echo "==> PostgreSQL ready."
fi

# ── Neo4j init ───────────────────────────────────────────
NEO4J_DATA="${NEO4J_HOME}/data"

if [ ! -d "$NEO4J_DATA/databases/neo4j" ]; then
    echo "==> Initialising Neo4j 5..."
    chown -R neo4j:neo4j "${NEO4J_HOME}" 2>/dev/null || true

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

# Preflight: verify qdrant binary is executable.
if [ ! -x /usr/local/bin/qdrant ] || [ -d /usr/local/bin/qdrant ]; then
    echo "ERROR: /usr/local/bin/qdrant is missing or not executable. Rebuild the image." >&2
elif ! /usr/local/bin/qdrant --version >/dev/null 2>&1; then
    echo "WARNING: qdrant binary exists but failed --version (runtime dep missing?)." >&2
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

