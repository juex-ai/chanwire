package mcp_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestMCPServerStdioE2E(t *testing.T) {
	fake := newFakeChanwire(t)
	aliceToken := fake.registerHTTP(t, "alice")

	mcp := startMCPServer(t, fake.URL(), t.TempDir())

	mcp.send(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{},
			"clientInfo": map[string]any{
				"name":    "chanwire-e2e",
				"version": "test",
			},
		},
	})
	assertNoRPCError(t, mcp.waitResponse(1))

	mcp.send(map[string]any{
		"jsonrpc": "2.0",
		"method":  "notifications/initialized",
		"params":  map[string]any{},
	})

	notRegistered := mcp.waitNotification("notifications/claude/channel", func(params map[string]any) bool {
		return params["content"] == "chanwire: agent not registered. Use the chanwire_register_agent tool to register, then messages will stream automatically."
	})
	assertChannelEvent(t, notRegistered, "not_registered", "not registered")

	mcp.send(map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/list",
		"params":  map[string]any{},
	})
	toolsResp := mcp.waitResponse(2)
	assertNoRPCError(t, toolsResp)
	assertToolNames(t, toolsResp, []string{
		"chanwire_register_agent",
		"chanwire_list_agents",
		"chanwire_send_msg",
	})

	callTool(t, mcp, 3, "chanwire_register_agent", map[string]any{"agent_name": "bob"})
	registerResp := mcp.waitResponse(3)
	assertNoRPCError(t, registerResp)
	assertToolTextContains(t, registerResp, "registered: agent_name=bob")
	fake.waitForWS(t, "bob")

	callTool(t, mcp, 4, "chanwire_list_agents", map[string]any{})
	listResp := mcp.waitResponse(4)
	assertNoRPCError(t, listResp)
	assertToolTextContains(t, listResp, "alice")
	assertToolTextContains(t, listResp, "bob")

	callTool(t, mcp, 5, "chanwire_send_msg", map[string]any{
		"to_agent": "alice",
		"content":  "hello from bob",
	})
	sendResp := mcp.waitResponse(5)
	assertNoRPCError(t, sendResp)
	assertToolTextContains(t, sendResp, "ok: message_id=")

	fake.sendHTTP(t, aliceToken, "bob", "hello bob realtime")
	msgNotification := mcp.waitNotification("notifications/claude/channel", func(params map[string]any) bool {
		content, _ := params["content"].(string)
		return strings.Contains(content, "[realtime] from alice") &&
			strings.Contains(content, "hello bob realtime")
	})
	assertChannelEvent(t, msgNotification, "message", "hello bob realtime")
}

func callTool(t *testing.T, c *stdioMCP, id int, name string, args map[string]any) {
	t.Helper()
	c.send(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      name,
			"arguments": args,
		},
	})
}

type rpcMessage map[string]any

type stdioMCP struct {
	t       *testing.T
	cancel  context.CancelFunc
	stdin   io.WriteCloser
	msgs    chan rpcMessage
	done    chan error
	stderr  safeBuffer
	backlog []rpcMessage
}

func startMCPServer(t *testing.T, endpoint, dataDir string) *stdioMCP {
	t.Helper()

	cliRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve cli root: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, "go", "run", "./cmd/chanwire", "mcp")
	cmd.Dir = cliRoot
	cmd.Env = append(os.Environ(),
		"CHANWIRE_ENDPOINT="+endpoint,
		"CHANWIRE_DIR="+dataDir,
	)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		t.Fatalf("stdin pipe: %v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		t.Fatalf("stdout pipe: %v", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		t.Fatalf("stderr pipe: %v", err)
	}

	c := &stdioMCP{
		t:      t,
		cancel: cancel,
		stdin:  stdin,
		msgs:   make(chan rpcMessage, 32),
		done:   make(chan error, 1),
	}

	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatalf("start MCP server: %v", err)
	}

	go c.readStdout(stdout)
	go c.readStderr(stderr)
	go func() {
		c.done <- cmd.Wait()
		close(c.done)
	}()

	t.Cleanup(func() {
		_ = stdin.Close()
		cancel()
		select {
		case <-c.done:
		case <-time.After(5 * time.Second):
			t.Logf("MCP subprocess did not exit within timeout; stderr:\n%s", c.stderr.String())
		}
	})

	return c
}

