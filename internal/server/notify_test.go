package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/xavier-niu/paylist/internal/store"
)

// hook is a stand-in channel: it keeps what it's sent, and answers with status.
type hook struct {
	*httptest.Server
	mu      sync.Mutex
	got     []string
	headers []http.Header
	status  int
}

func newHook(t *testing.T) *hook {
	h := &hook{status: 200}
	h.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		h.mu.Lock()
		h.got = append(h.got, string(b))
		h.headers = append(h.headers, r.Header.Clone())
		code := h.status
		h.mu.Unlock()
		w.WriteHeader(code)
		if code != 200 {
			w.Write([]byte("nope"))
		}
	}))
	t.Cleanup(h.Close)
	return h
}

func (h *hook) bodies() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.got...)
}

func (h *hook) fail() {
	h.mu.Lock()
	h.status = 500
	h.mu.Unlock()
}

// addSpending fills a user's ledger: October, and November up to the 24th.
func addSpending(t *testing.T, st *store.Store, ledgerID int64) {
	t.Helper()
	var ins []store.EntryInput
	add := func(name, typ, date string, amount float64) {
		ins = append(ins, store.EntryInput{Name: &name, Type: &typ, Date: &date, Amount: &amount})
	}
	add("Rent", "Home", "2026-09-01", 3000)
	add("Rent", "Home", "2026-10-01", 3000)
	add("Groceries", "Food", "2026-10-12", 400)
	add("Train", "Transport", "2026-11-03", 120)
	add("Lunch", "Food", "2026-11-17", 35)
	add("Taxi", "Transport", "2026-11-21", 60.5)
	add("Coffee", "Food", "2026-11-24", 28)
	add("Book", "", "2026-11-24", 72)
	if _, err := st.CreateEntries(t.Context(), ledgerID, ins); err != nil {
		t.Fatal(err)
	}
}

func str(s string) *string { return &s }

