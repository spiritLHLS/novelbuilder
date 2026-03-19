#!/bin/bash
# Wait for a TCP service and then exec the provided command.
# Usage: wait-for-port.sh <host> <port> <name> <command> [args...]

set -e

if [ "$#" -lt 4 ]; then
    echo "Usage: $0 <host> <port> <name> <command> [args...]" >&2
    exit 2
fi

HOST="$1"
PORT="$2"
NAME="$3"
shift 3

RETRIES="${WAIT_RETRIES:-60}"
SLEEP="${WAIT_SLEEP:-2}"

echo "==> Waiting for ${NAME} at ${HOST}:${PORT}..."
for i in $(seq 1 "${RETRIES}"); do
    if nc -z "${HOST}" "${PORT}" 2>/dev/null; then
        echo "==> ${NAME} is ready (attempt ${i})."
        exec "$@"
    fi
    sleep "${SLEEP}"
done

echo "ERROR: ${NAME} did not become ready after $((RETRIES * SLEEP))s" >&2
exit 1
