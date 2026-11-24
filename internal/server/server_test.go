package server

import (
	"bufio"
	"bytes"
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
	if m := decodeJSON[[]store.Entry](t, e.do(t, c, "GET", "/api/refund/matches?name=lun&amount=30&today=2026-11-20", nil)); len(m) != 1 || m[0].ID != created.ID {
		t.Errorf("refund matches: %+v", m)
	}
	if m := decodeJSON[[]store.Entry](t, e.do(t, c, "GET", "/api/refund/matches?name=lun&amount=31&today=2026-11-20", nil)); len(m) != 0 {
		t.Errorf("refund matches, wrong amount: %+v", m)
	}
	if code := status(t, e.do(t, c, "GET", "/api/refund/matches?amount=lots", nil)); code != 400 {
		t.Errorf("refund matches, bad amount: %d", code)
	}
	gift := decodeJSON[store.RefundResult](t, e.do(t, c, "POST", "/api/refund", map[string]any{"name": "Gift card", "amount": 50, "today": "2026-11-20"}))
	if gift.Added == nil || gift.Added.Amount != -50 || gift.Added.Date != "2026-11-20" {
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
	if err != nil || len(tools.Tools) != 7 {
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
	del, _ := callTool[mcpDeleteOut](t, s, "delete_entries", map[string]any{"ids": []int64{added.Added[0].ID, added.Added[1].ID}})
	if del.Deleted != 2 {
		t.Errorf("deleted %d", del.Deleted)
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
