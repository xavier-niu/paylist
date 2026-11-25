// Package store persists users, sessions and each user's spending table in SQLite.
//
// Every query that touches user data is scoped by user_id; nothing here returns
// another user's rows. API keys are sealed with AES-256-GCM (see crypto.go).
package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite" // pure-Go driver: keeps the binary CGO-free
)

var (
	ErrNotFound           = errors.New("not found")
	ErrEmailTaken         = errors.New("an account with this email already exists")
	ErrNameTaken          = errors.New("an account with this name already exists")
	ErrInvalidCredentials = errors.New("email or password is incorrect")
	ErrSetupDone          = errors.New("this server already has accounts")
)

type Store struct {
	db     *sql.DB
	sealer *sealer
}

// Open opens (creating if needed) the database at path and applies migrations.
// secret is the 32-byte key used to encrypt stored API keys.
func Open(path string, secret []byte) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	sl, err := newSealer(secret)
	if err != nil {
		return nil, err
	}
	// Transactions take the write lock as they begin (_txlock=immediate), so
	// writes at once (the agent in the background, the page) wait their turn
	// rather than failing with "database is locked".
	dsn := "file:" + path + "?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)&_txlock=immediate"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(4)
	s := &Store{db: db, sealer: sl}
	if err := s.migrate(context.Background()); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

