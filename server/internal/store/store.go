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
	"strings"
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

	// The "file:" URI scheme is required for modernc.org/sqlite to parse
	// the query parameters (otherwise the whole DSN — pragmas included —
	// is treated as a literal filename). Pragmas are then applied on every
	// connection in the pool.
	dsn := "file:" + dbPath + "?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	// Cap the pool to a sane bound. WAL allows multiple concurrent readers
	// alongside one writer; SQLite serialises writes itself.
	db.SetMaxOpenConns(8)

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return s, nil
}

// OpenMemory opens an in-memory SQLite database for testing.
func OpenMemory() (*Store, error) {
	// Use a URI with shared cache so multiple connections from the pool
	// see the same database. Note: in-memory databases do not actually
	// switch into WAL mode (SQLite ignores journal_mode for :memory:), so
	// only verify the FK pragma in tests against this connector.
	dsn := "file::memory:?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&cache=shared"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite memory: %w", err)
	}
	// In-memory + shared cache requires a single connection to keep
	// every query targeting the same instance.
	db.SetMaxOpenConns(1)

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return s, nil
}

// PragmaInt returns the integer value of a SQLite pragma (e.g., "foreign_keys").
func (s *Store) PragmaInt(ctx context.Context, name string) (int, error) {
	var v int
	if err := s.db.QueryRowContext(ctx, "PRAGMA "+name).Scan(&v); err != nil {
		return 0, err
	}
	return v, nil
}

// PragmaString returns the string value of a SQLite pragma (e.g., "journal_mode").
func (s *Store) PragmaString(ctx context.Context, name string) (string, error) {
	var v string
	if err := s.db.QueryRowContext(ctx, "PRAGMA "+name).Scan(&v); err != nil {
		return "", err
	}
	return v, nil
}

// Close closes the underlying database.
func (s *Store) Close() error {
	return s.db.Close()
}

// migrate creates tables and indexes if they don't already exist, then upgrades
// older message schemas so web-originated "system" messages can be persisted
// without requiring a registered system agent.
func (s *Store) migrate() error {
	ddl := `
CREATE TABLE IF NOT EXISTS agents (
    id             INTEGER PRIMARY KEY,
    name           TEXT    UNIQUE NOT NULL,
    token          TEXT    UNIQUE NOT NULL,
    last_active_at INTEGER,
    created_at     INTEGER NOT NULL,
    deleted        INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS messages (
    id              INTEGER PRIMARY KEY,
    from_agent_id   INTEGER REFERENCES agents(id),
    from_agent_name TEXT    NOT NULL,
    to_agent_id     INTEGER NOT NULL REFERENCES agents(id),
    content         TEXT    NOT NULL,
    created_at      INTEGER NOT NULL
);
`
	if _, err := s.db.ExecContext(context.Background(), ddl); err != nil {
		return err
	}
	if err := s.ensureAgentDeletedSchema(context.Background()); err != nil {
		return err
	}
	if err := s.ensureSystemMessageSchema(context.Background()); err != nil {
		return err
	}
	indexes := `
CREATE INDEX IF NOT EXISTS idx_messages_to ON messages(to_agent_id, id);
CREATE INDEX IF NOT EXISTS idx_messages_created ON messages(created_at, id);
CREATE INDEX IF NOT EXISTS idx_agents_deleted_name ON agents(deleted, name);
`
	_, err := s.db.ExecContext(context.Background(), indexes)
	return err
}

func (s *Store) ensureAgentDeletedSchema(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `PRAGMA table_info(agents)`)
	if err != nil {
		return err
	}
	defer rows.Close()

	hasDeleted := false
	for rows.Next() {
		var cid int
		var name, typ string
		var notnull int
		var dflt any
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			return err
		}
		if name == "deleted" {
			hasDeleted = true
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if hasDeleted {
		return nil
	}
	_, err = s.db.ExecContext(ctx, `ALTER TABLE agents ADD COLUMN deleted INTEGER NOT NULL DEFAULT 0`)
	return err
}

