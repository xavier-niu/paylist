package store

import (
	"context"
	"fmt"
	"math"
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
func (s *Store) GetStats(ctx context.Context, userID int64, month string, months int, today time.Time) (*Stats, error) {
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
		WHERE user_id = ? AND date BETWEEN ? AND ? GROUP BY date`, userID, from, to)
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

	group := func(col, kind string) ([]GroupTotal, error) {
		rows, err := s.db.QueryContext(ctx, `SELECT e.`+col+`, COALESCE(o.color, 'gray'), SUM(e.amount), COUNT(*)
			FROM entries e LEFT JOIN options o ON o.user_id = e.user_id AND o.kind = ? AND o.name = e.`+col+`
			WHERE e.user_id = ? AND e.date BETWEEN ? AND ?
			GROUP BY e.`+col+` ORDER BY SUM(e.amount) DESC, e.`+col, kind, userID, from, to)
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
	if st.ByType, err = group("type", KindType); err != nil {
		return nil, err
	}
	if st.ByCard, err = group("card", KindCard); err != nil {
		return nil, err
	}

	// Monthly totals for the last `months` months, zero-filled.
	first := start.AddDate(0, -(months - 1), 0)
	byMonth := map[string]MonthTotal{}
	rows, err = s.db.QueryContext(ctx, `SELECT substr(date, 1, 7), SUM(amount), COUNT(*) FROM entries
		WHERE user_id = ? AND date BETWEEN ? AND ? GROUP BY substr(date, 1, 7)`,
		userID, start.AddDate(0, -months, 0).Format(dateLayout), to)
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
			err := s.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(amount), 0) FROM entries WHERE user_id = ? AND date BETWEEN ? AND ?`,
				userID, from.Format(dateLayout), to.Format(dateLayout)).Scan(&cents)
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
	return st, nil
}

// AllMonths lists every month up to upTo (YYYY-MM) that has entries, newest first.
func (s *Store) AllMonths(ctx context.Context, userID int64, upTo string) ([]MonthTotal, error) {
	end, err := time.Parse(monthLayout, upTo)
	if err != nil {
		return nil, fmt.Errorf("the month %q must be written YYYY-MM", upTo)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT substr(date, 1, 7) AS m, SUM(amount), COUNT(*) FROM entries
		WHERE user_id = ? AND date < ? GROUP BY m ORDER BY m DESC`,
		userID, end.AddDate(0, 1, 0).Format(dateLayout))
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
