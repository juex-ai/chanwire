package client_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/juex-ai/chanwire/cli/internal/client"
)

// ── HTTP integration tests ────────────────────────────────────────────────────

func TestRegister(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/agent/register" {
			http.NotFound(w, r)
			return
		}
		var req struct {
			AgentName string `json:"agent_name"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"agent_name": req.AgentName,
			"token":      "test-token",
		})
	}))
	defer srv.Close()

	hc := client.NewHTTP(srv.URL, "")
	resp, err := hc.Register("alice")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if resp.AgentName != "alice" {
		t.Errorf("agent_name: got %q want %q", resp.AgentName, "alice")
	}
	if resp.Token != "test-token" {
		t.Errorf("token: got %q want %q", resp.Token, "test-token")
	}
}

// TestRegisterTrailingSlash verifies that a base URL with a trailing slash
// is normalized — no double slash in the request path.
func TestRegisterTrailingSlash(t *testing.T) {
	gotPath := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"agent_name": "alice",
			"token":      "tok",
		})
	}))
	defer srv.Close()

	hc := client.NewHTTP(srv.URL+"/", "")
	if _, err := hc.Register("alice"); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if gotPath != "/api/v1/agent/register" {
		t.Errorf("path: got %q want %q", gotPath, "/api/v1/agent/register")
	}
}

func TestList(t *testing.T) {
	now := int64(1778154123)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/agent/list" {
			http.NotFound(w, r)
			return
		}
		// Verify auth header.
		auth := r.Header.Get("Authorization")
		if auth != "Bearer test-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"agents": []map[string]interface{}{
				{"agent_name": "alice", "last_active_at": now},
				{"agent_name": "bob", "last_active_at": nil},
			},
		})
	}))
	defer srv.Close()

	hc := client.NewHTTP(srv.URL, "test-token")
	resp, err := hc.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(resp.Agents) != 2 {
		t.Fatalf("agents count: got %d want 2", len(resp.Agents))
	}
	if resp.Agents[0].AgentName != "alice" {
		t.Errorf("first agent: got %q want %q", resp.Agents[0].AgentName, "alice")
	}
	if resp.Agents[0].LastActiveAt == nil || *resp.Agents[0].LastActiveAt != now {
		t.Errorf("alice last_active_at: unexpected value")
	}
	if resp.Agents[1].LastActiveAt != nil {
		t.Errorf("bob last_active_at: expected nil, got %v", resp.Agents[1].LastActiveAt)
	}
}

func TestSendOK(t *testing.T) {
	sentAt := int64(1778154123)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/msg/send" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"message_id": 1,
			"sent_at":    sentAt,
		})
	}))
	defer srv.Close()

	hc := client.NewHTTP(srv.URL, "test-token")
	resp, err := hc.Send("bob", "hello")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if resp.MessageID != 1 {
		t.Errorf("message_id: got %d want 1", resp.MessageID)
	}
}

func TestSend404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"unknown agent"}`, http.StatusNotFound)
	}))
	defer srv.Close()

	hc := client.NewHTTP(srv.URL, "test-token")
	_, err := hc.Send("ghost", "hello")
	if err == nil {
		t.Fatal("expected error on 404, got nil")
	}
	var unknownErr *client.ErrUnknownAgent
	ok := false
	if uErr, isUnknown := err.(*client.ErrUnknownAgent); isUnknown {
		unknownErr = uErr
		ok = true
	}
	if !ok || unknownErr == nil {
		t.Errorf("expected *ErrUnknownAgent, got %T: %v", err, err)
	}
}

func TestSendSystemAgentRejectedBeforeHTTP(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		http.Error(w, "unexpected request", http.StatusInternalServerError)
	}))
	defer srv.Close()

	hc := client.NewHTTP(srv.URL, "test-token")
	_, err := hc.Send("System", "hello")
	if err == nil {
		t.Fatal("expected error when sending to system, got nil")
	}
	var systemErr *client.ErrSystemAgent
	if !errors.As(err, &systemErr) {
		t.Fatalf("expected *ErrSystemAgent, got %T: %v", err, err)
	}
	for _, want := range []string{"cannot send to system", "noreply", "user's own communication channel"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("system send error should contain %q, got %q", want, err.Error())
		}
	}
	if called {
		t.Fatal("client should reject system sends before making an HTTP request")
	}
}

// ── WebSocket integration test ────────────────────────────────────────────────

var wsUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// noWait is a Waiter that returns immediately, respecting ctx cancellation.
func noWait(ctx context.Context, _ time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}