func (s *Store) ensureSystemMessageSchema(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `PRAGMA table_info(messages)`)
	if err != nil {
		return err
	}
	defer rows.Close()

	hasFromName := false
	fromIDNotNull := false
	for rows.Next() {
		var cid int
		var name, typ string
		var notnull int
		var dflt any
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			return err
		}
		if name == "from_agent_name" {
			hasFromName = true
		}
		if name == "from_agent_id" {
			fromIDNotNull = notnull == 1
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if hasFromName && !fromIDNotNull {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	fromNameExpr := "COALESCE(a.name, 'system')"
	if hasFromName {
		fromNameExpr = "COALESCE(NULLIF(m.from_agent_name, ''), a.name, 'system')"
	}
	copyMessages := fmt.Sprintf(`INSERT INTO messages (id, from_agent_id, from_agent_name, to_agent_id, content, created_at)
SELECT m.id,
       m.from_agent_id,
       %s AS from_agent_name,
       m.to_agent_id,
       m.content,
       m.created_at
  FROM messages_old m
  LEFT JOIN agents a ON a.id = m.from_agent_id`, fromNameExpr)

	stmts := []string{
		`DROP INDEX IF EXISTS idx_messages_to`,
		`DROP INDEX IF EXISTS idx_messages_created`,
		`ALTER TABLE messages RENAME TO messages_old`,
		`CREATE TABLE messages (
    id              INTEGER PRIMARY KEY,
    from_agent_id   INTEGER REFERENCES agents(id),
    from_agent_name TEXT    NOT NULL,
    to_agent_id     INTEGER NOT NULL REFERENCES agents(id),
    content         TEXT    NOT NULL,
    created_at      INTEGER NOT NULL
)`,
		copyMessages,
		`DROP TABLE messages_old`,
	}
	for _, stmt := range stmts {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return tx.Commit()
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
	Deleted      bool
}

// AgentAdminInfo is the settings-page projection for one active agent.
type AgentAdminInfo struct {
	Agent
	RelatedAgentCount int
}

// RegisterAgent registers a new agent or returns the existing one (idempotent on name).
func (s *Store) RegisterAgent(ctx context.Context, name string) (*Agent, error) {
	// Check if already exists.
	existing, err := s.getAgentByNameAny(ctx, name)
	if err == nil {
		if existing.Deleted {
			if err := s.reactivateAgent(ctx, existing.ID); err != nil {
				return nil, fmt.Errorf("reactivate agent: %w", err)
			}
			existing.Deleted = false
		}
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
		existing, err2 := s.getAgentByNameAny(ctx, name)
		if err2 == nil {
			if existing.Deleted {
				if err := s.reactivateAgent(ctx, existing.ID); err != nil {
					return nil, fmt.Errorf("reactivate agent after race: %w", err)
				}
				existing.Deleted = false
			}
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
		`SELECT id, name, token, last_active_at, created_at, deleted FROM agents WHERE token = ? AND deleted = 0`,
		token,
	)
	return scanAgent(row)
}

// GetAgentByName looks up an agent by name. Returns sql.ErrNoRows if not found.
func (s *Store) GetAgentByName(ctx context.Context, name string) (*Agent, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, name, token, last_active_at, created_at, deleted FROM agents WHERE name = ? AND deleted = 0`,
		name,
	)
	return scanAgent(row)
}

func (s *Store) getAgentByNameAny(ctx context.Context, name string) (*Agent, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, name, token, last_active_at, created_at, deleted FROM agents WHERE name = ?`,
		name,
	)
	return scanAgent(row)
}

func scanAgent(row *sql.Row) (*Agent, error) {
	a := &Agent{}
	deleted := 0
	err := row.Scan(&a.ID, &a.Name, &a.Token, &a.LastActiveAt, &a.CreatedAt, &deleted)
	if err != nil {
		return nil, err
	}
	a.Deleted = deleted != 0
	return a, nil
}

func (s *Store) reactivateAgent(ctx context.Context, agentID int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE agents SET deleted = 0 WHERE id = ?`, agentID)
	return err
}

// DeleteAgentByName soft-deletes an active agent by name.
func (s *Store) DeleteAgentByName(ctx context.Context, name string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE agents SET deleted = 1 WHERE name = ? AND deleted = 0`, name)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
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
		`SELECT id, name, token, last_active_at, created_at, deleted FROM agents WHERE deleted = 0 ORDER BY id ASC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var agents []Agent
	for rows.Next() {
		a := Agent{}
		deleted := 0
		if err := rows.Scan(&a.ID, &a.Name, &a.Token, &a.LastActiveAt, &a.CreatedAt, &deleted); err != nil {
			return nil, err
		}
		a.Deleted = deleted != 0
		agents = append(agents, a)
	}
	return agents, rows.Err()
}

// ListAdminAgents returns non-deleted agents ordered for the settings table.
// hasMore reports whether another page exists after the returned slice.
func (s *Store) ListAdminAgents(ctx context.Context, limit, offset int, sinceMillis int64) ([]AgentAdminInfo, bool, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, token, last_active_at, created_at, deleted
		   FROM agents
		  WHERE deleted = 0
		  ORDER BY name ASC
		  LIMIT ? OFFSET ?`,
		limit+1,
		offset,
	)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()

	adminAgents := []AgentAdminInfo{}
	for rows.Next() {
		info := AgentAdminInfo{}
		deleted := 0
		if err := rows.Scan(&info.ID, &info.Name, &info.Token, &info.LastActiveAt, &info.CreatedAt, &deleted); err != nil {
			return nil, false, err
		}
		info.Deleted = deleted != 0
		adminAgents = append(adminAgents, info)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}

	hasMore := len(adminAgents) > limit
	if hasMore {
		adminAgents = adminAgents[:limit]
	}
	for i := range adminAgents {
		count, err := s.countRelatedAgentsSince(ctx, adminAgents[i].ID, sinceMillis)
		if err != nil {
			return nil, false, err
		}
		adminAgents[i].RelatedAgentCount = count
	}
	return adminAgents, hasMore, nil
}

