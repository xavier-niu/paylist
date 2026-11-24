package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"hash/fnv"
	"strings"
	"unicode/utf8"
)

// Options are the choices for an entry's type and card, like a spreadsheet
// column's dropdown. Entries keep the option's name, so renaming an option
// renames it on every entry, and deleting one clears it from them.

const (
	KindType = "type"
	KindCard = "card"

	maxOptionName = 40
	maxOptions    = 100 // of each kind
)

// Colors an option can have; the web app maps each to its palette.
var Colors = []string{"gray", "brown", "orange", "yellow", "green", "blue", "purple", "pink", "red"}

var defaultTypes = []string{"Food", "Groceries", "Transport", "Shopping", "Housing", "Utilities", "Entertainment", "Health", "Travel", "Other"}

type Option struct {
	ID    int64  `json:"id"`
	Kind  string `json:"kind"`
	Name  string `json:"name"`
	Color string `json:"color"`
	Uses  int    `json:"uses"` // how many entries have it
}

type Options struct {
	Types []Option `json:"types"`
	Cards []Option `json:"cards"`
}

func validKind(kind string) error {
	if kind != KindType && kind != KindCard {
		return fmt.Errorf("kind must be %q or %q", KindType, KindCard)
	}
	return nil
}

func validColor(c string) bool {
	for _, x := range Colors {
		if x == c {
			return true
		}
	}
	return false
}

// colorFor picks a stable color for a new option from its name.
func colorFor(name string) string {
	h := fnv.New32a()
	h.Write([]byte(strings.ToLower(name)))
	return Colors[1+int(h.Sum32()%uint32(len(Colors)-1))] // gray is kept for "Other"-like picks
}

func cleanOptionName(name string) (string, error) {
	name = strings.Join(strings.Fields(name), " ")
	if name == "" {
		return "", errors.New("an option needs a name")
	}
	if utf8.RuneCountInString(name) > maxOptionName {
		return "", fmt.Errorf("option names are at most %d characters", maxOptionName)
	}
	return name, nil
}

func (s *Store) seedOptions(ctx context.Context, ledgerID int64) error {
	for i, name := range defaultTypes {
		color := colorFor(name)
		if name == "Other" {
			color = "gray"
		}
		if _, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO options (ledger_id, kind, name, color, position) VALUES (?, 'type', ?, ?, ?)`,
			ledgerID, name, color, i); err != nil {
			return err
		}
	}
	_, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO options (ledger_id, kind, name, color, position) VALUES (?, 'card', 'Cash', 'green', 0)`, ledgerID)
	return err
}

