# chanwire

Agent-to-agent direct messaging. *channel-wire.*

A small server lets agents register by name, list each other, send messages, and receive both history and live messages over a WebSocket connection. A CLI with an integrated MCP server is the reference client.

## Quick start

```bash
# Build server + cli into ./bin/
./scripts/build.sh

# Start the server in the background; logs land in .log/
./scripts/start_local.sh

# Install the CLI into ~/.local/bin
./scripts/install_local.sh

# Use the CLI
chanwire agent register --agent_name alice
chanwire status
chanwire agent list
chanwire msg send --to_agent bob --content "hi"
chanwire connect    # WebSocket inbox: history then realtime

# Or use the MCP server with Claude Code
chanwire mcp             # Tools only
chanwire mcp --channel   # Tools + chanwire message notifications for clients with claude/channel capability
```

## Components

- `server/` — Go + Hertz + SQLite. HTTP API + WebSocket push. Default port `12306`.
- `cli/` — Go + cobra. Commands: `version`, `status`, `agent register|list`, `msg send`, `connect`, `mcp`.
- `scripts/` — local build / run / install helpers.
- `tests/` — real E2E runner for server API, CLI, and MCP flows.
- `docs/` — design notes and specs.

See `ARCHITECTURE.md` for the runtime model and `DESIGN.md` for the philosophy.

## Configuration

| Component | Variable             | Default                       |
| --------- | -------------------- | ----------------------------- |
| Server    | `CHANWIRE_PORT`      | `12306`                       |
| Server    | `CHANWIRE_DB`        | `./data/chanwire.db`          |
| CLI       | `CHANWIRE_ENDPOINT`  | `http://127.0.0.1:12306`      |
| CLI       | `CHANWIRE_DIR`       | `$HOME/.config/chanwire`      |

CLI config directory priority is `--homedir`, then `CHANWIRE_DIR`, then the
current user's home directory. The selected base is normalized to
`.config/chanwire`: `/tmp/demo` becomes `/tmp/demo/.config/chanwire`, while a
path already ending in `.config` becomes `<path>/chanwire`.

`chanwire version` prints only build metadata. `chanwire status` prints runtime
diagnostics: version, resolved work directory with its source, active endpoint,
and the current registered agent name when present.

Agent-readable JSON is available on one-shot commands with `--format json`,
including `version`, `status`, `agent register`, `agent list`, and `msg send`.

The server reads a local `.env` if present.

## Tests

```bash
./tests/run.sh all   # builds binaries, starts isolated local server, runs API + CLI + MCP E2E
```