func TestReports(t *testing.T) {
	e := newEnv(t)
	s := New(Options{Store: e.st})
	u, err := e.st.CreateUser(t.Context(), "r@example.com", "", testPassword, false)
	if err != nil {
		t.Fatal(err)
	}
	ls, _ := e.st.ListLedgers(t.Context(), u.ID)
	l := &ls[0]
	addSpending(t, e.st, l.ID)
	today := time.Date(2026, 11, 25, 9, 0, 0, 0, time.UTC) // a Wednesday

	// Yesterday against a usual day (the 28 before), and the month so far by type.
	daily, err := s.buildReport(t.Context(), u, l, store.ReportDaily, "en", true, today)
	if err != nil {
		t.Fatal(err)
	}
	f := daily.Figures
	if daily.From != "2026-11-24" || f.Total != 100 || f.Usual == nil || *f.Usual != 7.7 || *f.MonthToDate != 315.5 || daily.Suggestion != "" {
		t.Fatalf("daily: %+v %+v", daily, f)
	}
	// The month it's sent in (Sep and Oct before it, ¥3,200 on average): ¥315.50
	// so far against ¥3,200 by now; the rest as usual adds nothing, so it
	// ends at ¥315.50, with ¥2,884.50 left over 6 days to end at its usual.
	if p := f.Month; p == nil || *p != (monthPace{Month: "2026-11", SoFar: 315.5, UsualByNow: 3200, UsualMonth: 3200, Estimate: 315.5, LeftAtUsual: 2884.5, DaysLeft: 6}) {
		t.Errorf("the month: %+v", p)
	}
	want := "Yesterday (Tue, Nov 24)\n  Total ¥100.00\n  ↑ much higher than usual (¥7.70 a day)\n\n" +
		"Month so far\n  Total ¥315.50\n  ↓ much lower than usual (¥3,200.00 by now)\n\n" +
		"By type, month so far\n    ↑ Transport (new)\n    ↓ Home -100%"
	if daily.Title != "💰 Paylist Report · Personal" || daily.Text != want {
		t.Errorf("daily:\n%s\n%s\nwant\n%s", daily.Title, daily.Text, want)
	}

	// Last week against the 8 before it. Rent and the train came once in
	// those 8 weeks: neither is named.
	weekly, _ := s.buildReport(t.Context(), u, l, store.ReportWeekly, "zh", true, today)
	want = "上周（11月16日 – 11月22日）\n  合计 ¥95.50\n  ↓ 远低于往常（每周 ¥440.00）"
	if weekly.From != "2026-11-16" || weekly.To != "2026-11-22" || weekly.Text != want || weekly.Title != "💰 Paylist 报告 · Personal" {
		t.Errorf("weekly:\n%s\n%s\nwant\n%s", weekly.Title, weekly.Text, want)
	}

	if weekly.Figures.Month == nil || weekly.Figures.Month.LeftAtUsual != 2884.5 {
		t.Errorf("a weekly report's month: %+v", weekly.Figures.Month)
	}

	// Last month against the usual month.
	monthly, _ := s.buildReport(t.Context(), u, l, store.ReportMonthly, "en", true, today)
	if monthly.Figures.Month != nil {
		t.Errorf("a monthly report has the month it's sent in: %+v", monthly.Figures.Month)
	}
	want = "October 2026\n  Total ¥3,400.00\n  ≈ about usual (¥3,000.00 a month)\n\nBy type\n    ↑ Food (new)"
	if monthly.From != "2026-10-01" || monthly.Text != want {
		t.Errorf("monthly:\n%s\nwant\n%s", monthly.Text, want)
	}

	// Groceries every Monday, more of them last week: Food is named.
	var market []store.EntryInput
	for i := 0; i < 9; i++ {
		amount := 50.0
		if i == 0 {
			amount = 150
		}
		market = append(market, store.EntryInput{Name: str("Market"), Type: str("Food"), Amount: &amount,
			Date: str(time.Date(2026, 11, 16-7*i, 0, 0, 0, 0, time.UTC).Format("2006-01-02"))})
	}
	if _, err := e.st.CreateEntries(t.Context(), l.ID, market); err != nil {
		t.Fatal(err)
	}
	weekly, _ = s.buildReport(t.Context(), u, l, store.ReportWeekly, "en", true, today)
	if want = "Last week (Nov 16 – 22)\n  Total ¥245.50\n  ↓ lower than usual (¥490.00 a week)\n\nBy type\n    ↑ Food +85%"; weekly.Text != want {
		t.Errorf("weekly with groceries:\n%s\nwant\n%s", weekly.Text, want)
	}

	// A ledger with nothing at all.
	trip, _ := e.st.CreateLedger(t.Context(), u.ID, "Trip", "USD")
	if r, _ := s.buildReport(t.Context(), u, trip, store.ReportDaily, "en", true, today); r.Text != "Yesterday (Tue, Nov 24)\n  Total $0.00" || r.Title != "💰 Paylist Report · Trip" {
		t.Errorf("an empty ledger: %q %q", r.Title, r.Text)
	}
}

