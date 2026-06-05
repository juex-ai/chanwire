// Package store_test exercises pragma application and FK enforcement on the
// real (file-backed) SQLite connector. Regression tests for the DSN/pragma
// fix: without the "file:" URI prefix, modernc.org/sqlite silently ignored
// our pragmas and we would never have noticed.
package store_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/juex-ai/chanwire/server/internal/store"
)

// fileBackedStore opens a Store backed by a real file in t.TempDir so we can
// observe pragmas (in-memory DBs don't honour journal_mode=WAL).
func fileBackedStore(t *testing.T) *store.Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "chanwire-test.db")
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// TestPragmasApplied is the regression test for the missing "file:" URI
// prefix bug. Without it, modernc.org/sqlite treated the pragmas as part of
// the filename and never applied them — FK checks were off and journal_mode
// stayed at "delete". This test fails loudly if that ever regresses.
func TestPragmasApplied(t *testing.T) {
	s := fileBackedStore(t)
	ctx := context.Background()

	fk, err := s.PragmaInt(ctx, "foreign_keys")
	if err != nil {
		t.Fatalf("PRAGMA foreign_keys: %v", err)
	}
	if fk != 1 {
		t.Fatalf("PRAGMA foreign_keys: want 1, got %d", fk)
	}

	jm, err := s.PragmaString(ctx, "journal_mode")
	if err != nil {
		t.Fatalf("PRAGMA journal_mode: %v", err)
	}
	if !strings.EqualFold(jm, "wal") {
		t.Fatalf("PRAGMA journal_mode: want wal, got %q", jm)
	}
}

// TestForeignKeyEnforced is the positive regression test that pragmas are not
// just reported as ON but actually enforced by the SQL engine. Inserting a
// message that references a non-existent agent must fail.
func TestForeignKeyEnforced(t *testing.T) {
	s := fileBackedStore(t)
	ctx := context.Background()

	// Register one valid agent to use as the recipient (otherwise both FKs
	// fail and we wouldn't be sure which one tripped).
	bob, err := s.RegisterAgent(ctx, "bob")
	if err != nil {
		t.Fatalf("register bob: %v", err)
	}

	// Now try to insert a message whose from_agent_id does NOT exist.
	// This MUST fail when FK enforcement is on. If it succeeds, FKs are
	// off and the schema is silently corrupt.
	_, err = s.InsertMessage(ctx, 99999, "ghost", bob.ID, "should not persist")
	if err == nil {
		t.Fatal("InsertMessage with bogus from_agent_id succeeded — foreign keys are NOT being enforced")
	}
	// modernc.org/sqlite returns an error mentioning "FOREIGN KEY".
	if !strings.Contains(strings.ToLower(err.Error()), "foreign key") {
		t.Fatalf("unexpected error (want a FOREIGN KEY constraint failure): %v", err)
	}
}

// TestRegisterIdempotent re-tests the basic register-twice case against the
// file-backed store, since the rest of the suite uses in-memory.
func TestRegisterIdempotent(t *testing.T) {
	s := fileBackedStore(t)
	ctx := context.Background()

	a1, err := s.RegisterAgent(ctx, "alice")
	if err != nil {
		t.Fatalf("first register: %v", err)
	}
	a2, err := s.RegisterAgent(ctx, "alice")
	if err != nil {
		t.Fatalf("second register: %v", err)
	}
	if a1.Token != a2.Token {
		t.Fatalf("idempotent register: tokens differ (%q vs %q)", a1.Token, a2.Token)
	}
	if a1.ID != a2.ID {
		t.Fatalf("idempotent register: ids differ (%d vs %d)", a1.ID, a2.ID)
	}
}

func TestSystemMessageDoesNotRequireRegisteredSender(t *testing.T) {
	s := fileBackedStore(t)
	ctx := context.Background()

	bob, err := s.RegisterAgent(ctx, "bob")
	if err != nil {
		t.Fatalf("register bob: %v", err)
	}
	msg, err := s.InsertSystemMessage(ctx, bob.ID, "from web")
	if err != nil {
		t.Fatalf("insert system message: %v", err)
	}
	if msg.FromAgent != "system" || msg.ToAgent != "bob" {
		t.Fatalf("unexpected endpoints: %+v", msg)
	}

	messages, err := s.ListMessages(ctx, 20, 0)
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	if len(messages) != 1 || messages[0].FromAgent != "system" || messages[0].ToAgent != "bob" {
		t.Fatalf("unexpected listed messages: %+v", messages)
	}
}
