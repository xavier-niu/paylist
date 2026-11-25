package store

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"sort"
	"time"
)

// Stats summarise one month of spending, and the months before it.
type Stats struct {
	Month     string  `json:"month"` // YYYY-MM
	Total     float64 `json:"total"`
	Count     int     `json:"count"`
	PrevTotal float64 `json:"prev_total"` // the whole month before
	// Month over month, like for like: in the current month, this month up to
	// today against the month before up to the same day (or its last day, when
	// it is shorter); for any other month, the two whole months.
	ToDate     float64      `json:"to_date"`      // this month, up to CompareDay
	PrevToDate float64      `json:"prev_to_date"` // the month before, up to the same day
	CompareDay int          `json:"compare_day"`  // the day of the month compared up to; 0 for whole months
	DailyAvg   float64      `json:"daily_avg"`    // over the days so far in the current month, all days otherwise
	Days       []DayTotal   `json:"days"`         // every day of the month, in order
	ByType     []GroupTotal `json:"by_type"`      // largest first; "" is entries without a type
	ByCard     []GroupTotal `json:"by_card"`
	Monthly    []MonthTotal `json:"monthly"` // the last N months up to Month, oldest first
	// Each type over the same months as Monthly.
	TypeMonthly []TypeSeries `json:"type_monthly"`
	// What's usual: the average month over the AvgMonths months before Month
	// (up to UsualMonths, from the first with entries), each counted up to
	// CompareDay like ToDate (whole months for a past month), for all
	// spending and for each type. Against it, ToDate and ByType show what's
	// running above or below the usual.
	AvgMonths int          `json:"avg_months"`
	AvgToDate float64      `json:"avg_to_date"`
	TypeAvg   []GroupTotal `json:"type_avg"`
	// ByType counted like ToDate: up to CompareDay, or the whole month.
	ByTypeToDate []GroupTotal `json:"by_type_to_date"`
	Year         YearStats    `json:"year"` // the year Month is in
}

// UsualMonths is how many months before a month make up its usual.
const UsualMonths = 6

// TypeSeries is one type's totals, month by month.
type TypeSeries struct {
	Name   string    `json:"name"`
	Color  string    `json:"color"`
	Totals []float64 `json:"totals"`
}

// YearStats summarise a year, and compare it with the year before: in the
// current year, up to today against the same day a year earlier; for any
// other year, the two whole years.
type YearStats struct {
	Year        int          `json:"year"`
	Total       float64      `json:"total"`
	Count       int          `json:"count"`
	ToDate      float64      `json:"to_date"`
	PrevToDate  float64      `json:"prev_to_date"`
	CompareDate string       `json:"compare_date"` // the day compared up to (YYYY-MM-DD in this year), "" for whole years
	Months      []MonthTotal `json:"months"`       // January to December
	PrevMonths  []MonthTotal `json:"prev_months"`  // of the year before
	ByType      []GroupTotal `json:"by_type"`
}

type DayTotal struct {
	Date  string  `json:"date"`
	Total float64 `json:"total"`
	Count int     `json:"count"`
}

type GroupTotal struct {
	Name  string  `json:"name"`
	Color string  `json:"color"`
	Total float64 `json:"total"`
	Count int     `json:"count"`
}

type MonthTotal struct {
	Month string  `json:"month"`
	Total float64 `json:"total"`
	Count int     `json:"count"`
}