// With an AI model chosen, it adds a suggestion after the note, having
// looked into the ledger with the tools that only read it; when it fails,
// or AI is off, the note goes alone.
func TestReportByModel(t *testing.T) {
	e := newEnv(t)
	s := New(Options{Store: e.st, PublicURL: "https://pay.example.com/"})
	u, _ := e.st.CreateUser(t.Context(), "m@example.com", "", testPassword, false)
	ls, _ := e.st.ListLedgers(t.Context(), u.ID)
	addSpending(t, e.st, ls[0].ID)
	today := time.Date(2026, 11, 25, 9, 0, 0, 0, time.UTC)

	var mu sync.Mutex
	var asked []string
	fail := false
	send := func(w http.ResponseWriter, delta map[string]any, finish string) {
		w.Header().Set("Content-Type", "text/event-stream")
		c, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"index": 0, "delta": delta}}})
		io.WriteString(w, "data: "+string(c)+"\n\n")
		c, _ = json.Marshal(map[string]any{"choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": finish}}})
		io.WriteString(w, "data: "+string(c)+"\n\ndata: [DONE]\n\n")
	}
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		asked = append(asked, string(b))
		failing := fail
		mu.Unlock()
		switch {
		case failing:
			http.Error(w, "down", 500)
		case !strings.Contains(string(b), `"role":"tool"`):
			// First, a look at November's entries.
			send(w, map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": "c1", "type": "function",
				"function": map[string]any{"name": "list_entries", "arguments": `{"from":"2026-11-01","to":"2026-11-24"}`}}}}, "tool_calls")
		default:
			send(w, map[string]any{"content": "交通是本月新出现的支出，主要是 Train 和 Taxi。"}, "stop")
		}
	}))
	t.Cleanup(model.Close)
	key := "k"
	if _, err := e.st.SaveProvider(t.Context(), u.ID, "custom", store.ProviderConf{Kind: "openai", BaseURL: model.URL, Model: "m"}, &key); err != nil {
		t.Fatal(err)
	}

	rep, err := s.buildReport(t.Context(), u, &ls[0], store.ReportDaily, "zh", true, today)
	want := "交通是本月新出现的支出，主要是 Train 和 Taxi。"
	if err != nil || rep.Suggestion != want || !strings.HasPrefix(rep.Text, "昨天（11月24日 周二）\n  合计 ¥100.00") ||
		!strings.HasSuffix(rep.Text, "\n\n✨ AI 建议\n"+want+"\n\n继续提问：https://pay.example.com/#/agent") {
		t.Fatalf("with a suggestion: %v %+v", err, rep)
	}
	mu.Lock()
	if len(asked) != 2 {
		t.Fatalf("asked the model %d times, want 2", len(asked))
	}
	for _, fact := range []string{"Simplified Chinese", "much higher than usual", "¥7.70", "Personal", "types_lower_than_usual_this_month_so_far", "spent_by_type", "50 to 100 words", "the 28 days before", "for today and the rest of the month", `left_to_end_at_usual\":\"¥2,884.50`, `days_left_including_today\":6`, `"name":"list_entries"`, `"name":"get_stats"`} {
		if !strings.Contains(asked[0], fact) {
			t.Errorf("the model wasn't told %q", fact)
		}
	}
	// Only the tools that read.
	for _, tool := range []string{"add_entries", "update_entry", "delete_entries", "refund", "manage_option", "manage_ledger", "ledger_people", "channels", "ledger_reports"} {
		if strings.Contains(asked[0], `"name":"`+tool+`"`) {
			t.Errorf("the model was offered %s", tool)
		}
	}
	// What it looked up came back to it.
	if !strings.Contains(asked[1], "Taxi") || !strings.Contains(asked[1], "Train") {
		t.Errorf("the entries didn't reach the model: %s", asked[1])
	}
	fail = true
	mu.Unlock()
	if rep, _ = s.buildReport(t.Context(), u, &ls[0], store.ReportDaily, "zh", false, today); rep.Suggestion != "" || strings.Contains(rep.Text, "✨") || strings.Contains(rep.Text, "继续提问") {
		t.Errorf("with AI off: %+v", rep)
	}
	if !s.aiReady(t.Context(), u.ID) {
		t.Error("a model, not ready")
	}
	if rep, _ = s.buildReport(t.Context(), u, &ls[0], store.ReportDaily, "zh", true, today); rep.Suggestion != "" || !strings.HasPrefix(rep.Text, "昨天（11月24日 周二）\n  合计 ¥100.00") || strings.Contains(rep.Text, "✨") ||
		!strings.HasSuffix(rep.Text, "\n\n继续提问：https://pay.example.com/#/agent") {
		t.Errorf("the model failing (the link stays): %+v", rep)
	}
}

