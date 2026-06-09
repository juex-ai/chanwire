package mcp

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/juex-ai/chanwire/cli/internal/client"
)

func TestConnectionLifecycleSerializesConcurrentResetAndStop(t *testing.T) {
	t.Setenv("CHANWIRE_DIR", t.TempDir())
	t.Setenv("CHANWIRE_ENDPOINT", "http://127.0.0.1:1")

	srv := NewServer("test", false)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	srv.mu.Lock()
	srv.runCtx = ctx
	srv.mu.Unlock()

	var wg sync.WaitGroup
	for range 8 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			srv.resetConnect(ctx)
		}()
		go func() {
			defer wg.Done()
			srv.stopConnect()
		}()
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("connection lifecycle operations did not finish")
	}

	cancel()
	srv.stopConnect()
}

func TestForceChannelAllowsMissingClientCapability(t *testing.T) {
	if NewServer("test", false).channelAllowed(nil) {
		t.Fatal("channel should not be allowed without capability or force flag")
	}
	if !NewServer("test", true).channelAllowed(nil) {
		t.Fatal("forced channel flag should allow channel notifications without client capability")
	}
}

func TestMessageTimeFormattingUsesLocalTimezone(t *testing.T) {
	origLocal := time.Local
	time.Local = time.FixedZone("client-test", 8*60*60)
	t.Cleanup(func() { time.Local = origLocal })

	sec := int64(1778154123)
	got := safeFrameTS(&sec)
	if got != "2026-05-07 19:42:03" {
		t.Fatalf("safeFrameTS should format unix seconds in local time, got %q", got)
	}

	if got := safeISO(sec); got != "2026-05-07T19:42:03+08:00" {
		t.Fatalf("safeISO should format unix seconds in local time, got %q", got)
	}
}

func TestSystemNoReplyFormatting(t *testing.T) {
	sec := int64(1778154123)
	frame := &client.Frame{
		Type:      "realtime",
		FromAgent: "SYSTEM",
		Content:   "hello",
		SentAt:    &sec,
	}
	got := formatFrame("realtime", frame)
	for _, want := range []string{"from SYSTEM (noreply:", "system messages cannot be replied to", "user's own communication channel"} {
		if !strings.Contains(got, want) {
			t.Fatalf("system frame should contain %q, got %q", want, got)
		}
	}

	batch := formatHistoryBatch([]client.HistoryMessage{{
		FromAgent: "System",
		Content:   "history",
		SentAt:    sec,
	}})
	if !strings.Contains(batch, "from System (noreply:") {
		t.Fatalf("system history should include noreply hint, got %q", batch)
	}
}
