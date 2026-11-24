package main

import (
	"context"
	"fmt"
	"math"
	"math/rand/v2"
	"time"

	"github.com/xavier-niu/paylist/internal/store"
)

// seedDemo fills the table of the account with this email with six months of
// made-up spending, so a demo has something to show. It does nothing when the
// account already has entries.
func seedDemo(ctx context.Context, st *store.Store, email string) (int, error) {
	u, err := st.GetUserByEmail(ctx, email)
	if err != nil {
		return 0, fmt.Errorf("no account for %s to fill with demo data", email)
	}
	if n, err := st.CountEntries(ctx, u.ID); err != nil || n > 0 {
		return 0, err
	}
	type item struct {
		name, typ string
		lo, hi    float64
		perMonth  int
	}
	items := []item{
		{"Lunch", "Food", 18, 45, 14},
		{"Coffee", "Food", 12, 32, 10},
		{"Dinner with friends", "Food", 80, 260, 3},
		{"Supermarket", "Groceries", 60, 320, 5},
		{"Fruit and vegetables", "Groceries", 20, 70, 4},
		{"Metro", "Transport", 3, 9, 12},
		{"Taxi", "Transport", 18, 75, 3},
		{"Clothes", "Shopping", 120, 600, 1},
		{"Online order", "Shopping", 30, 260, 3},
		{"Electricity", "Utilities", 90, 220, 1},
		{"Phone bill", "Utilities", 58, 58, 1},
		{"Movie tickets", "Entertainment", 45, 120, 1},
		{"Streaming subscription", "Entertainment", 25, 25, 1},
		{"Pharmacy", "Health", 25, 140, 1},
	}
	cards := []string{"Credit card", "Credit card", "Debit card", "Alipay", "Alipay", "Cash"}
	r := rand.New(rand.NewPCG(7, 42))
	today := time.Now()
	start := time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, time.Local).AddDate(0, -5, 0)
	var ins []store.EntryInput
	add := func(name, typ, card string, amount float64, d time.Time) {
		if d.After(today) {
			return
		}
		amount = math.Round(amount*100) / 100
		date := d.Format("2006-01-02")
		ins = append(ins, store.EntryInput{Name: &name, Type: &typ, Card: &card, Amount: &amount, Date: &date})
	}
	for m := start; !m.After(today); m = m.AddDate(0, 1, 0) {
		days := m.AddDate(0, 1, -1).Day()
		add("Rent", "Housing", "Debit card", 4500, m)
		if m.Month()%3 == 0 {
			add("Weekend trip", "Travel", "Credit card", 900+r.Float64()*1400, m.AddDate(0, 0, 9+r.IntN(10)))
		}
		for _, it := range items {
			for range it.perMonth {
				add(it.name, it.typ, cards[r.IntN(len(cards))], it.lo+r.Float64()*(it.hi-it.lo), m.AddDate(0, 0, r.IntN(days)))
			}
		}
	}
	out, err := st.CreateEntries(ctx, u.ID, ins)
	return len(out), err
}
