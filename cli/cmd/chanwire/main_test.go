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

// TestVersionCommand checks that `chanwire version` only prints build metadata.
func TestVersionCommand(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CHANWIRE_DIR", dir)

	stdout, _, err := runArgs("version")
	if err != nil {
		t.Fatalf("version command: %v", err)
	}
	if !strings.Contains(stdout, "version:") || !strings.Contains(stdout, "commit:") {
		t.Errorf("expected version and commit in output, got:\n%s", stdout)
	}
	if strings.Contains(stdout, "work_dir") || strings.Contains(stdout, "endpoint") || strings.Contains(stdout, "agent_name") {
		t.Errorf("version should not print runtime status, got:\n%s", stdout)
	}
}

func TestVersionJSON(t *testing.T) {
	stdout, _, err := runArgs("version", "--format", "json")
	if err != nil {
		t.Fatalf("version --format json: %v", err)
	}
	var got struct {
		Version string `json:"version"`
		Commit  string `json:"commit"`
	}
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("version JSON: %v\n%s", err, stdout)
	}
	if got.Version == "" || got.Commit == "" {
		t.Fatalf("version JSON missing fields: %+v", got)
	}
}

func TestStatusShowsRuntimeConfigAndAgentName(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CHANWIRE_DIR", dir)
	t.Setenv("CHANWIRE_ENDPOINT", "http://status.example:12306")

	agentJSON := filepath.Join(dir, ".config", "chanwire", "agent.json")
	writeAgentJSON(t, agentJSON, "alice", "tok", "http://saved.example:9999")

	stdout, _, err := runArgs("status")
	if err != nil {
		t.Fatalf("status command: %v", err)
	}
	wantDir := "work_dir(env):     " + filepath.Join(dir, ".config", "chanwire")
	for _, want := range []string{
		"version:",
		wantDir,
		"endpoint:          http://status.example:12306",
		"agent_name:        alice",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("expected %q in output, got:\n%s", want, stdout)
		}
	}
	if strings.Contains(stdout, "http://saved.example:9999") {
		t.Errorf("status should not print saved endpoint, got:\n%s", stdout)
	}
}

func TestStatusJSON(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CHANWIRE_DIR", dir)
	t.Setenv("CHANWIRE_ENDPOINT", "http://status.example:12306")

	agentJSON := filepath.Join(dir, ".config", "chanwire", "agent.json")
	writeAgentJSON(t, agentJSON, "alice", "tok", "http://saved.example:9999")

	stdout, _, err := runArgs("status", "--format", "json")
	if err != nil {
		t.Fatalf("status --format json: %v", err)
	}
	var got struct {
		Version       string `json:"version"`
		WorkDirSource string `json:"work_dir_source"`
		WorkDir       string `json:"work_dir"`
		Endpoint      string `json:"endpoint"`
		AgentName     string `json:"agent_name"`
	}
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("status JSON: %v\n%s", err, stdout)
	}
	if got.WorkDirSource != "env" || got.WorkDir != filepath.Join(dir, ".config", "chanwire") || got.Endpoint != "http://status.example:12306" || got.AgentName != "alice" {
		t.Fatalf("unexpected status JSON: %+v", got)
	}
}

func TestStatusShowsEmptyAgentNameWhenUnregistered(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CHANWIRE_DIR", dir)

	stdout, _, err := runArgs("status")
	if err != nil {
		t.Fatalf("status command: %v", err)
	}
	if !strings.Contains(stdout, "agent_name:") {
		t.Fatalf("expected agent_name line, got:\n%s", stdout)
	}
	if strings.Contains(stdout, "(not registered)") {
		t.Fatalf("status should use an empty agent name when unregistered, got:\n%s", stdout)
	}
}

func TestHomeDirFlagWinsOverEnvAndNormalizes(t *testing.T) {
	envRoot := t.TempDir()
	flagRoot := t.TempDir()
	t.Setenv("CHANWIRE_DIR", envRoot)

	stdout, _, err := runArgs("--homedir", flagRoot, "status")
	if err != nil {
		t.Fatalf("status --homedir: %v", err)
	}

	want := "work_dir(flag):    " + filepath.Join(flagRoot, ".config", "chanwire")
	if !strings.Contains(stdout, want) {
		t.Fatalf("expected %q in output, got:\n%s", want, stdout)
	}
	if strings.Contains(stdout, envRoot) {
		t.Fatalf("expected --homedir to override CHANWIRE_DIR, got:\n%s", stdout)
	}
}