func (s *Store) countRelatedAgentsSince(ctx context.Context, agentID int64, sinceMillis int64) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `
SELECT COUNT(DISTINCT rel.other_id)
  FROM (
        SELECT m.to_agent_id AS other_id
          FROM messages m
         WHERE m.from_agent_id = ?
           AND m.created_at >= ?
        UNION
        SELECT m.from_agent_id AS other_id
          FROM messages m
         WHERE m.to_agent_id = ?
           AND m.from_agent_id IS NOT NULL
           AND m.created_at >= ?
       ) rel
  JOIN agents a ON a.id = rel.other_id
 WHERE rel.other_id <> ?
   AND a.deleted = 0`, agentID, sinceMillis, agentID, sinceMillis, agentID).Scan(&count)
	return count, err
}

// Message is a row from the messages table (with endpoint agent names resolved).
type Message struct {
	ID          int64
	FromAgentID *int64
	FromAgent   string
	ToAgentID   int64
	ToAgent     string
	Content     string
	CreatedAt   int64
}

// InsertMessage persists a new agent-authenticated message and returns it. The
// caller passes the already-known sender name (resolved by auth middleware) so
// this is a single-query write — no extra SELECT for the sender name on the hot path.
func (s *Store) InsertMessage(ctx context.Context, fromAgentID int64, fromAgentName string, toAgentID int64, content string) (*Message, error) {
	if strings.TrimSpace(fromAgentName) == "" {
		return nil, fmt.Errorf("from agent name is required")
	}
	return s.insertMessage(ctx, &fromAgentID, fromAgentName, toAgentID, content)
}

// InsertSystemMessage persists a web-originated message from the special system sender.
func (s *Store) InsertSystemMessage(ctx context.Context, toAgentID int64, content string) (*Message, error) {
	return s.insertMessage(ctx, nil, "system", toAgentID, content)
}

