// Package server_test runs integration tests against an in-process Hertz server.
package server_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
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
	api.GET("/web/state", handlers.WebState(s, h))
	api.GET("/web/messages", handlers.WebMessages(s))
	api.POST("/web/msg/send", handlers.WebMsgSend(s, h))
	api.GET("/web/ws", handlers.WebWS(h))

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

func assertNoFrame(t *testing.T, conn *websocket.Conn, timeout time.Duration) {
	t.Helper()
	if err := conn.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		t.Fatalf("set read deadline: %v", err)
	}
	if _, msg, err := conn.ReadMessage(); err == nil {
		var f proto.Frame
		if err := json.Unmarshal(msg, &f); err != nil {
			t.Fatalf("unexpected non-json WS frame %q", msg)
		}
		t.Fatalf("unexpected WS frame: %+v", f)
	} else if netErr, ok := err.(net.Error); !ok || !netErr.Timeout() {
		t.Fatalf("expected timeout while checking for absent WS frame, got: %v", err)
	}
	if err := conn.SetReadDeadline(time.Time{}); err != nil {
		t.Fatalf("clear read deadline: %v", err)
	}
}

func sendUntilRealtime(t *testing.T, baseURL, token, toAgent, content string, conns ...*websocket.Conn) []proto.Frame {
	t.Helper()

	if len(conns) == 0 {
		return nil
	}

	type result struct {
		index int
		frame proto.Frame
		err   error
	}
	results := make(chan result, len(conns))
	for i, conn := range conns {
		if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
			t.Fatalf("set read deadline: %v", err)
		}
		go func(index int, c *websocket.Conn) {
			for {
				_, raw, err := c.ReadMessage()
				if err != nil {
					results <- result{index: index, err: err}
					return
				}
				var frame proto.Frame
				if err := json.Unmarshal(raw, &frame); err != nil {
					results <- result{index: index, err: fmt.Errorf("unmarshal frame %q: %w", raw, err)}
					return
				}
				if frame.Type == "realtime" && frame.Content == content {
					results <- result{index: index, frame: frame}
					return
				}
			}
		}(i, conn)
	}

	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	timeout := time.NewTimer(5 * time.Second)
	defer timeout.Stop()

	frames := make([]proto.Frame, len(conns))
	seen := make([]bool, len(conns))
	seenCount := 0
	for seenCount < len(conns) {
		doPost(t, baseURL+"/api/v1/msg/send", token, proto.SendRequest{
			ToAgent: toAgent,
			Content: content,
		})

		select {
		case res := <-results:
			if res.err != nil {
				t.Fatalf("read realtime frame: %v", res.err)
			}
			if !seen[res.index] {
				seen[res.index] = true
				frames[res.index] = res.frame
				seenCount++
			}
		case <-ticker.C:
		case <-timeout.C:
			t.Fatalf("timed out waiting for realtime frame %q", content)
		}
	}

	for _, conn := range conns {
		if err := conn.SetReadDeadline(time.Time{}); err != nil {
			t.Fatalf("clear read deadline: %v", err)
		}
	}
	return frames
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

// 6. WS connect: receives recent history as one batch, then realtime.
func TestWSHistoryAndRealtime(t *testing.T) {
	baseURL, cleanup := testServer(t)
	defer cleanup()

	tokenA := register(t, baseURL, "alice")
	tokenB := register(t, baseURL, "bob")

	// Alice sends six messages to Bob before Bob connects. Only the latest five
	// should be replayed, and they should arrive as a single history batch.
	for _, content := range []string{"msg1", "msg2", "msg3", "msg4", "msg5", "msg6"} {
		doPost(t, baseURL+"/api/v1/msg/send", tokenA, proto.SendRequest{ToAgent: "bob", Content: content})
	}

	// Bob connects to WS.
	conn := dialWS(t, baseURL, tokenB)
	defer conn.Close()

	// Expect one history batch: msg2..msg6.
	f1 := readFrame(t, conn)
	if f1.Type != "history_batch" {
		t.Fatalf("history frame: want history_batch, got %+v", f1)
	}
	if len(f1.Messages) != 5 {
		t.Fatalf("history batch len: got %d want 5 (%+v)", len(f1.Messages), f1.Messages)
	}
	for i, want := range []string{"msg2", "msg3", "msg4", "msg5", "msg6"} {
		if f1.Messages[i].Content != want {
			t.Fatalf("history batch message %d: got %q want %q", i, f1.Messages[i].Content, want)
		}
	}
	frames := sendUntilRealtime(t, baseURL, tokenA, "bob", "realtime-msg", conn)
	fr := frames[0]
	if fr.Type != "realtime" || fr.Content != "realtime-msg" {
		t.Fatalf("realtime frame: want realtime/realtime-msg, got %+v", fr)
	}
}

func TestWSDoesNotSendHistoryDone(t *testing.T) {
	baseURL, cleanup := testServer(t)
	defer cleanup()

	tokenA := register(t, baseURL, "alice")
	tokenB := register(t, baseURL, "bob")

	doPost(t, baseURL+"/api/v1/msg/send", tokenA, proto.SendRequest{ToAgent: "bob", Content: "history-only"})

	conn := dialWS(t, baseURL, tokenB)
	defer conn.Close()

	f := readFrame(t, conn)
	if f.Type != "history_batch" || len(f.Messages) != 1 || f.Messages[0].Content != "history-only" {
		t.Fatalf("history replay: want one history_batch, got %+v", f)
	}
	assertNoFrame(t, conn, 100*time.Millisecond)
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

	frames := sendUntilRealtime(t, baseURL, tokenA, "bob", "fanout", conn1, conn2)
	f1 := frames[0]
	f2 := frames[1]

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
	if f.Type != "history_batch" || len(f.Messages) != 1 || f.Messages[0].Content != "offline-msg" {
		t.Fatalf("offline→online: want history_batch/offline-msg, got %+v", f)
	}
}

