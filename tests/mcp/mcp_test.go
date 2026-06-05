package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/juex-ai/chanwire/tests/internal/e2e"
)

func TestMCPFlow(t *testing.T) {
	endpoint := e2e.Endpoint()
	bin := e2e.Binary(t)
	suffix := e2e.UniqueSuffix()

	alice := "mcp-alice-" + suffix
	bob := "mcp-bob-" + suffix
	bobToAliceContent := "mcp bob to alice " + suffix
	aliceToBobContent := "mcp alice to bob line 1\nline 2 " + suffix
	bobHistoryContent := "mcp bob history batch " + suffix

	aliceToken := e2e.RegisterAgent(t, endpoint, alice)
	aliceConn := e2e.DialWS(t, endpoint, aliceToken)
	defer aliceConn.Close()

	mcpDataDir := t.TempDir()
	mcp := startMCPServer(t, bin, endpoint, mcpDataDir)
	mcp.send(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    channelClientCapabilities(),
			"clientInfo": map[string]any{
				"name":    "chanwire-e2e",
				"version": "test",
			},
		},
	})
	initResp := mcp.waitResponse(1)
	assertNoRPCError(t, initResp)
	assertServerChannelCapability(t, initResp, true)

	mcp.send(map[string]any{
		"jsonrpc": "2.0",
		"method":  "notifications/initialized",
		"params":  map[string]any{},
	})

	notRegistered := mcp.waitNotification("notifications/claude/channel", func(params map[string]any) bool {
		content, _ := params["content"].(string)
		return strings.Contains(content, "agent not registered")
	})
	assertChannelEvent(t, notRegistered, "not_registered", "agent not registered")
	mcp.assertNoNotification("notifications/claude/channel", 1200*time.Millisecond)
	assertStderrContainsCount(t, mcp, "[chanwire] connect: not registered.", 1)
	assertStderrNotContains(t, mcp, "connect blocked")

	callTool(t, mcp, 20, "chanwire_list_agents", map[string]any{})
	unregisteredListResp := mcp.waitResponse(20)
	assertNoRPCError(t, unregisteredListResp)
	assertToolErrorTextContains(t, unregisteredListResp, "not registered")
	assertStderrContainsCountEventually(t, mcp, "[chanwire] tool chanwire_list_agents: not registered.", 1, time.Second)

	callTool(t, mcp, 21, "chanwire_send_msg", map[string]any{
		"to_agent": alice,
		"content":  "should fail before registration",
	})
	unregisteredSendResp := mcp.waitResponse(21)
	assertNoRPCError(t, unregisteredSendResp)
	assertToolErrorTextContains(t, unregisteredSendResp, "not registered")
	assertStderrContainsCountEventually(t, mcp, "[chanwire] tool chanwire_send_msg: not registered.", 1, time.Second)
	mcp.assertNoNotification("notifications/claude/channel", 300*time.Millisecond)

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
		"chanwire_status",
	})

	callTool(t, mcp, 3, "chanwire_status", map[string]any{})
	statusResp := mcp.waitResponse(3)
	assertNoRPCError(t, statusResp)
	assertToolTextContains(t, statusResp, "version:")
	assertToolTextContains(t, statusResp, "work_dir(env):")
	assertToolTextContains(t, statusResp, "endpoint:          "+endpoint)
	assertToolTextContains(t, statusResp, "agent_name:")

	callTool(t, mcp, 4, "chanwire_register_agent", map[string]any{"agent_name": bob})
	registerResp := mcp.waitResponse(4)
	assertNoRPCError(t, registerResp)
	assertToolTextContains(t, registerResp, "registered: agent_name="+bob)

	waitForAgentActive(t, endpoint, aliceToken, bob)

	callTool(t, mcp, 5, "chanwire_status", map[string]any{})
	registeredStatusResp := mcp.waitResponse(5)
	assertNoRPCError(t, registeredStatusResp)
	assertToolTextContains(t, registeredStatusResp, "agent_name:        "+bob)

	callTool(t, mcp, 6, "chanwire_list_agents", map[string]any{})
	listResp := mcp.waitResponse(6)
	assertNoRPCError(t, listResp)
	assertToolTextContains(t, listResp, alice)
	assertToolTextContains(t, listResp, bob)

	callTool(t, mcp, 7, "chanwire_send_msg", map[string]any{
		"to_agent": alice,
		"content":  bobToAliceContent,
	})
	sendResp := mcp.waitResponse(7)
	assertNoRPCError(t, sendResp)
	assertToolTextContains(t, sendResp, "ok: message_id=")

	frame := e2e.ReadMatchingFrame(t, aliceConn, "realtime", bobToAliceContent)
	if frame.FromAgent != bob {
		t.Fatalf("MCP send realtime from_agent: got %q want %q", frame.FromAgent, bob)
	}

	e2e.SendMessage(t, endpoint, aliceToken, bob, aliceToBobContent, http.StatusOK)
	msgNotification := mcp.waitNotification("notifications/claude/channel", func(params map[string]any) bool {
		content, _ := params["content"].(string)
		return strings.Contains(content, "[realtime] from "+alice) &&
			strings.Contains(content, aliceToBobContent)
	})
	assertChannelEvent(t, msgNotification, "message", aliceToBobContent)

	mcp.stop()
	e2e.SendMessage(t, endpoint, aliceToken, bob, bobHistoryContent, http.StatusOK)

	mcp2 := startMCPServer(t, bin, endpoint, mcpDataDir)
	mcp2.send(map[string]any{
		"jsonrpc": "2.0",
		"id":      8,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    channelClientCapabilities(),
			"clientInfo": map[string]any{
				"name":    "chanwire-e2e",
				"version": "test",
			},
		},
	})
	initResp2 := mcp2.waitResponse(8)
	assertNoRPCError(t, initResp2)
	assertServerChannelCapability(t, initResp2, true)

	mcp2.send(map[string]any{
		"jsonrpc": "2.0",
		"method":  "notifications/initialized",
		"params":  map[string]any{},
	})
	historyNotification := mcp2.waitNotification("notifications/claude/channel", func(params map[string]any) bool {
		content, _ := params["content"].(string)
		return strings.Contains(content, "history batch: one-time review") &&
			strings.Contains(content, "[history]  from "+alice) &&
			strings.Contains(content, bobHistoryContent)
	})
	assertChannelEvent(t, historyNotification, "message", bobHistoryContent)
	mcp2.assertNoNotification("notifications/claude/channel", 500*time.Millisecond)
}

