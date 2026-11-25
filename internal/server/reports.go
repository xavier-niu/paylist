package server

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/xavier-niu/paylist/internal/agent"
	"github.com/xavier-niu/paylist/internal/llm"
	"github.com/xavier-niu/paylist/internal/store"
)

// Reports: a brief note on what a ledger spent over the day, week or month
// before, against what it usually does, and the types that stood out.
// Paylist writes the note; the user's AI model, when they have one and want
// it, adds a suggestion of what to do about it, after looking into the
// ledger with the MCP tools that read it.

const (
	usualDays  = 28              // a daily report's usual day is the average of the days before, up to this many
	usualWeeks = 8               // and a weekly report's usual week, of the weeks before
	minDays    = 7               // fewer days than this before, and there's no usual day
	maxMovers  = 3               // types named going each way
	reportTime = 3 * time.Minute // for the AI suggestion, tools and all
)

// report is one report on one ledger.
type report struct {
	Period string `json:"period"` // daily, weekly, monthly; test for a test message
	Ledger string `json:"ledger,omitempty"`
	From   string `json:"from,omitempty"`
	To     string `json:"to,omitempty"`
	Title  string `json:"title"`
	Text   string `json:"text"` // the note, and the suggestion after it (a webhook's JSON has the title first)
	// Suggestion is the user's AI model's suggestion ("" without one).
	Suggestion string        `json:"suggestion,omitempty"`
	Figures    *reportLedger `json:"figures,omitempty"`
}

type reportLedger struct {
	Currency string             `json:"currency"`
	Total    float64            `json:"total"`
	Count    int                `json:"count"`
	ByType   []store.GroupTotal `json:"by_type"`
	// What the period usually comes to: a day (the days before, up to 28), a
	// week (the 8 before) or a month (the 6 before); none without the history.
	Usual *float64 `json:"usual,omitempty"`
	// Daily: the month up to the day, and what it usually is by then.
	MonthToDate      *float64 `json:"month_to_date,omitempty"`
	UsualMonthToDate *float64 `json:"usual_month_to_date,omitempty"`
	// The types well above and below their usual (for a daily report, over
	// the month so far), furthest first.
	Higher []typeChange `json:"higher"`
	Lower  []typeChange `json:"lower"`
}

type typeChange struct {
	Name  string  `json:"name"` // "" for entries without a type
	Total float64 `json:"total"`
	Usual float64 `json:"usual"` // 0: new this time
}

// reportRange is the days a report sent on today covers.
func reportRange(period string, today time.Time) (from, to time.Time) {
	day := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.UTC)
	switch period {
	case store.ReportWeekly:
		monday := day.AddDate(0, 0, -((int(day.Weekday()) + 6) % 7))
		return monday.AddDate(0, 0, -7), monday.AddDate(0, 0, -1)
	case store.ReportMonthly:
		first := day.AddDate(0, 0, 1-day.Day())
		return first.AddDate(0, -1, 0), first.AddDate(0, 0, -1)
	default:
		to = day.AddDate(0, 0, -1)
		return to, to
	}
}

// reportLanguage is the language a user's reports are written in.
func reportLanguage(u *store.User, r store.Reports) string {
	if r.Language != "" {
		return r.Language
	}
	if u.Prefs.Language == "zh" {
		return "zh"
	}
	return "en"
}

func day(t time.Time) string { return t.Format("2006-01-02") }

