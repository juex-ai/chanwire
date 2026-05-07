/**
 * Tiny runtime validation helpers for MCP tool arguments.
 *
 * The MCP SDK's input schemas are advisory; nothing rejects a non-string
 * value at runtime. Validate here so we never coerce surprising types
 * (numbers, booleans, undefined) into chanwire CLI arguments.
 */

/**
 * Assert that `args[key]` is a non-empty string. Throws otherwise.
 *
 * @param args - The MCP tool arguments object (may be undefined / null / non-object).
 * @param key  - Property name to look up.
 * @returns The validated string value.
 */
export function requireString(args: unknown, key: string): string {
  const obj = (args ?? {}) as Record<string, unknown>;
  const v = obj[key];
  if (typeof v !== 'string') {
    throw new Error(`tool argument '${key}' must be a non-empty string`);
  }
  if (v.length === 0) {
    throw new Error(`tool argument '${key}' must be a non-empty string`);
  }
  return v;
}

/**
 * Format a unix-millis timestamp as ISO 8601, defending against bad inputs.
 *
 * `new Date(Infinity).toISOString()` and similar throw `RangeError`. Guard
 * the conversion so callers can format any incoming numeric value safely.
 */
export function safeISO(ms: number): string {
  if (typeof ms !== 'number' || !Number.isFinite(ms)) {
    return '(invalid timestamp)';
  }
  const d = new Date(ms);
  const t = d.getTime();
  if (Number.isNaN(t)) {
    return '(invalid timestamp)';
  }
  try {
    return d.toISOString();
  } catch {
    return '(invalid timestamp)';
  }
}
