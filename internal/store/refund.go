package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// RefundLookbackDays is how far back a refund looks for the purchase it undoes.
const RefundLookbackDays = 120

// A refund undoes a purchase. The ledger's Refund form (R), the MCP tool and
// the agent all go through Refund, so they follow one rule: a refund matching
// a recent purchase by name and amount deletes it; one that matches nothing
// is recorded as a negative amount, with its own type, date and card. A
// refund of part of a purchase, picked by its id, takes that much off it.

// RefundInput says what came back: a purchase's id, or its name and amount
// (with the type, date and card to record it under if nothing matches). With
// an id, an amount below the purchase's refunds that part of it.
type RefundInput struct {
	ID     int64    `json:"id,omitempty"`
	Name   string   `json:"name,omitempty"`
	Amount *float64 `json:"amount,omitempty"`
	Type   string   `json:"type,omitempty"`
	Card   string   `json:"card,omitempty"`
	Date   string   `json:"date,omitempty"` // today when empty
	// Paid back in another currency: how much there, and its code. Amount
	// is still the ledger's, and is what's matched.
	OrigAmount   *float64 `json:"orig_amount,omitempty"`
	OrigCurrency string   `json:"orig_currency,omitempty"`
	// New: add it as a refund of its own, even if a purchase matches.
	New bool `json:"new,omitempty"`
}

// RefundResult is what a refund did: deleted one purchase, added a negative
// amount, or, when several purchases match, nothing yet (pick one by id).
type RefundResult struct {
	Refunded *Entry  `json:"refunded,omitempty"`
	Reduced  *Entry  `json:"reduced,omitempty"` // part refunded: the purchase as it now is
	Added    *Entry  `json:"added,omitempty"`
	Matches  []Entry `json:"matches,omitempty"`
}

