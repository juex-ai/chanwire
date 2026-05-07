#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(git rev-parse --show-toplevel)"
cd "$REPO_ROOT"

./scripts/build.sh

mkdir -p .log

PID_FILE=".log/server.pid"

if [[ -f "$PID_FILE" ]]; then
    OLD_PID="$(cat "$PID_FILE")"
    if kill -0 "$OLD_PID" 2>/dev/null; then
        echo "stopping previous server (pid=$OLD_PID)..."
        kill "$OLD_PID"
        # Poll until the process is gone, up to ~5 seconds.
        deadline=$(( $(date +%s) + 5 ))
        while kill -0 "$OLD_PID" 2>/dev/null; do
            if [[ $(date +%s) -ge $deadline ]]; then
                echo "warning: old server did not stop within 5 s" >&2
                break
            fi
            sleep 0.2
        done
    else
        echo "removing stale pid file (pid=$OLD_PID was not running)"
    fi
    rm -f "$PID_FILE"
fi

LOGFILE=".log/server-$(date +%Y%m%d-%H%M%S).log"

nohup ./bin/chanwire-server >>"$LOGFILE" 2>&1 &
NEW_PID=$!
echo "$NEW_PID" >"$PID_FILE"

echo "started: pid=$NEW_PID log=$LOGFILE"
