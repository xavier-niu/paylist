package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode/utf8"
)

// An entry is one row of the spending table: what was paid for, its type, how
// much, when, and with which card. Amounts are stored in cents.

const (
	maxEntryName = 200
	maxAmount    = 1e10 // in currency units
	dateLayout   = "2006-01-02"
	monthLayout  = "2006-01"
)

type Entry struct {
	ID        int64   `json:"id"`
	Name      string  `json:"name"`
	Type      string  `json:"type"`
	Amount    float64 `json:"amount"`
	Date      string  `json:"date"`
	Card      string  `json:"card"`
	CreatedAt string  `json:"created_at"`
	UpdatedAt string  `json:"updated_at"`
}

// EntryInput is a new entry, or the fields of one to change (nil: unchanged).
type EntryInput struct {
	Name   *string  `json:"name"`
	Type   *string  `json:"type"`
	Amount *float64 `json:"amount"`
	Date   *string  `json:"date"`
	Card   *string  `json:"card"`
}

func toCents(amount float64) (int64, error) {
	if math.IsNaN(amount) || math.IsInf(amount, 0) || math.Abs(amount) > maxAmount {
		return 0, errors.New("the amount must be a number below 10 billion")
	}
	return int64(math.Round(amount * 100)), nil
}

func fromCents(c int64) float64 { return float64(c) / 100 }

// ParseDate checks a YYYY-MM-DD date.
func ParseDate(d string) (time.Time, error) {
	t, err := time.Parse(dateLayout, strings.TrimSpace(d))
	if err != nil {
		return time.Time{}, fmt.Errorf("the date %q must be written YYYY-MM-DD", d)
	}
	return t, nil
}

// ParseMonth checks a YYYY-MM month.
func ParseMonth(m string) (time.Time, error) {
	t, err := time.Parse(monthLayout, strings.TrimSpace(m))
	if err != nil {
		return time.Time{}, fmt.Errorf("the month %q must be written YYYY-MM", m)
	}
	return t, nil
}

// MonthRange returns the first and last day of month (YYYY-MM).
func MonthRange(month string) (string, string, error) {
	t, err := ParseMonth(month)
	if err != nil {
		return "", "", err
	}
	return t.Format(dateLayout), t.AddDate(0, 1, -1).Format(dateLayout), nil
}

func cleanEntryName(name string) (string, error) {
	name = strings.Join(strings.Fields(name), " ")
	if name == "" {
		return "", errors.New("an entry needs a name")
	}
	if utf8.RuneCountInString(name) > maxEntryName {
		return "", fmt.Errorf("names are at most %d characters", maxEntryName)
	}
	return name, nil
}

const entryCols = `id, name, type, amount, date, card, created_at, updated_at`

