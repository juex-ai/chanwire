# AGENTS.md

Guidance for agents (and humans) working in this repository.

`CLAUDE.md` is a symlink to this file — keep updates here.

For product overview and quick start, see `README.md`. For architecture
internals see `ARCHITECTURE.md`; for the philosophy behind the model see
`DESIGN.md`.

## Repo layout

- `server/`  — Go module, its own `go.mod`. Hertz + SQLite. **No code shared with `cli/`.**
- `cli/`     — Go module, its own `go.mod`. cobra-based, includes the `mcp` stdio server subcommand.
- `scripts/` — `build.sh` / `start_local.sh` / `install_local.sh`. Bash, `set -euo pipefail`.
- `tests/`   — real E2E tests. `tests/run.sh` builds binaries, starts an isolated local server, then runs API, CLI, and MCP flows.
- `docs/`    — long-form notes; `docs/specs/` for design specs.
- `.log/`    — local server logs (gitignored).

## Conventions

- Two Go modules. Do not factor a shared library. If duplication appears, copy first; only abstract once the shape is the same on both sides.
- Server config via `.env`: `CHANWIRE_PORT` (default `12306`), `CHANWIRE_DB` (default `./data/chanwire.db`).
- CLI config via `--homedir`, then `CHANWIRE_DIR`, then home. The final directory is normalized to `.config/chanwire`; `CHANWIRE_ENDPOINT` defaults to `http://127.0.0.1:12306`.
- Authorization on every endpoint except `POST /api/v1/agent/register` is `Authorization: Bearer <token>`. WebSocket uses the same header.
- Version metadata is build-time: `version` from `git describe --always --tags --dirty`, `commit` from `git rev-parse --short HEAD`, injected via `-ldflags`.

## Development workflow

Tasks are tracked with [`taskline`](https://github.com/your-org/taskline) under project name `chanwire`. Each task moves through `created → design → dev → review → done`. Branch per task, minimal commits, PR before merging to `main`.

Run `./scripts/build.sh` to build both binaries. Run `./scripts/start_local.sh` to launch the server in the background. Logs land in `.log/`. Run `./tests/run.sh all` for isolated API + CLI + MCP E2E validation.