func (s *Store) ListOptions(ctx context.Context, ledgerID int64) (Options, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT o.id, o.kind, o.name, o.color,
			(SELECT COUNT(*) FROM entries e WHERE e.ledger_id = o.ledger_id AND
				CASE o.kind WHEN 'type' THEN e.type ELSE e.card END = o.name)
		FROM options o WHERE o.ledger_id = ? ORDER BY o.kind, o.position, o.id`, ledgerID)
	if err != nil {
		return Options{}, err
	}
	defer rows.Close()
	out := Options{Types: []Option{}, Cards: []Option{}}
	for rows.Next() {
		var o Option
		if err := rows.Scan(&o.ID, &o.Kind, &o.Name, &o.Color, &o.Uses); err != nil {
			return Options{}, err
		}
		if o.Kind == KindType {
			out.Types = append(out.Types, o)
		} else {
			out.Cards = append(out.Cards, o)
		}
	}
	return out, rows.Err()
}

func (s *Store) getOption(ctx context.Context, q querier, ledgerID, id int64) (*Option, error) {
	var o Option
	err := q.QueryRowContext(ctx, `SELECT id, kind, name, color FROM options WHERE ledger_id = ? AND id = ?`, ledgerID, id).
		Scan(&o.ID, &o.Kind, &o.Name, &o.Color)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &o, err
}

// FindOption returns the option of this kind with this name (any case).
func (s *Store) FindOption(ctx context.Context, ledgerID int64, kind, name string) (*Option, error) {
	var o Option
	err := s.db.QueryRowContext(ctx, `SELECT id, kind, name, color FROM options WHERE ledger_id = ? AND kind = ? AND name = ?`,
		ledgerID, kind, strings.TrimSpace(name)).Scan(&o.ID, &o.Kind, &o.Name, &o.Color)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &o, err
}

// CreateOption adds a choice. color "" picks one. An existing option with the
// same name (any case) is returned as it is.
func (s *Store) CreateOption(ctx context.Context, ledgerID int64, kind, name, color string) (*Option, error) {
	if err := validKind(kind); err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	name, err = s.ensureOption(ctx, tx, ledgerID, kind, name, color)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.FindOption(ctx, ledgerID, kind, name)
}

type querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// ensureOption returns the stored name of the option called name, creating it
// if needed. An empty name stays empty (no option).
func (s *Store) ensureOption(ctx context.Context, q querier, ledgerID int64, kind, name, color string) (string, error) {
	if strings.TrimSpace(name) == "" {
		return "", nil
	}
	name, err := cleanOptionName(name)
	if err != nil {
		return "", err
	}
	var stored string
	err = q.QueryRowContext(ctx, `SELECT name FROM options WHERE ledger_id = ? AND kind = ? AND name = ?`, ledgerID, kind, name).Scan(&stored)
	if err == nil {
		return stored, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	var n, pos int
	if err := q.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(MAX(position), -1) + 1 FROM options WHERE ledger_id = ? AND kind = ?`,
		ledgerID, kind).Scan(&n, &pos); err != nil {
		return "", err
	}
	if n >= maxOptions {
		return "", fmt.Errorf("you have %d %s options already; delete one first", n, kind)
	}
	if color == "" || !validColor(color) {
		color = colorFor(name)
	}
	if _, err := q.ExecContext(ctx, `INSERT INTO options (ledger_id, kind, name, color, position) VALUES (?, ?, ?, ?, ?)`,
		ledgerID, kind, name, color, pos); err != nil {
		return "", err
	}
	return name, nil
}

// UpdateOption renames an option (on every entry that has it, too) or recolors it.
func (s *Store) UpdateOption(ctx context.Context, ledgerID, id int64, name, color *string) (*Option, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	o, err := s.getOption(ctx, tx, ledgerID, id)
	if err != nil {
		return nil, err
	}
	if color != nil {
		if !validColor(*color) {
			return nil, fmt.Errorf("color must be one of %s", strings.Join(Colors, ", "))
		}
		if _, err := tx.ExecContext(ctx, `UPDATE options SET color = ? WHERE id = ?`, *color, id); err != nil {
			return nil, err
		}
	}
	if name != nil {
		newName, err := cleanOptionName(*name)
		if err != nil {
			return nil, err
		}
		var clash int64
		err = tx.QueryRowContext(ctx, `SELECT id FROM options WHERE ledger_id = ? AND kind = ? AND name = ? AND id != ?`,
			ledgerID, o.Kind, newName, id).Scan(&clash)
		if err == nil {
			return nil, fmt.Errorf("there is already a %s called %q", o.Kind, newName)
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE options SET name = ? WHERE id = ?`, newName, id); err != nil {
			return nil, err
		}
		col := o.Kind // "type" or "card": both are entries columns
		if _, err := tx.ExecContext(ctx, `UPDATE entries SET `+col+` = ?, updated_at = ? WHERE ledger_id = ? AND `+col+` = ?`,
			newName, now(), ledgerID, o.Name); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.getOption(ctx, s.db, ledgerID, id)
}

// DeleteOption removes an option and clears it from the entries that had it.
// It returns how many entries were cleared.
func (s *Store) DeleteOption(ctx context.Context, ledgerID, id int64) (*Option, int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, 0, err
	}
	defer tx.Rollback()
	o, err := s.getOption(ctx, tx, ledgerID, id)
	if err != nil {
		return nil, 0, err
	}
	res, err := tx.ExecContext(ctx, `UPDATE entries SET `+o.Kind+` = '', updated_at = ? WHERE ledger_id = ? AND `+o.Kind+` = ?`,
		now(), ledgerID, o.Name)
	if err != nil {
		return nil, 0, err
	}
	cleared, _ := res.RowsAffected()
	if _, err := tx.ExecContext(ctx, `DELETE FROM options WHERE id = ?`, id); err != nil {
		return nil, 0, err
	}
	return o, cleared, tx.Commit()
}
