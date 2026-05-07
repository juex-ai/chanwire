/**
 * Unit tests for runtime validation helpers (validate.ts).
 */

import { describe, it } from 'node:test';
import assert from 'node:assert/strict';

import { requireString, safeISO } from './validate.js';

// ── requireString ─────────────────────────────────────────────────────────────

describe('requireString', () => {
  it('returns the string when valid', () => {
    assert.equal(requireString({ foo: 'bar' }, 'foo'), 'bar');
  });

  it('throws when arg is missing', () => {
    assert.throws(() => requireString({}, 'foo'), /must be a non-empty string/);
  });

  it('throws when arg is null/undefined args object', () => {
    assert.throws(() => requireString(undefined, 'foo'), /must be a non-empty string/);
    assert.throws(() => requireString(null, 'foo'), /must be a non-empty string/);
  });

  it('throws when value is empty string', () => {
    assert.throws(() => requireString({ foo: '' }, 'foo'), /must be a non-empty string/);
  });

  it('throws when value is a number', () => {
    assert.throws(() => requireString({ foo: 42 }, 'foo'), /must be a non-empty string/);
  });

  it('throws when value is a boolean', () => {
    assert.throws(() => requireString({ foo: true }, 'foo'), /must be a non-empty string/);
  });

  it('throws when value is an object', () => {
    assert.throws(() => requireString({ foo: {} }, 'foo'), /must be a non-empty string/);
  });

  it('throws when value is null', () => {
    assert.throws(() => requireString({ foo: null }, 'foo'), /must be a non-empty string/);
  });

  it('error message includes the key', () => {
    try {
      requireString({}, 'agent_name');
      assert.fail('should have thrown');
    } catch (err) {
      assert.match((err as Error).message, /agent_name/);
    }
  });
});

// ── safeISO ───────────────────────────────────────────────────────────────────

describe('safeISO', () => {
  it('formats a valid unix-millis timestamp', () => {
    // 2026-05-07T12:34:56.789Z (deterministic)
    const ms = Date.UTC(2026, 4, 7, 12, 34, 56, 789);
    const out = safeISO(ms);
    assert.equal(out, '2026-05-07T12:34:56.789Z');
  });

  it('handles 0 (the unix epoch)', () => {
    assert.equal(safeISO(0), '1970-01-01T00:00:00.000Z');
  });

  it('returns "(invalid timestamp)" for NaN', () => {
    assert.equal(safeISO(NaN), '(invalid timestamp)');
  });

  it('returns "(invalid timestamp)" for Infinity', () => {
    assert.equal(safeISO(Infinity), '(invalid timestamp)');
  });

  it('returns "(invalid timestamp)" for -Infinity', () => {
    assert.equal(safeISO(-Infinity), '(invalid timestamp)');
  });

  it('returns "(invalid timestamp)" for non-numbers', () => {
    // Forced through the type system to verify runtime defence.
    assert.equal(safeISO('not a number' as unknown as number), '(invalid timestamp)');
    assert.equal(safeISO(undefined as unknown as number), '(invalid timestamp)');
    assert.equal(safeISO(null as unknown as number), '(invalid timestamp)');
  });

  it('handles values outside JS Date range gracefully', () => {
    // JS Date range is roughly +/- 8.64e15 ms from epoch. Beyond that it
    // becomes "Invalid Date" (NaN getTime); we should never throw.
    const tooBig = 1e20;
    const out = safeISO(tooBig);
    assert.equal(out, '(invalid timestamp)');
  });

  it('handles MIN/MAX safe Date timestamps', () => {
    // Documented JS limits: ±8,640,000,000,000,000 ms.
    const MAX = 8_640_000_000_000_000;
    const MIN = -8_640_000_000_000_000;
    const maxOut = safeISO(MAX);
    const minOut = safeISO(MIN);
    assert.match(maxOut, /^\+?275760-/); // year 275760
    assert.match(minOut, /^-271821-/);   // year -271821
  });
});
