#!/usr/bin/env node

/**
 * Chanwire Claude Code MCP server.
 *
 * Stdio MCP server that exposes three tools for interacting with the chanwire
 * agent-messaging system, and streams incoming messages via a Claude Code
 * channel using `chanwire connect`.
 *
 * Prerequisites:
 *   - Node >= 20
 *   - `chanwire` binary on $PATH (provided by scripts/install_local.sh)
 *
 * All logging MUST go to stderr — stdout is reserved for MCP stdio transport.
 */

import { Server } from '@modelcontextprotocol/sdk/server/index.js';
import { StdioServerTransport } from '@modelcontextprotocol/sdk/server/stdio.js';
import {
  CallToolRequestSchema,
  ListToolsRequestSchema,
} from '@modelcontextprotocol/sdk/types.js';

import { registerAgent, listAgents, sendMsg } from './tools.js';
import { ConnectManager } from './connect.js';
import { requireString } from './validate.js';

const log = console.error;

// Exit cleanly on broken pipe (parent disconnected). Cover both stdout (the
// JSON-RPC transport) and stderr (logging) — a write error on either is
// terminal.
function onStreamError(err: NodeJS.ErrnoException): void {
  if (err.code === 'EPIPE') {
    process.exit(0);
  }
  process.exit(1);
}
process.stdout.on('error', onStreamError);
process.stderr.on('error', onStreamError);

const mcp = new Server(
  { name: 'chanwire', version: '0.1.0' },
  {
    capabilities: {
      tools: {},
      experimental: { 'claude/channel': {} },
    },
    instructions: `You are connected to the chanwire agent-messaging system.

Incoming messages from other agents arrive as <channel source="chanwire" event_type="message"> tags in your context.

## Available tools

- **chanwire_register_agent** — Register yourself (or a named agent) with the chanwire server.
- **chanwire_list_agents** — List all registered agents and their last-active timestamps.
- **chanwire_send_msg** — Send a message to another agent by name.

## Important
- If you see a "not registered" channel event, call chanwire_register_agent before sending messages.
- Messages stream automatically once registered; no polling needed.`,
  },
);

// Created here so request handlers below can refer to it; actually started
// from the `oninitialized` callback once the MCP handshake has completed.
const connectMgr = new ConnectManager(mcp);

// ── Tool: list ────────────────────────────────────────────────────────────────

mcp.setRequestHandler(ListToolsRequestSchema, async () => ({
  tools: [
    {
      name: 'chanwire_register_agent',
      description: 'Register a named agent with the chanwire server. Saves credentials locally.',
      inputSchema: {
        type: 'object',
        properties: {
          agent_name: {
            type: 'string',
            description: 'The name to register (e.g. "alice").',
          },
        },
        required: ['agent_name'],
      },
    },
    {
      name: 'chanwire_list_agents',
      description: 'List all agents registered on the chanwire server, with last-active timestamps.',
      inputSchema: {
        type: 'object',
        properties: {},
        required: [],
      },
    },
    {
      name: 'chanwire_send_msg',
      description: 'Send a direct message to another agent by name. Returns the message ID on success.',
      inputSchema: {
        type: 'object',
        properties: {
          to_agent: {
            type: 'string',
            description: 'Recipient agent name.',
          },
          content: {
            type: 'string',
            description: 'Message text.',
          },
        },
        required: ['to_agent', 'content'],
      },
    },
  ],
}));

// ── Tool: call ────────────────────────────────────────────────────────────────

/** A tool result text starting with "error:" indicates the underlying CLI failed. */
function isErrorResult(text: string): boolean {
  return text.startsWith('error:');
}

mcp.setRequestHandler(CallToolRequestSchema, async (request) => {
  const { name, arguments: args } = request.params;

  switch (name) {
    case 'chanwire_register_agent': {
      let agentName: string;
      try {
        agentName = requireString(args, 'agent_name');
      } catch (err) {
        return {
          content: [{ type: 'text', text: `error: ${(err as Error).message}` }],
          isError: true,
        };
      }
      const result = await registerAgent(agentName);
      // Only unblock the connect subprocess when registration succeeded —
      // a failed CLI call should NOT trigger a respawn.
      if (!isErrorResult(result.text)) {
        connectMgr.reset();
      }
      return { content: [result] };
    }

    case 'chanwire_list_agents': {
      const result = await listAgents();
      return { content: [result] };
    }

    case 'chanwire_send_msg': {
      let toAgent: string;
      let content: string;
      try {
        toAgent = requireString(args, 'to_agent');
        content = requireString(args, 'content');
      } catch (err) {
        return {
          content: [{ type: 'text', text: `error: ${(err as Error).message}` }],
          isError: true,
        };
      }
      const result = await sendMsg(toAgent, content);
      return { content: [result] };
    }

    default:
      return {
        content: [{ type: 'text', text: `Unknown tool: ${name}` }],
        isError: true,
      };
  }
});

// ── Error handler ─────────────────────────────────────────────────────────────

mcp.onerror = (error) => {
  log(`[chanwire] MCP error: ${error instanceof Error ? error.message : String(error)}`);
};

// ── Startup ───────────────────────────────────────────────────────────────────

// Hook into the MCP `initialized` notification — the deterministic signal
// that the client has finished the handshake and is ready to receive
// notifications. This replaces the ad-hoc setTimeout we used before.
mcp.oninitialized = () => {
  log('[chanwire] client initialized — starting connect subprocess');
  connectMgr.start();
};

await mcp.connect(new StdioServerTransport());
log('[chanwire] MCP server connected via stdio');

// ── Shutdown ──────────────────────────────────────────────────────────────────

function shutdown(sig: string): void {
  log(`[chanwire] ${sig}`);
  connectMgr.stop();
}

process.on('SIGTERM', () => shutdown('SIGTERM'));
process.on('SIGINT',  () => shutdown('SIGINT'));

function isPipeBreak(err: unknown): boolean {
  const code = (err as NodeJS.ErrnoException | undefined)?.code;
  return code === 'EPIPE' || code === 'ERR_STREAM_DESTROYED';
}

process.on('unhandledRejection', (err) => {
  if (isPipeBreak(err)) { process.exit(0); }
  log(`[chanwire] unhandled rejection: ${err}`);
});

process.on('uncaughtException', (err) => {
  if (isPipeBreak(err)) { process.exit(0); }
  log(`[chanwire] uncaught exception: ${err.message}`);
});