func (c *stdioMCP) readStdout(stdout io.Reader) {
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		var msg rpcMessage
		if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
			c.stderr.WriteString(fmt.Sprintf("invalid stdout JSON %q: %v\n", scanner.Text(), err))
			continue
		}
		c.msgs <- msg
	}
	if err := scanner.Err(); err != nil {
		c.stderr.WriteString(fmt.Sprintf("stdout scanner: %v\n", err))
	}
	close(c.msgs)
}

func (c *stdioMCP) readStderr(stderr io.Reader) {
	scanner := bufio.NewScanner(stderr)
	for scanner.Scan() {
		c.stderr.WriteString(scanner.Text() + "\n")
	}
	if err := scanner.Err(); err != nil {
		c.stderr.WriteString(fmt.Sprintf("stderr scanner: %v\n", err))
	}
}

func (c *stdioMCP) send(msg map[string]any) {
	c.t.Helper()
	data, err := json.Marshal(msg)
	if err != nil {
		c.t.Fatalf("marshal request: %v", err)
	}
	data = append(data, '\n')
	if _, err := c.stdin.Write(data); err != nil {
		c.t.Fatalf("write request: %v\nstderr:\n%s", err, c.stderr.String())
	}
}

func (c *stdioMCP) waitResponse(id int) rpcMessage {
	c.t.Helper()
	return c.waitFor(20*time.Second, func(msg rpcMessage) bool {
		return idMatches(msg["id"], id)
	})
}

func (c *stdioMCP) waitNotification(method string, pred func(map[string]any) bool) rpcMessage {
	c.t.Helper()
	return c.waitFor(10*time.Second, func(msg rpcMessage) bool {
		if msg["method"] != method {
			return false
		}
		params, ok := msg["params"].(map[string]any)
		return ok && pred(params)
	})
}

func (c *stdioMCP) waitFor(timeout time.Duration, pred func(rpcMessage) bool) rpcMessage {
	c.t.Helper()

	deadline := time.NewTimer(timeout)
	defer deadline.Stop()

	for {
		for i, msg := range c.backlog {
			if pred(msg) {
				c.backlog = append(c.backlog[:i], c.backlog[i+1:]...)
				return msg
			}
		}

		select {
		case msg, ok := <-c.msgs:
			if !ok {
				c.t.Fatalf("MCP stdout closed before expected message\nstderr:\n%s", c.stderr.String())
			}
			if pred(msg) {
				return msg
			}
			c.backlog = append(c.backlog, msg)
		case <-deadline.C:
			c.t.Fatalf("timed out waiting for MCP message\nbacklog=%v\nstderr:\n%s", c.backlog, c.stderr.String())
		}
	}
}

func idMatches(v any, id int) bool {
	switch x := v.(type) {
	case float64:
		return int(x) == id
	case string:
		return x == strconv.Itoa(id)
	default:
		return false
	}
}

func assertNoRPCError(t *testing.T, msg rpcMessage) {
	t.Helper()
	if errVal, ok := msg["error"]; ok {
		t.Fatalf("unexpected JSON-RPC error: %v", errVal)
	}
}

func assertToolNames(t *testing.T, msg rpcMessage, want []string) {
	t.Helper()

	result := resultMap(t, msg)
	rawTools, ok := result["tools"].([]any)
	if !ok {
		t.Fatalf("tools/list result missing tools array: %v", result)
	}

	got := make([]string, 0, len(rawTools))
	for _, raw := range rawTools {
		tool, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("tool entry has unexpected shape: %v", raw)
		}
		name, ok := tool["name"].(string)
		if !ok {
			t.Fatalf("tool entry missing name: %v", tool)
		}
		got = append(got, name)
	}
	sort.Strings(got)
	sort.Strings(want)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("tool names: got %v want %v", got, want)
	}
}

