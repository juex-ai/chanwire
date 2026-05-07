/**
 * Chanwire MCP tool implementations.
 *
 * Each function wraps exactly one chanwire CLI invocation via execFile.
 * No shell is involved — args are passed as an array directly to the process.
 *
 * All logging goes to stderr; stdout is reserved for MCP stdio transport.
 */

import { execFile as _execFile } from 'child_process';
import { promisify } from 'util';

import { safeISO } from './validate.js';

const execFile = promisify(_execFile);

export interface ExecFileFn {
  (cmd: string, args: string[]): Promise<{ stdout: string; stderr: string }>;
}

/**
 * The default execFile implementation used in production.
 * Can be replaced in tests via the exec parameter.
 */
const defaultExec: ExecFileFn = (cmd, args) => execFile(cmd, args);

// ── chanwire_register_agent ───────────────────────────────────────────────────

export interface RegisterResult {
  type: 'text';
  text: string;
}

/**
 * Register an agent with the chanwire server.
 * Runs: chanwire agent register --agent_name <name>
 */
export async function registerAgent(
  agentName: string,
  exec: ExecFileFn = defaultExec,
): Promise<RegisterResult> {
  try {
    const { stdout, stderr } = await exec('chanwire', [
      'agent',
      'register',
      '--agent_name',
      agentName,
    ]);
    const out = stdout.trim() || stderr.trim();
    return { type: 'text', text: out };
  } catch (err: unknown) {
    const execErr = err as { stdout?: string; stderr?: string; message?: string };
    const out =
      (execErr.stderr?.trim() || execErr.stdout?.trim() || execErr.message || String(err));
    return { type: 'text', text: `error: ${out}` };
  }
}

// ── chanwire_list_agents ──────────────────────────────────────────────────────

export interface Agent {
  agent_name: string;
  last_active_at: number | null;
}

export interface ListAgentsResult {
  type: 'text';
  text: string;
}

/**
 * List all registered agents.
 * Runs: chanwire agent list --json
 * Parses the single JSON line and returns a formatted text summary.
 */
export async function listAgents(
  exec: ExecFileFn = defaultExec,
): Promise<ListAgentsResult> {
  try {
    const { stdout, stderr } = await exec('chanwire', ['agent', 'list', '--json']);
    const line = stdout.trim();
    if (!line) {
      const msg = stderr.trim() || 'no output from chanwire agent list --json';
      return { type: 'text', text: `error: ${msg}` };
    }

    const parsed = JSON.parse(line) as { agents: Agent[] };
    const agents = parsed.agents ?? [];

    if (agents.length === 0) {
      return { type: 'text', text: 'No agents registered.' };
    }

    const lines = agents.map((a) => {
      const lastActive =
        typeof a.last_active_at === 'number'
          ? safeISO(a.last_active_at)
          : '(never)';
      return `${a.agent_name}  last_active=${lastActive}`;
    });

    return { type: 'text', text: lines.join('\n') };
  } catch (err: unknown) {
    const execErr = err as { stdout?: string; stderr?: string; message?: string };
    const out =
      execErr.stderr?.trim() || execErr.stdout?.trim() || execErr.message || String(err);
    return { type: 'text', text: `error: ${out}` };
  }
}

// ── chanwire_send_msg ─────────────────────────────────────────────────────────

export interface SendMsgResult {
  type: 'text';
  text: string;
}

/**
 * Send a message to another agent.
 * Runs: chanwire msg send --to_agent <name> --content <text>
 * On non-zero exit (including 404 "no such agent"), surfaces the error text.
 */
export async function sendMsg(
  toAgent: string,
  content: string,
  exec: ExecFileFn = defaultExec,
): Promise<SendMsgResult> {
  try {
    const { stdout, stderr } = await exec('chanwire', [
      'msg',
      'send',
      '--to_agent',
      toAgent,
      '--content',
      content,
    ]);
    const out = stdout.trim() || stderr.trim();
    return { type: 'text', text: out };
  } catch (err: unknown) {
    const execErr = err as { stdout?: string; stderr?: string; message?: string };
    // stderr carries the cobra error message ("no such agent: <name>", etc.)
    const out =
      execErr.stderr?.trim() || execErr.stdout?.trim() || execErr.message || String(err);
    return { type: 'text', text: `error: ${out}` };
  }
}
