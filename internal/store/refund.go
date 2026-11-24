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

// A refund undoes a purchase. The ledger's Refund dialog (R), the MCP tool and
// the agent all go through Refund, so they follow one rule: a refund that
// matches a recent purchase deletes it; one that matches nothing is recorded
// as a negative amount.

// RefundInput says what came back: a purchase's id, or its name and/or amount.
type RefundInput struct {
	ID     int64    `json:"id,omitempty"`
	Name   string   `json:"name,omitempty"`
	Amount *float64 `json:"amount,omitempty"`
	Date   string   `json:"date,omitempty"` // for a refund that is added; today when empty
}

// RefundResult is what a refund did: deleted one purchase, added a negative
// amount, or, when several purchases match, nothing yet (pick one by id).
type RefundResult struct {
	Refunded *Entry  `json:"refunded,omitempty"`
	Added    *Entry  `json:"added,omitempty"`
	Matches  []Entry `json:"matches,omitempty"`
}

// RefundMatches finds the purchases a refund could undo: dated within
// RefundLookbackDays up to today, the name holding name (when given) and the
// amount the same to the cent (when given), newest first.
func (s *Store) RefundMatches(ctx context.Context, userID int64, name string, amount *float64, today time.Time, limit int) ([]Entry, error) {
	name = strings.TrimSpace(name)
	if name == "" && amount == nil {
		return []Entry{}, nil
	}
	limit = min(max(limit, 1), 50)
	q := `SELECT ` + entryCols + ` FROM entries WHERE user_id = ? AND amount > 0 AND date BETWEEN ? AND ?`
	args := []any{userID, today.AddDate(0, 0, -RefundLookbackDays).Format(dateLayout), today.Format(dateLayout)}
	if name != "" {
		q += ` AND instr(lower(name), lower(?)) > 0`
		args = append(args, name)
	}
	if amount != nil {
		cents, err := toCents(math.Abs(*amount))
		if err != nil {
			return nil, err
		}
		q += ` AND amount = ?`
		args = append(args, cents)
	}
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
// the purchases matching the name and amount decide: one is deleted; several
// are returned for the caller to pick from; none, and the refund is added as
// a negative amount (which needs the amount).
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
	if name == "" && in.Amount == nil {
		return nil, errors.New("say what was refunded: its name, its amount, or both")
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
	case in.Amount == nil:
		return nil, fmt.Errorf("no purchase from the last %d days matches; give the amount to record the refund", RefundLookbackDays)
	}
	if name == "" {
		name = "Refund"
	}
	amount := -math.Abs(*in.Amount)
	date := strings.TrimSpace(in.Date)
	if date == "" {
		date = today.Format(dateLayout)
	}
	empty := ""
	added, err := s.CreateEntries(ctx, userID, []EntryInput{{Name: &name, Amount: &amount, Date: &date, Type: &empty, Card: &empty}})
	if err != nil {
		return nil, err
	}
	return &RefundResult{Added: &added[0]}, nil
}
