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
