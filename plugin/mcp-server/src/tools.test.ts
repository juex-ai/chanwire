/**
 * Unit tests for chanwire MCP tool wrappers.
 *
 * Uses a stub execFile to verify:
 *   - The correct chanwire command + args are constructed.
 *   - Stdout / stderr are surfaced appropriately.
 *   - Non-zero exit (thrown error) is handled as a user-facing error string.
 *   - JSON from `chanwire agent list --json` is parsed correctly.
 *
 * Run with:  node --test dist/tools.test.js   (after tsc)
 *        or: node --experimental-strip-types --test src/tools.test.ts
 */

import { describe, it } from 'node:test';
import assert from 'node:assert/strict';

import {
  registerAgent,
  listAgents,
  sendMsg,
  type ExecFileFn,
} from './tools.js';

// ── helpers ───────────────────────────────────────────────────────────────────

/** Record the last (cmd, args) call and return a fixed result. */
function makeExec(stdout: string, stderr = ''): { exec: ExecFileFn; calls: string[][] } {
  const calls: string[][] = [];
  const exec: ExecFileFn = async (cmd, args) => {
    calls.push([cmd, ...args]);
    return { stdout, stderr };
  };
  return { exec, calls };
}

/** Stub that always throws, simulating a non-zero exit. */
function makeFailingExec(stderr: string, stdout = ''): ExecFileFn {
  return async () => {
    const err = Object.assign(new Error('Command failed'), { stdout, stderr });
    throw err;
  };
}

// ── chanwire_register_agent ───────────────────────────────────────────────────

describe('registerAgent', () => {
  it('calls chanwire agent register --agent_name <name>', async () => {
    const { exec, calls } = makeExec('registered: agent_name=alice\n');
    const result = await registerAgent('alice', exec);

    assert.deepEqual(calls[0], ['chanwire', 'agent', 'register', '--agent_name', 'alice']);
    assert.equal(result.type, 'text');
    assert.equal(result.text, 'registered: agent_name=alice');
  });

  it('returns stdout trimmed', async () => {
    const { exec } = makeExec('  registered: agent_name=bob  \n');
    const result = await registerAgent('bob', exec);
    assert.equal(result.text, 'registered: agent_name=bob');
  });

  it('surfaces error on non-zero exit', async () => {
    const exec = makeFailingExec('registration failed: connection refused');
    const result = await registerAgent('alice', exec);
    assert.ok(result.text.startsWith('error:'));
    assert.ok(result.text.includes('registration failed'));
  });
});

// ── chanwire_list_agents ──────────────────────────────────────────────────────

describe('listAgents', () => {
  it('calls chanwire agent list --json', async () => {
    const { exec, calls } = makeExec(
      JSON.stringify({ agents: [{ agent_name: 'alice', last_active_at: 1778154123456 }] }) + '\n',
    );
    await listAgents(exec);
    assert.deepEqual(calls[0], ['chanwire', 'agent', 'list', '--json']);
  });

  it('parses JSON and formats agent list', async () => {
    const payload = {
      agents: [
        { agent_name: 'alice', last_active_at: 1778154123456 },
        { agent_name: 'bob', last_active_at: null },
      ],
    };
    const { exec } = makeExec(JSON.stringify(payload) + '\n');
    const result = await listAgents(exec);

    assert.equal(result.type, 'text');
    assert.ok(result.text.includes('alice'));
    assert.ok(result.text.includes('bob'));
    assert.ok(result.text.includes('(never)'));
    // alice has a real timestamp; verify it's ISO-formatted
    assert.ok(result.text.includes('2026-'));
  });

  it('returns "No agents registered." when list is empty', async () => {
    const { exec } = makeExec(JSON.stringify({ agents: [] }) + '\n');
    const result = await listAgents(exec);
    assert.equal(result.text, 'No agents registered.');
  });

  it('surfaces error on non-zero exit', async () => {
    const exec = makeFailingExec('not registered. run: chanwire agent register --agent_name <name>');
    const result = await listAgents(exec);
    assert.ok(result.text.startsWith('error:'));
  });

  it('surfaces error when stdout is empty', async () => {
    const { exec } = makeExec('');
    const result = await listAgents(exec);
    assert.ok(result.text.startsWith('error:'));
  });
});

// ── chanwire_send_msg ─────────────────────────────────────────────────────────

describe('sendMsg', () => {
  it('calls chanwire msg send --to_agent <name> --content <text>', async () => {
    const { exec, calls } = makeExec('ok: message_id=42\n');
    await sendMsg('bob', 'hello', exec);

    assert.deepEqual(calls[0], [
      'chanwire', 'msg', 'send',
      '--to_agent', 'bob',
      '--content', 'hello',
    ]);
  });

  it('returns stdout trimmed on success', async () => {
    const { exec } = makeExec('ok: message_id=7\n');
    const result = await sendMsg('alice', 'hi', exec);
    assert.equal(result.text, 'ok: message_id=7');
  });

  it('surfaces 404 / unknown-agent error on non-zero exit', async () => {
    const exec = makeFailingExec('Error: no such agent: ghost');
    const result = await sendMsg('ghost', 'ping', exec);
    assert.ok(result.text.startsWith('error:'));
    assert.ok(result.text.includes('no such agent'));
  });
});