// The link at the end of a report goes to where people open the app: the
// address it was last opened at, or PAYLIST_PUBLIC_URL when that's set.
func TestLinkBase(t *testing.T) {
	e := newEnv(t)
	if b := New(Options{Store: e.st}).linkBase(t.Context()); b != "" {
		t.Errorf("before the app was opened: %q", b)
	}
	c := e.signIn(t, "link@example.com")
	e.do(t, c, "GET", "/api/me", nil).Body.Close()
	if v, _ := e.st.Setting(t.Context(), seenURLKey); v != e.srv.URL {
		t.Errorf("remembered %q, want %q", v, e.srv.URL)
	}
	if b := New(Options{Store: e.st}).linkBase(t.Context()); b != e.srv.URL {
		t.Errorf("after a restart: %q", b)
	}
	if b := New(Options{Store: e.st, PublicURL: "https://pay.example.com/"}).linkBase(t.Context()); b != "https://pay.example.com" {
		t.Errorf("with PAYLIST_PUBLIC_URL: %q", b)
	}
}

// Each kind of report has instructions of its own, with the rules all share.
func TestSuggestPrompts(t *testing.T) {
	for period, want := range map[string]string{
		store.ReportDaily:   "about 50 to 100 words",
		store.ReportWeekly:  "about 80 to 150 words",
		store.ReportMonthly: "about 100 to 200 words",
	} {
		p := suggestPrompt(period, "zh")
		for _, part := range []string{want, "# The report", "# What to write", "# How to find out", "# Rules", "Write in Simplified Chinese.", "spent_by_type"} {
			if !strings.Contains(p, part) {
				t.Errorf("%s: no %q", period, part)
			}
		}
		// The days and weeks look at where the month is heading; a month is done.
		if pace := strings.Contains(suggestPrompt(period, "en"), "this_month"); pace == (period == store.ReportMonthly) {
			t.Errorf("%s: about the month ahead: %v", period, pace)
		}
	}
}

func TestDueReports(t *testing.T) {
	sh, _ := time.LoadLocation("Asia/Shanghai")
	rs := store.ReportSchedule{Reports: store.Reports{Daily: true, Weekly: true, Monthly: true, Time: "08:30", TZ: "Asia/Shanghai"}, Sent: map[string]string{}}
	at := func(y int, m time.Month, d, h, min int) time.Time { return time.Date(y, m, d, h, min, 0, 0, sh) }
	cases := []struct {
		now  time.Time
		want string
	}{
		{at(2026, 11, 25, 8, 29), ""},             // not yet
		{at(2026, 11, 25, 8, 30), "daily"},        // a Wednesday
		{at(2026, 11, 30, 23, 0), "daily weekly"}, // a Monday, later on
		{at(2026, 12, 1, 9, 0), "daily monthly"},  // the 1st
		{at(2026, 11, 25, 0, 30).UTC(), ""},       // the zone counts
		{at(2026, 11, 25, 8, 30).UTC(), "daily"},  // the same moment, as UTC
	}
	for _, c := range cases {
		due, _ := dueReports(rs, c.now)
		if got := strings.Join(due, " "); got != c.want {
			t.Errorf("%v: %q, want %q", c.now, got, c.want)
		}
	}
	rs.Sent["daily"] = "2026-11-25"
	if due, _ := dueReports(rs, at(2026, 11, 25, 10, 0)); len(due) != 0 {
		t.Errorf("sent twice: %v", due)
	}
}

