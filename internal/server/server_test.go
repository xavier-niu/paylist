package server

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/xavier-niu/paylist/internal/store"
)

const testPassword = "correct horse"

func init() { store.FastHashesForTests() }

type env struct {
	srv *httptest.Server
	st  *store.Store
}

func newEnv(t *testing.T) *env {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"), bytes.Repeat([]byte{1}, 32))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	static := fstest.MapFS{"index.html": {Data: []byte("<!doctype html><title>Paylist</title>")}}
	srv := httptest.NewServer(New(Options{Store: st, Static: static, AllowFake: true}).Handler())
	t.Cleanup(srv.Close)
	return &env{srv, st}
}

// signIn creates an account, signs in with its own cookie jar, and selects the demo model.
func (e *env) signIn(t *testing.T, email string) *http.Client {
	t.Helper()
	if _, err := e.st.CreateUser(t.Context(), email, "", testPassword, false); err != nil {
		t.Fatal(err)
	}
	jar, _ := cookiejar.New(nil)
	c := &http.Client{Jar: jar}
	if code := status(t, e.do(t, c, "POST", "/api/auth/login", map[string]string{"email": email, "password": testPassword})); code != 200 {
		t.Fatalf("login: %d", code)
	}
	if code := status(t, e.do(t, c, "PUT", "/api/me/model", map[string]string{"preset": "fake", "kind": "fake"})); code != 200 {
		t.Fatalf("model: %d", code)
	}
	return c
}

func (e *env) do(t *testing.T, c *http.Client, method, path string, body any) *http.Response {
	t.Helper()
	var r io.Reader
	if body != nil {
		buf, _ := json.Marshal(body)
		r = bytes.NewReader(buf)
	}
	req, _ := http.NewRequest(method, e.srv.URL+path, r)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

// doIn is do on a ledger (the X-Paylist-Ledger header).
func (e *env) doIn(t *testing.T, c *http.Client, ledger int64, method, path string, body any) *http.Response {
	t.Helper()
	var r io.Reader
	if body != nil {
		buf, _ := json.Marshal(body)
		r = bytes.NewReader(buf)
	}
	req, _ := http.NewRequest(method, e.srv.URL+path, r)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("X-Paylist-Ledger", itoa(ledger))
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func status(t *testing.T, resp *http.Response) int {
	t.Helper()
	resp.Body.Close()
	return resp.StatusCode
}

func decodeJSON[T any](t *testing.T, resp *http.Response) T {
	t.Helper()
	defer resp.Body.Close()
	var v T
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return v
}

func readEvents(t *testing.T, resp *http.Response) []map[string]any {
	t.Helper()
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status %d: %s", resp.StatusCode, b)
	}
	var out []map[string]any
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		var ev map[string]any
		if err := json.Unmarshal(sc.Bytes(), &ev); err != nil {
			t.Fatalf("bad ndjson line %q: %v", sc.Text(), err)
		}
		out = append(out, ev)
	}
	return out
}

type entriesPage struct {
	Month   string        `json:"month"`
	Entries []store.Entry `json:"entries"`
}

