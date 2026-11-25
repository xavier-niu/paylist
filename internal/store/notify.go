package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// Notifications: the channels a user has set up to be sent messages on (a
// webhook, Slack, …), and for each ledger, the reports a member wants of it,
// at what time of day in which time zone, sent to which of their channels.

const (
	ReportDaily   = "daily"   // the day before, every day
	ReportWeekly  = "weekly"  // the week before (Monday to Sunday), on Mondays
	ReportMonthly = "monthly" // the month before, on the 1st

	maxChannels    = 20
	maxChannelName = 60
	maxChannelURL  = 2048
	maxHeaders     = 10
)

// ReportPeriods are the reports there are, in order.
var ReportPeriods = []string{ReportDaily, ReportWeekly, ReportMonthly}

// ChannelKinds are the channels there are: a webhook given Paylist's JSON,
// or what each chat or push service's incoming webhook takes.
var ChannelKinds = []string{"webhook", "slack", "discord", "feishu", "dingtalk", "wecom", "bark", "ntfy"}

// TakesHeaders says whether a kind of channel sends headers of the user's
// own, such as Authorization.
func TakesHeaders(kind string) bool { return kind == "webhook" || kind == "ntfy" }

var (
	ErrTooManyChannels = fmt.Errorf("you can have up to %d channels", maxChannels)
	errChannelURL      = errors.New("the address must start with http:// or https://")
	errChannelKind     = fmt.Errorf("the kind of channel must be one of %s", strings.Join(ChannelKinds, ", "))
	errChannelName     = fmt.Errorf("a channel's name is up to %d characters", maxChannelName)
	errReportTime      = errors.New("the time must be written HH:MM, such as 09:00")
	timeRE             = regexp.MustCompile(`^([01]\d|2[0-3]):[0-5]\d$`)
	headerNameRE       = regexp.MustCompile(`^[A-Za-z0-9!#$%&'*+.^_|~-]{1,100}$`)
)

// Header is a header a channel sends, such as Authorization: Bearer ….
type Header struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// Channel is somewhere messages go. Its headers' values are kept sealed and
// never shown again: only their names are.
type Channel struct {
	ID          int64    `json:"id"`
	Kind        string   `json:"kind"`
	Name        string   `json:"name"`
	URL         string   `json:"url"`
	HeaderNames []string `json:"headers"`
	LastAt      string   `json:"last_at,omitempty"`    // when something was last sent on it
	LastError   string   `json:"last_error,omitempty"` // why that failed; "" when it went
	CreatedAt   string   `json:"created_at"`
	Headers     []Header `json:"-"` // to send
}

// ChannelInput is a new channel, or what to change of one (nil: unchanged).
// A header given without a value keeps the value it had.
type ChannelInput struct {
	Kind    *string   `json:"kind"`
	Name    *string   `json:"name"`
	URL     *string   `json:"url"`
	Headers *[]Header `json:"headers"`
}

const channelCols = `id, kind, name, url, sealed_headers, last_at, last_error, created_at`

