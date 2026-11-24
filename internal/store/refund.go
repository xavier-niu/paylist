package store

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
)

// RefundLookbackDays is how far back a refund looks for the purchase it undoes.
const RefundLookbackDays = 120

// A refund undoes a purchase. The ledger's Refund form (R), the MCP tool and
// the agent all go through Refund, so they follow one rule: a refund matching
// a recent purchase by name and amount deletes it; one that matches nothing
// is recorded as a negative amount, with its own type, date and card.

// RefundInput says what came back: a purchase's id, or its name and amount
// (with the type, date and card to record it under if nothing matches).
type RefundInput struct {
	ID     int64    `json:"id,omitempty"`
	Name   string   `json:"name,omitempty"`
	Amount *float64 `json:"amount,omitempty"`
	Type   string   `json:"type,omitempty"`
	Card   string   `json:"card,omitempty"`
	Date   string   `json:"date,omitempty"` // today when empty
}

// RefundResult is what a refund did: deleted one purchase, added a negative
// amount, or, when several purchases match, nothing yet (pick one by id).
type RefundResult struct {
	Refunded *Entry  `json:"refunded,omitempty"`
	Added    *Entry  `json:"added,omitempty"`
	Matches  []Entry `json:"matches,omitempty"`
}

// RefundMatches finds the purchases a refund could undo: dated within
// RefundLookbackDays up to today, with the same name (ignoring case) and the
// same amount to the cent, newest first.
func (s *Store) RefundMatches(ctx context.Context, userID int64, name string, amount *float64, today time.Time, limit int) ([]Entry, error) {
	name = strings.TrimSpace(name)
	if name == "" || amount == nil {
		return []Entry{}, nil
	}
	limit = min(max(limit, 1), 50)
	q := `SELECT ` + entryCols + ` FROM entries WHERE user_id = ? AND amount > 0 AND date BETWEEN ? AND ?`
	args := []any{userID, today.AddDate(0, 0, -RefundLookbackDays).Format(dateLayout), today.Format(dateLayout)}
	cents, err := toCents(math.Abs(*amount))
	if err != nil {
		return nil, err
	}
	q += ` AND lower(trim(name)) = lower(?) AND amount = ?`
	args = append(args, name, cents)
	q += fmt.Sprintf(` ORDER BY date DESC, id DESC LIMIT %d`, limit)
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
func (s *Store) Refund(ctx context.Context, userID int64, in RefundInput, today time.Time) (*RefundResult, error) {
	if in.ID != 0 {
		e, err := s.GetEntry(ctx, userID, in.ID)
		if err != nil {
			return nil, err
		}
		if e.Amount <= 0 {
			return nil, fmt.Errorf("entry %d is a refund, not a purchase", in.ID)
		}
		if _, err := s.DeleteEntries(ctx, userID, []int64{e.ID}); err != nil {
			return nil, err
		}
		return &RefundResult{Refunded: e}, nil
	}
	name := strings.TrimSpace(in.Name)
	if name == "" || in.Amount == nil || *in.Amount == 0 {
		return nil, errors.New("say what was refunded: its name and its amount")
	}
	matches, err := s.RefundMatches(ctx, userID, name, in.Amount, today, 5)
	if err != nil {
		return nil, err
	}
	switch {
	case len(matches) == 1:
		return s.Refund(ctx, userID, RefundInput{ID: matches[0].ID}, today)
	case len(matches) > 1:
		return &RefundResult{Matches: matches}, nil
	}
	amount := -math.Abs(*in.Amount)
	date := strings.TrimSpace(in.Date)
	if date == "" {
		date = today.Format(dateLayout)
	}
	typ, card := strings.TrimSpace(in.Type), strings.TrimSpace(in.Card)
	added, err := s.CreateEntries(ctx, userID, []EntryInput{{Name: &name, Amount: &amount, Date: &date, Type: &typ, Card: &card}})
	if err != nil {
		return nil, err
	}
	return &RefundResult{Added: &added[0]}, nil
}