func TestTableAPI(t *testing.T) {
	e := newEnv(t)
	c := e.signIn(t, "a@example.com")

	if code := status(t, e.do(t, &http.Client{}, "GET", "/api/entries", nil)); code != 401 {
		t.Errorf("signed out: %d", code)
	}
	created := decodeJSON[store.Entry](t, e.do(t, c, "POST", "/api/entries",
		map[string]any{"name": "Lunch", "type": "Food", "amount": 23.5, "date": "2026-11-03", "card": "Visa"}))
	if created.ID == 0 || created.Card != "Visa" {
		t.Fatalf("created: %+v", created)
	}
	if code := status(t, e.do(t, c, "POST", "/api/entries", map[string]any{"name": "", "amount": 1, "date": "2026-11-03"})); code != 400 {
		t.Errorf("nameless entry: %d", code)
	}
	upd := decodeJSON[store.Entry](t, e.do(t, c, "PUT", "/api/entries/"+itoa(created.ID), map[string]any{"amount": 30}))
	if upd.Amount != 30 || upd.Name != "Lunch" {
		t.Errorf("updated: %+v", upd)
	}
	page := decodeJSON[entriesPage](t, e.do(t, c, "GET", "/api/entries?month=2026-11", nil))
	if len(page.Entries) != 1 || page.Month != "2026-11" {
		t.Errorf("November: %+v", page)
	}
	if got := decodeJSON[entriesPage](t, e.do(t, c, "GET", "/api/entries?month=2026-10", nil)); len(got.Entries) != 0 {
		t.Errorf("October: %+v", got)
	}
	opts := decodeJSON[store.Options](t, e.do(t, c, "GET", "/api/options", nil))
	if len(opts.Cards) != 2 {
		t.Errorf("cards: %+v", opts.Cards)
	}
	stats := decodeJSON[store.Stats](t, e.do(t, c, "GET", "/api/stats?month=2026-11&months=6", nil))
	if stats.Total != 30 || len(stats.Monthly) != 6 || len(stats.ByType) != 1 {
		t.Errorf("stats: %+v", stats)
	}
	resp := e.do(t, c, "GET", "/api/export.csv?month=2026-11", nil)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(body), "Lunch,Food,30.00,2026-11-03,Visa") {
		t.Errorf("csv: %s", body)
	}

	// The Refund dialog: matches, then a refund by the picked id.
	if m := decodeJSON[[]store.Entry](t, e.do(t, c, "GET", "/api/refund/matches?name=LUNCH&amount=30&today=2026-11-20", nil)); len(m) != 1 || m[0].ID != created.ID {
		t.Errorf("refund matches: %+v", m)
	}
	if m := decodeJSON[[]store.Entry](t, e.do(t, c, "GET", "/api/refund/matches?name=lun&amount=30&today=2026-11-20", nil)); len(m) != 0 {
		t.Errorf("refund matches, part of the name: %+v", m)
	}
	if code := status(t, e.do(t, c, "GET", "/api/refund/matches?amount=lots", nil)); code != 400 {
		t.Errorf("refund matches, bad amount: %d", code)
	}
	gift := decodeJSON[store.RefundResult](t, e.do(t, c, "POST", "/api/refund", map[string]any{"name": "Gift card", "amount": 50, "type": "Shopping", "card": "Visa", "date": "2026-11-19", "today": "2026-11-20"}))
	if gift.Added == nil || gift.Added.Amount != -50 || gift.Added.Date != "2026-11-19" || gift.Added.Type != "Shopping" || gift.Added.Card != "Visa" {
		t.Errorf("refund, added: %+v", gift)
	}
	if code := status(t, e.do(t, c, "POST", "/api/refund", map[string]any{"id": gift.Added.ID})); code != 400 {
		t.Errorf("refunding a refund: %d", code)
	}
	if got := decodeJSON[store.RefundResult](t, e.do(t, c, "POST", "/api/refund", map[string]any{"name": "Gift card", "amount": -50, "today": "2026-11-20"})); got.Added == nil {
		t.Errorf("a refund never matches a refund: %+v", got)
	}
	e.do(t, c, "POST", "/api/entries/delete", map[string]any{"ids": []int64{gift.Added.ID}}).Body.Close()

	// The New entry form's suggestions: this account's names only.
	if sg := decodeJSON[[]store.Suggestion](t, e.do(t, c, "GET", "/api/entries/suggest?q=lun", nil)); len(sg) != 1 || sg[0].Name != "Lunch" || sg[0].Amount != 30 || sg[0].Card != "Visa" {
		t.Errorf("suggest: %+v", sg)
	}

	// Another account can't see or change it.
	other := e.signIn(t, "b@example.com")
	if sg := decodeJSON[[]store.Suggestion](t, e.do(t, other, "GET", "/api/entries/suggest?q=lun", nil)); len(sg) != 0 {
		t.Errorf("other's suggestions: %+v", sg)
	}
	if code := status(t, e.do(t, other, "PUT", "/api/entries/"+itoa(created.ID), map[string]any{"amount": 1})); code != 404 {
		t.Errorf("other's update: %d", code)
	}
	if got := decodeJSON[map[string]int](t, e.do(t, c, "POST", "/api/entries/delete", map[string]any{"ids": []int64{created.ID}})); got["deleted"] != 1 {
		t.Errorf("delete: %v", got)
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

// bearer adds an agent token to every request, as an MCP client configured
// with an Authorization header does.
type bearer string

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+string(b))
	return http.DefaultTransport.RoundTrip(r)
}

