#!/bin/bash
set -euo pipefail

export APP_PROFILE="${APP_PROFILE:-standard}"
export DB_DRIVER="${DB_DRIVER:-postgres}"
export DB_HOST="${DB_HOST:-127.0.0.1}"
export DB_PORT="${DB_PORT:-5432}"
export DB_USER="${DB_USER:-novelbuilder}"
export DB_PASSWORD="${DB_PASSWORD:-novelbuilder}"
export DB_NAME="${DB_NAME:-novelbuilder}"
export REDIS_ADDR="${REDIS_ADDR:-127.0.0.1:6379}"

PGDATA="/var/lib/postgresql/data"
PG_BIN="/usr/lib/postgresql/16/bin"

if [ ! -d "$PGDATA/base" ]; then
    echo "==> Initialising PostgreSQL 16..."
    mkdir -p "$PGDATA"
    chown -R postgres:postgres "$PGDATA"
    chmod 700 "$PGDATA"
    su - postgres -c "$PG_BIN/initdb -D $PGDATA"

    cat >> "$PGDATA/pg_hba.conf" <<'HBA'
host  all  all  127.0.0.1/32  trust
local all  all               trust
HBA

    su - postgres -c "$PG_BIN/pg_ctl -D $PGDATA -l /tmp/pg_init.log start -w"
    su - postgres -c "psql -c \"CREATE USER $DB_USER WITH PASSWORD '$DB_PASSWORD';\""
    su - postgres -c "psql -c \"CREATE DATABASE $DB_NAME OWNER $DB_USER;\""
    su - postgres -c "psql -c \"ALTER USER $DB_USER CREATEDB;\""
    su - postgres -c "psql -d $DB_NAME -c 'CREATE EXTENSION IF NOT EXISTS vector;'"
    su - postgres -c "psql -d $DB_NAME -c 'CREATE EXTENSION IF NOT EXISTS \"uuid-ossp\";'"
    su - postgres -c "$PG_BIN/pg_ctl -D $PGDATA stop -w"
    echo "==> PostgreSQL ready. Go backend will run GORM AutoMigrate."
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

echo "==> Starting standard services via supervisord..."
exec /usr/bin/supervisord -c /etc/supervisor/conf.d/supervisord.conf
