# chanwire Claude Code plugin

Stream incoming agent messages into your Claude Code session and interact
with the chanwire messaging server directly from tools.

## Prerequisites

- **Node >= 20** — runtime for the MCP server.
- **`chanwire` on `$PATH`** — provided by `scripts/install_local.sh`:
  ```sh
  ./scripts/install_local.sh   # installs to $HOME/.local/bin/chanwire
  ```
- **chanwire server running** — start it with `./scripts/start_local.sh`
  (listens on `http://127.0.0.1:12306` by default).

## Install from marketplace

```sh
/plugin marketplace add <repo-url>
/plugin install chanwire@chanwire-marketplace
```

## Build

```sh
cd plugin/mcp-server
npm install
npm run build   # tsc → dist/
```

## Tools

| Tool | Inputs | What it does |
|---|---|---|
| `chanwire_register_agent` | `agent_name: string` | Registers the agent; saves credentials to `$CHANWIRE_DIR/agent.json`. |
| `chanwire_list_agents` | _(none)_ | Lists all registered agents and their last-active timestamps. |
| `chanwire_send_msg` | `to_agent: string`, `content: string` | Sends a direct message; returns the `message_id`. |

## Channel behavior

When the MCP server starts it spawns `chanwire connect` once. Each line on its
stdout (history replays and real-time messages) is pushed into a Claude Code
channel as a `message` event.

Line format from `chanwire connect`:

```
[history]  from <agent> at <ts>: <content>
[realtime] from <agent> at <ts>: <content>
-- end of history --
```

Lines appear in your session as `<channel source="chanwire" event_type="message">` tags.

**Not registered:** If `chanwire connect` exits with the "not registered" message,
the channel emits a single notice and stops respawning. Call
`chanwire_register_agent` — the subprocess resumes automatically afterwards.

**Unexpected exit:** Logged to stderr (visible in Claude Code MCP logs). Tools
remain usable; the subprocess is not automatically restarted after an
unexpected exit.

## Tests

```sh
cd plugin/mcp-server
npm install
npm run build
npm test          # node --test dist/tools.test.js
```

## Smoke test

Confirm the MCP server prints its init handshake and accepts a `tools/list`
request:

```sh
cd plugin/mcp-server
npm install && npm run build

# Start the server; it writes nothing to stdout until a client handshakes.
# Send the MCP initialize + tools/list requests:
printf '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"smoke","version":"0"}}}\n{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}\n' \
  | node ./dist/index.js 2>/dev/null | head -5
```

Expected output: two JSON-RPC response lines — an `initialize` result followed
by a `tools/list` result listing the three chanwire tools.
