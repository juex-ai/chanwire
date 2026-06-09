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

The CLI speaks HTTP + WebSocket protocol to the server. The `mcp` subcommand runs an MCP server that exposes tools; it streams messages via claude/channel notifications after the MCP client declares the experimental `claude/channel` capability.

## Server

### Storage (SQLite)

```sql
CREATE TABLE agents (
    id             INTEGER PRIMARY KEY,
    name           TEXT    UNIQUE NOT NULL,
    token          TEXT    UNIQUE NOT NULL,
    last_active_at INTEGER,            -- unix seconds
    created_at     INTEGER NOT NULL,
    deleted        INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE messages (
    id              INTEGER PRIMARY KEY,
    from_agent_id   INTEGER REFERENCES agents(id),
    from_agent_name TEXT    NOT NULL,
    to_agent_id     INTEGER NOT NULL REFERENCES agents(id),
    content         TEXT    NOT NULL,
    created_at      INTEGER NOT NULL
);

CREATE INDEX idx_messages_to ON messages(to_agent_id, id);
CREATE INDEX idx_messages_created ON messages(created_at, id);
CREATE INDEX idx_messages_from_created ON messages(from_agent_id, created_at);
```

SQLite does not enforce foreign keys by default. The server must run
`PRAGMA foreign_keys = ON` on each connection (set it in the connector
init or in a `_pragma=foreign_keys(1)` DSN parameter).

`from_agent_id` is nullable so the web console can persist messages from the
special `system` sender without registering a fake agent. Registered-agent
messages still store both `from_agent_id` and the denormalized
`from_agent_name`; migrations upgrade older message tables into this shape.
Agent deletion is soft: `deleted=1` hides the agent from normal lookup, auth,
list, settings, and graph queries. Re-registering the same `name` reactivates
the existing row with its original token.

### HTTP API (prefix `/api/v1`)

| Method | Path              | Auth | Body / Response                                             |
| ------ | ----------------- | ---- | ----------------------------------------------------------- |
| POST   | `/agent/register` | no   | `{agent_name}` → `{agent_name, token}` (idempotent by name) |
| GET    | `/agent/list`     | yes  | `{agents:[{agent_name, last_active_at}]}`                   |
| POST   | `/msg/send`       | yes  | `{to_agent, content}` → `{message_id, sent_at}`             |
| GET    | `/ws`             | yes  | upgrades to WebSocket                                       |
| GET    | `/web/state`      | no   | online agent graph + latest 20 messages for the web console |
| GET    | `/web/messages`   | no   | latest/older global messages, `before_id` pagination        |
| POST   | `/web/msg/send`   | no   | `{to_agent, content}` → sends from special `system` sender  |
| GET    | `/web/settings/agents` | no | settings Agents table, `limit`/`offset` pagination      |
| DELETE | `/web/settings/agents/:agent_name` | no | soft-deletes an agent by name                 |
| GET    | `/web/ws`         | no   | web-console realtime event WebSocket                        |

Auth middleware: parses `Authorization: Bearer <token>`, resolves to `agent_id`, injects into request context, updates `last_active_at`.
Time fields such as `created_at`, `last_active_at`, and `sent_at` are stored
and transferred as UTC Unix timestamps in seconds. Clients are responsible for
formatting those timestamps in the user's local system timezone.

### Hub (in-memory)

```go
type hub struct {
    mu    sync.RWMutex
    conns map[int64][]*wsConn  // agent_id → live connections
}
```

- Same agent may have multiple concurrent WS connections; deliveries fan out to all of them.
- On WS connect: send the latest five persisted messages for the agent as one `type=history_batch` frame when history exists, then switch to realtime. No separate history-end frame is sent.
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
      "sent_at": 1778154123
    }
  ]
}

// type=realtime
{
  "type": "realtime",
  "message_id": 42,
  "from_agent": "alice",
  "content": "hello",
  "sent_at": 1778154123
}
```


### Embedded web console

The server serves a single-page web console at `/`, `/web`, and `/settings`. Its unauthenticated web API is intentionally local-dashboard oriented: it can read the current online-agent graph, page through global messages 20 at a time, subscribe to global message/presence events, send messages as the special `system` sender, and manage registered agents from the settings page. `system` is persisted as a message sender name, not as a registered agent, so it never appears in `/agent/list` or as an online node.

The online graph is derived from live agent WebSocket connections. Directed edges include agent-to-agent messages from the last seven days where both endpoints are currently online; reciprocal messages naturally render as bidirectional arrows in the browser.
The browser also applies realtime agent-to-agent web WS message frames to the visible graph immediately, adding the directed edge and transient arrow animation without waiting for a full `/web/state` refresh. Initial loads and refresh data render only the final graph state without animation.

## CLI

- `cobra` root, sub-commands `version`, `status`, `agent register`, `agent list`, `msg send`, `connect`, `mcp`.
- Token store: `<resolved-config-dir>/agent.json` with `{agent_name, token, endpoint}`. The CLI resolves the config directory from `--homedir`, then `CHANWIRE_DIR`, then the user's home directory, and normalizes the result to `.config/chanwire`.
- `version` prints build metadata only; `status` prints runtime diagnostics, including the active endpoint from `CHANWIRE_ENDPOINT` or its default, but not the endpoint saved in `agent.json`.
- Bounded one-shot commands expose machine-readable output with `--format json`; streaming `connect` remains line-oriented.
- `connect` prints `history_batch` content as one review block; realtime messages print individually as they arrive.
- Reconnect backoff (seconds): `1, 5, 15, 30, 60, 120`, capped at `120`; resets on successful connect.