// buildReport makes a ledger's report for period, as sent on the day today is.
// With ai, the user's AI model adds a suggestion, when they have one, and
// the report ends with a link to ask the agent more.
func (s *Server) buildReport(ctx context.Context, u *store.User, l *store.Ledger, period, lang string, ai bool, today time.Time) (*report, error) {
	if period != store.ReportDaily && period != store.ReportWeekly && period != store.ReportMonthly {
		return nil, fmt.Errorf("the report is daily, weekly or monthly, not %q", period)
	}
	from, to := reportRange(period, today)
	figures, err := s.reportLedger(ctx, l, period, from, to, today)
	if err != nil {
		return nil, err
	}
	rep := &report{Period: period, Ledger: l.Name, From: day(from), To: day(to), Figures: figures}
	rep.Title = reportTitle(l.Name, lang)
	rep.Text = writeReport(rep, lang)
	if ai && s.aiReady(ctx, u.ID) {
		if rep.Suggestion = s.suggest(ctx, u, l, rep, lang); rep.Suggestion != "" {
			rep.Text += "\n\n" + tr(lang, "✨ AI suggestion", "✨ AI 建议") + "\n" + rep.Suggestion
		}
		// Where to ask the agent about it.
		if base := s.linkBase(ctx); base != "" {
			rep.Text += "\n\n" + tr(lang, "Ask more: ", "继续提问：") + base + "/#/agent"
		}
	}
	return rep, nil
}

func (s *Server) reportLedger(ctx context.Context, l *store.Ledger, period string, from, to, today time.Time) (*reportLedger, error) {
	st := s.opts.Store
	sum, err := st.Summarize(ctx, l.ID, day(from), day(to), false)
	if err != nil {
		return nil, err
	}
	rl := &reportLedger{Currency: l.Currency, Total: sum.Total, Count: sum.Count, ByType: sum.ByType}
	first, err := st.FirstEntryDate(ctx, l.ID)
	if err != nil || first == "" {
		return rl, err
	}
	firstDay, _ := store.ParseDate(first)

	switch period {
	case store.ReportDaily:
		// A usual day: the days before it, since the first entry.
		start := to.AddDate(0, 0, -usualDays)
		if firstDay.After(start) {
			start = firstDay
		}
		if n := int(to.Sub(start).Hours() / 24); n >= minDays {
			before, err := st.Summarize(ctx, l.ID, day(start), day(to.AddDate(0, 0, -1)), false)
			if err != nil {
				return nil, err
			}
			rl.Usual = ptr(math.Round(before.Total/float64(n)*100) / 100)
		}
		// The month so far, against the usual by then, overall and by type.
		stats, err := st.GetStats(ctx, l.ID, to.Format("2006-01"), 1, to)
		if err != nil {
			return nil, err
		}
		rl.MonthToDate = &stats.ToDate
		if stats.AvgMonths > 0 {
			rl.UsualMonthToDate = &stats.AvgToDate
			rl.Higher, rl.Lower = movers(stats.ByTypeToDate, stats.TypeAvg, stats.AvgToDate)
		}
	case store.ReportWeekly:
		// A usual week: the whole weeks before, since the first entry.
		n := 0
		for n < usualWeeks && !from.AddDate(0, 0, -7*(n+1)).Before(firstDay) {
			n++
		}
		if n > 0 {
			// Each week's by type: a type in fewer than half of them (rent,
			// paid once a month) comes and goes, so isn't named for it.
			var total float64
			byType := map[string]*store.GroupTotal{}
			weeks := map[string]int{}
			var order []string
			for i := 1; i <= n; i++ {
				w := from.AddDate(0, 0, -7*i)
				ws, err := st.Summarize(ctx, l.ID, day(w), day(w.AddDate(0, 0, 6)), false)
				if err != nil {
					return nil, err
				}
				total += ws.Total
				for _, g := range ws.ByType {
					if byType[g.Name] == nil {
						byType[g.Name] = &store.GroupTotal{Name: g.Name, Color: g.Color}
						order = append(order, g.Name)
					}
					byType[g.Name].Total += g.Total
					weeks[g.Name]++
				}
			}
			usual := math.Round(total/float64(n)*100) / 100
			rl.Usual = &usual
			var avg, now []store.GroupTotal
			for _, name := range order {
				if weeks[name]*2 >= n {
					g := *byType[name]
					g.Total = math.Round(g.Total/float64(n)*100) / 100
					avg = append(avg, g)
				}
			}
			for _, g := range sum.ByType {
				if byType[g.Name] == nil || weeks[g.Name]*2 >= n {
					now = append(now, g)
				}
			}
			rl.Higher, rl.Lower = movers(now, avg, usual)
		}
	case store.ReportMonthly:
		stats, err := st.GetStats(ctx, l.ID, from.Format("2006-01"), 1, today)
		if err != nil {
			return nil, err
		}
		if stats.AvgMonths > 0 {
			rl.Usual = &stats.AvgToDate
			rl.Higher, rl.Lower = movers(sum.ByType, stats.TypeAvg, stats.AvgToDate)
		}
	}
	return rl, nil
}

