package mcp

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestConnectionLifecycleSerializesConcurrentResetAndStop(t *testing.T) {
	t.Setenv("CHANWIRE_DIR", t.TempDir())
	t.Setenv("CHANWIRE_ENDPOINT", "http://127.0.0.1:1")

	srv := NewServer("test")
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
