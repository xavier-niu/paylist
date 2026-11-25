package server

import (
	"net/http"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/xavier-niu/paylist/internal/fx"
	"github.com/xavier-niu/paylist/internal/store"
)

// What the app does, MCP does too: amounts in another currency, history,
// the order of types, and ledgers made, changed and shared.
func TestMCPParity(t *testing.T) {
	e := newEnv(t)
	c := e.signIn(t, "parity@example.com")
	e.signIn(t, "friend@example.com")
	tok := decodeJSON[struct {
		Token string `json:"token"`
	}](t, e.do(t, c, "POST", "/api/me/tokens", map[string]string{"name": "test"}))
	s, err := mcp.NewClient(&mcp.Implementation{Name: "t", Version: "1"}, nil).Connect(t.Context(), &mcp.StreamableClientTransport{
		Endpoint: e.srv.URL + "/mcp", HTTPClient: &http.Client{Transport: bearer(tok.Token)},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	// Paid 12.50 USD on Nov 20 (1 USD = 6.6777 CNY there): 83.47, and the 12.50 USD kept.
	added, msg := callTool[mcpAddOut](t, s, "add_entries", map[string]any{"entries": []any{
		map[string]any{"name": "Hotel", "amount": 12.5, "currency": "USD", "date": "2026-11-20", "type": "Travel"},
	}})
	if msg != "" || len(added.Added) != 1 {
		t.Fatalf("add in USD: %s %+v", msg, added)
	}
	h := added.Added[0]
	if h.Amount != 83.47 || h.OrigAmount == nil || *h.OrigAmount != 12.5 || h.OrigCurrency != "USD" {
		t.Fatalf("converted: %+v", h)
	}
	// Changed to 10 USD, then to 50 in the ledger's own currency, which forgets the USD.
	if e2, msg := callTool[store.Entry](t, s, "update_entry", map[string]any{"id": h.ID, "amount": 10, "currency": "USD"}); msg != "" || e2.Amount != 66.78 || *e2.OrigAmount != 10 {
		t.Errorf("update in USD: %s %+v", msg, e2)
	}
	if e3, msg := callTool[store.Entry](t, s, "update_entry", map[string]any{"id": h.ID, "amount": 50, "currency": "CNY"}); msg != "" || e3.Amount != 50 || e3.OrigCurrency != "" {
		t.Errorf("update in CNY: %s %+v", msg, e3)
	}
	if _, msg := callTool[store.Entry](t, s, "update_entry", map[string]any{"id": h.ID, "currency": "USD"}); !strings.Contains(msg, "amount") {
		t.Errorf("a currency without an amount: %q", msg)
	}
	// A refund in USD, of part of a purchase in USD: both go down.
	added, _ = callTool[mcpAddOut](t, s, "add_entries", map[string]any{"entries": []any{
		map[string]any{"name": "Jacket", "amount": 100, "currency": "USD", "date": "2026-11-20"},
	}})
	j := added.Added[0]
	res, msg := callTool[store.RefundResult](t, s, "refund", map[string]any{"id": j.ID, "amount": 40, "currency": "USD", "date": "2026-11-20"})
	if msg != "" || res.Reduced == nil || res.Reduced.OrigAmount == nil || *res.Reduced.OrigAmount != 60 || res.Reduced.Amount != 400.66 {
		t.Fatalf("part refund in USD: %s %+v", msg, res.Reduced)
	}

	// The history, made through MCP.
	hist, msg := callTool[mcpHistoryOut](t, s, "get_history", map[string]any{"id": j.ID})
	if msg != "" || len(hist.Events) != 2 || hist.Events[0].Via != "mcp" || hist.Events[1].Action != store.EntryRefunded {
		t.Errorf("history: %s %+v", msg, hist)
	}
	if r, msg := callTool[fx.Rate](t, s, "get_exchange_rate", map[string]any{"from": "USD", "to": "CNY", "date": "2026-11-20"}); msg != "" || r.Rate != 6.6777 {
		t.Errorf("rate: %s %+v", msg, r)
	}

	// The order of the cards.
	opts, _ := callTool[store.Options](t, s, "list_options", map[string]any{})
	names := []string{}
	for i := len(opts.Cards) - 1; i >= 0; i-- {
		names = append(names, opts.Cards[i].Name)
	}
	if _, msg := callTool[mcpOptionOut](t, s, "manage_option", map[string]any{"kind": "card", "action": "reorder", "name": "", "order": names}); msg != "" {
		t.Fatalf("reorder: %s", msg)
	}
	if again, _ := callTool[store.Options](t, s, "list_options", map[string]any{}); again.Cards[0].Name != names[0] {
		t.Errorf("reordered: %+v", again.Cards)
	}

	// A ledger: made, renamed, re-currencied, shared, and deleted.
	made, msg := callTool[mcpManageLedgerOut](t, s, "manage_ledger", map[string]any{"action": "create", "name": "Trip", "currency": "JPY"})
	if msg != "" || made.Ledger == nil || made.Ledger.Currency != "JPY" {
		t.Fatalf("create: %s %+v", msg, made)
	}
	if r, msg := callTool[mcpManageLedgerOut](t, s, "manage_ledger", map[string]any{"action": "rename", "ledger": "Trip", "name": "Japan"}); msg != "" || r.Ledger.Name != "Japan" {
		t.Errorf("rename: %s %+v", msg, r)
	}
	if r, msg := callTool[mcpManageLedgerOut](t, s, "manage_ledger", map[string]any{"action": "currency", "ledger": "Japan", "currency": "USD"}); msg != "" || r.Ledger.Currency != "USD" {
		t.Errorf("currency: %s %+v", msg, r)
	}
	p, msg := callTool[mcpPeopleOut](t, s, "ledger_people", map[string]any{"ledger": "Japan", "action": "invite", "email": "friend@example.com", "role": "viewer"})
	if msg != "" || len(p.People) != 2 {
		t.Fatalf("invite: %s %+v", msg, p)
	}
	if p, msg = callTool[mcpPeopleOut](t, s, "ledger_people", map[string]any{"ledger": "Japan", "action": "set_role", "email": "friend@example.com", "role": "editor"}); msg != "" || p.People[1].Role != "editor" {
		t.Errorf("set role: %s %+v", msg, p)
	}
	if p, msg = callTool[mcpPeopleOut](t, s, "ledger_people", map[string]any{"ledger": "Japan", "action": "remove", "email": "friend@example.com"}); msg != "" || len(p.People) != 1 {
		t.Errorf("remove: %s %+v", msg, p)
	}
	if _, msg := callTool[mcpManageLedgerOut](t, s, "manage_ledger", map[string]any{"action": "leave", "ledger": "Japan"}); !strings.Contains(msg, "owner") {
		t.Errorf("the owner leaving: %q", msg)
	}
	if d, msg := callTool[mcpManageLedgerOut](t, s, "manage_ledger", map[string]any{"action": "delete", "ledger": "Japan"}); msg != "" || !d.Done {
		t.Errorf("delete: %s %+v", msg, d)
	}
	if _, msg := callTool[mcpManageLedgerOut](t, s, "manage_ledger", map[string]any{"action": "delete", "ledger": "Personal"}); msg == "" {
		t.Error("the last ledger was deleted")
	}
}
