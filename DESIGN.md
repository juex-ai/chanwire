# Design

## Why a server, not peer-to-peer?

Agents come and go. Their addresses change. The simplest way to make "agent A messages agent B" reliable is a third party that always answers and never loses a message. A small HTTP/WS service backed by SQLite is the smallest possible such third party.

## Why push for delivery, persist for catch-up?

Two modes serve different needs:

- **Realtime** — push over WebSocket while the recipient is connected. Latency is the only cost that matters.
- **History** — durable, replayed in order on reconnect. Answers "what did I miss?" without any client-side bookkeeping.

The same store powers both. The `type=history` vs `type=realtime` split is advisory for the client (e.g., to suppress notification chimes on history). The server treats them identically as far as ordering and persistence go.

## Why distinguish history from realtime?

A client can't tell from a message alone whether it's old or new — only the server knows it was streamed as catch-up. Telling the client lets it choose UX: a notification for realtime, a silent backfill for history.

## Why a `history_done` marker?

Without an explicit boundary the client can't tell whether the next message is the last history row or the first new one. The marker is one extra frame of protocol that removes the ambiguity entirely.

## Why two Go modules, not one?

The server pulls in Hertz, `modernc.org/sqlite`, and an HTTP stack — heavy dependencies. The CLI pulls in cobra and a small WebSocket client. They share *nothing* meaningful at runtime: tokens are opaque strings, the wire is JSON. A shared library would invite accidental coupling and bloat the CLI binary. Two `go.mod`s, no shared code, separate release cadences.

## Why is the MCP server inside the CLI?

One protocol implementation, one place to fix bugs. The `chanwire mcp` subcommand uses the same Go HTTP and WebSocket client code as the human CLI, exposes the three MCP tools directly, and forwards WebSocket lines as Claude Code channel notifications over the MCP stdio connection. The old Node plugin shape added a second runtime and a shell-out layer without changing the product model.

## Why exponential backoff `1,5,15,30,60,120`?

Aggressive enough that a transient blip recovers in seconds; gentle enough that a server outage doesn't hammer the box. The `120s` cap makes the worst case predictable. Resetting on a successful connect prevents a flapping link from accumulating long delays.

## What this is *not*

- Not a chat product. There are no rooms, threads, attachments, presence indicators, or read receipts. Just direct messages between named agents.
- Not multi-tenant. One SQLite file, one process, run locally.
- Not authenticated against humans. The token is the agent's identity. Anyone holding it is that agent.