func assertToolTextContains(t *testing.T, msg rpcMessage, want string) {
	t.Helper()
	text := toolText(t, msg)
	if !strings.Contains(text, want) {
		t.Fatalf("tool text missing %q:\n%s", want, text)
	}
}

func toolText(t *testing.T, msg rpcMessage) string {
	t.Helper()

	result := resultMap(t, msg)
	rawContent, ok := result["content"].([]any)
	if !ok || len(rawContent) == 0 {
		t.Fatalf("tool result missing content: %v", result)
	}
	block, ok := rawContent[0].(map[string]any)
	if !ok {
		t.Fatalf("tool content has unexpected shape: %v", rawContent[0])
	}
	text, ok := block["text"].(string)
	if !ok {
		t.Fatalf("tool content missing text: %v", block)
	}
	return text
}

func resultMap(t *testing.T, msg rpcMessage) map[string]any {
	t.Helper()
	result, ok := msg["result"].(map[string]any)
	if !ok {
		t.Fatalf("response missing result object: %v", msg)
	}
	return result
}

func assertChannelEvent(t *testing.T, msg rpcMessage, eventType, contentPart string) {
	t.Helper()

	params, ok := msg["params"].(map[string]any)
	if !ok {
		t.Fatalf("notification missing params: %v", msg)
	}
	content, ok := params["content"].(string)
	if !ok || !strings.Contains(content, contentPart) {
		t.Fatalf("notification content = %v, want substring %q", params["content"], contentPart)
	}
	meta, ok := params["meta"].(map[string]any)
	if !ok {
		t.Fatalf("notification missing meta: %v", params)
	}
	if meta["event_type"] != eventType {
		t.Fatalf("event_type = %v, want %q", meta["event_type"], eventType)
	}
}

type safeBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *safeBuffer) WriteString(s string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf.WriteString(s)
}

