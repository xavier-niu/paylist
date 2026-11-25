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

	"github.com/xavier-niu/paylist/internal/llm"
	"github.com/xavier-niu/paylist/internal/store"
)

// Reports: a brief note on what a ledger spent over the day, week or month
// before, against what it usually does, and the types that stood out.
// Paylist writes the note; the user's AI model, when they have one and want
// it, adds a suggestion of what to do about it.

const (
	usualDays  = 28 // a daily report's usual day is the average of the days before, up to this many
	usualWeeks = 8  // and a weekly report's usual week, of the weeks before
	minDays    = 7  // fewer days than this before, and there's no usual day
	maxMovers  = 3  // types named going each way
	reportTime = 90 * time.Second
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
// With ai, the user's AI model adds a suggestion, when they have one.
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
	if ai {
		if rep.Suggestion = s.suggest(ctx, u.ID, rep, lang); rep.Suggestion != "" {
			rep.Text += "\n\n" + tr(lang, "✨ AI suggestion", "✨ AI 建议") + "\n" + rep.Suggestion
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

const suggestPrompt = `Paylist, a personal spending tracker, sends its user a note on what one of their ledgers spent over a period against what it usually does. You get the note's figures as JSON, with "note": the note itself, which the user reads just before your words.

Add a suggestion to it: one short paragraph, about 50 to 100 words (a guide, not a limit), with one or two concrete, practical things the user could do next %s, such as where to cut back, a type to keep an eye on, or that they're doing well and how to keep it so. Ground it in the figures, naming the types; don't list the figures again, and don't invent any. If the %s was ordinary, say so briefly and keep the suggestion small. Plain text: one paragraph, no greeting, no heading, no markdown, no emoji. Write in %s.`

// suggest has the user's AI model suggest what to do about a report; "" when
// they have no model (or only the demo one) or it fails.
func (s *Server) suggest(ctx context.Context, userID int64, rep *report, lang string) string {
	client, cfg, err := s.llmClientFor(ctx, userID)
	if err != nil || cfg.Kind == "fake" {
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, reportTime)
	defer cancel()
	f := reportFacts(rep)
	f["note"] = rep.Text
	facts, _ := json.Marshal(f)
	unit := map[string]string{store.ReportDaily: "day", store.ReportWeekly: "week", store.ReportMonthly: "month"}[rep.Period]
	system := fmt.Sprintf(suggestPrompt, unit, unit, tr(lang, "English", "Simplified Chinese"))
	msg, _, err := client.Step(ctx, system, []llm.Message{{Role: "user", Text: string(facts)}}, nil, func(string) error { return nil })
	if err != nil {
		s.opts.Logger.Warn("report: the model failed; sending it without a suggestion", "user", userID, "err", err)
		return ""
	}
	// Its length is the model's to judge; this only stops a runaway.
	return cut(strings.Join(strings.Fields(msg.Text), " "), 2000)
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