func (s *Store) scanChannel(row interface{ Scan(...any) error }, userID int64) (*Channel, error) {
	var c Channel
	var sealed []byte
	if err := row.Scan(&c.ID, &c.Kind, &c.Name, &c.URL, &sealed, &c.LastAt, &c.LastError, &c.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	c.HeaderNames = []string{}
	if len(sealed) > 0 {
		plain, err := s.sealer.open(sealed, userID)
		if err != nil {
			return nil, fmt.Errorf("channel %d's headers: %w", c.ID, err)
		}
		if err := json.Unmarshal(plain, &c.Headers); err != nil {
			return nil, err
		}
		for _, h := range c.Headers {
			c.HeaderNames = append(c.HeaderNames, h.Name)
		}
	}
	return &c, nil
}

func (s *Store) ListChannels(ctx context.Context, userID int64) ([]Channel, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+channelCols+` FROM channels WHERE user_id = ? ORDER BY id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Channel{}
	for rows.Next() {
		c, err := s.scanChannel(rows, userID)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

func (s *Store) GetChannel(ctx context.Context, userID, id int64) (*Channel, error) {
	return s.scanChannel(s.db.QueryRowContext(ctx, `SELECT `+channelCols+` FROM channels WHERE user_id = ? AND id = ?`, userID, id), userID)
}

// cleanChannel checks a channel, made from was (nil for a new one) and in.
func cleanChannel(was *Channel, in ChannelInput) (Channel, error) {
	var c Channel
	if was != nil {
		c = *was
	}
	if in.Kind != nil {
		c.Kind = strings.ToLower(strings.TrimSpace(*in.Kind))
	}
	if !slices.Contains(ChannelKinds, c.Kind) {
		return c, errChannelKind
	}
	if in.URL != nil {
		c.URL = strings.TrimSpace(*in.URL)
	}
	u, err := url.Parse(c.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || len(c.URL) > maxChannelURL {
		return c, errChannelURL
	}
	if in.Name != nil {
		c.Name = strings.Join(strings.Fields(*in.Name), " ")
	}
	if c.Name == "" {
		c.Name = u.Hostname()
	}
	if utf8.RuneCountInString(c.Name) > maxChannelName {
		return c, errChannelName
	}
	if in.Headers != nil {
		old := map[string]string{}
		for _, h := range c.Headers {
			old[strings.ToLower(h.Name)] = h.Value
		}
		c.Headers = nil
		seen := map[string]bool{}
		for _, h := range *in.Headers {
			h.Name = strings.TrimSpace(h.Name)
			if h.Name == "" && h.Value == "" {
				continue
			}
			key := strings.ToLower(h.Name)
			if !headerNameRE.MatchString(h.Name) || key == "host" || key == "content-length" || key == "content-type" {
				return c, fmt.Errorf("%q can't be a header's name", h.Name)
			}
			if seen[key] {
				return c, fmt.Errorf("the header %s is there twice", h.Name)
			}
			seen[key] = true
			if h.Value == "" {
				h.Value = old[key]
			}
			if h.Value == "" {
				return c, fmt.Errorf("the header %s needs a value", h.Name)
			}
			if strings.ContainsAny(h.Value, "\r\n") || len(h.Value) > 4096 {
				return c, fmt.Errorf("the header %s's value can't be used", h.Name)
			}
			c.Headers = append(c.Headers, h)
		}
	}
	if !TakesHeaders(c.Kind) {
		c.Headers = nil
	}
	if len(c.Headers) > maxHeaders {
		return c, fmt.Errorf("a channel sends up to %d headers", maxHeaders)
	}
	return c, nil
}

func (s *Store) sealHeaders(userID int64, hs []Header) ([]byte, error) {
	if len(hs) == 0 {
		return nil, nil
	}
	b, err := json.Marshal(hs)
	if err != nil {
		return nil, err
	}
	return s.sealer.seal(b, userID), nil
}

func (s *Store) AddChannel(ctx context.Context, userID int64, in ChannelInput) (*Channel, error) {
	c, err := cleanChannel(nil, in)
	if err != nil {
		return nil, err
	}
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM channels WHERE user_id = ?`, userID).Scan(&n); err != nil {
		return nil, err
	}
	if n >= maxChannels {
		return nil, ErrTooManyChannels
	}
	sealed, err := s.sealHeaders(userID, c.Headers)
	if err != nil {
		return nil, err
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO channels (user_id, kind, name, url, sealed_headers, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		userID, c.Kind, c.Name, c.URL, sealed, now())
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return s.GetChannel(ctx, userID, id)
}

func (s *Store) UpdateChannel(ctx context.Context, userID, id int64, in ChannelInput) (*Channel, error) {
	was, err := s.GetChannel(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	c, err := cleanChannel(was, in)
	if err != nil {
		return nil, err
	}
	sealed, err := s.sealHeaders(userID, c.Headers)
	if err != nil {
		return nil, err
	}
	// A new address or headers: how the last send went no longer says anything.
	if _, err := s.db.ExecContext(ctx, `UPDATE channels SET kind = ?, name = ?, url = ?, sealed_headers = ?, last_error = '' WHERE user_id = ? AND id = ?`,
		c.Kind, c.Name, c.URL, sealed, userID, id); err != nil {
		return nil, err
	}
	return s.GetChannel(ctx, userID, id)
}

func (s *Store) DeleteChannel(ctx context.Context, userID, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM channels WHERE user_id = ? AND id = ?`, userID, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// NoteChannel records a send on a channel, and why it failed ("" when it went).
func (s *Store) NoteChannel(ctx context.Context, id int64, failure string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE channels SET last_at = ?, last_error = ? WHERE id = ?`, now(), failure, id)
	return err
}

// Reports says which reports of a ledger a member gets, and when: at Time
// (HH:MM) in the time zone TZ (an IANA name; "" for the server's), written
// in Language (with a suggestion by their AI model, with AI), sent to
// Channels (ids of their channels).
type Reports struct {
	Daily    bool    `json:"daily"`
	Weekly   bool    `json:"weekly"`
	Monthly  bool    `json:"monthly"`
	Time     string  `json:"time"`
	TZ       string  `json:"timezone"`
	Language string  `json:"language"` // en or zh; "" for the account's
	AI       bool    `json:"ai"`       // the user's AI model adds a suggestion, when they have one
	Channels []int64 `json:"channels"`
	// AIReady says whether the user has an AI model to suggest (filled in by
	// the server, not kept): without one, AI is off whatever it says.
	AIReady bool `json:"ai_ready"`
}

// On says whether the report for period is sent.
func (r Reports) On(period string) bool {
	switch period {
	case ReportDaily:
		return r.Daily
	case ReportWeekly:
		return r.Weekly
	case ReportMonthly:
		return r.Monthly
	}
	return false
}

// Location is the time zone the reports go by.
func (r Reports) Location() *time.Location {
	if r.TZ != "" {
		if loc, err := time.LoadLocation(r.TZ); err == nil {
			return loc
		}
	}
	return time.Local
}

// Clean checks the settings.
func (r Reports) Clean() (Reports, error) {
	r.Time = strings.TrimSpace(r.Time)
	if r.Time == "" {
		r.Time = "09:00"
	}
	if !timeRE.MatchString(r.Time) {
		return r, errReportTime
	}
	r.TZ = strings.TrimSpace(r.TZ)
	if r.TZ != "" {
		if _, err := time.LoadLocation(r.TZ); err != nil {
			return r, fmt.Errorf("%q isn't a time zone this server knows, such as Asia/Shanghai", r.TZ)
		}
	}
	if r.Language != "en" && r.Language != "zh" {
		r.Language = ""
	}
	if r.Channels == nil {
		r.Channels = []int64{}
	}
	slices.Sort(r.Channels)
	r.Channels = slices.Compact(r.Channels)
	return r, nil
}

// GetReports is what a member gets of a ledger.
func (s *Store) GetReports(ctx context.Context, ledgerID, userID int64) (Reports, error) {
	r := Reports{Time: "09:00", AI: true, Channels: []int64{}}
	err := s.db.QueryRowContext(ctx, `SELECT daily, weekly, monthly, time, tz, language, ai FROM ledger_reports WHERE ledger_id = ? AND user_id = ?`, ledgerID, userID).
		Scan(&r.Daily, &r.Weekly, &r.Monthly, &r.Time, &r.TZ, &r.Language, &r.AI)
	if errors.Is(err, sql.ErrNoRows) {
		return r, nil
	}
	if err != nil {
		return r, err
	}
	r.Channels, err = s.reportChannels(ctx, ledgerID, userID)
	return r, err
}

func (s *Store) reportChannels(ctx context.Context, ledgerID, userID int64) ([]int64, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT channel_id FROM ledger_report_channels WHERE ledger_id = ? AND user_id = ? ORDER BY channel_id`, ledgerID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// SaveReports saves what a member gets of a ledger; sent marks reports as
// already sent on a day (period → YYYY-MM-DD), so turning one on after its
// time doesn't send it at once.
func (s *Store) SaveReports(ctx context.Context, ledgerID, userID int64, r Reports, sent map[string]string) (Reports, error) {
	r, err := r.Clean()
	if err != nil {
		return r, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return r, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO ledger_reports (ledger_id, user_id, daily, weekly, monthly, time, tz, language, ai) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (ledger_id, user_id) DO UPDATE SET daily = excluded.daily, weekly = excluded.weekly, monthly = excluded.monthly,
			time = excluded.time, tz = excluded.tz, language = excluded.language, ai = excluded.ai`,
		ledgerID, userID, r.Daily, r.Weekly, r.Monthly, r.Time, r.TZ, r.Language, r.AI); err != nil {
		return r, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM ledger_report_channels WHERE ledger_id = ? AND user_id = ?`, ledgerID, userID); err != nil {
		return r, err
	}
	for _, id := range r.Channels {
		res, err := tx.ExecContext(ctx, `INSERT INTO ledger_report_channels (ledger_id, user_id, channel_id)
			SELECT ?, ?, id FROM channels WHERE id = ? AND user_id = ?`, ledgerID, userID, id, userID)
		if err != nil {
			return r, err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return r, fmt.Errorf("you have no channel %d", id)
		}
	}
	for period, day := range sent {
		if !slices.Contains(ReportPeriods, period) {
			continue
		}
		if _, err := tx.ExecContext(ctx, `UPDATE ledger_reports SET `+period+`_sent = ? WHERE ledger_id = ? AND user_id = ?`, day, ledgerID, userID); err != nil {
			return r, err
		}
	}
	return r, tx.Commit()
}

// ReportSchedule is what a member gets of a ledger, with the day each report
// was last sent.
type ReportSchedule struct {
	LedgerID int64
	UserID   int64
	Reports
	Sent map[string]string // period → YYYY-MM-DD
}

// ReportSchedules lists what members get of their ledgers, where some report
// is on and has a channel to go to.
func (s *Store) ReportSchedules(ctx context.Context) ([]ReportSchedule, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT r.ledger_id, r.user_id, daily, weekly, monthly, time, tz, language, ai, daily_sent, weekly_sent, monthly_sent
		FROM ledger_reports r JOIN ledger_members m ON m.ledger_id = r.ledger_id AND m.user_id = r.user_id
		WHERE (daily OR weekly OR monthly)
			AND EXISTS (SELECT 1 FROM ledger_report_channels c WHERE c.ledger_id = r.ledger_id AND c.user_id = r.user_id)`)
	if err != nil {
		return nil, err
	}
	var out []ReportSchedule
	for rows.Next() {
		var rs ReportSchedule
		var d, w, m string
		if err := rows.Scan(&rs.LedgerID, &rs.UserID, &rs.Daily, &rs.Weekly, &rs.Monthly, &rs.Time, &rs.TZ, &rs.Language, &rs.AI, &d, &w, &m); err != nil {
			rows.Close()
			return nil, err
		}
		rs.Sent = map[string]string{ReportDaily: d, ReportWeekly: w, ReportMonthly: m}
		out = append(out, rs)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		if out[i].Channels, err = s.reportChannels(ctx, out[i].LedgerID, out[i].UserID); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// MarkReportSent records that a member's report of a ledger for period went
// out on day, unless it already had: it says whether this call was the one
// to mark it.
func (s *Store) MarkReportSent(ctx context.Context, ledgerID, userID int64, period, day string) (bool, error) {
	if !slices.Contains(ReportPeriods, period) {
		return false, fmt.Errorf("no report %q", period)
	}
	res, err := s.db.ExecContext(ctx, `UPDATE ledger_reports SET `+period+`_sent = ? WHERE ledger_id = ? AND user_id = ? AND `+period+`_sent <> ?`,
		day, ledgerID, userID, day)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// Summary is what a ledger spent from one day to another.
type Summary struct {
	Total   float64      `json:"total"`
	Count   int          `json:"count"`
	ByType  []GroupTotal `json:"by_type"`           // largest first; "" is entries without a type
	Entries []Entry      `json:"entries,omitempty"` // when asked for, oldest first
}

// Summarize totals a ledger's entries from from to to (YYYY-MM-DD, both
// included), by type, with the entries themselves when entries is set.
func (s *Store) Summarize(ctx context.Context, ledgerID int64, from, to string, entries bool) (*Summary, error) {
	sum := &Summary{ByType: []GroupTotal{}}
	rows, err := s.db.QueryContext(ctx, `SELECT e.type, COALESCE(o.color, 'gray'), SUM(e.amount), COUNT(*)
		FROM entries e LEFT JOIN options o ON o.ledger_id = e.ledger_id AND o.kind = 'type' AND o.name = e.type
		WHERE e.ledger_id = ? AND e.date BETWEEN ? AND ?
		GROUP BY e.type ORDER BY SUM(e.amount) DESC, e.type`, ledgerID, from, to)
	if err != nil {
		return nil, err
	}
	var cents int64
	for rows.Next() {
		var g GroupTotal
		var c int64
		if err := rows.Scan(&g.Name, &g.Color, &c, &g.Count); err != nil {
			rows.Close()
			return nil, err
		}
		g.Total = fromCents(c)
		cents += c
		sum.Count += g.Count
		sum.ByType = append(sum.ByType, g)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sum.Total = fromCents(cents)
	if entries {
		if sum.Entries, err = s.ListEntries(ctx, ledgerID, EntryFilter{From: from, To: to}); err != nil {
			return nil, err
		}
	}
	return sum, nil
}

// FirstEntryDate is the date of a ledger's earliest entry ("" when it has none).
func (s *Store) FirstEntryDate(ctx context.Context, ledgerID int64) (string, error) {
	var d sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT MIN(date) FROM entries WHERE ledger_id = ?`, ledgerID).Scan(&d)
	return d.String, err
}
