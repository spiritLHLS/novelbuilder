#!/bin/bash
set -e

# ---- PostgreSQL Setup ----
PGDATA="/var/lib/postgresql/data"

if [ ! -d "$PGDATA/base" ]; then
    echo "==> Initializing PostgreSQL..."
    su - postgres -c "initdb -D $PGDATA"

    # Allow local connections
    echo "host all all 127.0.0.1/32 trust" >> "$PGDATA/pg_hba.conf"
    echo "local all all trust" >> "$PGDATA/pg_hba.conf"

    # Start temporarily for setup
    su - postgres -c "pg_ctl -D $PGDATA -l /tmp/pg_init.log start -w"

    # Create database and user
    su - postgres -c "psql -c \"CREATE USER novelbuilder WITH PASSWORD 'novelbuilder';\""
    su - postgres -c "psql -c \"CREATE DATABASE novelbuilder OWNER novelbuilder;\""
    su - postgres -c "psql -c \"ALTER USER novelbuilder CREATEDB;\""

    # Enable extensions
    su - postgres -c "psql -d novelbuilder -c 'CREATE EXTENSION IF NOT EXISTS vector;'"
    su - postgres -c "psql -d novelbuilder -c 'CREATE EXTENSION IF NOT EXISTS \"uuid-ossp\";'"

    # Run migrations
    echo "==> Running migrations..."
    su - postgres -c "psql -d novelbuilder -f /app/migrations/001_init.sql"

    su - postgres -c "pg_ctl -D $PGDATA stop -w"
    echo "==> PostgreSQL initialized."
fi

# ---- Set environment defaults ----
export DB_HOST="${DB_HOST:-127.0.0.1}"
export DB_PORT="${DB_PORT:-5432}"
export DB_USER="${DB_USER:-novelbuilder}"
export DB_PASSWORD="${DB_PASSWORD:-novelbuilder}"
export DB_NAME="${DB_NAME:-novelbuilder}"
export REDIS_ADDR="${REDIS_ADDR:-127.0.0.1:6379}"

# ---- Start all services via supervisord ----
echo "==> Starting services..."
exec /usr/bin/supervisord -c /etc/supervisor/conf.d/supervisord.conf