func ptr[T any](v T) *T { return &v }

// movers are the types well above their usual (by a quarter, and by 5% of the
// usual total) and those well below, the furthest first.
func movers(now, usual []store.GroupTotal, usualTotal float64) (higher, lower []typeChange) {
	higher, lower = []typeChange{}, []typeChange{}
	least := math.Max(1, usualTotal*0.05)
	was := map[string]float64{}
	for _, g := range usual {
		was[g.Name] = g.Total
	}
	seen := map[string]bool{}
	for _, g := range now {
		seen[g.Name] = true
		u := was[g.Name]
		switch {
		case g.Total-u >= least && (u <= 0 || g.Total >= u*1.25):
			higher = append(higher, typeChange{g.Name, g.Total, math.Max(u, 0)})
		case u-g.Total >= least && g.Total <= u*0.75:
			lower = append(lower, typeChange{g.Name, g.Total, u})
		}
	}
	for _, g := range usual {
		if !seen[g.Name] && g.Total >= least {
			lower = append(lower, typeChange{g.Name, 0, g.Total})
		}
	}
	sort.SliceStable(higher, func(i, j int) bool { return higher[i].Total-higher[i].Usual > higher[j].Total-higher[j].Usual })
	sort.SliceStable(lower, func(i, j int) bool { return lower[i].Usual-lower[i].Total > lower[j].Usual-lower[j].Total })
	return higher[:min(len(higher), maxMovers)], lower[:min(len(lower), maxMovers)]
}

// testReport is the message a test sends.
func testReport(lang string) *report {
	return &report{
		Period: "test",
		Title:  tr(lang, "Paylist test", "Paylist 测试"),
		Text:   tr(lang, "Notifications from Paylist will arrive here.", "Paylist 的通知会发到这里。"),
	}
}

func tr(lang, en, zh string) string {
	if lang == "zh" {
		return zh
	}
	return en
}

var zhWeekdays = []string{"周日", "周一", "周二", "周三", "周四", "周五", "周六"}

// reportTitle is a report's title: "💰 Paylist Report · Personal".
func reportTitle(ledger, lang string) string {
	return "💰 " + tr(lang, "Paylist Report", "Paylist 报告") + " · " + ledger
}

// verdict says how v compares with usual, in words.
func verdict(lang string, v, usual float64) string {
	if usual <= 0 {
		return ""
	}
	r := v / usual
	switch {
	case r >= 1.5:
		return tr(lang, "much higher than usual", "远高于往常")
	case r >= 1.15:
		return tr(lang, "higher than usual", "高于往常")
	case r <= 0.5:
		return tr(lang, "much lower than usual", "远低于往常")
	case r <= 0.85:
		return tr(lang, "lower than usual", "低于往常")
	}
	return tr(lang, "about usual", "和往常差不多")
}

