#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(git rev-parse --show-toplevel)"
cd "$REPO_ROOT"

./scripts/build.sh

mkdir -p .log

PID_FILE=".log/server.pid"

require_lsof() {
    if ! command -v lsof >/dev/null 2>&1; then
        echo "error: lsof is required to detect listeners on the server port" >&2
        exit 1
    fi
}

trim() {
    local value="$1"
    value="${value#"${value%%[![:space:]]*}"}"
    value="${value%"${value##*[![:space:]]}"}"
    printf '%s' "$value"
}

env_file_value() {
    local key="$1"
    local line name value

    [[ -f .env ]] || return 0

    while IFS= read -r line || [[ -n "$line" ]]; do
        line="$(trim "$line")"
        [[ -z "$line" || "$line" == \#* ]] && continue

        if [[ "$line" =~ ^([A-Za-z_][A-Za-z0-9_]*)[[:space:]]*=(.*)$ ]]; then
            name="${BASH_REMATCH[1]}"
            value="$(trim "${BASH_REMATCH[2]%%#*}")"
            if [[ "$value" == \"*\" && "$value" == *\" ]]; then
                value="${value:1:${#value}-2}"
            elif [[ "$value" == \'*\' && "$value" == *\' ]]; then
                value="${value:1:${#value}-2}"
            fi

            if [[ "$name" == "$key" ]]; then
                printf '%s' "$value"
                return 0
            fi
        fi
    done < .env
}

server_port() {
    local port="${CHANWIRE_PORT:-}"
    if [[ -z "$port" ]]; then
        port="$(env_file_value CHANWIRE_PORT)"
    fi
    port="${port:-12306}"
    if [[ ! "$port" =~ ^[0-9]+$ ]]; then
        echo "error: CHANWIRE_PORT must be numeric, got: $port" >&2
        exit 1
    fi
    printf '%s' "$port"
}

listening_pids() {
    local port="$1"
    lsof -nP -t -iTCP:"$port" -sTCP:LISTEN 2>/dev/null | sort -u || true
}

kill_port_listeners() {
    local port="$1"
    local pids remaining deadline kill_deadline pid

    pids="$(listening_pids "$port")"
    if [[ -z "$pids" ]]; then
        return 0
    fi

    echo "stopping process(es) listening on port $port: $(echo "$pids" | tr '\n' ' ')"
    for pid in $pids; do
        kill "$pid" 2>/dev/null || true
    done

    deadline=$(( $(date +%s) + 5 ))
    while true; do
        remaining="$(listening_pids "$port")"
        [[ -z "$remaining" ]] && return 0

        if [[ $(date +%s) -ge $deadline ]]; then
            echo "warning: listener(s) on port $port did not stop within 5 s, sending SIGKILL..." >&2
            for pid in $remaining; do
                kill -9 "$pid" 2>/dev/null || true
            done

            kill_deadline=$(( $(date +%s) + 2 ))
            while true; do
                remaining="$(listening_pids "$port")"
                [[ -z "$remaining" ]] && return 0
                if [[ $(date +%s) -ge $kill_deadline ]]; then
                    echo "error: port $port is still being listened on by: $(echo "$remaining" | tr '\n' ' ')" >&2
                    exit 1
                fi
                sleep 0.2
            done
        fi
        sleep 0.2
    done
}

pid_is_listening() {
    local pid="$1"
    local port="$2"
    listening_pids "$port" | grep -qx "$pid"
}

http_ready() {
    local port="$1"
    local code
    code="$(curl --max-time 1 -s -o /dev/null -w '%{http_code}' "http://127.0.0.1:$port/api/v1/agent/list" || true)"
    [[ "$code" != "000" && -n "$code" ]]
}

wait_for_server() {
    local pid="$1"
    local port="$2"
    local logfile="$3"
    local deadline

    deadline=$(( $(date +%s) + 8 ))
    while true; do
        if ! kill -0 "$pid" 2>/dev/null; then
            echo "error: server failed to start; log follows ($logfile):" >&2
            cat "$logfile" >&2 || true
            rm -f "$PID_FILE"
            exit 1
        fi

        if pid_is_listening "$pid" "$port" && http_ready "$port"; then
            return 0
        fi

        if [[ $(date +%s) -ge $deadline ]]; then
            echo "error: server pid=$pid did not become ready on port $port; log follows ($logfile):" >&2
            cat "$logfile" >&2 || true
            kill "$pid" 2>/dev/null || true
            rm -f "$PID_FILE"
            exit 1
        fi
        sleep 0.2
    done
}

require_lsof

PORT="$(server_port)"

kill_port_listeners "$PORT"
rm -f "$PID_FILE"

LOGFILE=".log/server-$(date +%Y%m%d-%H%M%S).log"

nohup ./bin/chanwire-server >>"$LOGFILE" 2>&1 &
NEW_PID=$!

wait_for_server "$NEW_PID" "$PORT" "$LOGFILE"

echo "$NEW_PID" >"$PID_FILE"

echo "started: pid=$NEW_PID port=$PORT log=$LOGFILE"
