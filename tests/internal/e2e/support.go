package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

type Agent struct {
	AgentName    string `json:"agent_name"`
	LastActiveAt *int64 `json:"last_active_at"`
}

type WSFrame struct {
	Type      string `json:"type"`
	MessageID int64  `json:"message_id,omitempty"`
	FromAgent string `json:"from_agent,omitempty"`
	Content   string `json:"content,omitempty"`
	SentAt    int64  `json:"sent_at,omitempty"`
}

type agentListResponse struct {
	Agents []Agent `json:"agents"`
}

type registerResponse struct {
	AgentName string `json:"agent_name"`
	Token     string `json:"token"`
}

type sendResponse struct {
	MessageID int64 `json:"message_id"`
	SentAt    int64 `json:"sent_at"`
}

func Endpoint() string {
	if v := os.Getenv("CHANWIRE_ENDPOINT"); v != "" {
		return strings.TrimSuffix(v, "/")
	}
	return "http://127.0.0.1:12306"
}

func Binary(t *testing.T) string {
	t.Helper()
	if v := os.Getenv("CHANWIRE_BIN"); v != "" {
		return v
	}
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	return filepath.Join(root, "bin", "chanwire")
}

func Env(endpoint, dir string) []string {
	return append(os.Environ(),
		"CHANWIRE_ENDPOINT="+endpoint,
		"CHANWIRE_DIR="+dir,
	)
}

func UniqueSuffix() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

func RegisterAgent(t *testing.T, endpoint, name string) string {
	t.Helper()

	var out registerResponse
	doJSON(t, http.MethodPost, endpoint+"/api/v1/agent/register", "", map[string]string{
		"agent_name": name,
	}, http.StatusOK, &out)

	if out.AgentName != name {
		t.Fatalf("register agent_name: got %q want %q", out.AgentName, name)
	}
	if out.Token == "" {
		t.Fatalf("register returned empty token for %q", name)
	}
	return out.Token
}

func ListAgents(t *testing.T, endpoint, token string) []Agent {
	t.Helper()

	var out agentListResponse
	doJSON(t, http.MethodGet, endpoint+"/api/v1/agent/list", token, nil, http.StatusOK, &out)
	return out.Agents
}

func SendMessage(t *testing.T, endpoint, token, toAgent, content string, wantStatus int) {
	t.Helper()

	var out sendResponse
	doJSON(t, http.MethodPost, endpoint+"/api/v1/msg/send", token, map[string]string{
		"to_agent": toAgent,
		"content":  content,
	}, wantStatus, &out)

	if wantStatus == http.StatusOK {
		if out.MessageID == 0 {
			t.Fatalf("send returned empty message_id")
		}
		if out.SentAt == 0 {
			t.Fatalf("send returned empty sent_at")
		}
	}
}

func DialWS(t *testing.T, endpoint, token string) *websocket.Conn {
	t.Helper()

	u, err := url.Parse(strings.TrimSuffix(endpoint, "/"))
	if err != nil {
		t.Fatalf("parse endpoint %q: %v", endpoint, err)
	}
	switch u.Scheme {
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	default:
		t.Fatalf("unsupported endpoint scheme %q", u.Scheme)
	}
	u.Path = "/api/v1/ws"
	u.RawQuery = ""

	header := http.Header{"Authorization": []string{"Bearer " + token}}
	conn, resp, err := websocket.DefaultDialer.Dial(u.String(), header)
	if err != nil {
		if resp != nil {
			t.Fatalf("dial websocket: %v (HTTP %d)", err, resp.StatusCode)
		}
		t.Fatalf("dial websocket: %v", err)
	}
	return conn
}

func ReadUntilHistoryDone(t *testing.T, conn *websocket.Conn, wantContent string) bool {
	t.Helper()

	found := false
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		frame := readWSFrame(t, conn, deadline)
		if frame.Type == "history" && frame.Content == wantContent {
			found = true
		}
		if frame.Type == "history_done" {
			return found
		}
	}
	t.Fatalf("timed out before history_done")
	return false
}

func ReadMatchingFrame(t *testing.T, conn *websocket.Conn, frameType, content string) WSFrame {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		frame := readWSFrame(t, conn, deadline)
		if frame.Type == frameType && frame.Content == content {
			return frame
		}
	}
	t.Fatalf("timed out waiting for %s frame with content %q", frameType, content)
	return WSFrame{}
}

func AssertAgentPresent(t *testing.T, agents []Agent, name string) {
	t.Helper()
	for _, a := range agents {
		if a.AgentName == name {
			return
		}
	}
	t.Fatalf("agent %q not found in list: %+v", name, agents)
}

func AssertContains(t *testing.T, got, want string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Fatalf("output missing %q:\n%s", want, got)
	}
}

func doJSON(t *testing.T, method, target, token string, body any, wantStatus int, out any) {
	t.Helper()

	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request body: %v", err)
		}
		reader = bytes.NewReader(data)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, method, target, reader)
	if err != nil {
		t.Fatalf("new request %s %s: %v", method, target, err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, target, err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	if resp.StatusCode != wantStatus {
		t.Fatalf("%s %s: got HTTP %d want %d; body=%s", method, target, resp.StatusCode, wantStatus, raw)
	}
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			t.Fatalf("decode response %s: %v", raw, err)
		}
	}
}

func readWSFrame(t *testing.T, conn *websocket.Conn, deadline time.Time) WSFrame {
	t.Helper()

	if err := conn.SetReadDeadline(deadline); err != nil {
		t.Fatalf("set websocket read deadline: %v", err)
	}
	_, raw, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read websocket frame: %v", err)
	}
	var frame WSFrame
	if err := json.Unmarshal(raw, &frame); err != nil {
		t.Fatalf("decode websocket frame %s: %v", raw, err)
	}
	return frame
}

type SafeBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *SafeBuffer) Write(s string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf.WriteString(s)
}

func (b *SafeBuffer) WriteString(s string) {
	b.Write(s)
}

func (b *SafeBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