// RefundMatches finds the purchases a refund could undo, wholly or in part:
// dated within RefundLookbackDays up to today, with the same name (ignoring
// case) and costing at least the refund; those of the same amount to the
// cent first, then the newest. Without a name, those whose amount starts
// with it, as typed (238 finds 238.40 and 238.00), the same amount first.
func (s *Store) RefundMatches(ctx context.Context, ledgerID int64, name string, amount *float64, today time.Time, limit int) ([]Entry, error) {
	name = strings.TrimSpace(name)
	if amount == nil || *amount == 0 {
		return []Entry{}, nil
	}
	limit = min(max(limit, 1), 50)
	q := `SELECT ` + entryCols + ` FROM entries WHERE ledger_id = ? AND amount > 0 AND date BETWEEN ? AND ?`
	args := []any{ledgerID, today.AddDate(0, 0, -RefundLookbackDays).Format(dateLayout), today.Format(dateLayout)}
	cents, err := toCents(math.Abs(*amount))
	if err != nil {
		return nil, err
	}
	if name != "" {
		q += ` AND lower(trim(name)) = lower(?) AND amount >= ?`
		args = append(args, name, cents)
	} else {
		q += ` AND printf('%.2f', amount / 100.0) LIKE ?`
		args = append(args, strconv.FormatFloat(math.Abs(*amount), 'f', -1, 64)+"%")
	}
	q += fmt.Sprintf(` ORDER BY amount = ? DESC, date DESC, id DESC LIMIT %d`, limit)
	args = append(args, cents)
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

// Refund applies a refund. With an id, that purchase is deleted. Otherwise
// the purchases with the same name and amount decide: one is deleted; several
// are returned for the caller to pick from; none, and the refund is added as
// a negative amount with the type, card and date given.
func (s *Store) Refund(ctx context.Context, ledgerID int64, in RefundInput, today time.Time) (*RefundResult, error) {
	if in.ID != 0 {
		e, err := s.GetEntry(ctx, ledgerID, in.ID)
		if err != nil {
			return nil, err
		}
		if e.Amount <= 0 {
			return nil, fmt.Errorf("entry %d is a refund, not a purchase", in.ID)
		}
		if in.Amount != nil && *in.Amount != 0 {
			back, err := toCents(math.Abs(*in.Amount))
			if err != nil {
				return nil, err
			}
			cost, _ := toCents(e.Amount)
			if back > cost {
				return nil, fmt.Errorf("the refund is more than %s cost", e.Name)
			}
			if back < cost {
				return s.refundPart(ctx, ledgerID, e, cost-back, in)
			}
		}
		if _, err := s.DeleteEntries(ctx, ledgerID, []int64{e.ID}); err != nil {
			return nil, err
		}
		return &RefundResult{Refunded: e}, nil
	}
	name := strings.TrimSpace(in.Name)
	if name == "" || in.Amount == nil || *in.Amount == 0 {
		return nil, errors.New("say what was refunded: its name and its amount")
	}
	found := []Entry{}
	var err error
	if !in.New {
		if found, err = s.RefundMatches(ctx, ledgerID, name, in.Amount, today, 5); err != nil {
			return nil, err
		}
	}
	// Without an id, only a purchase of the same amount is undone; part of
	// one is refunded only when picked.
	matches := []Entry{}
	for _, e := range found {
		if roundCents(e.Amount) == roundCents(math.Abs(*in.Amount)) {
			matches = append(matches, e)
		}
	}
	switch {
	case len(matches) == 1:
		return s.Refund(ctx, ledgerID, RefundInput{ID: matches[0].ID}, today)
	case len(matches) > 1:
		return &RefundResult{Matches: matches}, nil
	}
	amount := -math.Abs(*in.Amount)
	date := strings.TrimSpace(in.Date)
	if date == "" {
		date = today.Format(dateLayout)
	}
	typ, card := strings.TrimSpace(in.Type), strings.TrimSpace(in.Card)
	add := EntryInput{Name: &name, Amount: &amount, Date: &date, Type: &typ, Card: &card}
	if in.OrigCurrency != "" && in.OrigAmount != nil {
		orig, cur := -math.Abs(*in.OrigAmount), in.OrigCurrency
		add.OrigAmount, add.OrigCurrency = &orig, &cur
	}
	added, err := s.CreateEntries(ctx, ledgerID, []EntryInput{add})
	if err != nil {
		return nil, err
	}
	return &RefundResult{Added: &added[0]}, nil
}

// refundPart takes a refund off a purchase: its amount becomes left (cents).
// What it cost in another currency goes down by what came back in that
// currency; a refund in any other (the ledger's, say) is worked back into it
// at the purchase's own rate: 100 USD at ¥666.45, ¥333 back, leaves ¥333.45
// and 50.03 USD.
func (s *Store) refundPart(ctx context.Context, ledgerID int64, e *Entry, left int64, in RefundInput) (*RefundResult, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var orig sql.NullInt64
	origCur := ""
	if e.OrigAmount != nil {
		had, _ := toCents(*e.OrigAmount)
		var rest int64
		if in.OrigAmount != nil && strings.EqualFold(in.OrigCurrency, e.OrigCurrency) {
			back, _ := toCents(math.Abs(*in.OrigAmount))
			rest = had - back
		} else if cost, _ := toCents(e.Amount); cost > 0 {
			rest = int64(math.Round(float64(had) * float64(left) / float64(cost)))
		}
		if rest > 0 {
			orig, origCur = sql.NullInt64{Int64: rest, Valid: true}, e.OrigCurrency
		}
	}
	var wasOrig sql.NullInt64
	if e.OrigAmount != nil {
		c, _ := toCents(*e.OrigAmount)
		wasOrig = sql.NullInt64{Int64: c, Valid: true}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE entries SET amount = ?, orig_amount = ?, orig_currency = ?, updated_at = ? WHERE ledger_id = ? AND id = ?`,
		left, orig, origCur, now(), ledgerID, e.ID); err != nil {
		return nil, err
	}
	c := change{}
	c.add("amount", e.Amount, fromCents(left))
	c.add("orig", origText(wasOrig, e.OrigCurrency), origText(orig, origCur))
	if err := logEntry(ctx, tx, e.ID, EntryRefunded, c); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	after, err := s.GetEntry(ctx, ledgerID, e.ID)
	if err != nil {
		return nil, err
	}
	return &RefundResult{Reduced: after}, nil
}
