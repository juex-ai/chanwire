# Design

## Why a server, not peer-to-peer?

Agents come and go. Their addresses change. The simplest way to make
"agent A messages agent B" reliable is a third party that always answers and
never loses a message. A small HTTP/WS service backed by SQLite is the
smallest possible such third party.

## Why push for delivery, persist for catch-up?

Two modes serve different needs:

- **Realtime** - push over WebSocket while the recipient is connected. Latency
  is the only cost that matters.
- **History** - durable rows replayed on reconnect. Answers "what did I miss?"
  without any client-side bookkeeping.

The same store powers both. The `type=history_batch` vs `type=realtime` split
is advisory for the client, so it can present catch-up quietly and highlight
new traffic.

## Why a single history batch?

The server sends the latest persisted messages as one `history_batch` frame
before registering the connection for realtime delivery. That gives the client
a clear boundary without a separate "history done" frame and keeps reconnect
behavior simple.

## Why two Go modules, not one?

The server pulls in Hertz, SQLite, and an HTTP stack. The CLI pulls in cobra and
a small WebSocket client. They share nothing meaningful at runtime: tokens are
opaque strings, the wire is JSON. A shared library would invite accidental
coupling and bloat the CLI binary. Two `go.mod` files keep release and
dependency boundaries explicit.

## Why is the MCP server inside the CLI?

One protocol implementation, one place to fix bugs. The `chanwire mcp`
subcommand uses the same Go HTTP and WebSocket client code as the human CLI,
exposes the MCP tools directly, and forwards WebSocket messages as
Claude-channel notifications after the MCP client declares the capability.

## Why an embedded web console?

Chanwire is local-first and operational. A small embedded console lets a human
inspect live agents, recent agent-to-agent edges, and the global message feed
without running another frontend service. The web API is intentionally
unauthenticated because it is a local dashboard surface, not a public product
surface.

Messages sent from the console use the special `system` sender. `system` is a
message author name, not a registered agent, so it can appear in message
history without showing up in `/agent/list` or online presence.

## Why exponential backoff `1,5,15,30,60,120`?

Aggressive enough that a transient blip recovers in seconds; gentle enough that
a server outage does not hammer the box. The `120s` cap makes the worst case
predictable. Resetting on a successful connect prevents a flapping link from
accumulating long delays.

## What this is not

- Not a chat product. There are no rooms, threads, attachments, read receipts,
  or human accounts. Just direct messages between named agents.
- Not multi-tenant. One SQLite file, one process, run locally.
- Not authenticated against humans. The token is the agent identity. Anyone
  holding it is that agent.
