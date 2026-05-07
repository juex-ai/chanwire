package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runArgs runs the root command with the given args, capturing stdout/stderr.
// Returns (stdout, stderr, error).
func runArgs(args ...string) (string, string, error) {
	cmd := rootCmd()

	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs(args)

	err := cmd.Execute()
	return stdout.String(), stderr.String(), err
}

// TestVersionCommand checks that `chanwire version` runs without error.
func TestVersionCommand(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CHANWIRE_DIR", dir)

	stdout, _, err := runArgs("version")
	if err != nil {
		t.Fatalf("version command: %v", err)
	}
	// Both endpoint lines must be present.
	if !strings.Contains(stdout, "endpoint (env):") {
		t.Errorf("expected 'endpoint (env):' in output, got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "endpoint (saved):") {
		t.Errorf("expected 'endpoint (saved):' in output, got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "(not registered)") {
		t.Errorf("expected '(not registered)' for missing agent.json, got:\n%s", stdout)
	}
}

// TestVersionShowsSavedEndpoint checks that a registered agent's saved
// endpoint is reflected in the version output.
func TestVersionShowsSavedEndpoint(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CHANWIRE_DIR", dir)

	agentJSON := filepath.Join(dir, "agent.json")
	writeAgentJSON(t, agentJSON, "alice", "tok", "http://saved.example:9999")

	stdout, _, err := runArgs("version")
	if err != nil {
		t.Fatalf("version command: %v", err)
	}
	if !strings.Contains(stdout, "http://saved.example:9999") {
		t.Errorf("expected saved endpoint in output, got:\n%s", stdout)
	}
}

// TestAgentRegisterMissingFlag checks that --agent_name is required.
func TestAgentRegisterMissingFlag(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CHANWIRE_DIR", dir)
	t.Setenv("CHANWIRE_ENDPOINT", "http://127.0.0.1:19999")

	_, _, err := runArgs("agent", "register")
	if err == nil {
		t.Fatal("expected error when --agent_name is missing, got nil")
	}
	if !strings.Contains(err.Error(), "agent_name") {
		t.Errorf("error should mention agent_name, got: %v", err)
	}
}

// TestMsgSendMissingToAgent checks that --to_agent is required.
func TestMsgSendMissingToAgent(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CHANWIRE_DIR", dir)

	agentJSON := filepath.Join(dir, "agent.json")
	writeAgentJSON(t, agentJSON, "alice", "tok", "http://127.0.0.1:19999")

	_, _, err := runArgs("msg", "send", "--content", "hello")
	if err == nil {
		t.Fatal("expected error when --to_agent is missing, got nil")
	}
	if !strings.Contains(err.Error(), "to_agent") {
		t.Errorf("error should mention to_agent, got: %v", err)
	}
}

// TestMsgSendMissingContent checks that --content is required.
func TestMsgSendMissingContent(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CHANWIRE_DIR", dir)

	agentJSON := filepath.Join(dir, "agent.json")
	writeAgentJSON(t, agentJSON, "alice", "tok", "http://127.0.0.1:19999")

	_, _, err := runArgs("msg", "send", "--to_agent", "bob")
	if err == nil {
		t.Fatal("expected error when --content is missing, got nil")
	}
	if !strings.Contains(err.Error(), "content") {
		t.Errorf("error should mention content, got: %v", err)
	}
}

// TestUnregistered checks that commands needing a token return the spec
// error message when agent.json is absent.
func TestUnregistered(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CHANWIRE_DIR", dir)

	_, _, err := runArgs("agent", "list")
	if err == nil {
		t.Fatal("expected error when not registered, got nil")
	}
	if !strings.Contains(err.Error(), "not registered") {
		t.Errorf("expected 'not registered' in error, got: %v", err)
	}
}

// TestMsgSend404ReturnsError verifies that a 404 from the server surfaces
// as a returned error (no os.Exit), so Cobra handles the exit code.
func TestMsgSend404ReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"unknown agent"}`, http.StatusNotFound)
	}))
	defer srv.Close()

	dir := t.TempDir()
	t.Setenv("CHANWIRE_DIR", dir)
	t.Setenv("CHANWIRE_ENDPOINT", srv.URL)

	agentJSON := filepath.Join(dir, "agent.json")
	writeAgentJSON(t, agentJSON, "alice", "tok", srv.URL)

	_, _, err := runArgs("msg", "send", "--to_agent", "ghost", "--content", "hi")
	if err == nil {
		t.Fatal("expected error for 404, got nil")
	}
	if !strings.Contains(err.Error(), "no such agent: ghost") {
		t.Errorf("expected 'no such agent: ghost' in error, got: %v", err)
	}
}

// TestAgentListJSON verifies --json emits a single line of valid JSON.
func TestAgentListJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"agents":[{"agent_name":"alice","last_active_at":1778154123456},{"agent_name":"bob","last_active_at":null}]}`))
	}))
	defer srv.Close()

	dir := t.TempDir()
	t.Setenv("CHANWIRE_DIR", dir)
	t.Setenv("CHANWIRE_ENDPOINT", srv.URL)

	agentJSON := filepath.Join(dir, "agent.json")
	writeAgentJSON(t, agentJSON, "alice", "tok", srv.URL)

	stdout, _, err := runArgs("agent", "list", "--json")
	if err != nil {
		t.Fatalf("agent list --json: %v", err)
	}
	// Expect exactly one trailing newline → one line of content.
	trimmed := strings.TrimRight(stdout, "\n")
	if strings.Contains(trimmed, "\n") {
		t.Errorf("expected single-line JSON, got multi-line:\n%s", stdout)
	}
	// Output must be valid JSON with the expected shape.
	var parsed struct {
		Agents []struct {
			AgentName    string `json:"agent_name"`
			LastActiveAt *int64 `json:"last_active_at"`
		} `json:"agents"`
	}
	if err := json.Unmarshal([]byte(trimmed), &parsed); err != nil {
		t.Fatalf("not valid JSON: %v\noutput: %s", err, trimmed)
	}
	if len(parsed.Agents) != 2 {
		t.Errorf("expected 2 agents, got %d", len(parsed.Agents))
	}
	if parsed.Agents[0].AgentName != "alice" {
		t.Errorf("first agent: got %q want alice", parsed.Agents[0].AgentName)
	}
}

// TestAgentListTable verifies the default human-readable table format.
func TestAgentListTable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"agents":[{"agent_name":"bob","last_active_at":null}]}`))
	}))
	defer srv.Close()

	dir := t.TempDir()
	t.Setenv("CHANWIRE_DIR", dir)
	t.Setenv("CHANWIRE_ENDPOINT", srv.URL)

	agentJSON := filepath.Join(dir, "agent.json")
	writeAgentJSON(t, agentJSON, "alice", "tok", srv.URL)

	stdout, _, err := runArgs("agent", "list")
	if err != nil {
		t.Fatalf("agent list: %v", err)
	}
	if !strings.Contains(stdout, "NAME") || !strings.Contains(stdout, "LAST_ACTIVE") {
		t.Errorf("expected header in output, got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "(never)") {
		t.Errorf("expected (never) in output, got:\n%s", stdout)
	}
}

// writeAgentJSON writes a minimal agent.json for testing.
func writeAgentJSON(t *testing.T, path, name, token, endpoint string) {
	t.Helper()
	data := `{"agent_name":"` + name + `","token":"` + token + `","endpoint":"` + endpoint + `"}`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatalf("writeAgentJSON: %v", err)
	}
}