func TestMCPWithoutClientCapabilityDoesNotStream(t *testing.T) {
	endpoint := e2e.Endpoint()
	bin := e2e.Binary(t)
	suffix := e2e.UniqueSuffix()

	observer := "mcp-observer-" + suffix
	bob := "mcp-bob-nocap-" + suffix
	message := "mcp no capability should not notify " + suffix

	observerToken := e2e.RegisterAgent(t, endpoint, observer)

	mcp := startMCPServer(t, bin, endpoint, t.TempDir())
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
	initResp := mcp.waitResponse(1)
	assertNoRPCError(t, initResp)
	assertServerChannelCapability(t, initResp, true)

	mcp.send(map[string]any{
		"jsonrpc": "2.0",
		"method":  "notifications/initialized",
		"params":  map[string]any{},
	})
	mcp.assertNoNotification("notifications/claude/channel", 500*time.Millisecond)

	callTool(t, mcp, 2, "chanwire_register_agent", map[string]any{"agent_name": bob})
	registerResp := mcp.waitResponse(2)
	assertNoRPCError(t, registerResp)
	assertToolTextContains(t, registerResp, "registered: agent_name="+bob)
	assertAgentInactive(t, endpoint, observerToken, bob)

	e2e.SendMessage(t, endpoint, observerToken, bob, message, http.StatusOK)
	mcp.assertNoNotification("notifications/claude/channel", 500*time.Millisecond)
}

func TestMCPRecoversAfterExternalRegistration(t *testing.T) {
	endpoint := e2e.Endpoint()
	bin := e2e.Binary(t)
	suffix := e2e.UniqueSuffix()

	observer := "mcp-observer-recover-" + suffix
	bob := "mcp-bob-recover-" + suffix
	observerToken := e2e.RegisterAgent(t, endpoint, observer)

	mcpDataDir := t.TempDir()
	mcp := startMCPServer(t, bin, endpoint, mcpDataDir)
	mcp.send(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    channelClientCapabilities(),
			"clientInfo": map[string]any{
				"name":    "chanwire-e2e",
				"version": "test",
			},
		},
	})
	initResp := mcp.waitResponse(1)
	assertNoRPCError(t, initResp)

	mcp.send(map[string]any{
		"jsonrpc": "2.0",
		"method":  "notifications/initialized",
		"params":  map[string]any{},
	})
	notRegistered := mcp.waitNotification("notifications/claude/channel", func(params map[string]any) bool {
		content, _ := params["content"].(string)
		return strings.Contains(content, "agent not registered")
	})
	assertChannelEvent(t, notRegistered, "not_registered", "agent not registered")

	out := e2e.RunCLI(t, bin, endpoint, mcpDataDir, "agent", "register", "--agent_name", bob)
	e2e.AssertContains(t, out, "registered: agent_name="+bob)

	callTool(t, mcp, 2, "chanwire_list_agents", map[string]any{})
	listResp := mcp.waitResponse(2)
	assertNoRPCError(t, listResp)
	assertToolTextContains(t, listResp, observer)
	assertToolTextContains(t, listResp, bob)
	waitForAgentActive(t, endpoint, observerToken, bob)
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

