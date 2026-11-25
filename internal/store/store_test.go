package store

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
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

// personal is the id of the user's own ledger, which every account starts with.
func personal(t *testing.T, st *Store, u *User) int64 {
	t.Helper()
	ls, err := st.ListLedgers(context.Background(), u.ID)
	if err != nil || len(ls) != 1 || ls[0].Role != RoleOwner {
		t.Fatalf("ledgers: %+v %v", ls, err)
	}
	return ls[0].ID
}

func entry(name, typ string, amount float64, date, card string) EntryInput {
	return EntryInput{Name: &name, Type: &typ, Amount: &amount, Date: &date, Card: &card}
}

func TestEntriesAndOptions(t *testing.T) {
	ctx := context.Background()
	st, u := newStore(t)
	L := personal(t, st, u)

	opts, err := st.ListOptions(ctx, L)
	if err != nil || len(opts.Types) != len(defaultTypes) || len(opts.Cards) != 1 {
		t.Fatalf("seeded options: %+v %v", opts, err)
	}

	added, err := st.CreateEntries(ctx, L, []EntryInput{
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
	if _, err := st.FindOption(ctx, L, KindCard, "visa"); err != nil {
		t.Errorf("Visa wasn't made an option: %v", err)
	}
	// Each option's uses and last use; one never used has neither.
	got := map[string]Option{}
	opts, _ = st.ListOptions(ctx, L)
	for _, o := range append(opts.Types, opts.Cards...) {
		got[o.Kind+":"+o.Name] = o
	}
	if o := got["card:Cash"]; o.Uses != 1 || o.LastUsed != "2026-10-31" {
		t.Errorf("Cash: %+v", o)
	}
	if o := got["type:Food"]; o.Uses != 1 || o.LastUsed != "2026-11-03" {
		t.Errorf("Food: %+v", o)
	}
	if o := got["type:Travel"]; o.Uses != 0 || o.LastUsed != "" {
		t.Errorf("Travel: %+v", o)
	}

	// Reordering: the cards, last first; a list missing one, or naming one twice or another's, is refused.
	ids := []int64{}
	for i := len(opts.Cards) - 1; i >= 0; i-- {
		ids = append(ids, opts.Cards[i].ID)
	}
	if err := st.ReorderOptions(ctx, L, KindCard, ids); err != nil {
		t.Fatal(err)
	}
	re, _ := st.ListOptions(ctx, L)
	if re.Cards[0].ID != ids[0] || re.Cards[len(ids)-1].ID != ids[len(ids)-1] {
		t.Errorf("reordered cards: %+v", re.Cards)
	}
	for _, bad := range [][]int64{ids[1:], append(ids, ids[0]), append(ids[1:], opts.Types[0].ID)} {
		if err := st.ReorderOptions(ctx, L, KindCard, bad); err == nil {
			t.Errorf("order %v accepted", bad)
		}
	}

	// All or nothing.
	if _, err := st.CreateEntries(ctx, L, []EntryInput{entry("ok", "", 1, "2026-11-02", ""), entry("bad", "", 1, "2026-13-02", "")}); err == nil {
		t.Error("a bad date was accepted")
	}

	from, to, _ := MonthRange("2026-11")
	nov, err := st.ListEntries(ctx, L, EntryFilter{From: from, To: to})
	if err != nil || len(nov) != 2 || nov[0].Name != "Rent" {
		t.Fatalf("November: %+v %v", nov, err)
	}

	e, err := st.UpdateEntry(ctx, L, added[0].ID, EntryInput{Amount: ptr(15.0), Type: ptr("")})
	if err != nil || e.Amount != 15 || e.Type != "" || e.Name != "Lunch" {
		t.Fatalf("update: %+v %v", e, err)
	}

	// Renaming an option renames it on its entries; deleting clears it.
	visa, _ := st.FindOption(ctx, L, KindCard, "Visa")
	if _, err := st.UpdateOption(ctx, L, visa.ID, ptr("Visa 4242"), nil); err != nil {
		t.Fatal(err)
	}
	if e, _ := st.GetEntry(ctx, L, added[0].ID); e.Card != "Visa 4242" {
		t.Errorf("card after rename: %q", e.Card)
	}
	cash, _ := st.FindOption(ctx, L, KindCard, "Cash")
	if _, err := st.UpdateOption(ctx, L, cash.ID, ptr("visa 4242"), nil); err == nil {
		t.Error("renamed onto an existing option")
	}
	if _, n, err := st.DeleteOption(ctx, L, cash.ID); err != nil || n != 1 {
		t.Errorf("delete Cash: cleared %d, %v", n, err)
	}
	if e, _ := st.GetEntry(ctx, L, added[2].ID); e.Card != "" {
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
	if n, _ := st.DeleteEntries(ctx, L, []int64{added[1].ID, added[2].ID}); n != 2 {
		t.Errorf("deleted %d", n)
	}
}

func TestStats(t *testing.T) {
	ctx := context.Background()
	st, u := newStore(t)
	L := personal(t, st, u)
	if _, err := st.CreateEntries(ctx, L, []EntryInput{
		entry("Lunch", "Food", 10.10, "2026-11-01", "Cash"),
		entry("Dinner", "Food", 20.20, "2026-11-01", "Visa"),
		entry("Bus", "Transport", 3, "2026-11-05", "Cash"),
		entry("Old", "Food", 100, "2026-10-15", "Cash"),
		entry("Older", "Food", 7, "2026-01-15", ""),
	}); err != nil {
		t.Fatal(err)
	}
	s, err := st.GetStats(ctx, L, "2026-11", 12, time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC))
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
	past, _ := st.GetStats(ctx, L, "2026-10", 3, time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC))
	if past.DailyAvg != 3.23 { // 100 / 31 days
		t.Errorf("October's daily average: %v", past.DailyAvg)
	}
}

// Month over month in the current month compares up to the same day.
func TestStatsMonthToDate(t *testing.T) {
	ctx := context.Background()
	st, u := newStore(t)
	L := personal(t, st, u)
	if _, err := st.CreateEntries(ctx, L, []EntryInput{
		entry("Early", "", 100, "2026-10-05", ""),
		entry("Late", "", 900, "2026-10-28", ""),
		entry("Now", "", 50, "2026-11-03", ""),
		entry("Planned", "", 20, "2026-11-20", ""), // after today
	}); err != nil {
		t.Fatal(err)
	}
	s, err := st.GetStats(ctx, L, "2026-11", 2, time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if s.CompareDay != 10 || s.ToDate != 50 || s.PrevToDate != 100 || s.PrevTotal != 1000 || s.Total != 70 {
		t.Errorf("month to date: %+v", s)
	}
	// A past month compares whole months.
	past, _ := st.GetStats(ctx, L, "2026-11", 2, time.Date(2026, 12, 5, 0, 0, 0, 0, time.UTC))
	if past.CompareDay != 0 || past.ToDate != 70 || past.PrevToDate != 1000 {
		t.Errorf("whole months: %+v", past)
	}
	// March 31 against February: up to February's last day.
	if _, err := st.CreateEntries(ctx, L, []EntryInput{entry("Feb", "", 30, "2027-02-28", "")}); err != nil {
		t.Fatal(err)
	}
	mar, _ := st.GetStats(ctx, L, "2027-03", 2, time.Date(2027, 3, 30, 0, 0, 0, 0, time.UTC))
	if mar.PrevToDate != 30 || mar.CompareDay != 30 {
		t.Errorf("short month before: %+v", mar)
	}
}

func TestAllMonths(t *testing.T) {
	ctx := context.Background()
	st, u := newStore(t)
	L := personal(t, st, u)
	if _, err := st.CreateEntries(ctx, L, []EntryInput{
		entry("Old", "", 10, "2024-03-02", ""),
		entry("A", "", 1, "2026-10-05", ""),
		entry("B", "", 2, "2026-10-28", ""),
		entry("Now", "", 5, "2026-11-30", ""),
		entry("Future", "", 7, "2026-12-01", ""),
	}); err != nil {
		t.Fatal(err)
	}
	got, err := st.AllMonths(ctx, L, "2026-11")
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
	if _, err := st.AllMonths(ctx, L, "nope"); err == nil {
		t.Error("a bad month was accepted")
	}
}

func TestSuggestEntries(t *testing.T) {
	ctx := context.Background()
	st, u := newStore(t)
	L := personal(t, st, u)
	if _, err := st.CreateEntries(ctx, L, []EntryInput{
		entry("Iced coffee", "Food", 25, "2026-11-20", "Alipay"),
		entry("Coffee", "Food", 18, "2026-11-01", "Cash"),
		entry("coffee", "Food", 19.5, "2026-11-10", "Credit card"),
		entry("Coffee", "", -18, "2026-11-12", ""), // a refund: not a suggestion
		entry("Lunch", "Food", 35, "2026-11-11", "Alipay"),
	}); err != nil {
		t.Fatal(err)
	}
	got, err := st.SuggestEntries(ctx, L, "COF", 5)
	if err != nil {
		t.Fatal(err)
	}
	// "coffee" starts with the text and was entered twice, so it leads, with its latest entry.
	if len(got) != 2 || got[0].Name != "coffee" || got[0].Uses != 2 || got[0].Amount != 19.5 || got[0].Card != "Credit card" || got[1].Name != "Iced coffee" {
		t.Errorf("suggestions: %+v", got)
	}
	// No text: every name, most used first.
	if all, _ := st.SuggestEntries(ctx, L, "  ", 100); len(all) != 3 || all[0].Name != "coffee" {
		t.Errorf("blank text: %+v", all)
	}
}

func TestRefund(t *testing.T) {
	ctx := context.Background()
	st, u := newStore(t)
	L := personal(t, st, u)
	today := time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)
	if _, err := st.CreateEntries(ctx, L, []EntryInput{
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
	r, err := st.Refund(ctx, L, RefundInput{Name: " coffee ", Amount: f(19.77)}, today)
	if err != nil || r.Refunded == nil || r.Refunded.Name != "Coffee" || r.Refunded.Date != "2026-11-02" || r.Added != nil {
		t.Fatalf("one match: %+v %v", r, err)
	}
	// The sign of the amount doesn't matter.
	if r, err := st.Refund(ctx, L, RefundInput{Name: "Headphones", Amount: f(-299)}, today); err != nil || r.Refunded == nil {
		t.Fatalf("negative amount: %+v %v", r, err)
	}
	// Several matches: nothing changes, they come back newest first; then one by id.
	if _, err := st.CreateEntries(ctx, L, []EntryInput{entry("Coffee", "Food", 18, "2026-11-22", "")}); err != nil {
		t.Fatal(err)
	}
	r, err = st.Refund(ctx, L, RefundInput{Name: "Coffee", Amount: f(18)}, today)
	if err != nil || len(r.Matches) != 2 || r.Matches[0].Date != "2026-11-22" || r.Refunded != nil {
		t.Fatalf("several: %+v %v", r, err)
	}
	if r, err := st.Refund(ctx, L, RefundInput{ID: r.Matches[1].ID}, today); err != nil || r.Refunded == nil || r.Refunded.Date != "2026-11-20" {
		t.Fatalf("by id: %+v %v", r, err)
	}
	// No match: added as a negative amount, with its own type, card and date.
	r, err = st.Refund(ctx, L, RefundInput{Name: "Old coat", Amount: f(500), Type: "Shopping", Card: "Cash", Date: "2026-11-23"}, today)
	if err != nil || r.Added == nil || r.Added.Amount != -500 || r.Added.Date != "2026-11-23" || r.Added.Type != "Shopping" || r.Added.Card != "Cash" {
		t.Fatalf("no match: %+v %v", r, err)
	}
	// Both the name and the amount are needed.
	for _, in := range []RefundInput{{Name: "Coffee"}, {Amount: f(18)}, {}} {
		if _, err := st.Refund(ctx, L, in, today); err == nil {
			t.Errorf("accepted %+v", in)
		}
	}
	// A refund entry can't be refunded.
	if _, err := st.Refund(ctx, L, RefundInput{ID: r.Added.ID}, today); err == nil {
		t.Error("a refund was refunded")
	}
}

func TestLedgers(t *testing.T) {
	ctx := context.Background()
	st, a := newStore(t)
	mine := personal(t, st, a)
	b, err := st.CreateUser(ctx, "b@example.com", "Bea", "password1", false)
	if err != nil {
		t.Fatal(err)
	}
	theirs := personal(t, st, b)

	// A new ledger has its own types, cards and currency.
	home, err := st.CreateLedger(ctx, a.ID, " Home ", "usd")
	if err != nil || home.Name != "Home" || home.Currency != "USD" || home.Role != RoleOwner || home.Members != 1 {
		t.Fatalf("create: %+v %v", home, err)
	}
	if opts, _ := st.ListOptions(ctx, home.ID); len(opts.Types) == 0 || len(opts.Cards) != 1 {
		t.Errorf("seeded options: %+v", opts)
	}
	if _, err := st.CreateLedger(ctx, a.ID, "  ", "USD"); err == nil {
		t.Error("a nameless ledger was made")
	}
	if _, err := st.CreateLedger(ctx, a.ID, "X", "dollars"); err == nil {
		t.Error("a bad currency was taken")
	}

	// Sharing: only by an account's email; once; editor or viewer.
	if _, err := st.AddLedgerMember(ctx, home.ID, "nobody@example.com", RoleEditor); err == nil {
		t.Error("shared with nobody")
	}
	if _, err := st.AddLedgerMember(ctx, home.ID, "B@example.com", "boss"); err == nil {
		t.Error("an unknown role was taken")
	}
	m, err := st.AddLedgerMember(ctx, home.ID, "B@example.com", RoleViewer)
	if err != nil || m.UserID != b.ID || m.Role != RoleViewer {
		t.Fatalf("add: %+v %v", m, err)
	}
	if _, err := st.AddLedgerMember(ctx, home.ID, "b@example.com", RoleEditor); !errors.Is(err, ErrAlreadyIn) {
		t.Errorf("added twice: %v", err)
	}
	bs, _ := st.ListLedgers(ctx, b.ID)
	if len(bs) != 2 || bs[0].ID != theirs || bs[1].ID != home.ID || bs[1].Role != RoleViewer || bs[1].CanWrite() || bs[1].Owner != "a@example.com" || bs[1].Members != 2 {
		t.Errorf("b's ledgers: %+v", bs)
	}
	if err := st.SetLedgerMemberRole(ctx, home.ID, b.ID, RoleEditor); err != nil {
		t.Error(err)
	}
	if l, _ := st.GetLedger(ctx, b.ID, home.ID); !l.CanWrite() {
		t.Errorf("editor: %+v", l)
	}
	// The owner stays the owner.
	if err := st.SetLedgerMemberRole(ctx, home.ID, a.ID, RoleViewer); !errors.Is(err, ErrOwnerRole) {
		t.Errorf("owner's role: %v", err)
	}
	if err := st.RemoveLedgerMember(ctx, home.ID, a.ID); !errors.Is(err, ErrOwnerRole) {
		t.Errorf("removing the owner: %v", err)
	}
	// Others can't see a ledger they don't have.
	if _, err := st.GetLedger(ctx, b.ID, mine); !errors.Is(err, ErrNotMember) {
		t.Errorf("someone else's ledger: %v", err)
	}
	if ms, _ := st.ListLedgerMembers(ctx, home.ID); len(ms) != 2 || ms[0].Role != RoleOwner || ms[1].Name != "Bea" {
		t.Errorf("members: %+v", ms)
	}
	if err := st.RemoveLedgerMember(ctx, home.ID, b.ID); err != nil {
		t.Error(err)
	}
	if _, err := st.GetLedger(ctx, b.ID, home.ID); !errors.Is(err, ErrNotMember) {
		t.Errorf("after leaving: %v", err)
	}

	// Renaming, and deleting, but never one's last ledger.
	name, cur := "House", "eur"
	if err := st.UpdateLedger(ctx, home.ID, &name, &cur); err != nil {
		t.Error(err)
	}
	if l, _ := st.GetLedger(ctx, a.ID, home.ID); l.Name != "House" || l.Currency != "EUR" {
		t.Errorf("updated: %+v", l)
	}
	if _, err := st.CreateEntries(ctx, home.ID, []EntryInput{entry("Sofa", "", 900, "2026-11-01", "")}); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteLedger(ctx, a.ID, home.ID); err != nil {
		t.Fatal(err)
	}
	if es, _ := st.ListEntries(ctx, home.ID, EntryFilter{}); len(es) != 0 {
		t.Errorf("the deleted ledger's entries: %+v", es)
	}
	if err := st.DeleteLedger(ctx, a.ID, mine); !errors.Is(err, ErrLastLedger) {
		t.Errorf("deleting the last ledger: %v", err)
	}
}

// An older database, from before ledgers, keeps everyone's entries and options in their personal ledger.
func TestLedgersMigration(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "old.db")
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		migrations[0],
		`INSERT INTO users (id, email, password_hash, prefs, created_at, updated_at) VALUES (7, 'old@example.com', 'x', '{"currency":"USD"}', '2026-01-01', '2026-01-01')`,
		`INSERT INTO options (user_id, kind, name, color) VALUES (7, 'type', 'Food', 'orange')`,
		`INSERT INTO entries (user_id, name, type, amount, date, card, created_at, updated_at) VALUES (7, 'Lunch', 'Food', 3500, '2026-01-02', '', '2026-01-02', '2026-01-02')`,
		`PRAGMA user_version = 1`,
	} {
		if _, err := db.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()

	st, err := Open(path, bytes.Repeat([]byte{1}, 32))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ls, err := st.ListLedgers(ctx, 7)
	if err != nil || len(ls) != 1 || ls[0].ID != 7 || ls[0].Name != "Personal" || ls[0].Currency != "USD" || ls[0].Role != RoleOwner {
		t.Fatalf("ledgers: %+v %v", ls, err)
	}
	if es, _ := st.ListEntries(ctx, 7, EntryFilter{}); len(es) != 1 || es[0].Name != "Lunch" || es[0].Amount != 35 {
		t.Errorf("entries: %+v", es)
	}
	if opts, _ := st.ListOptions(ctx, 7); len(opts.Types) != 1 || opts.Types[0].Uses != 1 {
		t.Errorf("options: %+v", opts)
	}
}

func TestStatsTrends(t *testing.T) {
	ctx := context.Background()
	st, u := newStore(t)
	L := personal(t, st, u)
	if _, err := st.CreateEntries(ctx, L, []EntryInput{
		entry("Lunch", "Food", 30, "2025-03-02", ""),
		entry("Bus", "Transport", 5, "2025-11-04", ""),
		entry("Lunch", "Food", 40, "2025-11-08", ""),
		entry("Lunch", "Food", 100, "2026-09-15", ""),
		entry("Lunch", "Food", 60, "2026-10-03", ""),
		entry("Dinner", "Food", 80, "2026-10-25", ""), // after the 10th: left out of last month to date
		entry("Bus", "Transport", 7, "2026-10-04", ""),
		entry("Lunch", "Food", 45, "2026-11-02", ""),
		entry("Train", "Transport", 20, "2026-11-06", ""),
		entry("Later", "Food", 10, "2026-11-20", ""), // after today
	}); err != nil {
		t.Fatal(err)
	}
	s, err := st.GetStats(ctx, L, "2026-11", 3, time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	// Each type over September to November, most spent first.
	if len(s.TypeMonthly) != 2 || s.TypeMonthly[0].Name != "Food" || fmt.Sprint(s.TypeMonthly[0].Totals) != "[100 140 55]" || fmt.Sprint(s.TypeMonthly[1].Totals) != "[0 7 20]" {
		t.Errorf("type monthly: %+v", s.TypeMonthly)
	}
	// This month by type up to today: the 20th's Food left out.
	if len(s.ByTypeToDate) != 2 || s.ByTypeToDate[0].Name != "Food" || s.ByTypeToDate[0].Total != 45 || s.ByType[0].Total != 55 {
		t.Errorf("by type to date: %+v (whole month %+v)", s.ByTypeToDate, s.ByType)
	}
	// The usual: the months since the first entry (Mar 2025), at most six,
	// each up to the 10th: May to October 2026 hold Food 60 (Oct 3) and
	// Transport 7 (Oct 4) up to then, so a usual month to date is 10 and 1.17.
	avg := map[string]float64{}
	for _, g := range s.TypeAvg {
		avg[g.Name] = g.Total
	}
	if s.AvgMonths != 6 || s.AvgToDate != 11.17 || avg["Food"] != 10 || avg["Transport"] != 1.17 {
		t.Errorf("usual: %d months, %v, %+v", s.AvgMonths, s.AvgToDate, s.TypeAvg)
	}
	// Early on, only the months since the first entry count, and a past
	// month's are whole: May 2025's usual is March (30) and April (0).
	may, _ := st.GetStats(ctx, L, "2025-05", 3, time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC))
	if may.AvgMonths != 2 || may.AvgToDate != 15 || len(may.TypeAvg) != 1 || may.TypeAvg[0].Total != 15 {
		t.Errorf("young usual: %d months, %v, %+v", may.AvgMonths, may.AvgToDate, may.TypeAvg)
	}
	// Nothing before the first entry: no usual yet.
	mar, _ := st.GetStats(ctx, L, "2025-03", 3, time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC))
	if mar.AvgMonths != 0 || len(mar.TypeAvg) != 0 {
		t.Errorf("no usual: %+v", mar)
	}
	// The year: all of it, to date against the same day last year, month by month.
	y := s.Year
	if y.Year != 2026 || y.Total != 322 || y.Count != 7 || y.ToDate != 312 || y.PrevToDate != 75 || y.CompareDate != "2026-11-10" {
		t.Errorf("year: %+v", y)
	}
	if len(y.Months) != 12 || y.Months[10].Total != 75 || y.Months[0].Month != "2026-01" || len(y.PrevMonths) != 12 || y.PrevMonths[10].Total != 45 || y.PrevMonths[2].Total != 30 {
		t.Errorf("year months: %+v / %+v", y.Months, y.PrevMonths)
	}
	if len(y.ByType) != 2 || y.ByType[0].Name != "Food" || y.ByType[0].Total != 295 {
		t.Errorf("year by type: %+v", y.ByType)
	}
	// A past year compares whole years.
	past, _ := st.GetStats(ctx, L, "2025-11", 3, time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC))
	if past.Year.CompareDate != "" || past.Year.ToDate != 75 || past.Year.PrevToDate != 0 {
		t.Errorf("past year: %+v", past.Year)
	}
}

