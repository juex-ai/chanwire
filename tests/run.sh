#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

usage() {
    cat <<'EOF'
usage: ./tests/run.sh [--skip-build] [--skip-start] [api|cli|mcp|all]

Runs chanwire end-to-end tests against compiled binaries.

Suites:
  api   server HTTP + WebSocket API flow
  cli   CLI flow against a live server
  mcp   MCP stdio flow against a live server
  all   all suites (default)

Options:
  --skip-build  reuse existing ./bin/chanwire-server and ./bin/chanwire
  --skip-start  use CHANWIRE_ENDPOINT instead of starting an isolated server

Environment:
  CHANWIRE_E2E_PORT     port for the isolated server (default: random free port)
  CHANWIRE_E2E_RUN_DIR  working directory for logs and DB (default: ./.cache/e2e/<run>)
  CHANWIRE_ENDPOINT     endpoint used with --skip-start
EOF
}

skip_build=false
skip_start=false
suites=()

while [[ $# -gt 0 ]]; do
    case "$1" in
        --skip-build)
            skip_build=true
            ;;
        --skip-start)
            skip_start=true
            ;;
        api|cli|mcp|all)
            suites+=("$1")
            ;;
        -h|--help)
            usage
            exit 0
            ;;
        *)
            echo "error: unknown argument: $1" >&2
            usage >&2
            exit 2
            ;;
    esac
    shift
done

if [[ ${#suites[@]} -eq 0 ]]; then
    suites=(all)
fi

choose_port() {
    for _ in {1..50}; do
        local port=$((20000 + RANDOM % 20000))
        if command -v lsof >/dev/null 2>&1; then
            if lsof -nP -iTCP:"$port" -sTCP:LISTEN >/dev/null 2>&1; then
                continue
            fi
        fi
        echo "$port"
        return 0
    done
    echo "error: could not find a free TCP port" >&2
    return 1
}

test_packages=()
collect_test_packages() {
    local want_api=false
    local want_cli=false
    local want_mcp=false

    for suite in "${suites[@]}"; do
        case "$suite" in
            all)
                want_api=true
                want_cli=true
                want_mcp=true
                ;;
            api)
                want_api=true
                ;;
            cli)
                want_cli=true
                ;;
            mcp)
                want_mcp=true
                ;;
        esac
    done

    if [[ "$want_api" == true ]]; then
        test_packages+=("./api")
    fi
    if [[ "$want_cli" == true ]]; then
        test_packages+=("./cli")
    fi
    if [[ "$want_mcp" == true ]]; then
        test_packages+=("./mcp")
    fi

    if [[ ${#test_packages[@]} -eq 0 ]]; then
        echo "error: no test suite selected" >&2
        exit 2
    fi
}

wait_for_server() {
    local endpoint="$1"
    local pid="$2"
    local logfile="$3"

    for _ in {1..80}; do
        if ! kill -0 "$pid" 2>/dev/null; then
            echo "error: server exited before becoming ready; log follows ($logfile):" >&2
            cat "$logfile" >&2 || true
            exit 1
        fi

        local code
        code="$(curl -s -o /dev/null -w '%{http_code}' "$endpoint/api/v1/agent/register" || true)"
        if [[ "$code" != "000" ]]; then
            return 0
        fi
        sleep 0.1
    done

    echo "error: server did not become ready; log follows ($logfile):" >&2
    cat "$logfile" >&2 || true
    exit 1
}

if [[ "$skip_build" == false ]]; then
    bash scripts/build.sh
fi

run_dir="${CHANWIRE_E2E_RUN_DIR:-$REPO_ROOT/.cache/e2e/$(date +%Y%m%d-%H%M%S)-$$}"
mkdir -p "$run_dir"

server_pid=""
cleanup() {
    if [[ -n "$server_pid" ]] && kill -0 "$server_pid" 2>/dev/null; then
        kill "$server_pid" 2>/dev/null || true
        wait "$server_pid" 2>/dev/null || true
    fi
}
trap cleanup EXIT

if [[ "$skip_start" == false ]]; then
    port="${CHANWIRE_E2E_PORT:-$(choose_port)}"
    endpoint="http://127.0.0.1:$port"
    server_log="$run_dir/server.log"

    CHANWIRE_PORT="$port" \
    CHANWIRE_DB="$run_dir/chanwire.db" \
        ./bin/chanwire-server >"$server_log" 2>&1 &
    server_pid=$!

    wait_for_server "$endpoint" "$server_pid" "$server_log"
else
    endpoint="${CHANWIRE_ENDPOINT:-http://127.0.0.1:12306}"
fi

export CHANWIRE_ENDPOINT="$endpoint"
export CHANWIRE_BIN="$REPO_ROOT/bin/chanwire"
export CHANWIRE_E2E_RUN_DIR="$run_dir"

collect_test_packages

echo "e2e endpoint: $CHANWIRE_ENDPOINT"
echo "e2e run dir:  $CHANWIRE_E2E_RUN_DIR"

(
    cd tests
    # These suites share one isolated chanwire server and SQLite database.
    # Run package test binaries sequentially so cross-suite setup does not
    # contend on shared runtime state.
    go test -p 1 -count=1 -v "${test_packages[@]}"
)
