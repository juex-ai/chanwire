package store_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/juex-ai/chanwire/cli/internal/store"
)

func TestWriteRead(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent.json")

	want := &store.AgentInfo{
		AgentName: "alice",
		Token:     "test-token-abc123",
		Endpoint:  "http://127.0.0.1:12306",
	}

	if err := store.Write(path, want); err != nil {
		t.Fatalf("Write: %v", err)
	}

	got, err := store.Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	if got.AgentName != want.AgentName {
		t.Errorf("AgentName: got %q want %q", got.AgentName, want.AgentName)
	}
	if got.Token != want.Token {
		t.Errorf("Token: got %q want %q", got.Token, want.Token)
	}
	if got.Endpoint != want.Endpoint {
		t.Errorf("Endpoint: got %q want %q", got.Endpoint, want.Endpoint)
	}
}

func TestWriteCreatesDir(t *testing.T) {
	base := t.TempDir()
	// Sub-directory that does not yet exist.
	dir := filepath.Join(base, "nested", "subdir")
	path := filepath.Join(dir, "agent.json")

	info := &store.AgentInfo{AgentName: "bob", Token: "tok", Endpoint: "http://x"}
	if err := store.Write(path, info); err != nil {
		t.Fatalf("Write with missing dir: %v", err)
	}

	if _, err := os.Stat(path); err != nil {
		t.Errorf("file not created: %v", err)
	}
}

func TestReadMissing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "no-such-file.json")

	_, err := store.Read(path)
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
	if err != store.ErrNotRegistered {
		t.Errorf("expected ErrNotRegistered, got %v", err)
	}
}

func TestReadPartialFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent.json")

	// Write a file with agent_name but no token.
	if err := os.WriteFile(path, []byte(`{"agent_name":"alice"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := store.Read(path)
	if err == nil {
		t.Fatal("expected error for partial file, got nil")
	}
}

func TestReadInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent.json")

	if err := os.WriteFile(path, []byte(`not-json`), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := store.Read(path)
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}
