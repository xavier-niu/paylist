package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"regexp"
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
	// Paid in another currency: what it was there (Amount is in the ledger's).
	OrigAmount   *float64 `json:"orig_amount,omitempty"`
	OrigCurrency string   `json:"orig_currency,omitempty"`
}

// EntryInput is a new entry, or the fields of one to change (nil: unchanged).
type EntryInput struct {
	Name   *string  `json:"name"`
	Type   *string  `json:"type"`
	Amount *float64 `json:"amount"`
	Date   *string  `json:"date"`
	Card   *string  `json:"card"`
	// Paid in another currency: the amount there and its code (USD); an
	// empty code clears them. Amount is still the ledger's. A change to
	// Amount alone clears them too: they'd no longer match.
	OrigAmount   *float64 `json:"orig_amount"`
	OrigCurrency *string  `json:"orig_currency"`
}

var currencyRE = regexp.MustCompile(`^[A-Z]{3}$`)

// origFields checks what an entry paid in another currency was: NULL and ""
// for none.
func origFields(amount *float64, currency *string) (sql.NullInt64, string, error) {
	if currency == nil || strings.TrimSpace(*currency) == "" {
		return sql.NullInt64{}, "", nil
	}
	cur := strings.ToUpper(strings.TrimSpace(*currency))
	if !currencyRE.MatchString(cur) {
		return sql.NullInt64{}, "", fmt.Errorf("the currency %q must be a three-letter code, such as USD", *currency)
	}
	if amount == nil {
		return sql.NullInt64{}, "", fmt.Errorf("say how much it was in %s", cur)
	}
	cents, err := toCents(*amount)
	if err != nil {
		return sql.NullInt64{}, "", err
	}
	return sql.NullInt64{Int64: cents, Valid: true}, cur, nil
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

const entryCols = `id, name, type, amount, date, card, created_at, updated_at, orig_amount, orig_currency`

func scanEntry(row interface{ Scan(...any) error }) (*Entry, error) {
	var e Entry
	var cents int64
	var orig sql.NullInt64
	if err := row.Scan(&e.ID, &e.Name, &e.Type, &cents, &e.Date, &e.Card, &e.CreatedAt, &e.UpdatedAt, &orig, &e.OrigCurrency); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	e.Amount = fromCents(cents)
	if orig.Valid && e.OrigCurrency != "" {
		v := fromCents(orig.Int64)
		e.OrigAmount = &v
	}
	return &e, nil
}

func (s *Store) GetEntry(ctx context.Context, ledgerID, id int64) (*Entry, error) {
	return scanEntry(s.db.QueryRowContext(ctx, `SELECT `+entryCols+` FROM entries WHERE ledger_id = ? AND id = ?`, ledgerID, id))
}

// EntryFilter selects entries. Dates are inclusive YYYY-MM-DD; empty fields don't filter.
type EntryFilter struct {
	From, To   string
	Type, Card string
	Query      string // part of the name
	Limit      int    // 0: no limit
}

func (s *Store) ListEntries(ctx context.Context, ledgerID int64, f EntryFilter) ([]Entry, error) {
	q := `SELECT ` + entryCols + ` FROM entries WHERE ledger_id = ?`
	args := []any{ledgerID}
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
func (s *Store) CreateEntries(ctx context.Context, ledgerID int64, ins []EntryInput) ([]Entry, error) {
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
		id, err := s.insertEntry(ctx, tx, ledgerID, in)
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
		e, err := s.GetEntry(ctx, ledgerID, id)
		if err != nil {
			return nil, err
		}
		out = append(out, *e)
	}
	return out, nil
}

func (s *Store) insertEntry(ctx context.Context, tx *sql.Tx, ledgerID int64, in EntryInput) (int64, error) {
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
	orig, origCur, err := origFields(in.OrigAmount, in.OrigCurrency)
	if err != nil {
		return 0, err
	}
	var typ, card string
	if in.Type != nil {
		if typ, err = s.ensureOption(ctx, tx, ledgerID, KindType, *in.Type, ""); err != nil {
			return 0, err
		}
	}
	if in.Card != nil {
		if card, err = s.ensureOption(ctx, tx, ledgerID, KindCard, *in.Card, ""); err != nil {
			return 0, err
		}
	}
	ts := now()
	res, err := tx.ExecContext(ctx, `INSERT INTO entries (ledger_id, name, type, amount, date, card, created_at, updated_at, orig_amount, orig_currency)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, ledgerID, name, typ, cents, d.Format(dateLayout), card, ts, ts, orig, origCur)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	// Its history starts with what it was made with.
	c := change{"name": {nil, name}, "amount": {nil, fromCents(cents)}}
	if o := origText(orig, origCur); o != "" {
		c["orig"] = []any{nil, o}
	}
	return id, logEntry(ctx, tx, id, EntryCreated, c)
}

// UpdateEntry changes the given fields of an entry.
func (s *Store) UpdateEntry(ctx context.Context, ledgerID, id int64, in EntryInput) (*Entry, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	e, err := scanEntry(tx.QueryRowContext(ctx, `SELECT `+entryCols+` FROM entries WHERE ledger_id = ? AND id = ?`, ledgerID, id))
	if err != nil {
		return nil, err
	}
	cents, _ := toCents(e.Amount)
	was := cents
	var orig sql.NullInt64
	if e.OrigAmount != nil {
		orig.Int64, _ = toCents(*e.OrigAmount)
		orig.Valid = true
	}
	origCur := e.OrigCurrency
	before := *e
	wasOrig := origText(orig, origCur)
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
		if e.Type, err = s.ensureOption(ctx, tx, ledgerID, KindType, *in.Type, ""); err != nil {
			return nil, err
		}
	}
	if in.Card != nil {
		if e.Card, err = s.ensureOption(ctx, tx, ledgerID, KindCard, *in.Card, ""); err != nil {
			return nil, err
		}
	}
	switch {
	case in.OrigCurrency != nil:
		if orig, origCur, err = origFields(in.OrigAmount, in.OrigCurrency); err != nil {
			return nil, err
		}
	case cents != was:
		// The amount changed by itself: what was paid elsewhere no longer matches it.
		orig, origCur = sql.NullInt64{}, ""
	}
	if _, err := tx.ExecContext(ctx, `UPDATE entries SET name = ?, type = ?, amount = ?, date = ?, card = ?, orig_amount = ?, orig_currency = ?, updated_at = ?
		WHERE ledger_id = ? AND id = ?`, e.Name, e.Type, cents, e.Date, e.Card, orig, origCur, now(), ledgerID, id); err != nil {
		return nil, err
	}
	c := change{}
	c.add("name", before.Name, e.Name)
	c.add("type", before.Type, e.Type)
	c.add("amount", fromCents(was), fromCents(cents))
	c.add("date", before.Date, e.Date)
	c.add("card", before.Card, e.Card)
	c.add("orig", wasOrig, origText(orig, origCur))
	if err := logEntry(ctx, tx, id, EntryChanged, c); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.GetEntry(ctx, ledgerID, id)
}

// DeleteEntries deletes the user's entries with these ids and returns how many went.
func (s *Store) DeleteEntries(ctx context.Context, ledgerID int64, ids []int64) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	if len(ids) > 1000 {
		return 0, errors.New("delete at most 1000 entries at a time")
	}
	ph := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := []any{ledgerID}
	for _, id := range ids {
		args = append(args, id)
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM entries WHERE ledger_id = ? AND id IN (`+ph+`)`, args...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// CountEntries reports how many entries the user has.
func (s *Store) CountEntries(ctx context.Context, ledgerID int64) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM entries WHERE ledger_id = ?`, ledgerID).Scan(&n)
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
// first, then the most used, then the most recent. With no q, every name
// comes, most used first (the web app matches them itself, by pinyin too).
func (s *Store) SuggestEntries(ctx context.Context, ledgerID int64, q string, limit int) ([]Suggestion, error) {
	q = strings.TrimSpace(q)
	limit = min(max(limit, 1), 1000)
	rows, err := s.db.QueryContext(ctx, `
		WITH m AS (
			SELECT name, type, card, amount, date,
				COUNT(*) OVER (PARTITION BY lower(name)) AS uses,
				ROW_NUMBER() OVER (PARTITION BY lower(name) ORDER BY date DESC, id DESC) AS rn
			FROM entries
			WHERE ledger_id = ? AND amount > 0 AND instr(lower(name), lower(?)) > 0
		)
		SELECT name, type, card, amount, date, uses FROM m WHERE rn = 1
		ORDER BY instr(lower(name), lower(?)) = 1 DESC, uses DESC, date DESC, name
		LIMIT ?`, ledgerID, q, q, limit)
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