func (s *Store) insertMessage(ctx context.Context, fromAgentID *int64, fromAgentName string, toAgentID int64, content string) (*Message, error) {
	now := nowMillis()
	var fromAgentValue any
	var storedFromAgentID *int64
	if fromAgentID != nil {
		id := *fromAgentID
		fromAgentValue = id
		storedFromAgentID = &id
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO messages (from_agent_id, from_agent_name, to_agent_id, content, created_at) VALUES (?, ?, ?, ?, ?)`,
		fromAgentValue, fromAgentName, toAgentID, content, now,
	)
	if err != nil {
		return nil, fmt.Errorf("insert message: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("last insert id: %w", err)
	}

	toAgent, err := s.GetAgentNameByID(ctx, toAgentID)
	if err != nil {
		return nil, fmt.Errorf("lookup recipient: %w", err)
	}
	return &Message{
		ID:          id,
		FromAgentID: storedFromAgentID,
		FromAgent:   fromAgentName,
		ToAgentID:   toAgentID,
		ToAgent:     toAgent,
		Content:     content,
		CreatedAt:   now,
	}, nil
}

// GetAgentNameByID returns an agent name for an id.
func (s *Store) GetAgentNameByID(ctx context.Context, id int64) (string, error) {
	var name string
	if err := s.db.QueryRowContext(ctx, `SELECT name FROM agents WHERE id = ?`, id).Scan(&name); err != nil {
		return "", err
	}
	return name, nil
}

// GetMessagesForAgent returns all messages addressed to toAgentID ordered by id ASC.
func (s *Store) GetMessagesForAgent(ctx context.Context, toAgentID int64) ([]Message, error) {
	return s.GetRecentMessagesForAgent(ctx, toAgentID, 0)
}

// GetRecentMessagesForAgent returns the latest limit messages for toAgentID in
// chronological order. A non-positive limit returns all messages.
func (s *Store) GetRecentMessagesForAgent(ctx context.Context, toAgentID int64, limit int) ([]Message, error) {
	query := `SELECT m.id, m.from_agent_id, m.from_agent_name, m.to_agent_id, ta.name AS to_agent, m.content, m.created_at
		   FROM messages m
		   JOIN agents ta ON ta.id = m.to_agent_id
		  WHERE m.to_agent_id = ?
		  ORDER BY m.id ASC`
	args := []any{toAgentID}

	if limit > 0 {
		query = `SELECT id, from_agent_id, from_agent_name, to_agent_id, to_agent, content, created_at
		   FROM (
		         SELECT m.id, m.from_agent_id, m.from_agent_name, m.to_agent_id, ta.name AS to_agent, m.content, m.created_at
		           FROM messages m
		           JOIN agents ta ON ta.id = m.to_agent_id
		          WHERE m.to_agent_id = ?
		          ORDER BY m.id DESC
		          LIMIT ?
		        )
		  ORDER BY id ASC`
		args = append(args, limit)
	}
	return s.scanMessages(ctx, query, args...)
}

// ListMessages returns recent messages across the system in chronological order.
// If beforeID is positive, only older messages with id < beforeID are returned.
func (s *Store) ListMessages(ctx context.Context, limit int, beforeID int64) ([]Message, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	where := ""
	args := []any{}
	if beforeID > 0 {
		where = "WHERE m.id < ?"
		args = append(args, beforeID)
	}
	args = append(args, limit)
	query := fmt.Sprintf(`SELECT id, from_agent_id, from_agent_name, to_agent_id, to_agent, content, created_at
		   FROM (
		         SELECT m.id, m.from_agent_id, m.from_agent_name, m.to_agent_id, ta.name AS to_agent, m.content, m.created_at
		           FROM messages m
		           JOIN agents ta ON ta.id = m.to_agent_id
		          %s
		          ORDER BY m.id DESC
		          LIMIT ?
		        )
		  ORDER BY id ASC`, where)
	return s.scanMessages(ctx, query, args...)
}

// ListMessageEdgesSince returns directed sender/recipient relationships for
// messages created at or after sinceMillis. System-originated messages are not
// agent-to-agent edges and are intentionally omitted.
func (s *Store) ListMessageEdgesSince(ctx context.Context, sinceMillis int64) ([]MessageEdge, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT DISTINCT m.from_agent_id, m.to_agent_id
  FROM messages m
 WHERE m.from_agent_id IS NOT NULL
   AND m.created_at >= ?`, sinceMillis)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var edges []MessageEdge
	for rows.Next() {
		var edge MessageEdge
		if err := rows.Scan(&edge.FromAgentID, &edge.ToAgentID); err != nil {
			return nil, err
		}
		edges = append(edges, edge)
	}
	return edges, rows.Err()
}

// MessageEdge is a directed relationship between two registered agents.
type MessageEdge struct {
	FromAgentID int64
	ToAgentID   int64
}

func (s *Store) scanMessages(ctx context.Context, query string, args ...any) ([]Message, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var msgs []Message
	for rows.Next() {
		m := Message{}
		if err := rows.Scan(&m.ID, &m.FromAgentID, &m.FromAgent, &m.ToAgentID, &m.ToAgent, &m.Content, &m.CreatedAt); err != nil {
			return nil, err
		}
		msgs = append(msgs, m)
	}
	return msgs, rows.Err()
}