func TestWSFrames(t *testing.T) {
	origLocal := time.Local
	time.Local = time.FixedZone("client-test", 8*60*60)
	t.Cleanup(func() { time.Local = origLocal })

	type Frame struct {
		Type      string `json:"type"`
		MessageID *int64 `json:"message_id,omitempty"`
		FromAgent string `json:"from_agent,omitempty"`
		Content   string `json:"content,omitempty"`
		SentAt    *int64 `json:"sent_at,omitempty"`
		NoReply   bool   `json:"noreply,omitempty"`
		Messages  []struct {
			MessageID *int64 `json:"message_id,omitempty"`
			FromAgent string `json:"from_agent,omitempty"`
			Content   string `json:"content,omitempty"`
			SentAt    *int64 `json:"sent_at,omitempty"`
			NoReply   bool   `json:"noreply,omitempty"`
		} `json:"messages,omitempty"`
	}

	ts1 := int64(1778154100)
	ts2 := int64(1778154200)
	id1 := int64(1)
	id2 := int64(2)

	frames := []Frame{
		{Type: "history_batch", Messages: []struct {
			MessageID *int64 `json:"message_id,omitempty"`
			FromAgent string `json:"from_agent,omitempty"`
			Content   string `json:"content,omitempty"`
			SentAt    *int64 `json:"sent_at,omitempty"`
			NoReply   bool   `json:"noreply,omitempty"`
		}{
			{MessageID: &id1, FromAgent: "alice", Content: "hello history", SentAt: &ts1},
			{MessageID: &id2, FromAgent: "system", Content: "hello system history", SentAt: &ts2, NoReply: true},
		}},
		{Type: "realtime", MessageID: &id2, FromAgent: "bob", Content: "hello realtime", SentAt: &ts2},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := wsUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for _, f := range frames {
			data, _ := json.Marshal(f)
			conn.WriteMessage(websocket.TextMessage, data)
		}
		// Keep connection alive briefly so client can read all frames.
		time.Sleep(50 * time.Millisecond)
	}))
	defer srv.Close()

	// bufWriter is goroutine-safe; the watcher goroutine in runOnce may
	// outrace the read loop in some edge cases, so guard concurrent writes.
	buf := &syncBuffer{}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	wsc := client.NewWS(srv.URL, "test-token", noWait)

	go func() {
		// Cancel after giving the client enough time to read frames.
		time.Sleep(200 * time.Millisecond)
		cancel()
	}()
	wsc.ConnectWithReset(ctx, buf)

	output := buf.String()

	t1 := time.Unix(ts1, 0).Local().Format("2006-01-02 15:04:05")
	t2 := time.Unix(ts2, 0).Local().Format("2006-01-02 15:04:05")

	expected := []string{
		"-- history batch (one-time review, 2 messages) --",
		"[history]  from alice at " + t1 + ": hello history",
		"[history]  from system (noreply: system messages cannot be replied to; if you need to contact the user, use the user's own communication channel) at " + t2 + ": hello system history",
		"-- end history batch --",
		"[realtime] from bob at " + t2 + ": hello realtime",
	}

	for _, line := range expected {
		if !strings.Contains(output, line) {
			t.Errorf("output missing line %q\nfull output:\n%s", line, output)
		}
	}
	if strings.Contains(output, "-- end of history --") {
		t.Errorf("extra history-end marker should not be printed\nfull output:\n%s", output)
	}
}

// syncBuffer is a goroutine-safe bytes accumulator used in WS tests.
type syncBuffer struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// ── Reconnect test with fake waiter ───────────────────────────────────────────

func TestWSReconnect(t *testing.T) {
	var connCount atomic.Int32
	done := make(chan struct{})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := wsUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		// Close immediately after handshake to force a reconnect.
		conn.Close()
		if connCount.Add(1) >= 2 {
			select {
			case done <- struct{}{}:
			default:
			}
		}
	}))
	defer srv.Close()

	// Track waiter calls under a mutex.
	var (
		waitMu    sync.Mutex
		waitCalls []time.Duration
	)
	fakeWait := func(ctx context.Context, d time.Duration) error {
		waitMu.Lock()
		waitCalls = append(waitCalls, d)
		waitMu.Unlock()
		// Honour ctx cancellation but otherwise return immediately.
		if err := ctx.Err(); err != nil {
			return err
		}
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	buf := &syncBuffer{}
	wsc := client.NewWS(srv.URL, "test-token", fakeWait)

	go func() {
		select {
		case <-done:
		case <-ctx.Done():
		}
		cancel()
	}()

	wsc.ConnectWithReset(ctx, buf)

	waitMu.Lock()
	defer waitMu.Unlock()

	if len(waitCalls) == 0 {
		t.Fatal("expected at least one wait call for reconnect, got zero")
	}

	// First wait after a successful 101 handshake must be 1s (backoff reset).
	if waitCalls[0] != 1*time.Second {
		t.Errorf("first wait after handshake: got %v want 1s (reset)", waitCalls[0])
	}
}