// GetStats computes the statistics for month (YYYY-MM) with `months` months of
// history (1–36). today decides how many days of the current month have passed.
func (s *Store) GetStats(ctx context.Context, ledgerID int64, month string, months int, today time.Time) (*Stats, error) {
	start, err := ParseMonth(month)
	if err != nil {
		return nil, err
	}
	months = min(max(months, 1), 36)
	end := start.AddDate(0, 1, -1)
	from, to := start.Format(dateLayout), end.Format(dateLayout)
	st := &Stats{Month: start.Format(monthLayout), Days: []DayTotal{}, ByType: []GroupTotal{}, ByCard: []GroupTotal{}, Monthly: []MonthTotal{}}

	// Daily totals, zero-filled.
	byDay := map[string]DayTotal{}
	var totalCents int64
	rows, err := s.db.QueryContext(ctx, `SELECT date, SUM(amount), COUNT(*) FROM entries
		WHERE ledger_id = ? AND date BETWEEN ? AND ? GROUP BY date`, ledgerID, from, to)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var d DayTotal
		var cents int64
		if err := rows.Scan(&d.Date, &cents, &d.Count); err != nil {
			rows.Close()
			return nil, err
		}
		d.Total = fromCents(cents)
		totalCents += cents
		byDay[d.Date] = d
	}
	rows.Close()
	for t := start; !t.After(end); t = t.AddDate(0, 0, 1) {
		key := t.Format(dateLayout)
		d, ok := byDay[key]
		if !ok {
			d = DayTotal{Date: key}
		}
		st.Days = append(st.Days, d)
		st.Count += d.Count
	}
	st.Total = fromCents(totalCents)

	days := end.Day()
	if y, m, _ := today.Date(); y == start.Year() && m == start.Month() {
		days = today.Day()
	} else if today.Before(start) {
		days = 0
	}
	if days > 0 {
		st.DailyAvg = math.Round(float64(totalCents)/float64(days)) / 100
	}

	group := func(col, kind, from, to string) ([]GroupTotal, error) {
		rows, err := s.db.QueryContext(ctx, `SELECT e.`+col+`, COALESCE(o.color, 'gray'), SUM(e.amount), COUNT(*)
			FROM entries e LEFT JOIN options o ON o.ledger_id = e.ledger_id AND o.kind = ? AND o.name = e.`+col+`
			WHERE e.ledger_id = ? AND e.date BETWEEN ? AND ?
			GROUP BY e.`+col+` ORDER BY SUM(e.amount) DESC, e.`+col, kind, ledgerID, from, to)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		out := []GroupTotal{}
		for rows.Next() {
			var g GroupTotal
			var cents int64
			if err := rows.Scan(&g.Name, &g.Color, &cents, &g.Count); err != nil {
				return nil, err
			}
			g.Total = fromCents(cents)
			out = append(out, g)
		}
		return out, rows.Err()
	}
	if st.ByType, err = group("type", KindType, from, to); err != nil {
		return nil, err
	}
	if st.ByCard, err = group("card", KindCard, from, to); err != nil {
		return nil, err
	}

	// Monthly totals for the last `months` months, zero-filled.
	first := start.AddDate(0, -(months - 1), 0)
	byMonth := map[string]MonthTotal{}
	rows, err = s.db.QueryContext(ctx, `SELECT substr(date, 1, 7), SUM(amount), COUNT(*) FROM entries
		WHERE ledger_id = ? AND date BETWEEN ? AND ? GROUP BY substr(date, 1, 7)`,
		ledgerID, start.AddDate(0, -months, 0).Format(dateLayout), to)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var m MonthTotal
		var cents int64
		if err := rows.Scan(&m.Month, &cents, &m.Count); err != nil {
			rows.Close()
			return nil, err
		}
		m.Total = fromCents(cents)
		byMonth[m.Month] = m
	}
	rows.Close()
	for t := first; !t.After(start); t = t.AddDate(0, 1, 0) {
		key := t.Format(monthLayout)
		m, ok := byMonth[key]
		if !ok {
			m = MonthTotal{Month: key}
		}
		st.Monthly = append(st.Monthly, m)
	}
	st.PrevTotal = byMonth[start.AddDate(0, -1, 0).Format(monthLayout)].Total

	st.ToDate, st.PrevToDate = st.Total, st.PrevTotal
	if y, m, d := today.Date(); y == start.Year() && m == start.Month() && d < end.Day() {
		prevStart := start.AddDate(0, -1, 0)
		prevDay := min(d, prevStart.AddDate(0, 1, -1).Day())
		sum := func(from, to time.Time) (float64, error) {
			var cents int64
			err := s.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(amount), 0) FROM entries WHERE ledger_id = ? AND date BETWEEN ? AND ?`,
				ledgerID, from.Format(dateLayout), to.Format(dateLayout)).Scan(&cents)
			return fromCents(cents), err
		}
		if st.ToDate, err = sum(start, start.AddDate(0, 0, d-1)); err != nil {
			return nil, err
		}
		if st.PrevToDate, err = sum(prevStart, prevStart.AddDate(0, 0, prevDay-1)); err != nil {
			return nil, err
		}
		st.CompareDay = d
	}

	st.ByTypeToDate = st.ByType
	if st.CompareDay > 0 {
		if st.ByTypeToDate, err = group("type", KindType, from, start.AddDate(0, 0, st.CompareDay-1).Format(dateLayout)); err != nil {
			return nil, err
		}
	}
	if st.TypeMonthly, err = s.typeMonthly(ctx, ledgerID, first, start); err != nil {
		return nil, err
	}
	if err := s.usual(ctx, ledgerID, start, st, func(from, to string) ([]GroupTotal, error) { return group("type", KindType, from, to) }); err != nil {
		return nil, err
	}

	if st.Year, err = s.yearStats(ctx, ledgerID, start.Year(), today, func(from, to string) ([]GroupTotal, error) { return group("type", KindType, from, to) }); err != nil {
		return nil, err
	}
	return st, nil
}

// usual fills in the average month before start, overall and by type: over
// the UsualMonths months before it, or those since the ledger's first entry,
// each up to st.CompareDay (or its last day, when shorter) when that's set.
func (s *Store) usual(ctx context.Context, ledgerID int64, start time.Time, st *Stats, byType func(from, to string) ([]GroupTotal, error)) error {
	st.TypeAvg = []GroupTotal{}
	var firstDate sql.NullString
	if err := s.db.QueryRowContext(ctx, `SELECT MIN(date) FROM entries WHERE ledger_id = ?`, ledgerID).Scan(&firstDate); err != nil {
		return err
	}
	if !firstDate.Valid || len(firstDate.String) < 7 {
		return nil
	}
	firstMonth := firstDate.String[:7]
	sums := map[string]*GroupTotal{}
	var order []string
	var all float64
	n := 0
	for i := 1; i <= UsualMonths; i++ {
		ms := start.AddDate(0, -i, 0)
		if ms.Format(monthLayout) < firstMonth {
			break
		}
		to := ms.AddDate(0, 1, -1)
		if st.CompareDay > 0 {
			to = ms.AddDate(0, 0, min(st.CompareDay, to.Day())-1)
		}
		groups, err := byType(ms.Format(dateLayout), to.Format(dateLayout))
		if err != nil {
			return err
		}
		for _, g := range groups {
			sg, ok := sums[g.Name]
			if !ok {
				sg = &GroupTotal{Name: g.Name, Color: g.Color}
				sums[g.Name] = sg
				order = append(order, g.Name)
			}
			sg.Total += g.Total
			sg.Count += g.Count
			all += g.Total
		}
		n++
	}
	if n == 0 {
		return nil
	}
	st.AvgMonths = n
	st.AvgToDate = math.Round(all/float64(n)*100) / 100
	for _, name := range order {
		g := *sums[name]
		g.Total = math.Round(g.Total/float64(n)*100) / 100
		st.TypeAvg = append(st.TypeAvg, g)
	}
	sort.SliceStable(st.TypeAvg, func(i, j int) bool { return st.TypeAvg[i].Total > st.TypeAvg[j].Total })
	return nil
}

// typeMonthly gives each type's total in every month from first to last,
// the types that spent most over them first.
func (s *Store) typeMonthly(ctx context.Context, ledgerID int64, first, last time.Time) ([]TypeSeries, error) {
	var months []string
	for t := first; !t.After(last); t = t.AddDate(0, 1, 0) {
		months = append(months, t.Format(monthLayout))
	}
	idx := map[string]int{}
	for i, m := range months {
		idx[m] = i
	}
	rows, err := s.db.QueryContext(ctx, `SELECT e.type, COALESCE(o.color, 'gray'), substr(e.date, 1, 7), SUM(e.amount)
		FROM entries e LEFT JOIN options o ON o.ledger_id = e.ledger_id AND o.kind = ? AND o.name = e.type
		WHERE e.ledger_id = ? AND e.date BETWEEN ? AND ?
		GROUP BY e.type, substr(e.date, 1, 7)`,
		KindType, ledgerID, first.Format(dateLayout), last.AddDate(0, 1, -1).Format(dateLayout))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byName := map[string]*TypeSeries{}
	sums := map[string]int64{}
	var order []string
	for rows.Next() {
		var name, color, month string
		var cents int64
		if err := rows.Scan(&name, &color, &month, &cents); err != nil {
			return nil, err
		}
		ts, ok := byName[name]
		if !ok {
			ts = &TypeSeries{Name: name, Color: color, Totals: make([]float64, len(months))}
			byName[name] = ts
			order = append(order, name)
		}
		ts.Totals[idx[month]] = fromCents(cents)
		sums[name] += cents
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.SliceStable(order, func(i, j int) bool {
		if sums[order[i]] != sums[order[j]] {
			return sums[order[i]] > sums[order[j]]
		}
		return order[i] < order[j]
	})
	out := make([]TypeSeries, 0, len(order))
	for _, n := range order {
		out = append(out, *byName[n])
	}
	return out, nil
}

// yearStats summarises a year against the one before it.
func (s *Store) yearStats(ctx context.Context, ledgerID int64, year int, today time.Time, byType func(from, to string) ([]GroupTotal, error)) (YearStats, error) {
	ys := YearStats{Year: year}
	start := time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC)
	prevStart := start.AddDate(-1, 0, 0)
	months := func(from time.Time) ([]MonthTotal, int64, int, error) {
		rows, err := s.db.QueryContext(ctx, `SELECT substr(date, 1, 7), SUM(amount), COUNT(*) FROM entries
			WHERE ledger_id = ? AND date BETWEEN ? AND ? GROUP BY substr(date, 1, 7)`,
			ledgerID, from.Format(dateLayout), from.AddDate(1, 0, -1).Format(dateLayout))
		if err != nil {
			return nil, 0, 0, err
		}
		defer rows.Close()
		got := map[string]MonthTotal{}
		var total int64
		var count int
		for rows.Next() {
			var m MonthTotal
			var cents int64
			if err := rows.Scan(&m.Month, &cents, &m.Count); err != nil {
				return nil, 0, 0, err
			}
			m.Total = fromCents(cents)
			got[m.Month] = m
			total += cents
			count += m.Count
		}
		out := make([]MonthTotal, 0, 12)
		for t := from; t.Year() == from.Year(); t = t.AddDate(0, 1, 0) {
			k := t.Format(monthLayout)
			m, ok := got[k]
			if !ok {
				m = MonthTotal{Month: k}
			}
			out = append(out, m)
		}
		return out, total, count, rows.Err()
	}
	var cents, prevCents int64
	var err error
	if ys.Months, cents, ys.Count, err = months(start); err != nil {
		return ys, err
	}
	if ys.PrevMonths, prevCents, _, err = months(prevStart); err != nil {
		return ys, err
	}
	ys.Total = fromCents(cents)
	ys.ToDate, ys.PrevToDate = ys.Total, fromCents(prevCents)
	if today.Year() == year {
		// Up to today, against the same day a year before (Feb 28 for a Feb 29).
		day := time.Date(year, today.Month(), today.Day(), 0, 0, 0, 0, time.UTC)
		prevDay := day.AddDate(-1, 0, 0)
		if prevDay.Month() != day.Month() {
			prevDay = prevDay.AddDate(0, 0, -prevDay.Day())
		}
		sum := func(from, to time.Time) (float64, error) {
			var c int64
			err := s.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(amount), 0) FROM entries WHERE ledger_id = ? AND date BETWEEN ? AND ?`,
				ledgerID, from.Format(dateLayout), to.Format(dateLayout)).Scan(&c)
			return fromCents(c), err
		}
		if ys.ToDate, err = sum(start, day); err != nil {
			return ys, err
		}
		if ys.PrevToDate, err = sum(prevStart, prevDay); err != nil {
			return ys, err
		}
		ys.CompareDate = day.Format(dateLayout)
	}
	if ys.ByType, err = byType(start.Format(dateLayout), start.AddDate(1, 0, -1).Format(dateLayout)); err != nil {
		return ys, err
	}
	return ys, nil
}

// AllMonths lists every month up to upTo (YYYY-MM) that has entries, newest first.
func (s *Store) AllMonths(ctx context.Context, ledgerID int64, upTo string) ([]MonthTotal, error) {
	end, err := time.Parse(monthLayout, upTo)
	if err != nil {
		return nil, fmt.Errorf("the month %q must be written YYYY-MM", upTo)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT substr(date, 1, 7) AS m, SUM(amount), COUNT(*) FROM entries
		WHERE ledger_id = ? AND date < ? GROUP BY m ORDER BY m DESC`,
		ledgerID, end.AddDate(0, 1, 0).Format(dateLayout))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []MonthTotal{}
	for rows.Next() {
		var m MonthTotal
		var cents int64
		if err := rows.Scan(&m.Month, &cents, &m.Count); err != nil {
			return nil, err
		}
		m.Total = fromCents(cents)
		out = append(out, m)
	}
	return out, rows.Err()
}