func TestWebSystemSendAndMessagePagination(t *testing.T) {
	baseURL, cleanup := testServer(t)
	defer cleanup()

	bobToken := register(t, baseURL, "bob")
	conn := dialWS(t, baseURL, bobToken)
	defer conn.Close()

	resp := doPost(t, baseURL+"/api/v1/web/msg/send", "", proto.WebSendRequest{
		ToAgent: "bob",
		Content: "hello from dashboard",
	})
	if resp.StatusCode != 200 {
		t.Fatalf("web system send: want 200, got %d", resp.StatusCode)
	}
	var send proto.SendResponse
	decodeBody(t, resp, &send)
	if send.MessageID == 0 {
		t.Fatal("web system send returned empty message id")
	}

	frame := readFrame(t, conn)
	if frame.Type != "realtime" || frame.FromAgent != "system" || frame.Content != "hello from dashboard" {
		t.Fatalf("unexpected realtime frame: %+v", frame)
	}

	resp = doGet(t, baseURL+"/api/v1/web/messages", "")
	if resp.StatusCode != 200 {
		t.Fatalf("web messages: want 200, got %d", resp.StatusCode)
	}
	var messages proto.WebMessagesResponse
	decodeBody(t, resp, &messages)
	if len(messages.Messages) != 1 {
		t.Fatalf("web messages: want 1, got %d", len(messages.Messages))
	}
	got := messages.Messages[0]
	if got.FromAgent != "system" || got.ToAgent != "bob" || got.Content != "hello from dashboard" {
		t.Fatalf("unexpected web message: %+v", got)
	}
}

func TestWebMessagesIncludeSafeMarkdownHTML(t *testing.T) {
	baseURL, cleanup := testServer(t)
	defer cleanup()

	_ = register(t, baseURL, "bob")
	content := "**bold**\n\n- item\n\n<script>alert(1)</script>"

	resp := doPost(t, baseURL+"/api/v1/web/msg/send", "", proto.WebSendRequest{
		ToAgent: "bob",
		Content: content,
	})
	if resp.StatusCode != 200 {
		t.Fatalf("web system send: want 200, got %d", resp.StatusCode)
	}

	resp = doGet(t, baseURL+"/api/v1/web/messages", "")
	if resp.StatusCode != 200 {
		t.Fatalf("web messages: want 200, got %d", resp.StatusCode)
	}
	var messages proto.WebMessagesResponse
	decodeBody(t, resp, &messages)
	if len(messages.Messages) != 1 {
		t.Fatalf("web messages: want 1, got %d", len(messages.Messages))
	}
	got := messages.Messages[0]
	if got.Content != content {
		t.Fatalf("web message should keep raw content, got %q", got.Content)
	}
	if !strings.Contains(got.ContentHTML, "<strong>bold</strong>") || !strings.Contains(got.ContentHTML, "<li>item</li>") {
		t.Fatalf("web message should include rendered markdown HTML, got %q", got.ContentHTML)
	}
	if strings.Contains(strings.ToLower(got.ContentHTML), "<script") {
		t.Fatalf("web message markdown HTML should not emit raw script tags, got %q", got.ContentHTML)
	}
}

func TestWebWSOriginCheck(t *testing.T) {
	baseURL, cleanup := testServer(t)
	defer cleanup()

	wsURL := "ws" + baseURL[4:] + "/api/v1/web/ws"
	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, http.Header{
		"Origin": {"http://evil.example"},
	})
	if err == nil {
		conn.Close()
		t.Fatal("cross-origin web websocket unexpectedly connected")
	}
	if resp == nil || resp.StatusCode != http.StatusForbidden {
		if resp == nil {
			t.Fatal("cross-origin web websocket: missing response")
		}
		t.Fatalf("cross-origin web websocket: want 403, got %d", resp.StatusCode)
	}

	conn, resp, err = websocket.DefaultDialer.Dial(wsURL, http.Header{
		"Origin": {baseURL},
	})
	if err != nil {
		if resp != nil {
			t.Fatalf("same-origin web websocket: %v (status %d)", err, resp.StatusCode)
		}
		t.Fatalf("same-origin web websocket: %v", err)
	}
	conn.Close()
}

func TestWebStateShowsOnlineAgentsAndRecentEdges(t *testing.T) {
	baseURL, cleanup := testServer(t)
	defer cleanup()

	aliceToken := register(t, baseURL, "alice")
	bobToken := register(t, baseURL, "bob")
	aliceConn := dialWS(t, baseURL, aliceToken)
	defer aliceConn.Close()
	bobConn := dialWS(t, baseURL, bobToken)
	defer bobConn.Close()

	resp := doPost(t, baseURL+"/api/v1/msg/send", aliceToken, proto.SendRequest{ToAgent: "bob", Content: "edge"})
	if resp.StatusCode != 200 {
		t.Fatalf("send edge: want 200, got %d", resp.StatusCode)
	}
	_ = readFrame(t, bobConn)

	resp = doGet(t, baseURL+"/api/v1/web/state", "")
	if resp.StatusCode != 200 {
		t.Fatalf("web state: want 200, got %d", resp.StatusCode)
	}
	var state proto.WebStateResponse
	decodeBody(t, resp, &state)
	if len(state.Agents) != 2 {
		t.Fatalf("web state agents: want 2, got %d (%+v)", len(state.Agents), state.Agents)
	}
	found := false
	for _, edge := range state.Edges {
		if edge.FromAgent == "alice" && edge.ToAgent == "bob" {
			found = true
		}
	}
	if !found {
		t.Fatalf("web state missing alice -> bob edge: %+v", state.Edges)
	}
}
