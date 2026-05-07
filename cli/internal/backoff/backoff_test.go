package backoff_test

import (
	"testing"
	"time"

	"github.com/juex-ai/chanwire/cli/internal/backoff"
)

func TestSequence(t *testing.T) {
	expected := []time.Duration{
		1 * time.Second,
		5 * time.Second,
		15 * time.Second,
		30 * time.Second,
		60 * time.Second,
		120 * time.Second,
	}

	b := backoff.New()
	for i, want := range expected {
		got := b.Next()
		if got != want {
			t.Errorf("step %d: got %v want %v", i, got, want)
		}
	}

	// After the last element, must stay at 120s.
	for i := 0; i < 5; i++ {
		got := b.Next()
		if got != 120*time.Second {
			t.Errorf("cap step %d: got %v want 120s", i, got)
		}
	}
}

func TestReset(t *testing.T) {
	b := backoff.New()

	// Exhaust the sequence.
	for i := 0; i < 8; i++ {
		b.Next()
	}

	b.Reset()

	// Should restart at 1s.
	got := b.Next()
	if got != 1*time.Second {
		t.Errorf("after Reset, first Next: got %v want 1s", got)
	}

	// Second call should be 5s.
	got = b.Next()
	if got != 5*time.Second {
		t.Errorf("after Reset, second Next: got %v want 5s", got)
	}
}