// writeReport is Paylist's own note, laid out for a chat app: sections
// apart, each its name and then its lines, indented:
//
//	Last week (Nov 16 – 22)
//	  Total ¥1,109.48
//	  ↓ lower than usual (¥2,034.89 a week)
//
//	By type
//	    ↓ Travel -100%
//
// A daily report has the month so far too, and its types go by that.
func writeReport(rep *report, lang string) string {
	from, _ := store.ParseDate(rep.From)
	to, _ := store.ParseDate(rep.To)
	l := rep.Figures
	m := func(v float64) string { return money(v, l.Currency) }

	var name, per string
	switch rep.Period {
	case store.ReportWeekly:
		span := from.Format("Jan 2") + " – " + to.Format("Jan 2")
		if from.Month() == to.Month() {
			span = from.Format("Jan 2") + " – " + strconv.Itoa(to.Day())
		}
		name = tr(lang, "Last week ("+span+")", fmt.Sprintf("上周（%d月%d日 – %d月%d日）", from.Month(), from.Day(), to.Month(), to.Day()))
		per = tr(lang, " a week", "每周 ")
	case store.ReportMonthly:
		name = tr(lang, from.Format("January 2006"), fmt.Sprintf("%d年%d月", from.Year(), from.Month()))
		per = tr(lang, " a month", "每月 ")
	default:
		name = tr(lang, "Yesterday ("+to.Format("Mon, Jan 2")+")", fmt.Sprintf("昨天（%d月%d日 %s）", to.Month(), to.Day(), zhWeekdays[to.Weekday()]))
		per = tr(lang, " a day", "每天 ")
	}
	// A section: its name, then its lines.
	section := func(name string, lines ...string) string {
		return name + "\n" + strings.Join(lines, "\n")
	}
	total := func(v float64) string { return "  " + tr(lang, "Total ", "合计 ") + m(v) }
	// against says how v compares with usual, as "↓ lower than usual (¥2,034.89 a week)".
	against := func(v, usual float64, per, by string) string {
		arrow := "≈"
		if r := v / usual; r >= 1.15 {
			arrow = "↑"
		} else if r <= 0.85 {
			arrow = "↓"
		}
		return "  " + arrow + " " + verdict(lang, v, usual) + tr(lang, " ("+m(usual)+per+by+")", "（"+by+per+m(usual)+"）")
	}

	period := []string{total(l.Total)}
	if l.Usual != nil && *l.Usual > 0 {
		period = append(period, against(l.Total, *l.Usual, per, ""))
	}
	sections := []string{section(name, period...)}

	byType := tr(lang, "By type", "按类型")
	if l.MonthToDate != nil {
		month := []string{total(*l.MonthToDate)}
		if u := l.UsualMonthToDate; u != nil && *u > 0 {
			month = append(month, against(*l.MonthToDate, *u, "", tr(lang, " by now", "往常此时 ")))
		}
		sections = append(sections, section(tr(lang, "Month so far", "本月至今"), month...))
		byType = tr(lang, "By type, month so far", "按类型（本月至今）")
	}

	var types []string
	for _, g := range []struct {
		arrow string
		cs    []typeChange
	}{{"↑", l.Higher}, {"↓", l.Lower}} {
		for _, c := range g.cs {
			n := c.Name
			if n == "" {
				n = tr(lang, "No type", "无类型")
			}
			if c.Usual <= 0 {
				types = append(types, "    "+g.arrow+" "+n+tr(lang, " (new)", "（新）"))
			} else {
				types = append(types, fmt.Sprintf("    %s %s %+.0f%%", g.arrow, n, math.Round((c.Total-c.Usual)/c.Usual*100)))
			}
		}
	}
	if len(types) > 0 {
		sections = append(sections, section(byType, types...))
	}
	return strings.Join(sections, "\n\n")
}

// The model's instructions for its paragraph: what every report shares
// (who reads it, its figures, the rules), and, for each kind of report, what
// it covers, what to write and how to find out (suggestPrompts).
const (
	suggestIntro = `You add a short paragraph to a spending report from Paylist, a personal spending tracker. The user reads the report in a chat app on their phone; your words come right after it, under "✨ AI suggestion".`

	suggestFigures = `You get the report's figures as JSON. "note" is the report as the user sees it. "spent" and "usual" are the period's total and its usual one. The types_higher_than_usual and types_lower_than_usual lists are the types well above or below their usual. "spent_by_type" is the period's spending by type, and "from" and "to" are its dates. Amounts are in the ledger's currency; a negative one is money back, such as a refund.`

	suggestRules = `# Rules

- Use only what the figures and the tools show. You may name entries and their amounts, and a type's usual amount; don't restate the report's totals and percentages, and never make up a number.
- The tools only read the ledger; without "ledger" they use the report's.
- Keep the user's names for types and entries exactly as they are, whatever language they're in.
- Leave out what was ordinary, and give nothing generic, such as "make a budget" or "track your spending".
- Be plain and kind, like a friend who is good with money: no judgment, no exclamation marks.
- Your last message is the paragraph and nothing else: plain text, no greeting, no heading, no list, no markdown, no emoji. Write in %s.`
)

