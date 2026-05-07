// Package backoff implements the reconnect backoff sequence defined in the spec:
// 1, 5, 15, 30, 60, 120 seconds, then capped at 120.
package backoff

import "time"

// sequence is the ordered list of backoff durations.
var sequence = []time.Duration{
	1 * time.Second,
	5 * time.Second,
	15 * time.Second,
	30 * time.Second,
	60 * time.Second,
	120 * time.Second,
}

// Backoff iterates through the reconnect delay sequence.
type Backoff struct {
	idx int
}

// New returns a fresh Backoff starting at the beginning of the sequence.
func New() *Backoff {
	return &Backoff{}
}

// Next returns the next delay in the sequence. After the last element,
// it always returns 120 seconds.
func (b *Backoff) Next() time.Duration {
	if b.idx >= len(sequence) {
		return sequence[len(sequence)-1]
	}
	d := sequence[b.idx]
	b.idx++
	return d
}

// Reset restarts the sequence from the beginning. Call this after a
// successful WebSocket handshake (101).
func (b *Backoff) Reset() {
	b.idx = 0
}