func TestChannelsAPI(t *testing.T) {
	e := newEnv(t)
	c := e.signIn(t, "ch@example.com")
	h := newHook(t)

	for _, bad := range []map[string]any{
		{"kind": "webhook", "url": "ftp://x"},
		{"kind": "pigeon", "url": h.URL},
		{"kind": "webhook", "url": h.URL, "headers": []map[string]string{{"name": "Authorization", "value": ""}}},
		{"kind": "webhook", "url": h.URL, "headers": []map[string]string{{"name": "Host", "value": "x"}}},
	} {
		if code := status(t, e.do(t, c, "POST", "/api/channels", bad)); code != 400 {
			t.Errorf("%v: %d", bad, code)
		}
	}
	// A webhook with a token: its value is sent, and never shown.
	ch := decodeJSON[store.Channel](t, e.do(t, c, "POST", "/api/channels", map[string]any{
		"kind": "webhook", "name": "Home server", "url": h.URL,
		"headers": []map[string]string{{"name": "Authorization", "value": "Bearer s3cret"}, {"name": "X-Team", "value": "home"}},
	}))
	if ch.ID == 0 || ch.Name != "Home server" || strings.Join(ch.HeaderNames, ",") != "Authorization,X-Team" {
		t.Fatalf("add: %+v", ch)
	}
	raw := e.do(t, c, "GET", "/api/channels", nil)
	b, _ := io.ReadAll(raw.Body)
	raw.Body.Close()
	if strings.Contains(string(b), "s3cret") {
		t.Errorf("the token is shown: %s", b)
	}
	slack := decodeJSON[store.Channel](t, e.do(t, c, "POST", "/api/channels", map[string]any{"kind": "slack", "url": h.URL + "/slack",
		"headers": []map[string]string{{"name": "X-Dropped", "value": "1"}}}))
	if slack.Name != "127.0.0.1" || len(slack.HeaderNames) != 0 {
		t.Errorf("a Slack channel, named for its host, sends no headers: %+v", slack)
	}

	// The test message: Paylist's JSON, with the headers.
	ch = decodeJSON[store.Channel](t, e.do(t, c, "POST", "/api/channels/"+itoa(ch.ID)+"/test", map[string]string{"language": "zh"}))
	h.mu.Lock()
	got, hd := h.got[0], h.headers[0]
	h.mu.Unlock()
	if !strings.Contains(got, `"event":"test"`) || !strings.Contains(got, `"text":"Paylist 测试\n\nPaylist 的通知会发到这里。"`) || hd.Get("Authorization") != "Bearer s3cret" || hd.Get("X-Team") != "home" || ch.LastAt == "" {
		t.Fatalf("test: %+v %q %v", ch, got, hd)
	}
	// Changed: a header without a value keeps it; one left out goes.
	ch = decodeJSON[store.Channel](t, e.do(t, c, "PUT", "/api/channels/"+itoa(ch.ID), map[string]any{"name": "Server", "headers": []map[string]string{{"name": "authorization", "value": ""}}}))
	e.do(t, c, "POST", "/api/channels/"+itoa(ch.ID)+"/test", map[string]string{}).Body.Close()
	h.mu.Lock()
	hd = h.headers[1]
	h.mu.Unlock()
	if ch.Name != "Server" || hd.Get("Authorization") != "Bearer s3cret" || hd.Get("X-Team") != "" {
		t.Errorf("update: %+v %v", ch, hd)
	}

	// One that fails says why, and keeps it.
	h.fail()
	resp := e.do(t, c, "POST", "/api/channels/"+itoa(slack.ID)+"/test", map[string]string{})
	msg := decodeJSON[map[string]string](t, resp)["error"]
	if resp.StatusCode != 502 || !strings.Contains(msg, "500") || !strings.Contains(msg, "nope") {
		t.Errorf("failing: %d %q", resp.StatusCode, msg)
	}
	chs := decodeJSON[[]store.Channel](t, e.do(t, c, "GET", "/api/channels", nil))
	if len(chs) != 2 || !strings.Contains(chs[1].LastError, "500") {
		t.Errorf("list: %+v", chs)
	}

	// Someone else's can't be changed, tested or removed.
	other := e.signIn(t, "o@example.com")
	for _, r := range [][2]string{{"PUT", ""}, {"POST", "/test"}, {"DELETE", ""}} {
		if code := status(t, e.do(t, other, r[0], "/api/channels/"+itoa(ch.ID)+r[1], map[string]string{})); code != 404 {
			t.Errorf("someone else's, %s: %d", r[0], code)
		}
	}
	if code := status(t, e.do(t, c, "DELETE", "/api/channels/"+itoa(ch.ID), nil)); code != 200 {
		t.Errorf("delete: %d", code)
	}
}

