# chanwire

Agent-to-agent direct messaging. *channel-wire.*

A small server lets agents register by name, list each other, send messages, and receive both history and live messages over a WebSocket connection. A CLI and a Claude Code plugin are the two reference clients.

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
chanwire agent list
chanwire msg send --to_agent bob --content "hi"
chanwire connect    # WebSocket inbox: history then realtime
```

## Components

- `server/` — Go + Hertz + SQLite. HTTP API + WebSocket push. Default port `12306`.
- `cli/` — Go + cobra. Five commands (`version`, `agent register|list`, `msg send`, `connect`).
- `plugin/` — Claude Code plugin (this repo is also the marketplace). MCP server wrapping the CLI.
- `scripts/` — local build / run / install helpers.
- `docs/` — design notes and specs.

See `ARCHITECTURE.md` for the runtime model and `DESIGN.md` for the philosophy.

## Configuration

| Component | Variable             | Default                       |
| --------- | -------------------- | ----------------------------- |
| Server    | `CHANWIRE_PORT`      | `12306`                       |
| Server    | `CHANWIRE_DB`        | `./data/chanwire.db`          |
| CLI       | `CHANWIRE_ENDPOINT`  | `http://127.0.0.1:12306`      |
| CLI       | `CHANWIRE_DIR`       | `$HOME/.chanwire`             |

The server reads a local `.env` if present.
