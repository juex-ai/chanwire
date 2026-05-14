---
name: chanwire-localtest
description: Use when feature development, bugfix, or refactoring is complete in the project and code needs validation. Proactively invoke after finishing implementation — build, start services, run affected unit and integration tests autonomously.
metadata:
  internal: true
---

# ChanWire Local Test

After completing any code change, build the binaries, run affected unit tests, and run the real E2E suites. Do NOT ask the user — the scripts are idempotent and use isolated `.cache/e2e/` state.

## Execution Steps

1. **Build** — `bash scripts/build.sh`
2. **Run unit tests** — run the affected package tests first:
   - Server changes: `( cd server && go test -v ./... )`
   - CLI changes: `( cd cli && go test -v ./... )`
   - Test runner changes: validate with step 3; the E2E packages expect a live server.
3. **Run E2E tests** — `./tests/run.sh all`
   - The runner rebuilds by default, starts `bin/chanwire-server` on an isolated random local port, uses an isolated SQLite DB under `.cache/e2e/`, and runs API, CLI, and MCP suites.
   - Use `./tests/run.sh api` for server HTTP/WebSocket API-only changes.
   - Use `./tests/run.sh cli` for CLI-only changes.
   - Use `./tests/run.sh mcp` for MCP stdio-only changes.
   - Use `./tests/run.sh --skip-build all` only after step 1 has already built the current code.
   - Use `./tests/run.sh --skip-start <suite>` only when you intentionally want to test against an already-running server via `CHANWIRE_ENDPOINT`.

## Failure Handling

- If build fails → fix compilation errors first, do not proceed to tests
- If unit tests fail → fix before running integration tests
- If E2E server startup fails → inspect `.cache/e2e/<run>/server.log`, fix the server or runner, then rerun `./tests/run.sh all`
- If E2E tests fail → fix the root cause, rerun the failing suite, then rerun `./tests/run.sh all` before finishing
