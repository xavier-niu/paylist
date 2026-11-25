package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
)

// Each entry keeps its history: who created it and who changed what, from
// what to what, and when, and through what: the app, the agent or MCP.

// Actor is who makes a change, and through what: "" for the app, "agent"
// or "mcp".
type Actor struct {
	UserID int64
	Via    string
}

type actorKey struct{}

// WithActor says who the changes made with ctx are by.
func WithActor(ctx context.Context, userID int64, via string) context.Context {
	return context.WithValue(ctx, actorKey{}, Actor{userID, via})
}

func actorOf(ctx context.Context) Actor {
	a, _ := ctx.Value(actorKey{}).(Actor)
	return a
}

// What happened to an entry.
const (
	EntryCreated  = "created"
	EntryChanged  = "changed"
	EntryRefunded = "refunded" // in part: its amount went down by the refund
)

// EntryEvent is one step in an entry's history. Changes holds, for each field
// that changed, what it was and what it became; amounts are in the ledger's
// currency, and "orig" (paid in another currency) is written "12.50 USD".
type EntryEvent struct {
	ID      int64            `json:"id"`
	Action  string           `json:"action"`
	Changes map[string][]any `json:"changes,omitempty"`
	UserID  int64            `json:"user_id,omitempty"`
	By      string           `json:"by"`                 // their name, or email; "" when the account is gone or unknown
	ByName  string           `json:"by_name,omitempty"`  // their name (unique), when they have one
	ByEmail string           `json:"by_email,omitempty"` // their email
	Via     string           `json:"via,omitempty"`
	At      string           `json:"at"`
}

// change is one field's before and after.
type change map[string][]any

func (c change) add(field string, before, after any) {
	if before != after {
		c[field] = []any{before, after}
	}
}

func origText(cents sql.NullInt64, cur string) string {
	if !cents.Valid || cur == "" {
		return ""
	}
	return fmt.Sprintf("%.2f %s", fromCents(cents.Int64), cur)
}

func roundCents(v float64) float64 { return math.Round(v*100) / 100 }

// logEntry records a step in an entry's history, by whoever ctx says.
func logEntry(ctx context.Context, tx *sql.Tx, entryID int64, action string, c change) error {
	if action == EntryChanged && len(c) == 0 {
		return nil
	}
	b, err := json.Marshal(c)
	if err != nil {
		return err
	}
	a := actorOf(ctx)
	var user sql.NullInt64
	if a.UserID != 0 {
		user = sql.NullInt64{Int64: a.UserID, Valid: true}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO entry_events (entry_id, user_id, via, action, changes, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		entryID, user, a.Via, action, string(b), now())
	return err
}

// EntryHistory is an entry's history, oldest first.
func (s *Store) EntryHistory(ctx context.Context, ledgerID, entryID int64) ([]EntryEvent, error) {
	if _, err := s.GetEntry(ctx, ledgerID, entryID); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT ev.id, ev.action, ev.changes, COALESCE(ev.user_id, 0),
			COALESCE(NULLIF(u.name, ''), u.email, ''), COALESCE(u.name, ''), COALESCE(u.email, ''), ev.via, ev.created_at
		FROM entry_events ev LEFT JOIN users u ON u.id = ev.user_id
		WHERE ev.entry_id = ? ORDER BY ev.id`, entryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []EntryEvent{}
	for rows.Next() {
		var ev EntryEvent
		var changes string
		if err := rows.Scan(&ev.ID, &ev.Action, &changes, &ev.UserID, &ev.By, &ev.ByName, &ev.ByEmail, &ev.Via, &ev.At); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(changes), &ev.Changes)
		out = append(out, ev)
	}
	return out, rows.Err()
}