// suggestPrompts are, for each kind of report, what it covers, what to write
// about it and how to find that out.
var suggestPrompts = map[string]string{
	store.ReportDaily: `# The report

A daily report: what was spent yesterday, against a usual day (the average of the 28 days before it); then the month so far, against what the months before had spent by the same day. Its types are the month so far's against their usual by then (types_higher_than_usual_this_month_so_far and types_lower_than_usual_this_month_so_far), as a single day is too little to judge a type by.

# What to write

One paragraph, about 50 to 100 words (a guide, not a limit). A day is noisy, as one dinner out can double it, so read yesterday together with the month so far:

1. What stands out: yesterday, if it was far from a usual day, and what it went on; and the type pulling the month furthest from its usual, in money, and what is behind it (one large purchase, a run of small ones, something new).
2. One concrete step for today and the rest of the month that would bring the month back to its usual, or keep it below: tied to what you found, such as a type to go easy on or a purchase to put off. If the month is on track, say so and what keeps it there.

# How to find out

list_entries with yesterday's date shows what was bought; with a type and the month's dates so far, what is driving the month. One or two calls are usually enough; then write.`,

	store.ReportWeekly: `# The report

A weekly report, sent on Monday: what was spent last week (Monday to Sunday), overall and by type, against a usual week (the average of up to 8 weeks before). Types that come and go, such as rent paid once a month, are left out of the types compared.

# What to write

One paragraph, about 80 to 150 words (a guide, not a limit):

1. What was unusual last week, and why: the one or two types that moved most in money, and what is behind each (one large purchase, a run of small ones, something new, or something that usually happens and didn't). Note a pattern within the week when there is one, such as most of it at the weekend or the same place again and again.
2. One or two concrete steps for this week that would keep its spending at or below a usual week: tied to what you found, such as holding a type to about its usual amount or putting off a purchase like the one that stood out. If last week was below usual, say what to keep doing.

# How to find out

list_entries with last week's dates, and a type, shows the entries behind a change; the same for the weeks before tells a one-off from a trend. Two or three calls are usually enough; then write.`,

	store.ReportMonthly: `# The report

A monthly report, sent on the 1st: what was spent last month, overall and by type, against a usual month (the average of up to 6 months before).

# What to write

One paragraph, about 100 to 200 words (a guide, not a limit). A month is long enough to show habits:

1. What was unusual last month, and why: the types that moved most in money, and what is behind each (one large purchase, a run of small ones, something new, a regular cost that grew, or something that usually happens and didn't). Say whether each is a one-off or part of a trend over the months before.
2. A short plan for this month, one to three concrete steps that would keep its spending at or below a usual month: tied to what you found, such as a type to hold to about its usual amount, a regular cost to look at again, or a purchase to put off. If last month was below usual, say what made it so and how to keep it.

# How to find out

get_stats for last month shows each type month by month (type_monthly), to tell a trend from a one-off; list_entries with a type and last month's dates shows the entries behind a change. Three or four calls are usually enough; then write.`,
}

// suggestPrompt is the model's instructions for a kind of report, in lang.
func suggestPrompt(period, lang string) string {
	return suggestIntro + "\n\n" + suggestPrompts[period] + "\n\n" + suggestFigures + "\n\n" + fmt.Sprintf(suggestRules, tr(lang, "English", "Simplified Chinese"))
}

