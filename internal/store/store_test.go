package store

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func init() { bcryptCost = bcrypt.MinCost }

func newStore(t *testing.T) (*Store, *User) {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "t.db"), bytes.Repeat([]byte{1}, 32))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	u, err := st.CreateUser(context.Background(), "a@example.com", "", "password1", true)
	if err != nil {
		t.Fatal(err)
	}
	return st, u
}

func ptr[T any](v T) *T { return &v }

func entry(name, typ string, amount float64, date, card string) EntryInput {
	return EntryInput{Name: &name, Type: &typ, Amount: &amount, Date: &date, Card: &card}
}

func TestEntriesAndOptions(t *testing.T) {
	ctx := context.Background()
	st, u := newStore(t)

	opts, err := st.ListOptions(ctx, u.ID)
	if err != nil || len(opts.Types) != len(defaultTypes) || len(opts.Cards) != 1 {
		t.Fatalf("seeded options: %+v %v", opts, err)
	}

	added, err := st.CreateEntries(ctx, u.ID, []EntryInput{
		entry("Lunch", "food", 12.345, "2026-11-03", "Visa"), // "food" matches Food; Visa is new
		entry("Rent", "Housing", 4500, "2026-11-01", ""),
		entry("Taxi", "", 30, "2026-10-31", "Cash"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if added[0].Type != "Food" || added[0].Amount != 12.35 || added[0].Card != "Visa" {
		t.Errorf("first entry: %+v", added[0])
	}
	if _, err := st.FindOption(ctx, u.ID, KindCard, "visa"); err != nil {
		t.Errorf("Visa wasn't made an option: %v", err)
	}

	// All or nothing.
	if _, err := st.CreateEntries(ctx, u.ID, []EntryInput{entry("ok", "", 1, "2026-11-02", ""), entry("bad", "", 1, "2026-13-02", "")}); err == nil {
		t.Error("a bad date was accepted")
	}

	from, to, _ := MonthRange("2026-11")
	nov, err := st.ListEntries(ctx, u.ID, EntryFilter{From: from, To: to})
	if err != nil || len(nov) != 2 || nov[0].Name != "Rent" {
		t.Fatalf("November: %+v %v", nov, err)
	}

	e, err := st.UpdateEntry(ctx, u.ID, added[0].ID, EntryInput{Amount: ptr(15.0), Type: ptr("")})
	if err != nil || e.Amount != 15 || e.Type != "" || e.Name != "Lunch" {
		t.Fatalf("update: %+v %v", e, err)
	}

	// Renaming an option renames it on its entries; deleting clears it.
	visa, _ := st.FindOption(ctx, u.ID, KindCard, "Visa")
	if _, err := st.UpdateOption(ctx, u.ID, visa.ID, ptr("Visa 4242"), nil); err != nil {
		t.Fatal(err)
	}
	if e, _ := st.GetEntry(ctx, u.ID, added[0].ID); e.Card != "Visa 4242" {
		t.Errorf("card after rename: %q", e.Card)
	}
	cash, _ := st.FindOption(ctx, u.ID, KindCard, "Cash")
	if _, err := st.UpdateOption(ctx, u.ID, cash.ID, ptr("visa 4242"), nil); err == nil {
		t.Error("renamed onto an existing option")
	}
	if _, n, err := st.DeleteOption(ctx, u.ID, cash.ID); err != nil || n != 1 {
		t.Errorf("delete Cash: cleared %d, %v", n, err)
	}
	if e, _ := st.GetEntry(ctx, u.ID, added[2].ID); e.Card != "" {
		t.Errorf("card after delete: %q", e.Card)
	}

	// Another user sees none of it.
	other, _ := st.CreateUser(ctx, "b@example.com", "", "password1", false)
	if list, _ := st.ListEntries(ctx, other.ID, EntryFilter{}); len(list) != 0 {
		t.Errorf("other user sees %d entries", len(list))
	}
	if n, _ := st.DeleteEntries(ctx, other.ID, []int64{added[1].ID}); n != 0 {
		t.Error("other user deleted an entry")
	}
	if n, _ := st.DeleteEntries(ctx, u.ID, []int64{added[1].ID, added[2].ID}); n != 2 {
		t.Errorf("deleted %d", n)
	}
}

func TestStats(t *testing.T) {
	ctx := context.Background()
	st, u := newStore(t)
	if _, err := st.CreateEntries(ctx, u.ID, []EntryInput{
		entry("Lunch", "Food", 10.10, "2026-11-01", "Cash"),
		entry("Dinner", "Food", 20.20, "2026-11-01", "Visa"),
		entry("Bus", "Transport", 3, "2026-11-05", "Cash"),
		entry("Old", "Food", 100, "2026-10-15", "Cash"),
		entry("Older", "Food", 7, "2026-01-15", ""),
	}); err != nil {
		t.Fatal(err)
	}
	s, err := st.GetStats(ctx, u.ID, "2026-11", 12, time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if s.Total != 33.30 || s.Count != 3 || s.PrevTotal != 100 || s.DailyAvg != 3.33 {
		t.Errorf("totals: %+v", s)
	}
	if len(s.Days) != 30 || s.Days[0].Total != 30.30 || s.Days[4].Total != 3 || s.Days[1].Total != 0 {
		t.Errorf("days: %+v", s.Days[:5])
	}
	if len(s.ByType) != 2 || s.ByType[0].Name != "Food" || s.ByType[0].Total != 30.30 || s.ByType[0].Color == "" {
		t.Errorf("by type: %+v", s.ByType)
	}
	if len(s.ByCard) != 2 || s.ByCard[0].Name != "Visa" {
		t.Errorf("by card: %+v", s.ByCard)
	}
	if len(s.Monthly) != 12 || s.Monthly[11].Month != "2026-11" || s.Monthly[10].Total != 100 || s.Monthly[1].Total != 7 || s.Monthly[0].Month != "2025-12" {
		t.Errorf("monthly: %+v", s.Monthly)
	}
	past, _ := st.GetStats(ctx, u.ID, "2026-10", 3, time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC))
	if past.DailyAvg != 3.23 { // 100 / 31 days
		t.Errorf("October's daily average: %v", past.DailyAvg)
	}
}

// Month over month in the current month compares up to the same day.
func TestStatsMonthToDate(t *testing.T) {
	ctx := context.Background()
	st, u := newStore(t)
	if _, err := st.CreateEntries(ctx, u.ID, []EntryInput{
		entry("Early", "", 100, "2026-10-05", ""),
		entry("Late", "", 900, "2026-10-28", ""),
		entry("Now", "", 50, "2026-11-03", ""),
		entry("Planned", "", 20, "2026-11-20", ""), // after today
	}); err != nil {
		t.Fatal(err)
	}
	s, err := st.GetStats(ctx, u.ID, "2026-11", 2, time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if s.CompareDay != 10 || s.ToDate != 50 || s.PrevToDate != 100 || s.PrevTotal != 1000 || s.Total != 70 {
		t.Errorf("month to date: %+v", s)
	}
	// A past month compares whole months.
	past, _ := st.GetStats(ctx, u.ID, "2026-11", 2, time.Date(2026, 12, 5, 0, 0, 0, 0, time.UTC))
	if past.CompareDay != 0 || past.ToDate != 70 || past.PrevToDate != 1000 {
		t.Errorf("whole months: %+v", past)
	}
	// March 31 against February: up to February's last day.
	if _, err := st.CreateEntries(ctx, u.ID, []EntryInput{entry("Feb", "", 30, "2027-02-28", "")}); err != nil {
		t.Fatal(err)
	}
	mar, _ := st.GetStats(ctx, u.ID, "2027-03", 2, time.Date(2027, 3, 30, 0, 0, 0, 0, time.UTC))
	if mar.PrevToDate != 30 || mar.CompareDay != 30 {
		t.Errorf("short month before: %+v", mar)
	}
}

func TestAllMonths(t *testing.T) {
	ctx := context.Background()
	st, u := newStore(t)
	if _, err := st.CreateEntries(ctx, u.ID, []EntryInput{
		entry("Old", "", 10, "2024-03-02", ""),
		entry("A", "", 1, "2026-10-05", ""),
		entry("B", "", 2, "2026-10-28", ""),
		entry("Now", "", 5, "2026-11-30", ""),
		entry("Future", "", 7, "2026-12-01", ""),
	}); err != nil {
		t.Fatal(err)
	}
	got, err := st.AllMonths(ctx, u.ID, "2026-11")
	if err != nil {
		t.Fatal(err)
	}
	want := []MonthTotal{{Month: "2026-11", Total: 5, Count: 1}, {Month: "2026-10", Total: 3, Count: 2}, {Month: "2024-03", Total: 10, Count: 1}}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("month %d: got %+v, want %+v", i, got[i], want[i])
		}
	}
	if _, err := st.AllMonths(ctx, u.ID, "nope"); err == nil {
		t.Error("a bad month was accepted")
	}
}

func TestSuggestEntries(t *testing.T) {
	ctx := context.Background()
	st, u := newStore(t)
	if _, err := st.CreateEntries(ctx, u.ID, []EntryInput{
		entry("Iced coffee", "Food", 25, "2026-11-20", "Alipay"),
		entry("Coffee", "Food", 18, "2026-11-01", "Cash"),
		entry("coffee", "Food", 19.5, "2026-11-10", "Credit card"),
		entry("Coffee", "", -18, "2026-11-12", ""), // a refund: not a suggestion
		entry("Lunch", "Food", 35, "2026-11-11", "Alipay"),
	}); err != nil {
		t.Fatal(err)
	}
	got, err := st.SuggestEntries(ctx, u.ID, "COF", 5)
	if err != nil {
		t.Fatal(err)
	}
	// "coffee" starts with the text and was entered twice, so it leads, with its latest entry.
	if len(got) != 2 || got[0].Name != "coffee" || got[0].Uses != 2 || got[0].Amount != 19.5 || got[0].Card != "Credit card" || got[1].Name != "Iced coffee" {
		t.Errorf("suggestions: %+v", got)
	}
	if none, _ := st.SuggestEntries(ctx, u.ID, "  ", 5); len(none) != 0 {
		t.Errorf("blank text: %+v", none)
	}
}

func TestRefund(t *testing.T) {
	ctx := context.Background()
	st, u := newStore(t)
	today := time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)
	if _, err := st.CreateEntries(ctx, u.ID, []EntryInput{
		entry("Coffee", "Food", 18, "2026-11-20", "Cash"),
		entry("Coffee", "Food", 19.77, "2026-11-02", "Cash"),
		entry("Iced coffee", "Food", 19.77, "2026-11-03", "Cash"),
		entry("Headphones", "Shopping", 299, "2026-10-15", "Credit card"),
		entry("Old coat", "Shopping", 500, "2026-05-01", ""), // too long ago
		entry("Coffee", "", -5, "2026-11-21", ""),            // a refund, never a match
	}); err != nil {
		t.Fatal(err)
	}
	f := func(v float64) *float64 { return &v }

	// The same name (ignoring case) and amount: deleted. "Iced coffee" isn't "coffee".
	r, err := st.Refund(ctx, u.ID, RefundInput{Name: " coffee ", Amount: f(19.77)}, today)
	if err != nil || r.Refunded == nil || r.Refunded.Name != "Coffee" || r.Refunded.Date != "2026-11-02" || r.Added != nil {
		t.Fatalf("one match: %+v %v", r, err)
	}
	// The sign of the amount doesn't matter.
	if r, err := st.Refund(ctx, u.ID, RefundInput{Name: "Headphones", Amount: f(-299)}, today); err != nil || r.Refunded == nil {
		t.Fatalf("negative amount: %+v %v", r, err)
	}
	// Several matches: nothing changes, they come back newest first; then one by id.
	if _, err := st.CreateEntries(ctx, u.ID, []EntryInput{entry("Coffee", "Food", 18, "2026-11-22", "")}); err != nil {
		t.Fatal(err)
	}
	r, err = st.Refund(ctx, u.ID, RefundInput{Name: "Coffee", Amount: f(18)}, today)
	if err != nil || len(r.Matches) != 2 || r.Matches[0].Date != "2026-11-22" || r.Refunded != nil {
		t.Fatalf("several: %+v %v", r, err)
	}
	if r, err := st.Refund(ctx, u.ID, RefundInput{ID: r.Matches[1].ID}, today); err != nil || r.Refunded == nil || r.Refunded.Date != "2026-11-20" {
		t.Fatalf("by id: %+v %v", r, err)
	}
	// No match: added as a negative amount, with its own type, card and date.
	r, err = st.Refund(ctx, u.ID, RefundInput{Name: "Old coat", Amount: f(500), Type: "Shopping", Card: "Cash", Date: "2026-11-23"}, today)
	if err != nil || r.Added == nil || r.Added.Amount != -500 || r.Added.Date != "2026-11-23" || r.Added.Type != "Shopping" || r.Added.Card != "Cash" {
		t.Fatalf("no match: %+v %v", r, err)
	}
	// Both the name and the amount are needed.
	for _, in := range []RefundInput{{Name: "Coffee"}, {Amount: f(18)}, {}} {
		if _, err := st.Refund(ctx, u.ID, in, today); err == nil {
			t.Errorf("accepted %+v", in)
		}
	}
	// A refund entry can't be refunded.
	if _, err := st.Refund(ctx, u.ID, RefundInput{ID: r.Added.ID}, today); err == nil {
		t.Error("a refund was refunded")
	}
}
