# Architecture

## Components

```
+---------------------+         +---------------------+
|  cli (chanwire)     | <-----> |    server (Hertz)   |
|  - agent commands   |   HTTP  |                     |
|  - msg commands     |         |  +---------------+  |
|  - connect          |   WS    |  |  message hub  |  |
|  - mcp (server)     | <-----> |  +-------+-------+  |
+---------------------+         |          |          |
                                |          v          |
                                |    SQLite (file)    |
                                +---------------------+
```

The CLI speaks HTTP + WebSocket protocol to the server. The `mcp` subcommand runs an MCP server that exposes tools and streams messages via claude/channel notifications.

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
    from_agent_id INTEGER NOT NULL REFERENCES agents(id),
    to_agent_id   INTEGER NOT NULL REFERENCES agents(id),
    content       TEXT    NOT NULL,
    created_at    INTEGER NOT NULL
);

CREATE INDEX idx_messages_to ON messages(to_agent_id, id);
```

SQLite does not enforce foreign keys by default. The server must run
`PRAGMA foreign_keys = ON` on each connection (set it in the connector
init or in a `_pragma=foreign_keys(1)` DSN parameter).

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
- On WS connect: send the latest five persisted messages for the agent as one `type=history_batch` frame, then a single `type=history_done` marker, then switch to realtime.
- On `POST /msg/send`: insert row, look up recipient's live connections, push as `type=realtime`. Recipient offline → only persisted; next reconnect picks it up through the history replay.

### WebSocket payload

All frames are JSON.

```jsonc
// type=history_batch — one-time historical review, latest five messages max
{
  "type": "history_batch",
  "messages": [
    {
      "message_id": 42,
      "from_agent": "alice",
      "content": "hello",
      "sent_at": 1778154123456
    }
  ]
}

// type=realtime
{
  "type": "realtime",
  "message_id": 42,
  "from_agent": "alice",
  "content": "hello",
  "sent_at": 1778154123456
}

// type=history_done — single frame, no other fields
{ "type": "history_done" }
```

## CLI

- `cobra` root, sub-commands `version`, `status`, `agent register`, `agent list`, `msg send`, `connect`, `mcp`.
- Token store: `<resolved-config-dir>/agent.json` with `{agent_name, token, endpoint}`. The CLI resolves the config directory from `--homedir`, then `CHANWIRE_DIR`, then the user's home directory, and normalizes the result to `.config/chanwire`.
- `version` prints build metadata only; `status` prints runtime diagnostics without the saved endpoint.
- Reconnect backoff (seconds): `1, 5, 15, 30, 60, 120`, capped at `120`; resets on successful connect.