func TestHomeDirFlagResolvesRelativeFromWorkingDirectory(t *testing.T) {
	cwd := t.TempDir()
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(cwd); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	cwd, err = os.Getwd()
	if err != nil {
		t.Fatalf("Getwd after Chdir: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(oldWD); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	})

	stdout, _, err := runArgs("--homedir", ".", "status")
	if err != nil {
		t.Fatalf("status --homedir .: %v", err)
	}

	want := "work_dir(flag):    " + filepath.Join(cwd, ".config", "chanwire")
	if !strings.Contains(stdout, want) {
		t.Fatalf("expected %q in output, got:\n%s", want, stdout)
	}
}

func TestHomeDirFlagRejectsParentTraversal(t *testing.T) {
	_, _, err := runArgs("--homedir", filepath.Join("..", "other"), "version")
	if err == nil {
		t.Fatal("expected error for parent traversal, got nil")
	}
	if !strings.Contains(err.Error(), "..") {
		t.Fatalf("expected error to mention '..', got: %v", err)
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

func TestAgentRegisterJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"agent_name":"alice","token":"tok"}`))
	}))
	defer srv.Close()

	dir := t.TempDir()
	t.Setenv("CHANWIRE_DIR", dir)
	t.Setenv("CHANWIRE_ENDPOINT", srv.URL)

	stdout, _, err := runArgs("agent", "register", "--agent_name", "alice", "--format", "json")
	if err != nil {
		t.Fatalf("agent register --format json: %v", err)
	}
	var got struct {
		AgentName string `json:"agent_name"`
	}
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("register JSON: %v\n%s", err, stdout)
	}
	if got.AgentName != "alice" {
		t.Fatalf("agent_name: got %q want alice", got.AgentName)
	}
}

// TestMsgSendMissingToAgent checks that --to_agent is required.
func TestMsgSendMissingToAgent(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CHANWIRE_DIR", dir)

	agentJSON := filepath.Join(dir, ".config", "chanwire", "agent.json")
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

	agentJSON := filepath.Join(dir, ".config", "chanwire", "agent.json")
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

	agentJSON := filepath.Join(dir, ".config", "chanwire", "agent.json")
	writeAgentJSON(t, agentJSON, "alice", "tok", srv.URL)

	_, _, err := runArgs("msg", "send", "--to_agent", "ghost", "--content", "hi")
	if err == nil {
		t.Fatal("expected error for 404, got nil")
	}
	if !strings.Contains(err.Error(), "no such agent: ghost") {
		t.Errorf("expected 'no such agent: ghost' in error, got: %v", err)
	}
}

func TestMsgSendJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"message_id":42,"sent_at":1778154123456}`))
	}))
	defer srv.Close()

	dir := t.TempDir()
	t.Setenv("CHANWIRE_DIR", dir)
	t.Setenv("CHANWIRE_ENDPOINT", srv.URL)

	agentJSON := filepath.Join(dir, ".config", "chanwire", "agent.json")
	writeAgentJSON(t, agentJSON, "alice", "tok", srv.URL)

	stdout, _, err := runArgs("msg", "send", "--to_agent", "bob", "--content", "hi", "--format", "json")
	if err != nil {
		t.Fatalf("msg send --format json: %v", err)
	}
	var got struct {
		MessageID int64 `json:"message_id"`
		SentAt    int64 `json:"sent_at"`
	}
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("send JSON: %v\n%s", err, stdout)
	}
	if got.MessageID != 42 || got.SentAt == 0 {
		t.Fatalf("unexpected send JSON: %+v", got)
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

	agentJSON := filepath.Join(dir, ".config", "chanwire", "agent.json")
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

func TestAgentListFormatJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"agents":[{"agent_name":"alice","last_active_at":1778154123456}]}`))
	}))
	defer srv.Close()

	dir := t.TempDir()
	t.Setenv("CHANWIRE_DIR", dir)
	t.Setenv("CHANWIRE_ENDPOINT", srv.URL)

	agentJSON := filepath.Join(dir, ".config", "chanwire", "agent.json")
	writeAgentJSON(t, agentJSON, "alice", "tok", srv.URL)

	stdout, _, err := runArgs("agent", "list", "--format", "json")
	if err != nil {
		t.Fatalf("agent list --format json: %v", err)
	}
	var parsed struct {
		Agents []struct {
			AgentName string `json:"agent_name"`
		} `json:"agents"`
	}
	if err := json.Unmarshal([]byte(stdout), &parsed); err != nil {
		t.Fatalf("not valid JSON: %v\noutput: %s", err, stdout)
	}
	if len(parsed.Agents) != 1 || parsed.Agents[0].AgentName != "alice" {
		t.Fatalf("unexpected agents JSON: %+v", parsed)
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

	agentJSON := filepath.Join(dir, ".config", "chanwire", "agent.json")
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
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir agent dir: %v", err)
	}
	data := `{"agent_name":"` + name + `","token":"` + token + `","endpoint":"` + endpoint + `"}`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatalf("writeAgentJSON: %v", err)
	}
}
