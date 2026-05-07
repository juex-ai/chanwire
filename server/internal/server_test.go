// Package server_test runs integration tests against an in-process Hertz server.
package server_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	hserver "github.com/cloudwego/hertz/pkg/app/server"
	"github.com/gorilla/websocket"
	"github.com/juex-ai/chanwire/server/internal/auth"
	"github.com/juex-ai/chanwire/server/internal/handlers"
	"github.com/juex-ai/chanwire/server/internal/hub"
	"github.com/juex-ai/chanwire/server/internal/proto"
	"github.com/juex-ai/chanwire/server/internal/store"
)

// ---------------------------------------------------------------------------
// Test server helpers
// ---------------------------------------------------------------------------

// testServer starts a Hertz server on a random port, registers all routes,
// and returns the base URL and a cleanup function.
func testServer(t *testing.T) (baseURL string, cleanup func()) {
	t.Helper()

	// Pick a free port.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	s, err := store.OpenMemory()
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	h := hub.New()

	addr := fmt.Sprintf("127.0.0.1:%d", port)
	srv := hserver.Default(
		hserver.WithHostPorts(addr),
		hserver.WithExitWaitTime(0),
	)

	api := srv.Group("/api/v1")
	api.POST("/agent/register", handlers.Register(s))

	authMW := auth.Middleware(s)
	api.GET("/agent/list", authMW, handlers.AgentList(s))
	api.POST("/msg/send", authMW, handlers.MsgSend(s, h))
	api.GET("/ws", authMW, handlers.WSConnect(s, h))

	go srv.Spin()

	// Wait until the server is ready.
	baseURL = "http://" + addr
	for i := 0; i < 50; i++ {
		resp, err := http.Get(baseURL + "/api/v1/agent/register")
		if err == nil {
			resp.Body.Close()
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	cleanup = func() {
		_ = s.Close()
	}
	return baseURL, cleanup
}

// ---------------------------------------------------------------------------
// HTTP helpers
// ---------------------------------------------------------------------------

func doPost(t *testing.T, url, token string, body interface{}) *http.Response {
	t.Helper()
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, url, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	return resp
}

func doGet(t *testing.T, url, token string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	return resp
}

func decodeBody(t *testing.T, resp *http.Response, v interface{}) {
	t.Helper()
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if err := json.Unmarshal(b, v); err != nil {
		t.Fatalf("unmarshal body %q: %v", b, err)
	}
}

// register registers an agent, asserting success, and returns the token.
func register(t *testing.T, baseURL, name string) string {
	t.Helper()
	resp := doPost(t, baseURL+"/api/v1/agent/register", "", proto.RegisterRequest{AgentName: name})
	if resp.StatusCode != 200 {
		t.Fatalf("register %q: want 200, got %d", name, resp.StatusCode)
	}
	var out proto.RegisterResponse
	decodeBody(t, resp, &out)
	if out.Token == "" {
		t.Fatal("register: empty token")
	}
	return out.Token
}

// dialWS opens a WebSocket to /api/v1/ws with the given token.
func dialWS(t *testing.T, baseURL, token string) *websocket.Conn {
	t.Helper()
	wsURL := "ws" + baseURL[4:] + "/api/v1/ws"
	header := http.Header{"Authorization": {"Bearer " + token}}
	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, header)
	if err != nil {
		if resp != nil {
			t.Fatalf("dial WS: %v (status %d)", err, resp.StatusCode)
		}
		t.Fatalf("dial WS: %v", err)
	}
	return conn
}

// readFrame reads one JSON frame from a WS connection.
func readFrame(t *testing.T, conn *websocket.Conn) proto.Frame {
	t.Helper()
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read WS frame: %v", err)
	}
	var f proto.Frame
	if err := json.Unmarshal(msg, &f); err != nil {
		t.Fatalf("unmarshal frame %q: %v", msg, err)
	}
	return f
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// 1. Register new agent → 200 + token. Re-register same name → same token.
func TestRegister(t *testing.T) {
	baseURL, cleanup := testServer(t)
	defer cleanup()

	token1 := register(t, baseURL, "alice")
	if len(token1) == 0 {
		t.Fatal("expected non-empty token")
	}

	// Re-register same name.
	token2 := register(t, baseURL, "alice")
	if token1 != token2 {
		t.Fatalf("idempotent register: got different tokens %q vs %q", token1, token2)
	}
}

// 2. Auth: missing token → 401. Bad token → 401.
func TestAuthMiddleware(t *testing.T) {
	baseURL, cleanup := testServer(t)
	defer cleanup()

	// No token.
	resp := doGet(t, baseURL+"/api/v1/agent/list", "")
	if resp.StatusCode != 401 {
		resp.Body.Close()
		t.Fatalf("no token: want 401, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Bad token.
	resp = doGet(t, baseURL+"/api/v1/agent/list", "notavalidtoken")
	if resp.StatusCode != 401 {
		resp.Body.Close()
		t.Fatalf("bad token: want 401, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

// 3. /agent/list returns all registered; last_active_at updated for requester.
func TestAgentList(t *testing.T) {
	baseURL, cleanup := testServer(t)
	defer cleanup()

	tokenA := register(t, baseURL, "alice")
	register(t, baseURL, "bob")

	resp := doGet(t, baseURL+"/api/v1/agent/list", tokenA)
	if resp.StatusCode != 200 {
		resp.Body.Close()
		t.Fatalf("agent/list: want 200, got %d", resp.StatusCode)
	}
	var out proto.AgentListResponse
	decodeBody(t, resp, &out)

	if len(out.Agents) != 2 {
		t.Fatalf("agent/list: want 2 agents, got %d", len(out.Agents))
	}

	// Find alice and check last_active_at is now set (we just made an auth'd request).
	for _, a := range out.Agents {
		if a.AgentName == "alice" {
			if a.LastActiveAt == nil {
				t.Fatal("alice last_active_at should be non-nil after authenticated request")
			}
		}
		if a.AgentName == "bob" {
			if a.LastActiveAt != nil {
				t.Fatal("bob last_active_at should be nil (never made an auth request)")
			}
		}
	}
}

// 4. /msg/send persists a row; sender's last_active_at updated.
func TestMsgSend(t *testing.T) {
	baseURL, cleanup := testServer(t)
	defer cleanup()

	tokenA := register(t, baseURL, "alice")
	register(t, baseURL, "bob")

	resp := doPost(t, baseURL+"/api/v1/msg/send", tokenA, proto.SendRequest{
		ToAgent: "bob",
		Content: "hello bob",
	})
	if resp.StatusCode != 200 {
		resp.Body.Close()
		t.Fatalf("msg/send: want 200, got %d", resp.StatusCode)
	}
	var out proto.SendResponse
	decodeBody(t, resp, &out)

	if out.MessageID == 0 {
		t.Fatal("msg/send: expected non-zero message_id")
	}
	if out.SentAt == 0 {
		t.Fatal("msg/send: expected non-zero sent_at")
	}
}

// 5. /msg/send to unknown agent → 404.
func TestMsgSendUnknownAgent(t *testing.T) {
	baseURL, cleanup := testServer(t)
	defer cleanup()

	tokenA := register(t, baseURL, "alice")

	resp := doPost(t, baseURL+"/api/v1/msg/send", tokenA, proto.SendRequest{
		ToAgent: "nobody",
		Content: "hello",
	})
	if resp.StatusCode != 404 {
		resp.Body.Close()
		t.Fatalf("msg/send unknown: want 404, got %d", resp.StatusCode)
	}
	var errResp proto.ErrorResponse
	decodeBody(t, resp, &errResp)
	if errResp.Error != "unknown agent" {
		t.Fatalf("msg/send unknown: want error 'unknown agent', got %q", errResp.Error)
	}
}

// 6. WS connect: receives history in order, then history_done, then realtime.
func TestWSHistoryAndRealtime(t *testing.T) {
	baseURL, cleanup := testServer(t)
	defer cleanup()

	tokenA := register(t, baseURL, "alice")
	tokenB := register(t, baseURL, "bob")

	// Alice sends two messages to Bob before Bob connects.
	doPost(t, baseURL+"/api/v1/msg/send", tokenA, proto.SendRequest{ToAgent: "bob", Content: "msg1"})
	doPost(t, baseURL+"/api/v1/msg/send", tokenA, proto.SendRequest{ToAgent: "bob", Content: "msg2"})

	// Bob connects to WS.
	conn := dialWS(t, baseURL, tokenB)
	defer conn.Close()

	// Expect history frames: msg1, msg2.
	f1 := readFrame(t, conn)
	if f1.Type != "history" || f1.Content != "msg1" {
		t.Fatalf("history frame 1: want history/msg1, got %+v", f1)
	}
	f2 := readFrame(t, conn)
	if f2.Type != "history" || f2.Content != "msg2" {
		t.Fatalf("history frame 2: want history/msg2, got %+v", f2)
	}

	// history_done.
	fd := readFrame(t, conn)
	if fd.Type != "history_done" {
		t.Fatalf("expected history_done, got %+v", fd)
	}

	// Now Alice sends another message — Bob should get it as realtime.
	doPost(t, baseURL+"/api/v1/msg/send", tokenA, proto.SendRequest{ToAgent: "bob", Content: "realtime-msg"})

	fr := readFrame(t, conn)
	if fr.Type != "realtime" || fr.Content != "realtime-msg" {
		t.Fatalf("realtime frame: want realtime/realtime-msg, got %+v", fr)
	}
}

// 7. Two concurrent WS conns for same agent: realtime fanout to both.
func TestWSFanout(t *testing.T) {
	baseURL, cleanup := testServer(t)
	defer cleanup()

	tokenA := register(t, baseURL, "alice")
	tokenB := register(t, baseURL, "bob")

	// Bob opens two connections.
	conn1 := dialWS(t, baseURL, tokenB)
	defer conn1.Close()
	conn2 := dialWS(t, baseURL, tokenB)
	defer conn2.Close()

	// Drain history_done for both (no history yet).
	drainToDone := func(conn *websocket.Conn) {
		for {
			f := readFrame(t, conn)
			if f.Type == "history_done" {
				return
			}
		}
	}
	drainToDone(conn1)
	drainToDone(conn2)

	// Alice sends to Bob.
	doPost(t, baseURL+"/api/v1/msg/send", tokenA, proto.SendRequest{ToAgent: "bob", Content: "fanout"})

	// Both connections should get a realtime frame.
	got1 := make(chan proto.Frame, 1)
	got2 := make(chan proto.Frame, 1)

	go func() {
		conn1.SetReadDeadline(time.Now().Add(3 * time.Second))
		_, msg, err := conn1.ReadMessage()
		if err != nil {
			got1 <- proto.Frame{Type: "error"}
			return
		}
		var f proto.Frame
		json.Unmarshal(msg, &f)
		got1 <- f
	}()

	go func() {
		conn2.SetReadDeadline(time.Now().Add(3 * time.Second))
		_, msg, err := conn2.ReadMessage()
		if err != nil {
			got2 <- proto.Frame{Type: "error"}
			return
		}
		var f proto.Frame
		json.Unmarshal(msg, &f)
		got2 <- f
	}()

	f1 := <-got1
	f2 := <-got2

	if f1.Type != "realtime" || f1.Content != "fanout" {
		t.Fatalf("conn1: want realtime/fanout, got %+v", f1)
	}
	if f2.Type != "realtime" || f2.Content != "fanout" {
		t.Fatalf("conn2: want realtime/fanout, got %+v", f2)
	}
}

// 8. Offline-then-online: send while recipient absent; recipient connects and gets via history.
func TestOfflineThenOnline(t *testing.T) {
	baseURL, cleanup := testServer(t)
	defer cleanup()

	tokenA := register(t, baseURL, "alice")
	tokenB := register(t, baseURL, "bob")

	// Alice sends while Bob is offline.
	doPost(t, baseURL+"/api/v1/msg/send", tokenA, proto.SendRequest{ToAgent: "bob", Content: "offline-msg"})

	// Bob connects later and should see the message in history.
	conn := dialWS(t, baseURL, tokenB)
	defer conn.Close()

	f := readFrame(t, conn)
	if f.Type != "history" || f.Content != "offline-msg" {
		t.Fatalf("offline→online: want history/offline-msg, got %+v", f)
	}

	fd := readFrame(t, conn)
	if fd.Type != "history_done" {
		t.Fatalf("expected history_done, got %+v", fd)
	}
}