func TestEntryOriginal(t *testing.T) {
	ctx := context.Background()
	st, u := newStore(t)
	L := personal(t, st, u)
	usd, amount, orig := "USD", 83.31, 12.5
	in := entry("Hotel deposit", "Travel", amount, "2026-11-24", "")
	in.OrigAmount, in.OrigCurrency = &orig, &usd
	got, err := st.CreateEntries(ctx, L, []EntryInput{in})
	if err != nil {
		t.Fatal(err)
	}
	e := got[0]
	if e.Amount != 83.31 || e.OrigAmount == nil || *e.OrigAmount != 12.5 || e.OrigCurrency != "USD" {
		t.Fatalf("saved: %+v", e)
	}
	// Other changes, and the same amount again, keep it.
	name, same := "Hotel", 83.31
	if e2, _ := st.UpdateEntry(ctx, L, e.ID, EntryInput{Name: &name, Amount: &same}); e2.OrigCurrency != "USD" || *e2.OrigAmount != 12.5 {
		t.Errorf("kept: %+v", e2)
	}
	// A new amount by itself drops it: it no longer matches.
	other := 90.0
	if e3, _ := st.UpdateEntry(ctx, L, e.ID, EntryInput{Amount: &other}); e3.OrigCurrency != "" || e3.OrigAmount != nil {
		t.Errorf("not dropped: %+v", e3)
	}
	// Set again with the amount, then cleared by an empty code.
	eur, back := "eur", 11.0
	if e4, _ := st.UpdateEntry(ctx, L, e.ID, EntryInput{Amount: &other, OrigAmount: &back, OrigCurrency: &eur}); e4.OrigCurrency != "EUR" || *e4.OrigAmount != 11 {
		t.Errorf("set: %+v", e4)
	}
	none := ""
	if e5, _ := st.UpdateEntry(ctx, L, e.ID, EntryInput{OrigCurrency: &none}); e5.OrigCurrency != "" || e5.OrigAmount != nil || e5.Amount != 90 {
		t.Errorf("cleared: %+v", e5)
	}
	// A bad code, or a code without an amount, is refused.
	bad := "dollars"
	in.OrigCurrency = &bad
	if _, err := st.CreateEntries(ctx, L, []EntryInput{in}); err == nil {
		t.Error("a bad currency was accepted")
	}
	in.OrigCurrency, in.OrigAmount = &usd, nil
	if _, err := st.CreateEntries(ctx, L, []EntryInput{in}); err == nil {
		t.Error("a currency without its amount was accepted")
	}
	// A refund that matches nothing keeps what came back in the other currency, negative.
	refAmount, refOrig := 53.32, 8.0
	res, err := st.Refund(ctx, L, RefundInput{Name: "Museum", Amount: &refAmount, Type: "Travel", OrigAmount: &refOrig, OrigCurrency: "USD"}, time.Date(2026, 11, 25, 0, 0, 0, 0, time.UTC))
	if err != nil || res.Added == nil || res.Added.Amount != -53.32 || res.Added.OrigAmount == nil || *res.Added.OrigAmount != -8 || res.Added.OrigCurrency != "USD" {
		t.Fatalf("refund: %+v %v", res, err)
	}
}

// Writes at once (the agent in the background, Undo restoring several) all
// land: one waits for the other rather than failing.
func TestConcurrentWrites(t *testing.T) {
	ctx := context.Background()
	st, u := newStore(t)
	L := personal(t, st, u)
	errs := make(chan error, 20)
	for i := 0; i < 20; i++ {
		go func(i int) {
			_, err := st.CreateEntries(ctx, L, []EntryInput{entry(fmt.Sprintf("Item %d", i), "Shopping", 1, "2026-11-25", "Cash")})
			errs <- err
		}(i)
	}
	for i := 0; i < 20; i++ {
		if err := <-errs; err != nil {
			t.Errorf("write %d: %v", i, err)
		}
	}
	if n, _ := st.CountEntries(ctx, L); n != 20 {
		t.Errorf("%d entries, want 20", n)
	}
}