func callTool[T any](t *testing.T, s *mcp.ClientSession, name string, args map[string]any) (out T, failed string) {
	t.Helper()
	res, err := s.CallTool(t.Context(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if res.IsError {
		var msg []string
		for _, c := range res.Content {
			if tc, ok := c.(*mcp.TextContent); ok {
				msg = append(msg, tc.Text)
			}
		}
		return out, strings.Join(msg, " ")
	}
	raw, _ := json.Marshal(res.StructuredContent)
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("%s: %v in %s", name, err, raw)
	}
	return out, ""
}

func TestMCP(t *testing.T) {
	e := newEnv(t)
	c := e.signIn(t, "mcp@example.com")
	created := decodeJSON[struct {
		Token    string `json:"token"`
		Endpoint string `json:"endpoint"`
	}](t, e.do(t, c, "POST", "/api/me/tokens", map[string]string{"name": "Claude Code"}))
	if !strings.HasPrefix(created.Token, "plt_") || created.Endpoint != e.srv.URL+"/mcp" {
		t.Fatalf("token: %+v", created)
	}

	// No token, or a wrong one: 401.
	for _, tok := range []string{"", "plt_nope"} {
		req, _ := http.NewRequest("POST", e.srv.URL+"/mcp", strings.NewReader(`{}`))
		req.Header.Set("Content-Type", "application/json")
		if tok != "" {
			req.Header.Set("Authorization", "Bearer "+tok)
		}
		resp, _ := http.DefaultClient.Do(req)
		if resp.StatusCode != 401 {
			t.Errorf("token %q: %d", tok, resp.StatusCode)
		}
		resp.Body.Close()
	}

	client := mcp.NewClient(&mcp.Implementation{Name: "test-agent", Version: "1"}, nil)
	s, err := client.Connect(t.Context(), &mcp.StreamableClientTransport{
		Endpoint: e.srv.URL + "/mcp", HTTPClient: &http.Client{Transport: bearer(created.Token)},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	tools, err := s.ListTools(t.Context(), nil)
	if err != nil || len(tools.Tools) != 12 {
		t.Fatalf("tools: %v %v", tools, err)
	}

	month := time.Now().Format("2006-01")
	added, msg := callTool[mcpAddOut](t, s, "add_entries", map[string]any{"entries": []any{
		map[string]any{"name": "Coffee", "amount": 4.5, "type": "food", "card": "Amex"},
		map[string]any{"name": "Book", "amount": 30, "date": month + "-01", "type": "Shopping"},
	}})
	if msg != "" || len(added.Added) != 2 || added.Added[0].Type != "Food" || added.Added[0].Date != time.Now().Format("2006-01-02") {
		t.Fatalf("add: %+v %s", added, msg)
	}
	if _, msg := callTool[mcpAddOut](t, s, "add_entries", map[string]any{"entries": []any{map[string]any{"name": "x", "amount": 1, "date": "soon"}}}); msg == "" {
		t.Error("a bad date was accepted")
	}
	list, _ := callTool[mcpListOut](t, s, "list_entries", map[string]any{})
	if list.Count != 2 || list.Total != 34.5 {
		t.Errorf("list: %+v", list)
	}
	upd, msg := callTool[store.Entry](t, s, "update_entry", map[string]any{"id": added.Added[1].ID, "card": "Amex"})
	if msg != "" || upd.Card != "Amex" || upd.Amount != 30 {
		t.Errorf("update: %+v %s", upd, msg)
	}
	if _, msg := callTool[mcpOptionOut](t, s, "manage_option", map[string]any{"kind": "card", "action": "rename", "name": "amex", "new_name": "Amex Gold"}); msg != "" {
		t.Errorf("rename: %s", msg)
	}
	stats, _ := callTool[store.Stats](t, s, "get_stats", map[string]any{"months": 3})
	if stats.Total != 34.5 || len(stats.ByCard) != 1 || stats.ByCard[0].Name != "Amex Gold" || len(stats.Monthly) != 3 {
		t.Errorf("stats: %+v", stats)
	}
	if months, _ := callTool[mcpMonthsOut](t, s, "list_months", map[string]any{}); len(months.Months) != 1 || months.Months[0].Month != month || months.Months[0].Total != 34.5 {
		t.Errorf("months: %+v", months)
	}
	if sg, _ := callTool[mcpSuggestOut](t, s, "suggest_entries", map[string]any{"query": "cof"}); len(sg.Suggestions) != 1 || sg.Suggestions[0].Type != "Food" || sg.Suggestions[0].Amount != 4.5 {
		t.Errorf("suggest: %+v", sg)
	}
	if p, _ := callTool[mcpProfileOut](t, s, "get_profile", map[string]any{}); p.Currency != "CNY" || p.Today != time.Now().Format("2006-01-02") {
		t.Errorf("profile: %+v", p)
	}

	// A refund that matches a purchase deletes it; one that doesn't is added as a negative amount.
	ref, msg := callTool[store.RefundResult](t, s, "refund", map[string]any{"name": "coffee", "amount": 4.5})
	if msg != "" || ref.Refunded == nil || ref.Refunded.ID != added.Added[0].ID {
		t.Fatalf("refund: %+v %s", ref, msg)
	}
	ref, msg = callTool[store.RefundResult](t, s, "refund", map[string]any{"name": "Concert ticket", "amount": 80})
	if msg != "" || ref.Added == nil || ref.Added.Amount != -80 {
		t.Fatalf("refund, no match: %+v %s", ref, msg)
	}
	if _, msg := callTool[store.RefundResult](t, s, "refund", map[string]any{"name": "nothing like it"}); msg == "" {
		t.Error("a refund with no match and no amount was accepted")
	}

	del, _ := callTool[mcpDeleteOut](t, s, "delete_entries", map[string]any{"ids": []int64{ref.Added.ID, added.Added[1].ID}})
	if del.Deleted != 2 {
		t.Errorf("deleted %d", del.Deleted)
	}
	// Ledgers: by name, and a viewer can't change one.
	u, _ := e.st.GetUserByEmail(t.Context(), "mcp@example.com")
	if _, err := e.st.CreateLedger(t.Context(), u.ID, "Trip", "JPY"); err != nil {
		t.Fatal(err)
	}
	other, _ := e.st.CreateUser(t.Context(), "owner@example.com", "", "password1", false)
	shared, _ := e.st.CreateLedger(t.Context(), other.ID, "Family", "CNY")
	if _, err := e.st.AddLedgerMember(t.Context(), shared.ID, "mcp@example.com", "viewer"); err != nil {
		t.Fatal(err)
	}
	ls, _ := callTool[mcpLedgersOut](t, s, "list_ledgers", map[string]any{})
	if len(ls.Ledgers) != 3 {
		t.Errorf("ledgers: %+v", ls)
	}
	if out, msg := callTool[mcpAddOut](t, s, "add_entries", map[string]any{"ledger": "trip", "entries": []any{map[string]any{"name": "Ramen", "amount": 1200}}}); msg != "" || len(out.Added) != 1 {
		t.Errorf("adding to Trip: %+v %s", out, msg)
	}
	if p, _ := callTool[mcpProfileOut](t, s, "get_profile", map[string]any{"ledger": "Trip"}); p.Currency != "JPY" || p.Role != "owner" {
		t.Errorf("Trip's profile: %+v", p)
	}
	if _, msg := callTool[mcpAddOut](t, s, "add_entries", map[string]any{"ledger": "Family", "entries": []any{map[string]any{"name": "x", "amount": 1}}}); !strings.Contains(msg, "only view") {
		t.Errorf("a viewer adding: %q", msg)
	}
	if list, msg := callTool[mcpListOut](t, s, "list_entries", map[string]any{"ledger": "Family"}); msg != "" || list.Count != 0 {
		t.Errorf("a viewer reading: %+v %s", list, msg)
	}
	if _, msg := callTool[mcpListOut](t, s, "list_entries", map[string]any{"ledger": "Nope"}); !strings.Contains(msg, "no ledger") {
		t.Errorf("a ledger they don't have: %q", msg)
	}
}

func TestAgentChat(t *testing.T) {
	e := newEnv(t)
	c := e.signIn(t, "agent@example.com")

	events := readEvents(t, e.do(t, c, "POST", "/api/agent/chat", map[string]any{"message": "add Coffee 12.5 Food Cash"}))
	var types []string
	for _, ev := range events {
		types = append(types, ev["type"].(string))
	}
	got := strings.Join(types, " ")
	if !strings.HasPrefix(got, "chat text tool_call tool_result text") || !strings.HasSuffix(got, "done") || strings.Contains(got, "error") {
		t.Fatalf("events: %s\n%v", got, events)
	}
	chatID := events[0]["id"].(string)

	page := decodeJSON[entriesPage](t, e.do(t, c, "GET", "/api/entries", nil))
	if len(page.Entries) != 1 || page.Entries[0].Name != "Coffee" || page.Entries[0].Amount != 12.5 || page.Entries[0].Card != "Cash" {
		t.Fatalf("the agent's entry: %+v", page.Entries)
	}

	// Continue the conversation; it is saved with every step.
	events = readEvents(t, e.do(t, c, "POST", "/api/agent/chat", map[string]any{"chat_id": chatID, "message": "how much this month?"}))
	if events[0]["id"] != chatID {
		t.Errorf("continued chat: %v", events[0])
	}
	chat := decodeJSON[struct {
		Chat     store.ChatSummary `json:"chat"`
		Messages []map[string]any  `json:"messages"`
	}](t, e.do(t, c, "GET", "/api/chats/"+chatID, nil))
	if chat.Chat.Title != "add Coffee 12.5 Food Cash" || len(chat.Messages) != 8 {
		t.Errorf("saved chat: %+v", chat)
	}
	list := decodeJSON[[]store.ChatSummary](t, e.do(t, c, "GET", "/api/chats", nil))
	if len(list) != 1 {
		t.Errorf("chats: %+v", list)
	}

	// Files: the demo model says it can't read them; the turn and its files are saved, titled by the first file.
	png := base64.StdEncoding.EncodeToString([]byte("not really a png"))
	events = readEvents(t, e.do(t, c, "POST", "/api/agent/chat", map[string]any{"attachments": []any{map[string]any{"name": "receipt.png", "type": "image/png", "data": png}}}))
	var reply strings.Builder
	for _, ev := range events {
		if ev["type"] == "text" {
			reply.WriteString(ev["text"].(string))
		}
	}
	if events[0]["title"] != "receipt.png" || !strings.Contains(reply.String(), "got 1 file") {
		t.Errorf("a file: %v", events)
	}
	withFile := decodeJSON[struct {
		Messages []map[string]any `json:"messages"`
	}](t, e.do(t, c, "GET", "/api/chats/"+events[0]["id"].(string), nil))
	if att, _ := withFile.Messages[0]["attachments"].([]any); len(att) != 1 {
		t.Errorf("the saved file: %v", withFile.Messages[0])
	}
	for name, body := range map[string]map[string]any{
		"an exe":    {"message": "x", "attachments": []any{map[string]any{"name": "a.exe", "type": "application/x-msdownload", "data": png}}},
		"bad data":  {"message": "x", "attachments": []any{map[string]any{"name": "a.png", "type": "image/png", "data": "!!"}}},
		"not text":  {"message": "x", "attachments": []any{map[string]any{"name": "a.csv", "type": "text/csv", "data": base64.StdEncoding.EncodeToString([]byte{0xff, 0xfe, 0x00})}}},
		"six files": {"message": "x", "attachments": []any{map[string]any{"name": "1.png", "type": "image/png", "data": png}, map[string]any{"name": "2.png", "type": "image/png", "data": png}, map[string]any{"name": "3.png", "type": "image/png", "data": png}, map[string]any{"name": "4.png", "type": "image/png", "data": png}, map[string]any{"name": "5.png", "type": "image/png", "data": png}, map[string]any{"name": "6.png", "type": "image/png", "data": png}}},
		"nothing":   {"message": " "},
	} {
		if code := status(t, e.do(t, c, "POST", "/api/agent/chat", body)); code != 400 {
			t.Errorf("%s: %d", name, code)
		}
	}

	// Without a model: a plain error, before streaming.
	other := e.signIn(t, "nomodel@example.com")
	if code := status(t, e.do(t, other, "PUT", "/api/me/model", map[string]string{"preset": "anthropic", "kind": "anthropic"})); code != 200 {
		t.Fatal(code)
	}
	if code := status(t, e.do(t, other, "POST", "/api/agent/chat", map[string]any{"message": "hi"})); code != 401 {
		t.Errorf("no key: %d", code)
	}
	if code := status(t, e.do(t, c, "DELETE", "/api/chats/"+chatID, nil)); code != 200 {
		t.Errorf("delete chat: %d", code)
	}
}

func TestPrefs(t *testing.T) {
	e := newEnv(t)
	c := e.signIn(t, "prefs@example.com")
	p := decodeJSON[store.Prefs](t, e.do(t, c, "PUT", "/api/me/prefs", map[string]any{}))
	if p.Currency != "CNY" || p.Theme != "system" || p.Language != "system" || p.LedgerOrder != "date-asc" {
		t.Errorf("defaults: %+v", p)
	}
	// Only what's sent changes, and only to something allowed.
	p = decodeJSON[store.Prefs](t, e.do(t, c, "PUT", "/api/me/prefs", map[string]any{"ledger_order": "amount-desc", "language": "zh"}))
	if p.LedgerOrder != "amount-desc" || p.Language != "zh" || p.Currency != "CNY" {
		t.Errorf("saved: %+v", p)
	}
	p = decodeJSON[store.Prefs](t, e.do(t, c, "PUT", "/api/me/prefs", map[string]any{"ledger_order": "sideways", "currency": "usd"}))
	if p.LedgerOrder != "amount-desc" || p.Currency != "USD" {
		t.Errorf("a bad order: %+v", p)
	}
	// It comes back with the account.
	me := decodeJSON[struct {
		User store.User `json:"user"`
	}](t, e.do(t, c, "GET", "/api/me", nil))
	if me.User.Prefs.LedgerOrder != "amount-desc" {
		t.Errorf("me: %+v", me.User.Prefs)
	}
}

func TestLedgersAPI(t *testing.T) {
	e := newEnv(t)
	a := e.signIn(t, "ann@example.com")
	b := e.signIn(t, "ben@example.com")
	type ledgers struct {
		Ledgers []store.Ledger `json:"ledgers"`
		Current int64          `json:"current"`
	}
	mine := decodeJSON[ledgers](t, e.do(t, a, "GET", "/api/ledgers", nil))
	if len(mine.Ledgers) != 1 || mine.Current != mine.Ledgers[0].ID || mine.Ledgers[0].Role != "owner" {
		t.Fatalf("ann's ledgers: %+v", mine)
	}
	personal := mine.Current

	// A second ledger, used through the header; the first is still the default.
	home := decodeJSON[store.Ledger](t, e.do(t, a, "POST", "/api/ledgers", map[string]any{"name": "Home", "currency": "USD"}))
	if home.ID == 0 || home.Currency != "USD" {
		t.Fatalf("new ledger: %+v", home)
	}
	e.doIn(t, a, home.ID, "POST", "/api/entries", map[string]any{"name": "Sofa", "amount": 900, "date": "2026-11-02"}).Body.Close()
	if got := decodeJSON[entriesPage](t, e.doIn(t, a, home.ID, "GET", "/api/entries?month=2026-11", nil)); len(got.Entries) != 1 {
		t.Errorf("home's entries: %+v", got)
	}
	if got := decodeJSON[entriesPage](t, e.do(t, a, "GET", "/api/entries?month=2026-11", nil)); len(got.Entries) != 0 {
		t.Errorf("the default ledger's entries: %+v", got)
	}
	// Switching makes it the default.
	e.do(t, a, "PUT", "/api/me/prefs", map[string]any{"ledger_id": home.ID}).Body.Close()
	if got := decodeJSON[ledgers](t, e.do(t, a, "GET", "/api/ledgers", nil)); got.Current != home.ID {
		t.Errorf("current after switching: %+v", got)
	}

	// Ben can't see it until it's shared with him; as a viewer he can read, not write.
	if code := status(t, e.doIn(t, b, home.ID, "GET", "/api/entries", nil)); code != 404 {
		t.Errorf("an outsider: %d", code)
	}
	if code := status(t, e.do(t, b, "POST", "/api/ledgers/"+itoa(home.ID)+"/members", map[string]any{"email": "ben@example.com", "role": "editor"})); code != 404 {
		t.Errorf("an outsider sharing: %d", code)
	}
	if code := status(t, e.do(t, a, "POST", "/api/ledgers/"+itoa(home.ID)+"/members", map[string]any{"email": "nobody@example.com", "role": "viewer"})); code != 400 {
		t.Errorf("sharing with nobody: %d", code)
	}
	ben := decodeJSON[store.LedgerMember](t, e.do(t, a, "POST", "/api/ledgers/"+itoa(home.ID)+"/members", map[string]any{"email": "ben@example.com", "role": "viewer"}))
	if got := decodeJSON[entriesPage](t, e.doIn(t, b, home.ID, "GET", "/api/entries?month=2026-11", nil)); len(got.Entries) != 1 {
		t.Errorf("the viewer reads: %+v", got)
	}
	if code := status(t, e.doIn(t, b, home.ID, "POST", "/api/entries", map[string]any{"name": "x", "amount": 1, "date": "2026-11-03"})); code != 403 {
		t.Errorf("the viewer writes: %d", code)
	}
	if code := status(t, e.do(t, b, "PUT", "/api/ledgers/"+itoa(home.ID), map[string]any{"name": "Mine now"})); code != 403 {
		t.Errorf("the viewer renames: %d", code)
	}
	// Made an editor, he can write; the agent's tools follow the same rule.
	e.do(t, a, "PUT", "/api/ledgers/"+itoa(home.ID)+"/members/"+itoa(ben.UserID), map[string]any{"role": "editor"}).Body.Close()
	if code := status(t, e.doIn(t, b, home.ID, "POST", "/api/entries", map[string]any{"name": "Lamp", "amount": 40, "date": "2026-11-03"})); code != 201 {
		t.Errorf("the editor writes: %d", code)
	}
	if ms := decodeJSON[[]store.LedgerMember](t, e.do(t, b, "GET", "/api/ledgers/"+itoa(home.ID)+"/members", nil)); len(ms) != 2 || ms[0].Email != "ann@example.com" || ms[1].Role != "editor" {
		t.Errorf("members: %+v", ms)
	}
	// He leaves; she can't delete her only other ledger... but can delete this one.
	if code := status(t, e.do(t, b, "DELETE", "/api/ledgers/"+itoa(home.ID)+"/members/"+itoa(ben.UserID), nil)); code != 200 {
		t.Errorf("leaving: %d", code)
	}
	if code := status(t, e.doIn(t, b, home.ID, "GET", "/api/entries", nil)); code != 404 {
		t.Errorf("after leaving: %d", code)
	}
	if code := status(t, e.do(t, a, "DELETE", "/api/ledgers/"+itoa(home.ID), nil)); code != 200 {
		t.Errorf("deleting: %d", code)
	}
	if code := status(t, e.do(t, a, "DELETE", "/api/ledgers/"+itoa(personal), nil)); code != 400 {
		t.Errorf("deleting the last ledger: %d", code)
	}
	// The default falls back to the ledger that's left.
	if got := decodeJSON[ledgers](t, e.do(t, a, "GET", "/api/ledgers", nil)); got.Current != personal {
		t.Errorf("current after deleting: %+v", got)
	}
}
