/**
 * Subprocess manager for `chanwire connect`.
 *
 * Spawns the process once and pushes every stdout line into a Claude Code
 * channel via an MCP notification. On unexpected exit, logs to stderr and
 * does NOT crash the MCP server — tools remain usable. If the process exits
 * with the "not registered" message, a single notice is pushed and the
 * manager stops respawning until reset() is called (typically after a
 * successful chanwire_register_agent tool call).
 *
 * All logging goes to stderr; stdout is reserved for MCP stdio transport.
 */

import { spawn, ChildProcess } from 'child_process';
import { createInterface } from 'readline';
import { Server } from '@modelcontextprotocol/sdk/server/index.js';

const log = console.error;

const NOT_REGISTERED_MARKER = 'not registered. run:';

export class ConnectManager {
  private mcp: Server;
  private child: ChildProcess | null = null;
  private stopping = false;
  private blocked = false; // true when "not registered" — stop respawning

  constructor(mcp: Server) {
    this.mcp = mcp;
  }

  /** Start the `chanwire connect` subprocess. */
  start(): void {
    if (this.blocked) {
      log('[chanwire:connect] blocked — agent not registered; call reset() after registering');
      return;
    }
    if (this.child) {
      log('[chanwire:connect] already running');
      return;
    }
    this.stopping = false;
    this.spawnProcess();
  }

  /**
   * Call after a successful chanwire_register_agent invocation to unblock
   * the subprocess and spawn it.
   */
  reset(): void {
    this.blocked = false;
    if (!this.child && !this.stopping) {
      this.start();
    }
  }

  /** Gracefully stop the subprocess (no respawn). */
  stop(): void {
    this.stopping = true;
    if (this.child) {
      this.child.kill('SIGTERM');
      this.child = null;
    }
  }

  private spawnProcess(): void {
    log('[chanwire:connect] spawning chanwire connect');

    const child = spawn('chanwire', ['connect'], {
      stdio: ['ignore', 'pipe', 'pipe'],
    });
    this.child = child;

    // Line-by-line stdout → channel notification
    const rl = createInterface({ input: child.stdout! });
    rl.on('line', (line) => {
      this.pushLine(line);
    });

    // Log stderr to our stderr
    child.stderr?.on('data', (chunk: Buffer) => {
      const text = chunk.toString().trim();
      if (text) {
        log(`[chanwire:connect] stderr: ${text}`);
      }
    });

    child.on('error', (err) => {
      log(`[chanwire:connect] process error: ${err.message}`);
      this.child = null;
      // Don't respawn on spawn errors (e.g. chanwire not on PATH)
    });

    child.on('exit', (code, signal) => {
      log(`[chanwire:connect] exited (code=${code}, signal=${signal})`);
      this.child = null;
      rl.close();

      if (this.stopping) {
        return;
      }

      // Exited non-zero — already handled line-by-line above if "not registered"
      if (this.blocked) {
        return;
      }

      // Unexpected exit: log but do not respawn automatically.
      // The tools still work; the user can restart if needed.
      if (code !== 0) {
        log(`[chanwire:connect] unexpected exit code=${code} signal=${signal}; not respawning`);
      }
    });
  }

  private pushLine(line: string): void {
    const trimmed = line.trim();
    if (!trimmed) {
      return;
    }

    // Detect "not registered" message
    if (trimmed.includes(NOT_REGISTERED_MARKER)) {
      log('[chanwire:connect] agent not registered — sending notice and stopping');
      this.blocked = true;
      this.pushNotification(
        'chanwire: agent not registered. Use the chanwire_register_agent tool to register, then messages will stream automatically.',
        'not_registered',
      );
      return;
    }

    this.pushNotification(trimmed, 'message');
  }

  private pushNotification(content: string, eventType: string): void {
    this.mcp
      .notification({
        method: 'notifications/claude/channel',
        params: {
          content,
          meta: { event_type: eventType },
        },
      })
      .catch((err: unknown) => {
        log(
          `[chanwire:connect] failed to send notification: ${
            err instanceof Error ? err.message : String(err)
          }`,
        );
      });
  }
}
