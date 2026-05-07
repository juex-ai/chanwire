package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// buildRoot creates a fresh cobra root command for each test.
func buildRoot() *rootCmdBuilder {
	return &rootCmdBuilder{}
}

type rootCmdBuilder struct{}

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
	_, _, err := runArgs("version")
	if err != nil {
		t.Fatalf("version command: %v", err)
	}
}

// TestAgentRegisterMissingFlag checks that --agent_name is required.
func TestAgentRegisterMissingFlag(t *testing.T) {
	// Provide a temporary dir so we don't touch real filesystem.
	dir := t.TempDir()
	t.Setenv("CHANWIRE_DIR", dir)
	// Point to a server that will never be reached.
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

	// Write a fake agent.json so the command gets past the auth check.
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

// writeAgentJSON writes a minimal agent.json for testing.
func writeAgentJSON(t *testing.T, path, name, token, endpoint string) {
	t.Helper()
	data := `{"agent_name":"` + name + `","token":"` + token + `","endpoint":"` + endpoint + `"}`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatalf("writeAgentJSON: %v", err)
	}
}