func scanEntry(row interface{ Scan(...any) error }) (*Entry, error) {
	var e Entry
	var cents int64
	if err := row.Scan(&e.ID, &e.Name, &e.Type, &cents, &e.Date, &e.Card, &e.CreatedAt, &e.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	e.Amount = fromCents(cents)
	return &e, nil
}

func (s *Store) GetEntry(ctx context.Context, userID, id int64) (*Entry, error) {
	return scanEntry(s.db.QueryRowContext(ctx, `SELECT `+entryCols+` FROM entries WHERE user_id = ? AND id = ?`, userID, id))
}

// EntryFilter selects entries. Dates are inclusive YYYY-MM-DD; empty fields don't filter.
type EntryFilter struct {
	From, To   string
	Type, Card string
	Query      string // part of the name
	Limit      int    // 0: no limit
}

func (s *Store) ListEntries(ctx context.Context, userID int64, f EntryFilter) ([]Entry, error) {
	q := `SELECT ` + entryCols + ` FROM entries WHERE user_id = ?`
	args := []any{userID}
	if f.From != "" {
		q += ` AND date >= ?`
		args = append(args, f.From)
	}
	if f.To != "" {
		q += ` AND date <= ?`
		args = append(args, f.To)
	}
	if f.Type != "" {
		q += ` AND type = ?`
		args = append(args, strings.TrimSpace(f.Type))
	}
	if f.Card != "" {
		q += ` AND card = ?`
		args = append(args, strings.TrimSpace(f.Card))
	}
	if f.Query != "" {
		q += ` AND instr(lower(name), lower(?)) > 0`
		args = append(args, strings.TrimSpace(f.Query))
	}
	q += ` ORDER BY date, id`
	if f.Limit > 0 {
		q += fmt.Sprintf(` LIMIT %d`, f.Limit)
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Entry{}
	for rows.Next() {
		e, err := scanEntry(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *e)
	}
	return out, rows.Err()
}

// CreateEntries adds entries in one transaction: all or none. A type or card
// that isn't an option yet becomes one.
func (s *Store) CreateEntries(ctx context.Context, userID int64, ins []EntryInput) ([]Entry, error) {
	if len(ins) == 0 {
		return nil, errors.New("no entries to add")
	}
	if len(ins) > 500 {
		return nil, errors.New("add at most 500 entries at a time")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	ids := make([]int64, 0, len(ins))
	for i, in := range ins {
		id, err := s.insertEntry(ctx, tx, userID, in)
		if err != nil {
			if len(ins) > 1 {
				return nil, fmt.Errorf("entry %d: %w", i+1, err)
			}
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	out := make([]Entry, 0, len(ids))
	for _, id := range ids {
		e, err := s.GetEntry(ctx, userID, id)
		if err != nil {
			return nil, err
		}
		out = append(out, *e)
	}
	return out, nil
}

func (s *Store) insertEntry(ctx context.Context, tx *sql.Tx, userID int64, in EntryInput) (int64, error) {
	if in.Name == nil || in.Amount == nil || in.Date == nil {
		return 0, errors.New("an entry needs a name, an amount and a date")
	}
	name, err := cleanEntryName(*in.Name)
	if err != nil {
		return 0, err
	}
	cents, err := toCents(*in.Amount)
	if err != nil {
		return 0, err
	}
	d, err := ParseDate(*in.Date)
	if err != nil {
		return 0, err
	}
	var typ, card string
	if in.Type != nil {
		if typ, err = s.ensureOption(ctx, tx, userID, KindType, *in.Type, ""); err != nil {
			return 0, err
		}
	}
	if in.Card != nil {
		if card, err = s.ensureOption(ctx, tx, userID, KindCard, *in.Card, ""); err != nil {
			return 0, err
		}
	}
	ts := now()
	res, err := tx.ExecContext(ctx, `INSERT INTO entries (user_id, name, type, amount, date, card, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, userID, name, typ, cents, d.Format(dateLayout), card, ts, ts)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// UpdateEntry changes the given fields of an entry.
func (s *Store) UpdateEntry(ctx context.Context, userID, id int64, in EntryInput) (*Entry, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	e, err := scanEntry(tx.QueryRowContext(ctx, `SELECT `+entryCols+` FROM entries WHERE user_id = ? AND id = ?`, userID, id))
	if err != nil {
		return nil, err
	}
	cents, _ := toCents(e.Amount)
	if in.Name != nil {
		if e.Name, err = cleanEntryName(*in.Name); err != nil {
			return nil, err
		}
	}
	if in.Amount != nil {
		if cents, err = toCents(*in.Amount); err != nil {
			return nil, err
		}
	}
	if in.Date != nil {
		d, err := ParseDate(*in.Date)
		if err != nil {
			return nil, err
		}
		e.Date = d.Format(dateLayout)
	}
	if in.Type != nil {
		if e.Type, err = s.ensureOption(ctx, tx, userID, KindType, *in.Type, ""); err != nil {
			return nil, err
		}
	}
	if in.Card != nil {
		if e.Card, err = s.ensureOption(ctx, tx, userID, KindCard, *in.Card, ""); err != nil {
			return nil, err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE entries SET name = ?, type = ?, amount = ?, date = ?, card = ?, updated_at = ?
		WHERE user_id = ? AND id = ?`, e.Name, e.Type, cents, e.Date, e.Card, now(), userID, id); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.GetEntry(ctx, userID, id)
}

// DeleteEntries deletes the user's entries with these ids and returns how many went.
func (s *Store) DeleteEntries(ctx context.Context, userID int64, ids []int64) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	if len(ids) > 1000 {
		return 0, errors.New("delete at most 1000 entries at a time")
	}
	ph := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := []any{userID}
	for _, id := range ids {
		args = append(args, id)
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM entries WHERE user_id = ? AND id IN (`+ph+`)`, args...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// CountEntries reports how many entries the user has.
func (s *Store) CountEntries(ctx context.Context, userID int64) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM entries WHERE user_id = ?`, userID).Scan(&n)
	return n, err
}

// Suggestion is a name used before, with how it was last entered: what the
// New entry form offers while the name is being typed.
type Suggestion struct {
	Name   string  `json:"name"`
	Type   string  `json:"type"`
	Card   string  `json:"card"`
	Amount float64 `json:"amount"` // the last amount
	Date   string  `json:"date"`   // when it was last entered
	Uses   int     `json:"uses"`   // how many entries have this name
}

// SuggestEntries finds names containing q among past purchases (refunds
// left out), one per name with its latest entry: names starting with q
// first, then the most used, then the most recent.
func (s *Store) SuggestEntries(ctx context.Context, userID int64, q string, limit int) ([]Suggestion, error) {
	q = strings.TrimSpace(q)
	if q == "" {
		return []Suggestion{}, nil
	}
	limit = min(max(limit, 1), 20)
	rows, err := s.db.QueryContext(ctx, `
		WITH m AS (
			SELECT name, type, card, amount, date,
				COUNT(*) OVER (PARTITION BY lower(name)) AS uses,
				ROW_NUMBER() OVER (PARTITION BY lower(name) ORDER BY date DESC, id DESC) AS rn
			FROM entries
			WHERE user_id = ? AND amount > 0 AND instr(lower(name), lower(?)) > 0
		)
		SELECT name, type, card, amount, date, uses FROM m WHERE rn = 1
		ORDER BY instr(lower(name), lower(?)) = 1 DESC, uses DESC, date DESC, name
		LIMIT ?`, userID, q, q, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Suggestion{}
	for rows.Next() {
		var sg Suggestion
		var cents int64
		if err := rows.Scan(&sg.Name, &sg.Type, &sg.Card, &cents, &sg.Date, &sg.Uses); err != nil {
			return nil, err
		}
		sg.Amount = fromCents(cents)
		out = append(out, sg)
	}
	return out, rows.Err()
}