func TestReportsAPI(t *testing.T) {
	e := newEnv(t)
	c := e.signIn(t, "rep@example.com")
	viewer := e.signIn(t, "view@example.com")
	h, h2 := newHook(t), newHook(t)
	lid := decodeJSON[struct {
		Current int64 `json:"current"`
	}](t, e.do(t, c, "GET", "/api/ledgers", nil)).Current
	a := decodeJSON[store.Channel](t, e.do(t, c, "POST", "/api/channels", map[string]any{"kind": "feishu", "url": h.URL}))
	decodeJSON[store.Channel](t, e.do(t, c, "POST", "/api/channels", map[string]any{"kind": "slack", "url": h2.URL}))
	theirs := decodeJSON[store.Channel](t, e.do(t, viewer, "POST", "/api/channels", map[string]any{"kind": "slack", "url": h2.URL}))

	// At first: nothing on, AI on, but no model to write (the demo one can't).
	if r := decodeJSON[store.Reports](t, e.doIn(t, c, lid, "GET", "/api/reports", nil)); r.Daily || r.Time != "09:00" || len(r.Channels) != 0 || !r.AI || r.AIReady {
		t.Errorf("at first: %+v", r)
	}
	// Without a model, AI can be turned off, but not on again.
	if r := decodeJSON[store.Reports](t, e.doIn(t, c, lid, "PUT", "/api/reports", map[string]any{"ai": false})); r.AI {
		t.Errorf("AI off: %+v", r)
	}
	if code := status(t, e.doIn(t, c, lid, "PUT", "/api/reports", map[string]any{"ai": true})); code != 400 {
		t.Errorf("AI on without a model: %d", code)
	}
	// Sending needs a channel.
	if code := status(t, e.doIn(t, c, lid, "POST", "/api/reports/send", map[string]string{"period": "daily"})); code != 502 {
		t.Errorf("no channel: %d", code)
	}
	for _, bad := range []map[string]any{{"time": "25:00"}, {"timezone": "Mars/Base"}, {"channels": []int64{theirs.ID}}} {
		if code := status(t, e.doIn(t, c, lid, "PUT", "/api/reports", bad)); code != 400 {
			t.Errorf("%v: %d", bad, code)
		}
	}
	r := decodeJSON[store.Reports](t, e.doIn(t, c, lid, "PUT", "/api/reports", map[string]any{
		"daily": true, "monthly": true, "time": "07:45", "timezone": "Asia/Shanghai", "language": "en", "channels": []int64{a.ID},
	}))
	if !r.Daily || r.Weekly || !r.Monthly || r.Time != "07:45" || r.TZ != "Asia/Shanghai" || len(r.Channels) != 1 || r.Channels[0] != a.ID {
		t.Errorf("saved: %+v", r)
	}

	// Send now goes to the channels chosen only.
	sent := decodeJSON[reportSent](t, e.doIn(t, c, lid, "POST", "/api/reports/send", map[string]string{"period": "weekly"}))
	if len(sent.Channels) != 1 || sent.Report.Period != "weekly" || len(h.bodies()) != 1 || len(h2.bodies()) != 0 || !strings.Contains(h.bodies()[0], `"msg_type":"text"`) {
		t.Errorf("send now: %+v %q %q", sent, h.bodies(), h2.bodies())
	}

	// A viewer of a shared ledger has reports of their own.
	e.doIn(t, c, lid, "POST", "/api/ledgers/"+itoa(lid)+"/members", map[string]string{"email": "view@example.com", "role": "viewer"}).Body.Close()
	if r := decodeJSON[store.Reports](t, e.doIn(t, viewer, lid, "PUT", "/api/reports", map[string]any{"weekly": true, "channels": []int64{theirs.ID}})); !r.Weekly || r.Daily {
		t.Errorf("the viewer's: %+v", r)
	}
	if r := decodeJSON[store.Reports](t, e.doIn(t, c, lid, "GET", "/api/reports", nil)); !r.Daily || r.Weekly {
		t.Errorf("the owner's, after: %+v", r)
	}
}

