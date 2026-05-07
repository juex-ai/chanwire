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

const log = console.error;

// Exit cleanly on broken pipe (parent disconnected)
process.stderr.on('error', () => { process.exit(0); });

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

mcp.setRequestHandler(CallToolRequestSchema, async (request) => {
  const { name, arguments: args } = request.params;
  const safeArgs = (args ?? {}) as Record<string, unknown>;

  switch (name) {
    case 'chanwire_register_agent': {
      const agentName = safeArgs.agent_name as string;
      const result = await registerAgent(agentName);
      // After registering, unblock the connect subprocess so messages stream.
      connectMgr.reset();
      return { content: [result] };
    }

    case 'chanwire_list_agents': {
      const result = await listAgents();
      return { content: [result] };
    }

    case 'chanwire_send_msg': {
      const toAgent = safeArgs.to_agent as string;
      const content = safeArgs.content as string;
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

await mcp.connect(new StdioServerTransport());
log('[chanwire] MCP server connected via stdio');

// Allow Claude Code to register the channel listener before we push anything.
await new Promise<void>((resolve) => setTimeout(resolve, 3000));

const connectMgr = new ConnectManager(mcp);
connectMgr.start();

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
