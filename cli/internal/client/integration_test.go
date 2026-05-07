package client_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestList(t *testing.T) {
	now := int64(1778154123456)
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
	sentAt := int64(1778154123456)
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

// ── WebSocket integration test ────────────────────────────────────────────────

var wsUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// sentAt is a helper to create a pointer to an int64.
func sentAtPtr(v int64) *int64 { return &v }

func TestWSFrames(t *testing.T) {
	// Build frames that the server will push.
	type Frame struct {
		Type      string `json:"type"`
		MessageID *int64 `json:"message_id,omitempty"`
		FromAgent string `json:"from_agent,omitempty"`
		Content   string `json:"content,omitempty"`
		SentAt    *int64 `json:"sent_at,omitempty"`
	}

	ts1 := int64(1778154100000)
	ts2 := int64(1778154200000)
	id1 := int64(1)
	id2 := int64(2)

	frames := []Frame{
		{Type: "history", MessageID: &id1, FromAgent: "alice", Content: "hello history", SentAt: &ts1},
		{Type: "history_done"},
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

	var buf strings.Builder
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Use a no-sleep sleep function and a short context.
	noSleep := func(d time.Duration) {
		// Don't actually sleep in tests.
	}

	wsc := client.NewWS(srv.URL, "test-token", noSleep)

	// Run ConnectWithReset — it will reconnect after close, but context
	// cancels it quickly.
	go func() {
		// Cancel after giving the client enough time to read frames.
		time.Sleep(200 * time.Millisecond)
		cancel()
	}()
	wsc.ConnectWithReset(ctx, &buf)

	output := buf.String()

	// Check all four expected output lines.
	t1 := time.UnixMilli(ts1).UTC().Format("2006-01-02 15:04:05")
	t2 := time.UnixMilli(ts2).UTC().Format("2006-01-02 15:04:05")

	expected := []string{
		"[history]  from alice at " + t1 + ": hello history",
		"-- end of history --",
		"[realtime] from bob at " + t2 + ": hello realtime",
	}

	for _, line := range expected {
		if !strings.Contains(output, line) {
			t.Errorf("output missing line %q\nfull output:\n%s", line, output)
		}
	}
}

// ── Reconnect test with fake backoff / sleep ──────────────────────────────────

func TestWSReconnect(t *testing.T) {
	// Count how many times the server receives a connection.
	connCount := 0
	done := make(chan struct{})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := wsUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		connCount++
		// Close immediately after first connection to force a reconnect.
		conn.Close()
		if connCount >= 2 {
			select {
			case done <- struct{}{}:
			default:
			}
		}
	}))
	defer srv.Close()

	// Track sleep calls to verify backoff is triggered.
	sleepCalls := []time.Duration{}
	fakeSleep := func(d time.Duration) {
		sleepCalls = append(sleepCalls, d)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	var buf strings.Builder
	wsc := client.NewWS(srv.URL, "test-token", fakeSleep)

	go func() {
		// Wait until we've seen 2 connections then cancel.
		select {
		case <-done:
		case <-ctx.Done():
		}
		cancel()
	}()

	wsc.ConnectWithReset(ctx, &buf)

	// Verify that at least one sleep/reconnect happened.
	if len(sleepCalls) == 0 {
		t.Error("expected at least one sleep call for reconnect, got zero")
	}

	// After a successful connect the backoff resets to 1s.
	// First sleep after reconnect should be 1s (reset happened).
	if len(sleepCalls) > 0 && sleepCalls[0] != 1*time.Second {
		t.Errorf("first sleep after reconnect: got %v want 1s (reset)", sleepCalls[0])
	}
}