// Reports go out when due, once, to the channels chosen for them; turning
// them on after the time doesn't send them at once; leaving the ledger
// stops them.
func TestSendDueReports(t *testing.T) {
	e := newEnv(t)
	s := New(Options{Store: e.st})
	u, _ := e.st.CreateUser(t.Context(), "due@example.com", "", testPassword, false)
	ls, _ := e.st.ListLedgers(t.Context(), u.ID)
	lid := ls[0].ID
	addSpending(t, e.st, lid)
	a, b, unused := newHook(t), newHook(t), newHook(t)
	ca, _ := e.st.AddChannel(t.Context(), u.ID, store.ChannelInput{Kind: str("webhook"), URL: &a.URL})
	cb, _ := e.st.AddChannel(t.Context(), u.ID, store.ChannelInput{Kind: str("slack"), URL: &b.URL})
	e.st.AddChannel(t.Context(), u.ID, store.ChannelInput{Kind: str("slack"), URL: &unused.URL})

	monday := time.Date(2026, 11, 30, 9, 0, 0, 0, time.UTC)
	set := store.Reports{Daily: true, Weekly: true, Time: "09:00", TZ: "UTC", Language: "en", Channels: []int64{ca.ID, cb.ID}}
	if _, err := s.saveReports(t.Context(), lid, u.ID, set, monday.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	s.sendDueReports(t.Context(), monday)
	s.sendDueReports(t.Context(), monday.Add(time.Minute))
	got := a.bodies()
	if len(got) != 2 || len(b.bodies()) != 2 || len(unused.bodies()) != 0 {
		t.Fatalf("sent %d, %d and %d, want 2, 2 and 0", len(got), len(b.bodies()), len(unused.bodies()))
	}
	var first struct {
		Event   string        `json:"event"`
		Period  string        `json:"period"`
		Ledger  string        `json:"ledger"`
		Text    string        `json:"text"`
		Figures *reportLedger `json:"figures"`
	}
	json.Unmarshal([]byte(got[0]), &first)
	if first.Event != "report" || first.Period != "daily" || first.Ledger != "Personal" || first.Figures == nil ||
		!strings.HasPrefix(first.Text, "💰 Paylist Report · Personal\n\nYesterday (Sun, Nov 29)\n  Total") {
		t.Errorf("json body: %s", got[0])
	}
	if !strings.Contains(b.bodies()[1], `"text":"💰 Paylist Report · Personal\n\nLast week (Nov 23 – 29)\n  Total ¥100.00\n  ↓ much lower than usual`) {
		t.Errorf("slack body: %s", b.bodies()[1])
	}

	// Turned on at 10:00, for 09:00: nothing until tomorrow.
	set.Monthly = true
	tuesday := monday.AddDate(0, 0, 1).Add(time.Hour)
	s.saveReports(t.Context(), lid, u.ID, set, tuesday)
	s.sendDueReports(t.Context(), tuesday)
	if n := len(a.bodies()); n != 2 {
		t.Errorf("sent on saving after the time: %d", n)
	}
	s.sendDueReports(t.Context(), monday.AddDate(0, 0, 2))
	if n := len(a.bodies()); n != 3 {
		t.Errorf("the next day: %d", n)
	}

	// Someone who leaves a shared ledger gets no more of its reports.
	owner, _ := e.st.CreateUser(t.Context(), "own@example.com", "", testPassword, false)
	shared, _ := e.st.CreateLedger(t.Context(), owner.ID, "Home", "CNY")
	e.st.AddLedgerMember(t.Context(), shared.ID, "due@example.com", "viewer")
	s.saveReports(t.Context(), shared.ID, u.ID, store.Reports{Daily: true, Time: "09:00", TZ: "UTC", Channels: []int64{ca.ID}}, monday)
	e.st.RemoveLedgerMember(t.Context(), shared.ID, u.ID)
	s.sendDueReports(t.Context(), monday.AddDate(0, 0, 3))
	for _, body := range a.bodies() {
		if strings.Contains(body, `"ledger":"Home"`) {
			t.Error("sent a report of a ledger they left")
		}
	}
}

func TestMCPNotifications(t *testing.T) {
	e := newEnv(t)
	c := e.signIn(t, "mn@example.com")
	h := newHook(t)
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

	out, msg := callTool[mcpChannelsOut](t, s, "channels", map[string]any{"action": "add", "kind": "ntfy", "name": "Phone", "url": h.URL,
		"headers": []map[string]string{{"name": "Authorization", "value": "Bearer tk_1"}}})
	if msg != "" || len(out.Channels) != 1 || out.Channels[0].HeaderNames[0] != "Authorization" {
		t.Fatalf("add: %s %+v", msg, out)
	}
	id := out.Channels[0].ID
	if out, msg = callTool[mcpChannelsOut](t, s, "channels", map[string]any{"action": "test", "id": id}); msg != "" || out.Channels[0].LastAt == "" {
		t.Errorf("test: %s %+v", msg, out)
	}
	h.mu.Lock()
	if len(h.got) != 1 || !strings.Contains(h.got[0], "Paylist") || h.headers[0].Get("Authorization") != "Bearer tk_1" || h.headers[0].Get("Title") == "" {
		t.Errorf("ntfy: %q %v", h.got, h.headers)
	}
	h.mu.Unlock()
	if out, msg = callTool[mcpChannelsOut](t, s, "channels", map[string]any{"action": "update", "id": id, "name": "iPad"}); msg != "" || out.Channels[0].Name != "iPad" || len(out.Channels[0].HeaderNames) != 1 {
		t.Errorf("update: %s %+v", msg, out)
	}

	r, msg := callTool[mcpReportsOut](t, s, "ledger_reports", map[string]any{"action": "set", "weekly": true, "time": "20:00", "timezone": "Europe/Paris", "channels": []int64{id}})
	if msg != "" || r.Ledger != "Personal" || !r.Reports.Weekly || r.Reports.Daily || r.Reports.Time != "20:00" || r.Reports.TZ != "Europe/Paris" || len(r.Reports.Channels) != 1 {
		t.Errorf("set: %s %+v", msg, r)
	}
	if r, msg = callTool[mcpReportsOut](t, s, "ledger_reports", map[string]any{"action": "set", "daily": true}); msg != "" || !r.Reports.Daily || !r.Reports.Weekly || len(r.Reports.Channels) != 1 {
		t.Errorf("set keeps what it isn't given: %s %+v", msg, r.Reports)
	}
	if r, msg = callTool[mcpReportsOut](t, s, "ledger_reports", map[string]any{"action": "preview", "period": "weekly"}); msg != "" || r.Report == nil || r.Report.Period != "weekly" || len(h.bodies()) != 1 {
		t.Errorf("preview: %s %+v", msg, r.Report)
	}
	if r, msg = callTool[mcpReportsOut](t, s, "ledger_reports", map[string]any{"action": "send", "period": "monthly"}); msg != "" || len(r.Sent) != 1 || len(h.bodies()) != 2 {
		t.Errorf("send: %s %+v", msg, r)
	}
	if _, msg = callTool[mcpReportsOut](t, s, "ledger_reports", map[string]any{"action": "set", "ai": false}); msg != "" {
		t.Errorf("AI off: %s", msg)
	}
	if _, msg = callTool[mcpReportsOut](t, s, "ledger_reports", map[string]any{"action": "set", "ai": true}); !strings.Contains(msg, "AI model") {
		t.Errorf("AI on without a model: %q", msg)
	}
	if _, msg = callTool[mcpReportsOut](t, s, "ledger_reports", map[string]any{"action": "set", "time": "noon"}); !strings.Contains(msg, "HH:MM") {
		t.Errorf("a bad time: %q", msg)
	}
	if out, msg = callTool[mcpChannelsOut](t, s, "channels", map[string]any{"action": "remove", "id": id}); msg != "" || len(out.Channels) != 0 {
		t.Errorf("remove: %s %+v", msg, out)
	}
	if r, _ = callTool[mcpReportsOut](t, s, "ledger_reports", map[string]any{}); len(r.Reports.Channels) != 0 {
		t.Errorf("a removed channel stays chosen: %+v", r.Reports)
	}
	if _, msg = callTool[mcpChannelsOut](t, s, "channels", map[string]any{"action": "remove", "id": id}); !strings.Contains(msg, "no channel") {
		t.Errorf("remove again: %q", msg)
	}
}