func (b *safeBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

type fakeChanwire struct {
	server *httptest.Server

	mu          sync.Mutex
	agents      map[string]string
	tokenToName map[string]string
	conns       map[string]map[*fakeWSConn]bool
	nextID      int64
}

func newFakeChanwire(t *testing.T) *fakeChanwire {
	t.Helper()

	f := &fakeChanwire{
		agents:      make(map[string]string),
		tokenToName: make(map[string]string),
		conns:       make(map[string]map[*fakeWSConn]bool),
	}
	f.server = httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakeChanwire) URL() string {
	return f.server.URL
}

func (f *fakeChanwire) handle(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/api/v1/agent/register":
		f.handleRegister(w, r)
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/agent/list":
		f.handleList(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/api/v1/msg/send":
		f.handleSend(w, r)
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/ws":
		f.handleWS(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (f *fakeChanwire) handleRegister(w http.ResponseWriter, r *http.Request) {
	var req struct {
		AgentName string `json:"agent_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.AgentName == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	f.mu.Lock()
	token := f.agents[req.AgentName]
	if token == "" {
		token = "token-" + req.AgentName
		f.agents[req.AgentName] = token
		f.tokenToName[token] = req.AgentName
	}
	f.mu.Unlock()

	writeJSON(w, map[string]any{
		"agent_name": req.AgentName,
		"token":      token,
	})
}

func (f *fakeChanwire) handleList(w http.ResponseWriter, r *http.Request) {
	if _, ok := f.authAgent(r); !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	f.mu.Lock()
	names := make([]string, 0, len(f.agents))
	for name := range f.agents {
		names = append(names, name)
	}
	f.mu.Unlock()

	sort.Strings(names)
	agents := make([]map[string]any, 0, len(names))
	for _, name := range names {
		agents = append(agents, map[string]any{
			"agent_name":     name,
			"last_active_at": nil,
		})
	}
	writeJSON(w, map[string]any{"agents": agents})
}

func (f *fakeChanwire) handleSend(w http.ResponseWriter, r *http.Request) {
	fromAgent, ok := f.authAgent(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		ToAgent string `json:"to_agent"`
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	f.mu.Lock()
	_, recipientExists := f.agents[req.ToAgent]
	if recipientExists {
		f.nextID++
	}
	messageID := f.nextID
	sentAt := time.Now().UnixMilli()
	f.mu.Unlock()

	if !recipientExists {
		w.WriteHeader(http.StatusNotFound)
		writeJSON(w, map[string]any{"error": "unknown agent"})
		return
	}

	f.pushRealtime(req.ToAgent, map[string]any{
		"type":       "realtime",
		"message_id": messageID,
		"from_agent": fromAgent,
		"content":    req.Content,
		"sent_at":    sentAt,
	})

	writeJSON(w, map[string]any{
		"message_id": messageID,
		"sent_at":    sentAt,
	})
}

func (f *fakeChanwire) handleWS(w http.ResponseWriter, r *http.Request) {
	agentName, ok := f.authAgent(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	conn, err := websocket.Upgrade(w, r, nil, 1024, 1024)
	if err != nil {
		return
	}

	wsConn := &fakeWSConn{conn: conn}
	defer conn.Close()
	defer f.removeConn(agentName, wsConn)

	wsConn.writeJSON(map[string]any{"type": "history_done"})
	f.addConn(agentName, wsConn)

	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			return
		}
	}
}

func (f *fakeChanwire) authAgent(r *http.Request) (string, bool) {
	const prefix = "Bearer "
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, prefix) {
		return "", false
	}
	token := strings.TrimPrefix(auth, prefix)
	f.mu.Lock()
	defer f.mu.Unlock()
	name, ok := f.tokenToName[token]
	return name, ok
}

func (f *fakeChanwire) addConn(agentName string, conn *fakeWSConn) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.conns[agentName] == nil {
		f.conns[agentName] = make(map[*fakeWSConn]bool)
	}
	f.conns[agentName][conn] = true
}

func (f *fakeChanwire) removeConn(agentName string, conn *fakeWSConn) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.conns[agentName], conn)
}

func (f *fakeChanwire) pushRealtime(agentName string, frame map[string]any) {
	f.mu.Lock()
	conns := make([]*fakeWSConn, 0, len(f.conns[agentName]))
	for conn := range f.conns[agentName] {
		conns = append(conns, conn)
	}
	f.mu.Unlock()

	for _, conn := range conns {
		conn.writeJSON(frame)
	}
}

func (f *fakeChanwire) waitForWS(t *testing.T, agentName string) {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		f.mu.Lock()
		count := len(f.conns[agentName])
		f.mu.Unlock()
		if count > 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for websocket connection for agent %q", agentName)
}

func (f *fakeChanwire) registerHTTP(t *testing.T, agentName string) string {
	t.Helper()

	body := strings.NewReader(fmt.Sprintf(`{"agent_name":%q}`, agentName))
	resp, err := http.Post(f.URL()+"/api/v1/agent/register", "application/json", body)
	if err != nil {
		t.Fatalf("register %s: %v", agentName, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("register %s: status %d", agentName, resp.StatusCode)
	}

	var out struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode register response: %v", err)
	}
	if out.Token == "" {
		t.Fatalf("register %s returned empty token", agentName)
	}
	return out.Token
}

func (f *fakeChanwire) sendHTTP(t *testing.T, token, toAgent, content string) {
	t.Helper()

	body := strings.NewReader(fmt.Sprintf(`{"to_agent":%q,"content":%q}`, toAgent, content))
	req, err := http.NewRequest(http.MethodPost, f.URL()+"/api/v1/msg/send", body)
	if err != nil {
		t.Fatalf("new send request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("send to %s: %v", toAgent, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("send to %s: status %d", toAgent, resp.StatusCode)
	}
}

type fakeWSConn struct {
	mu   sync.Mutex
	conn *websocket.Conn
}

func (c *fakeWSConn) writeJSON(v any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	_ = c.conn.WriteJSON(v)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