// Migrations run in order; PRAGMA user_version records how many have been applied.
var migrations = []string{
	`CREATE TABLE users (
		id            INTEGER PRIMARY KEY,
		email         TEXT NOT NULL UNIQUE COLLATE NOCASE,
		name          TEXT NOT NULL DEFAULT '',
		password_hash TEXT NOT NULL,
		is_admin      INTEGER NOT NULL DEFAULT 0,
		prefs         TEXT NOT NULL DEFAULT '{}',
		created_at    TEXT NOT NULL,
		updated_at    TEXT NOT NULL
	);
	CREATE TABLE sessions (
		token_hash TEXT PRIMARY KEY,
		user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		created_at TEXT NOT NULL,
		expires_at TEXT NOT NULL
	);
	CREATE INDEX sessions_user ON sessions(user_id);
	CREATE TABLE model_settings (
		user_id     INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
		active      TEXT NOT NULL DEFAULT '',
		providers   TEXT NOT NULL DEFAULT '{}',
		sealed_keys BLOB
	);
	CREATE TABLE agent_tokens (
		id           INTEGER PRIMARY KEY,
		user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		name         TEXT NOT NULL,
		token_hash   TEXT NOT NULL UNIQUE,
		hint         TEXT NOT NULL,
		created_at   TEXT NOT NULL,
		last_used_at TEXT
	);
	CREATE INDEX agent_tokens_user ON agent_tokens(user_id);
	-- The choices for an entry's type and card, like a spreadsheet's dropdowns.
	CREATE TABLE options (
		id       INTEGER PRIMARY KEY,
		user_id  INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		kind     TEXT NOT NULL CHECK (kind IN ('type', 'card')),
		name     TEXT NOT NULL COLLATE NOCASE,
		color    TEXT NOT NULL DEFAULT 'gray',
		position INTEGER NOT NULL DEFAULT 0,
		UNIQUE (user_id, kind, name)
	);
	-- One row of the table. Amounts are kept in cents; type and card hold an
	-- option's name ('' for none).
	CREATE TABLE entries (
		id         INTEGER PRIMARY KEY,
		user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		name       TEXT NOT NULL,
		type       TEXT NOT NULL DEFAULT '' COLLATE NOCASE,
		amount     INTEGER NOT NULL,
		date       TEXT NOT NULL,
		card       TEXT NOT NULL DEFAULT '' COLLATE NOCASE,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	);
	CREATE INDEX entries_user_date ON entries(user_id, date);
	-- Conversations with the agent page's assistant.
	CREATE TABLE agent_chats (
		id         TEXT NOT NULL,
		user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		title      TEXT NOT NULL,
		messages   TEXT NOT NULL,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		PRIMARY KEY (user_id, id)
	);
	CREATE INDEX agent_chats_recent ON agent_chats(user_id, updated_at);`,

	// Ledgers: entries and their types and cards belong to a ledger, which
	// its owner can share with editors and viewers. Each user's existing
	// entries become their personal ledger, with the user's id and currency.
	`CREATE TABLE ledgers (
		id         INTEGER PRIMARY KEY,
		owner_id   INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		name       TEXT NOT NULL,
		currency   TEXT NOT NULL DEFAULT 'CNY',
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	);
	CREATE TABLE ledger_members (
		ledger_id  INTEGER NOT NULL REFERENCES ledgers(id) ON DELETE CASCADE,
		user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		role       TEXT NOT NULL CHECK (role IN ('owner', 'editor', 'viewer')),
		created_at TEXT NOT NULL,
		PRIMARY KEY (ledger_id, user_id)
	);
	CREATE INDEX ledger_members_user ON ledger_members(user_id);
	INSERT INTO ledgers (id, owner_id, name, currency, created_at, updated_at)
		SELECT id, id, 'Personal', COALESCE(NULLIF(json_extract(prefs, '$.currency'), ''), 'CNY'), created_at, created_at FROM users;
	INSERT INTO ledger_members (ledger_id, user_id, role, created_at) SELECT id, id, 'owner', created_at FROM users;

	CREATE TABLE options_new (
		id        INTEGER PRIMARY KEY,
		ledger_id INTEGER NOT NULL REFERENCES ledgers(id) ON DELETE CASCADE,
		kind      TEXT NOT NULL CHECK (kind IN ('type', 'card')),
		name      TEXT NOT NULL COLLATE NOCASE,
		color     TEXT NOT NULL DEFAULT 'gray',
		position  INTEGER NOT NULL DEFAULT 0,
		UNIQUE (ledger_id, kind, name)
	);
	INSERT INTO options_new (id, ledger_id, kind, name, color, position) SELECT id, user_id, kind, name, color, position FROM options;
	DROP TABLE options;
	ALTER TABLE options_new RENAME TO options;

	CREATE TABLE entries_new (
		id         INTEGER PRIMARY KEY,
		ledger_id  INTEGER NOT NULL REFERENCES ledgers(id) ON DELETE CASCADE,
		name       TEXT NOT NULL,
		type       TEXT NOT NULL DEFAULT '' COLLATE NOCASE,
		amount     INTEGER NOT NULL,
		date       TEXT NOT NULL,
		card       TEXT NOT NULL DEFAULT '' COLLATE NOCASE,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	);
	INSERT INTO entries_new (id, ledger_id, name, type, amount, date, card, created_at, updated_at)
		SELECT id, user_id, name, type, amount, date, card, created_at, updated_at FROM entries;
	DROP TABLE entries;
	ALTER TABLE entries_new RENAME TO entries;
	CREATE INDEX entries_ledger_date ON entries(ledger_id, date);`,

	// 3: what an entry paid in another currency was: its amount there (in
	// cents) and the currency; the entry's amount is in the ledger's.
	`ALTER TABLE entries ADD COLUMN orig_amount INTEGER;
	ALTER TABLE entries ADD COLUMN orig_currency TEXT NOT NULL DEFAULT '';`,

	// 4: each entry's history: who created or changed it, how (the app, the
	// agent, MCP), when, and what changed, from what to what.
	`CREATE TABLE entry_events (
		id         INTEGER PRIMARY KEY,
		entry_id   INTEGER NOT NULL REFERENCES entries(id) ON DELETE CASCADE,
		user_id    INTEGER REFERENCES users(id) ON DELETE SET NULL,
		via        TEXT NOT NULL DEFAULT '',
		action     TEXT NOT NULL,
		changes    TEXT NOT NULL DEFAULT '{}',
		created_at TEXT NOT NULL
	);
	CREATE INDEX entry_events_entry ON entry_events(entry_id, id);`,

	// 5: names are unique (ignoring case), so the history can go by them; a
	// name already shared keeps its first holder, and the others get their
	// account number after it: "Sam (7)".
	`UPDATE users SET name = name || ' (' || id || ')'
		WHERE name <> '' AND id NOT IN (SELECT MIN(id) FROM users WHERE name <> '' GROUP BY lower(name));
	CREATE UNIQUE INDEX users_name ON users(name COLLATE NOCASE) WHERE name <> '';`,
}

func (s *Store) migrate(ctx context.Context) error {
	var version int
	if err := s.db.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil {
		return err
	}
	for i := version; i < len(migrations); i++ {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, migrations[i]); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %d: %w", i+1, err)
		}
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(`PRAGMA user_version = %d`, i+1)); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

func now() string { return time.Now().UTC().Format(time.RFC3339) }

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
