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
        # `|| true` avoids racing with a process that exits between the
        # kill -0 check above and the SIGTERM below.
        kill "$OLD_PID" 2>/dev/null || true
        # Poll until the process is gone, up to ~5 seconds.
        deadline=$(( $(date +%s) + 5 ))
        while kill -0 "$OLD_PID" 2>/dev/null; do
            if [[ $(date +%s) -ge $deadline ]]; then
                echo "warning: old server did not stop within 5 s, sending SIGKILL..." >&2
                kill -9 "$OLD_PID" 2>/dev/null || true
                # Re-poll briefly (~2 s) for the kernel to reap it.
                kill_deadline=$(( $(date +%s) + 2 ))
                while kill -0 "$OLD_PID" 2>/dev/null; do
                    if [[ $(date +%s) -ge $kill_deadline ]]; then
                        echo "error: pid=$OLD_PID still alive after SIGKILL; refusing to start a duplicate" >&2
                        exit 1
                    fi
                    sleep 0.2
                done
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

# Give the server ~300ms to fail fast on port conflicts, DB perms, etc.
sleep 0.3
if ! kill -0 "$NEW_PID" 2>/dev/null; then
    echo "error: server failed to start; log follows ($LOGFILE):" >&2
    cat "$LOGFILE" >&2 || true
    rm -f "$PID_FILE"
    exit 1
fi

echo "$NEW_PID" >"$PID_FILE"

echo "started: pid=$NEW_PID log=$LOGFILE"