func waitForAgentActive(t *testing.T, endpoint, token, agentName string) {
	t.Helper()

	var lastAgents []e2e.Agent
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		lastAgents = e2e.ListAgents(t, endpoint, token)
		for _, agent := range lastAgents {
			if agent.AgentName == agentName && agent.LastActiveAt != nil {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for agent %q to become active at %s; last agents=%+v", agentName, endpoint, lastAgents)
}

func assertAgentInactive(t *testing.T, endpoint, token, agentName string) {
	t.Helper()

	deadline := time.Now().Add(750 * time.Millisecond)
	seen := false
	for time.Now().Before(deadline) {
		for _, agent := range e2e.ListAgents(t, endpoint, token) {
			if agent.AgentName != agentName {
				continue
			}
			seen = true
			if agent.LastActiveAt != nil {
				t.Fatalf("agent %q became active without client channel capability", agentName)
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !seen {
		t.Fatalf("agent %q was not registered", agentName)
	}
}

type rpcMessage map[string]any

type stdioMCP struct {
	t       *testing.T
	cancel  context.CancelFunc
	stdin   io.WriteCloser
	msgs    chan rpcMessage
	done    chan error
	stderr  e2e.SafeBuffer
	backlog []rpcMessage
}

func startMCPServer(t *testing.T, bin, endpoint, dataDir string) *stdioMCP {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	args := []string{"mcp"}
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = e2e.Env(endpoint, dataDir)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		t.Fatalf("MCP stdin pipe: %v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		t.Fatalf("MCP stdout pipe: %v", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		t.Fatalf("MCP stderr pipe: %v", err)
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
		c.stop()
	})

	return c
}

func (c *stdioMCP) stop() {
	_ = c.stdin.Close()
	c.cancel()
	select {
	case <-c.done:
	case <-time.After(5 * time.Second):
		c.t.Logf("MCP subprocess did not exit within timeout; stderr:\n%s", c.stderr.String())
	}
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

func (c *stdioMCP) assertNoNotification(method string, timeout time.Duration) {
	c.t.Helper()

	deadline := time.NewTimer(timeout)
	defer deadline.Stop()

	for {
		select {
		case msg, ok := <-c.msgs:
			if !ok {
				c.t.Fatalf("MCP stdout closed while checking for absent notification\nstderr:\n%s", c.stderr.String())
			}
			if msg["method"] == method {
				c.t.Fatalf("unexpected MCP notification %s: %+v\nstderr:\n%s", method, msg, c.stderr.String())
			}
			c.backlog = append(c.backlog, msg)
		case <-deadline.C:
			return
		}
	}
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

func channelClientCapabilities() map[string]any {
	return map[string]any{
		"experimental": map[string]any{
			"claude/channel": map[string]any{},
		},
	}
}

func assertServerChannelCapability(t *testing.T, msg rpcMessage, want bool) {
	t.Helper()

	result := resultMap(t, msg)
	capabilities, ok := result["capabilities"].(map[string]any)
	if !ok {
		t.Fatalf("initialize result missing capabilities: %v", result)
	}
	experimental, _ := capabilities["experimental"].(map[string]any)
	_, got := experimental["claude/channel"]
	if got != want {
		t.Fatalf("server claude/channel capability = %v, want %v; capabilities=%v", got, want, capabilities)
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

func assertToolErrorTextContains(t *testing.T, msg rpcMessage, want string) {
	t.Helper()
	result := resultMap(t, msg)
	if isError, _ := result["isError"].(bool); !isError {
		t.Fatalf("tool result isError = %v, want true; result=%v", result["isError"], result)
	}
	assertToolTextContains(t, msg, want)
}

func assertStderrContainsCount(t *testing.T, c *stdioMCP, needle string, want int) {
	t.Helper()
	stderr := c.stderr.String()
	if got := strings.Count(stderr, needle); got != want {
		t.Fatalf("stderr count for %q = %d, want %d\nstderr:\n%s", needle, got, want, stderr)
	}
}

func assertStderrContainsCountEventually(t *testing.T, c *stdioMCP, needle string, want int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		stderr := c.stderr.String()
		if got := strings.Count(stderr, needle); got == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("stderr count for %q did not become %d within %s\nstderr:\n%s", needle, want, timeout, stderr)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func assertStderrNotContains(t *testing.T, c *stdioMCP, needle string) {
	t.Helper()
	stderr := c.stderr.String()
	if strings.Contains(stderr, needle) {
		t.Fatalf("stderr unexpectedly contains %q\nstderr:\n%s", needle, stderr)
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
