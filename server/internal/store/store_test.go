// Package store_test exercises pragma application and FK enforcement on the
// real (file-backed) SQLite connector. Regression tests for the DSN/pragma
// fix: without the "file:" URI prefix, modernc.org/sqlite silently ignored
// our pragmas and we would never have noticed.
package store_test

import (
	"context"
	"database/sql"
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

func TestAgentSoftDeleteAndReactivation(t *testing.T) {
	s := fileBackedStore(t)
	ctx := context.Background()

	alice, err := s.RegisterAgent(ctx, "alice")
	if err != nil {
		t.Fatalf("register alice: %v", err)
	}
	bob, err := s.RegisterAgent(ctx, "bob")
	if err != nil {
		t.Fatalf("register bob: %v", err)
	}
	if _, err := s.InsertMessage(ctx, alice.ID, alice.Name, bob.ID, "before delete"); err != nil {
		t.Fatalf("insert alice->bob: %v", err)
	}

	if err := s.DeleteAgentByName(ctx, "bob"); err != nil {
		t.Fatalf("delete bob: %v", err)
	}

	agents, err := s.ListAgents(ctx)
	if err != nil {
		t.Fatalf("list agents after delete: %v", err)
	}
	if len(agents) != 1 || agents[0].Name != "alice" {
		t.Fatalf("deleted agent should be hidden from normal list, got %+v", agents)
	}
	if _, err := s.GetAgentByName(ctx, "bob"); err != sql.ErrNoRows {
		t.Fatalf("deleted agent should be hidden by name: %v", err)
	}
	if _, err := s.GetAgentByToken(ctx, bob.Token); err != sql.ErrNoRows {
		t.Fatalf("deleted agent token should not authenticate: %v", err)
	}
	adminAgents, hasMore, err := s.ListAdminAgents(ctx, 10, 0, 0)
	if err != nil {
		t.Fatalf("list admin agents after delete: %v", err)
	}
	if hasMore {
		t.Fatal("list admin agents should not report more rows")
	}
	for _, agent := range adminAgents {
		if agent.Name == "bob" {
			t.Fatalf("deleted agent should be hidden from admin list: %+v", adminAgents)
		}
	}

	bobAgain, err := s.RegisterAgent(ctx, "bob")
	if err != nil {
		t.Fatalf("reactivate bob: %v", err)
	}
	if bobAgain.ID != bob.ID || bobAgain.Token != bob.Token {
		t.Fatalf("reactivated agent should keep id/token: before=%+v after=%+v", bob, bobAgain)
	}
	adminAgents, _, err = s.ListAdminAgents(ctx, 10, 0, 0)
	if err != nil {
		t.Fatalf("list admin agents after reactivation: %v", err)
	}
	foundBob := false
	for _, agent := range adminAgents {
		if agent.Name == "bob" {
			foundBob = true
			if agent.RelatedAgentCount != 1 {
				t.Fatalf("bob related count: want 1, got %d", agent.RelatedAgentCount)
			}
		}
	}
	if !foundBob {
		t.Fatalf("reactivated agent should return to admin list: %+v", adminAgents)
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
