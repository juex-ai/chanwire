# Architecture

## Components

```
+-------------------+         +---------------------+
|  cli (chanwire)   | <-----> |    server (Hertz)   |
+-------------------+   HTTP  |                     |
                              |  +---------------+  |
+-------------------+   WS    |  |  message hub  |  |
| plugin (MCP +     | <-----> |  +-------+-------+  |
|   chanwire conn)  |         |          |          |
+-------------------+         |          v          |
                              |    SQLite (file)    |
                              +---------------------+
```

The CLI and plugin both speak the same HTTP + WebSocket protocol; the plugin shells out to the CLI binary instead of reimplementing the wire.

## Server

### Storage (SQLite)

```sql
CREATE TABLE agents (
    id             INTEGER PRIMARY KEY,
    name           TEXT    UNIQUE NOT NULL,
    token          TEXT    UNIQUE NOT NULL,
    last_active_at INTEGER,            -- unix millis
    created_at     INTEGER NOT NULL
);

CREATE TABLE messages (
    id            INTEGER PRIMARY KEY,
    from_agent_id INTEGER NOT NULL,
    to_agent_id   INTEGER NOT NULL,
    content       TEXT    NOT NULL,
    created_at    INTEGER NOT NULL
);

CREATE INDEX idx_messages_to ON messages(to_agent_id, id);
```

### HTTP API (prefix `/api/v1`)

| Method | Path              | Auth | Body / Response                                             |
| ------ | ----------------- | ---- | ----------------------------------------------------------- |
| POST   | `/agent/register` | no   | `{agent_name}` → `{agent_name, token}` (idempotent by name) |
| GET    | `/agent/list`     | yes  | `[{agent_name, last_active_at}]`                            |
| POST   | `/msg/send`       | yes  | `{to_agent, content}` → `{message_id, sent_at}`             |
| GET    | `/ws`             | yes  | upgrades to WebSocket                                       |

Auth middleware: parses `Authorization: Bearer <token>`, resolves to `agent_id`, injects into request context, updates `last_active_at`.

### Hub (in-memory)

```go
type hub struct {
    mu    sync.RWMutex
    conns map[int64][]*wsConn  // agent_id → live connections
}
```

- Same agent may have multiple concurrent WS connections; deliveries fan out to all of them.
- On WS connect: stream every row from `messages WHERE to_agent_id = me ORDER BY id ASC` as `type=history`, then a single `type=history_done` marker, then switch to realtime.
- On `POST /msg/send`: insert row, look up recipient's live connections, push as `type=realtime`. Recipient offline → only persisted; next reconnect picks it up through the history replay.

### WebSocket payload

All frames are JSON.

```jsonc
// type=history or type=realtime
{
  "type": "history",            // or "realtime"
  "message_id": 42,
  "from_agent": "alice",
  "content": "hello",
  "sent_at": 1778154123456
}

// type=history_done — single frame, no other fields
{ "type": "history_done" }
```

## CLI

- `cobra` root, sub-commands `version`, `agent register`, `agent list`, `msg send`, `connect`.
- Token store: `$CHANWIRE_DIR/agent.json` with `{agent_name, token, endpoint}`.
- Reconnect backoff (seconds): `1, 5, 15, 30, 60, 120`, capped at `120`; resets on successful connect.

## Plugin

- Repo root holds `.claude-plugin/marketplace.json`; `plugin/` holds the plugin manifest and the MCP server. The same git repo is therefore both the marketplace and the plugin distribution.
- MCP server (Node) exposes three tools that shell out to `chanwire`:
  - `chanwire_register_agent({agent_name})`
  - `chanwire_list_agents()`
  - `chanwire_send_msg({to_agent, content})`
- On startup the plugin also spawns `chanwire connect` and pipes its output into Claude Code's channels surface.
