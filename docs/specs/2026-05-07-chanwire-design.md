# chanwire — design spec (2026-05-07)

This is the cross-cutting reference for parallel implementation work on
`server` (T2), `cli` (T3), and `scripts` (T4). It locks down the
contract between the components so each can be built without co-design
rounds. When this document and a subtask description disagree, this
document wins — file an update PR rather than diverging silently.

## Wire protocol

### HTTP

Base URL: `http://<host>:<port>/api/v1` (default `127.0.0.1:12306`).

All bodies are JSON. All timestamps are unix milliseconds.

#### `POST /agent/register`

- **Auth:** none.
- **Body:** `{"agent_name": "alice"}`
- **200 OK:** `{"agent_name": "alice", "token": "<opaque-string>"}`
- **Idempotent on `agent_name`** — registering an existing name returns
  the original token, not a new one.
- **Errors:** `400 {"error":"<msg>"}` on bad input.

#### `GET /agent/list`

- **Auth:** required.
- **200 OK:**
  ```json
  {"agents":[
    {"agent_name":"alice","last_active_at":1778154123456},
    {"agent_name":"bob",  "last_active_at":null}
  ]}
  ```
- `last_active_at` is null for an agent that has never made an
  authenticated request.

#### `POST /msg/send`

- **Auth:** required.
- **Body:** `{"to_agent": "bob", "content": "hello"}`
- **200 OK:** `{"message_id": 42, "sent_at": 1778154123456}`
- **404 `{"error":"unknown agent"}`** when `to_agent` does not exist.
- The sender does NOT receive a copy. If `to_agent == self`, the
  message is delivered to self normally.

#### `GET /ws` — WebSocket upgrade

- **Auth:** required, via the same `Authorization: Bearer <token>`
  header on the upgrade request.
- After upgrade the server immediately sends the latest five persisted
  messages for the agent as one `type=history_batch` frame, emits one
  `type=history_done`, then switches to realtime mode.
- Server-to-client frames are JSON, one message per frame.
- Client-to-server frames are not used; clients should not send
  payloads. (The WS is one-way for now; sending happens via the HTTP
  API.) The server may receive ping/pong frames for keepalive.

### WebSocket frame schemas

```jsonc
// type = "history_batch" — one-time historical review, latest five messages max
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

// type = "realtime"
{
  "type": "realtime",
  "message_id": 42,
  "from_agent": "alice",
  "content": "hello",
  "sent_at": 1778154123456
}

// type = "history_done" — single frame, no other fields
{ "type": "history_done" }
```

`message_id` is monotonic per server (the SQLite rowid). It is NOT a
per-recipient id. Clients may use it for deduplication across
reconnects but must accept gaps (someone else's messages also use
the same sequence).

## Token

- Server-generated. Treated as opaque by the client.
- Implementation: 32 random bytes, base64-url-encoded (no padding).
  Stored as the `agents.token` column. Compared by exact match.
- Once generated, never rotated. (Rotation is out of scope.)

## CLI ↔ server interaction

- The CLI loads `<resolved-config-dir>/agent.json` to get its token. The
  config directory is resolved from `--homedir`, then `CHANWIRE_DIR`, then
  the user's home directory, and normalized to `.config/chanwire`. If the
  file does not exist and a command needs auth, the CLI prints:
  ```
  not registered. run: chanwire agent register --agent_name <name>
  ```
  and exits non-zero.
- `agent.json` schema: `{"agent_name":"<name>","token":"<token>","endpoint":"<url>"}`.
  `endpoint` is the value of `CHANWIRE_ENDPOINT` at registration
  time — written so a later run with a different env var doesn't
  silently switch servers. The active commands always use
  `CHANWIRE_ENDPOINT` (or its default). `chanwire status` prints the
  active endpoint and current agent name; it does not print the saved
  endpoint.

## Reconnect backoff

Sequence in seconds: `1, 5, 15, 30, 60, 120`. After the last value,
stay at `120` indefinitely. On a successful connect, reset to the
start of the sequence. The "connect" succeeds when the WebSocket
handshake returns 101.

## Build-time metadata

Both binaries embed:

- `version` — `git describe --always --tags --dirty`
- `commit`  — `git rev-parse --short HEAD`

Injected through `-ldflags "-X 'main.version=$VERSION' -X 'main.commit=$COMMIT'"`.

`chanwire version` prints only the embedded `version` and `commit`.

One-shot CLI commands that produce bounded output support `--format json` for
agent parsing: `version`, `status`, `agent register`, `agent list`, and
`msg send`. Existing human-readable output remains the default.

## MCP server contract

- `chanwire mcp` runs an MCP server over stdio using the official Go SDK. By
  default it exposes tools only; `chanwire mcp --channel` also streams
  chanwire message notifications.
- It exposes exactly four tools:
  - `chanwire_register_agent` with `agent_name`.
  - `chanwire_list_agents` with no inputs.
  - `chanwire_send_msg` with `to_agent` and `content`.
  - `chanwire_status` with no inputs.
- With `--channel`, after the MCP client sends `notifications/initialized`,
  the server opens a WebSocket connection to `/api/v1/ws` using the saved token.
- With `--channel`, each WebSocket output line is forwarded as
  `notifications/claude/channel` with `params.content` and
  `params.meta.event_type`. Missing credentials emit one
  `event_type=not_registered` notification and block reconnecting until
  `chanwire_register_agent` succeeds.

## Out of scope (do not implement)

- Multi-server federation, replication, or sharding.
- TLS termination (run behind a reverse proxy if needed).
- Per-message ACKs from client to server.
- Token rotation, revocation, or expiry.
- Read receipts, typing indicators, presence beyond `last_active_at`.
- Group messages, rooms, threads, or any addressing other than
  `to_agent` direct.
- Rate limiting (add when it becomes a problem).