// suggest has the user's AI model suggest what to do about a ledger's
// report, looking into the ledger with the MCP tools that read it; "" when
// they have no model (or only the demo one) or it fails.
func (s *Server) suggest(ctx context.Context, u *store.User, l *store.Ledger, rep *report, lang string) string {
	client, cfg, err := s.llmClientFor(ctx, u.ID)
	if err != nil || cfg.Kind == "fake" {
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, reportTime)
	defer cancel()
	session, closeSession, err := s.mcpSession(ctx, u, l, true)
	if err != nil {
		s.opts.Logger.Warn("report: no tools for the suggestion", "user", u.ID, "err", err)
		return ""
	}
	defer closeSession()
	f := reportFacts(rep)
	f["note"] = rep.Text
	f["today"] = time.Now().Format("2006-01-02")
	facts, _ := json.Marshal(f)
	system := suggestPrompt(rep.Period, lang)
	history, err := agent.Run(ctx, client, session, system, []llm.Message{{Role: "user", Text: string(facts)}}, func(agent.Event) error { return nil }, nil)
	if err != nil {
		s.opts.Logger.Warn("report: the model failed; sending it without a suggestion", "user", u.ID, "err", err)
		return ""
	}
	// The answer: the model's last message, when it ended with words.
	last := history[len(history)-1]
	if last.Role != "assistant" || len(last.ToolCalls) > 0 {
		return ""
	}
	// Its length is the model's to judge; this only stops a runaway.
	return cut(strings.Join(strings.Fields(last.Text), " "), 4000)
}

// reportFacts are the figures the model writes from, amounts written out.
func reportFacts(rep *report) map[string]any {
	l := rep.Figures
	m := func(v float64) string { return money(v, l.Currency) }
	f := map[string]any{"report": rep.Period, "ledger": rep.Ledger, "from": rep.From, "to": rep.To, "spent": m(l.Total), "entries": l.Count}
	if l.Usual != nil {
		f["usual"] = m(*l.Usual)
		f["against_usual"] = verdict("en", l.Total, *l.Usual)
	}
	if l.MonthToDate != nil {
		f["month_so_far"] = m(*l.MonthToDate)
		if l.UsualMonthToDate != nil {
			f["month_so_far_usually"] = m(*l.UsualMonthToDate)
		}
	}
	changes := func(cs []typeChange) []map[string]string {
		out := []map[string]string{}
		for _, c := range cs {
			ch := map[string]string{"type": c.Name, "spent": m(c.Total), "usually": m(c.Usual)}
			if c.Name == "" {
				ch["type"] = "(no type)"
			}
			if c.Usual <= 0 {
				ch["usually"] = "nothing (new)"
			}
			out = append(out, ch)
		}
		return out
	}
	key := ""
	if rep.Period == store.ReportDaily {
		key = "_this_month_so_far"
	}
	f["types_higher_than_usual"+key] = changes(l.Higher)
	f["types_lower_than_usual"+key] = changes(l.Lower)
	spent := []map[string]string{}
	for _, g := range l.ByType {
		n := g.Name
		if n == "" {
			n = "(no type)"
		}
		spent = append(spent, map[string]string{"type": n, "spent": m(g.Total)})
	}
	f["spent_by_type"] = spent
	return f
}

var currencySymbols = map[string]string{
	"CNY": "¥", "JPY": "¥", "USD": "$", "EUR": "€", "GBP": "£", "HKD": "HK$", "TWD": "NT$", "KRW": "₩",
	"INR": "₹", "AUD": "A$", "CAD": "CA$", "SGD": "S$", "NZD": "NZ$", "MOP": "MOP$", "THB": "฿", "VND": "₫",
}

// money writes an amount with its currency: ¥1,234.50, -$3.00, CHF 12.00.
func money(v float64, cur string) string {
	decimals := 2
	if cur == "JPY" || cur == "KRW" || cur == "VND" {
		decimals = 0
	}
	sign := ""
	if v < 0 {
		sign, v = "-", -v
	}
	s := strconv.FormatFloat(v, 'f', decimals, 64)
	whole, frac, _ := strings.Cut(s, ".")
	var g strings.Builder
	for i, c := range whole {
		if i > 0 && (len(whole)-i)%3 == 0 {
			g.WriteByte(',')
		}
		g.WriteRune(c)
	}
	n := g.String()
	if frac != "" {
		n += "." + frac
	}
	if sym, ok := currencySymbols[cur]; ok {
		return sign + sym + n
	}
	return sign + cur + " " + n
}
