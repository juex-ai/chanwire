// Package store manages the SQLite database connection, migrations, and queries.
package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite" // pure-Go SQLite driver
)

// Store wraps the database connection and exposes all query methods.
type Store struct {
	db *sql.DB
}

// Open opens (or creates) the SQLite database at dbPath, runs migrations,
// and returns a ready Store.
func Open(dbPath string) (*Store, error) {
	// Auto-create the parent directory.
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, fmt.Errorf("create db dir: %w", err)
	}

	dsn := dbPath + "?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	// Each connection needs these pragmas. Using SetMaxOpenConns(1) is the
	// simplest way to guarantee WAL and FK are always in effect without a
	// ConnectHook.
	db.SetMaxOpenConns(1)

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return s, nil
}

// OpenMemory opens an in-memory SQLite database for testing.
func OpenMemory() (*Store, error) {
	// Use a URI with shared cache so multiple Open calls get the same DB.
	dsn := "file::memory:?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&cache=shared"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite memory: %w", err)
	}
	db.SetMaxOpenConns(1)

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return s, nil
}

// Close closes the underlying database.
func (s *Store) Close() error {
	return s.db.Close()
}

// migrate creates tables and indexes if they don't already exist.
func (s *Store) migrate() error {
	ddl := `
CREATE TABLE IF NOT EXISTS agents (
    id             INTEGER PRIMARY KEY,
    name           TEXT    UNIQUE NOT NULL,
    token          TEXT    UNIQUE NOT NULL,
    last_active_at INTEGER,
    created_at     INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS messages (
    id            INTEGER PRIMARY KEY,
    from_agent_id INTEGER NOT NULL REFERENCES agents(id),
    to_agent_id   INTEGER NOT NULL REFERENCES agents(id),
    content       TEXT    NOT NULL,
    created_at    INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_messages_to ON messages(to_agent_id, id);
`
	_, err := s.db.ExecContext(context.Background(), ddl)
	return err
}

// nowMillis returns the current time as unix milliseconds.
func nowMillis() int64 {
	return time.Now().UnixMilli()
}

// generateToken creates a 32-byte random token, base64url-encoded without padding.
func generateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// Agent represents a row from the agents table.
type Agent struct {
	ID           int64
	Name         string
	Token        string
	LastActiveAt *int64
	CreatedAt    int64
}

// RegisterAgent registers a new agent or returns the existing one (idempotent on name).
func (s *Store) RegisterAgent(ctx context.Context, name string) (*Agent, error) {
	// Check if already exists.
	existing, err := s.GetAgentByName(ctx, name)
	if err == nil {
		return existing, nil
	}
	if err != sql.ErrNoRows {
		return nil, fmt.Errorf("lookup agent: %w", err)
	}

	token, err := generateToken()
	if err != nil {
		return nil, fmt.Errorf("generate token: %w", err)
	}

	now := nowMillis()
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO agents (name, token, created_at) VALUES (?, ?, ?)`,
		name, token, now,
	)
	if err != nil {
		// Could be a race — try to fetch the existing row.
		existing, err2 := s.GetAgentByName(ctx, name)
		if err2 == nil {
			return existing, nil
		}
		return nil, fmt.Errorf("insert agent: %w", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("last insert id: %w", err)
	}

	return &Agent{
		ID:        id,
		Name:      name,
		Token:     token,
		CreatedAt: now,
	}, nil
}

// GetAgentByToken looks up an agent by token. Returns sql.ErrNoRows if not found.
func (s *Store) GetAgentByToken(ctx context.Context, token string) (*Agent, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, name, token, last_active_at, created_at FROM agents WHERE token = ?`,
		token,
	)
	return scanAgent(row)
}

// GetAgentByName looks up an agent by name. Returns sql.ErrNoRows if not found.
func (s *Store) GetAgentByName(ctx context.Context, name string) (*Agent, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, name, token, last_active_at, created_at FROM agents WHERE name = ?`,
		name,
	)
	return scanAgent(row)
}

func scanAgent(row *sql.Row) (*Agent, error) {
	a := &Agent{}
	err := row.Scan(&a.ID, &a.Name, &a.Token, &a.LastActiveAt, &a.CreatedAt)
	if err != nil {
		return nil, err
	}
	return a, nil
}

// UpdateLastActive sets last_active_at to now for the given agent id.
func (s *Store) UpdateLastActive(ctx context.Context, agentID int64) error {
	now := nowMillis()
	_, err := s.db.ExecContext(ctx,
		`UPDATE agents SET last_active_at = ? WHERE id = ?`,
		now, agentID,
	)
	return err
}

// ListAgents returns all agents ordered by id.
func (s *Store) ListAgents(ctx context.Context) ([]Agent, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, token, last_active_at, created_at FROM agents ORDER BY id ASC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var agents []Agent
	for rows.Next() {
		a := Agent{}
		if err := rows.Scan(&a.ID, &a.Name, &a.Token, &a.LastActiveAt, &a.CreatedAt); err != nil {
			return nil, err
		}
		agents = append(agents, a)
	}
	return agents, rows.Err()
}

// Message is a row from the messages table (with from_agent name resolved).
type Message struct {
	ID          int64
	FromAgentID int64
	FromAgent   string // joined from agents
	ToAgentID   int64
	Content     string
	CreatedAt   int64
}

// InsertMessage persists a new message and returns it.
func (s *Store) InsertMessage(ctx context.Context, fromAgentID, toAgentID int64, content string) (*Message, error) {
	now := nowMillis()
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO messages (from_agent_id, to_agent_id, content, created_at) VALUES (?, ?, ?, ?)`,
		fromAgentID, toAgentID, content, now,
	)
	if err != nil {
		return nil, fmt.Errorf("insert message: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("last insert id: %w", err)
	}

	// Fetch from_agent name.
	agent, err := s.getAgentByID(ctx, fromAgentID)
	if err != nil {
		return nil, fmt.Errorf("lookup from_agent: %w", err)
	}

	return &Message{
		ID:          id,
		FromAgentID: fromAgentID,
		FromAgent:   agent.Name,
		ToAgentID:   toAgentID,
		Content:     content,
		CreatedAt:   now,
	}, nil
}

// GetMessagesForAgent returns all messages addressed to toAgentID ordered by id ASC.
func (s *Store) GetMessagesForAgent(ctx context.Context, toAgentID int64) ([]Message, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT m.id, m.from_agent_id, a.name, m.to_agent_id, m.content, m.created_at
		   FROM messages m
		   JOIN agents a ON a.id = m.from_agent_id
		  WHERE m.to_agent_id = ?
		  ORDER BY m.id ASC`,
		toAgentID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var msgs []Message
	for rows.Next() {
		m := Message{}
		if err := rows.Scan(&m.ID, &m.FromAgentID, &m.FromAgent, &m.ToAgentID, &m.Content, &m.CreatedAt); err != nil {
			return nil, err
		}
		msgs = append(msgs, m)
	}
	return msgs, rows.Err()
}

func (s *Store) getAgentByID(ctx context.Context, id int64) (*Agent, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, name, token, last_active_at, created_at FROM agents WHERE id = ?`,
		id,
	)
	return scanAgent(row)
}
