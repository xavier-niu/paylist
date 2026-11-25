package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/xavier-niu/paylist/internal/store"
)

// Notifications: each user's channels, and the reports of each ledger they
// want sent on them, at the time of day they chose (reports.go writes them).
//
//   GET    /api/channels                 the user's channels
//   POST   /api/channels                 {kind, name, url, headers}
//   PUT    /api/channels/{id}            the same, what changes (a header without a value keeps it)
//   DELETE /api/channels/{id}
//   POST   /api/channels/{id}/test       {language?}: a short message
//   GET    /api/reports                  the open ledger's reports, for the user
//   PUT    /api/reports                  {daily, weekly, monthly, time, timezone, language, ai, channels}
//   POST   /api/reports/send             {period, timezone?, language?}: sent now

const webhookTimeout = 20 * time.Second

func (s *Server) handleListChannels(w http.ResponseWriter, r *http.Request) {
	chs, err := s.opts.Store.ListChannels(r.Context(), currentUser(r.Context()).ID)
	if err != nil {
		s.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, chs)
}

func (s *Server) handleAddChannel(w http.ResponseWriter, r *http.Request) {
	var in store.ChannelInput
	if !decode(w, r, &in) {
		return
	}
	c, err := s.opts.Store.AddChannel(r.Context(), currentUser(r.Context()).ID, in)
	if err != nil {
		writeError(w, http.StatusBadRequest, sentence(err))
		return
	}
	writeJSON(w, http.StatusCreated, c)
}

func (s *Server) handleUpdateChannel(w http.ResponseWriter, r *http.Request) {
	var in store.ChannelInput
	if !decode(w, r, &in) {
		return
	}
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	c, err := s.opts.Store.UpdateChannel(r.Context(), currentUser(r.Context()).ID, id, in)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "No such channel.")
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, sentence(err))
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (s *Server) handleDeleteChannel(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err := s.opts.Store.DeleteChannel(r.Context(), currentUser(r.Context()).ID, id); err != nil {
		writeError(w, http.StatusNotFound, "No such channel.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleTestChannel(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Language string `json:"language"`
	}
	if r.ContentLength > 0 && !decode(w, r, &req) {
		return
	}
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	c, err := s.testChannel(r.Context(), currentUser(r.Context()), id, req.Language)
	if err != nil {
		status := http.StatusBadGateway
		if errors.Is(err, store.ErrNotFound) {
			status, err = http.StatusNotFound, errors.New("no such channel")
		}
		writeError(w, status, sentence(err))
		return
	}
	writeJSON(w, http.StatusOK, c)
}

// testChannel sends a channel a short message, and returns it as it is after.
func (s *Server) testChannel(ctx context.Context, u *store.User, id int64, lang string) (*store.Channel, error) {
	c, err := s.opts.Store.GetChannel(ctx, u.ID, id)
	if err != nil {
		return nil, err
	}
	if lang != "en" && lang != "zh" {
		lang = reportLanguage(u, store.Reports{})
	}
	if err := s.deliver(ctx, c, testReport(lang)); err != nil {
		return nil, err
	}
	return s.opts.Store.GetChannel(ctx, u.ID, id)
}

func (s *Server) handleGetReports(w http.ResponseWriter, r *http.Request) {
	rs, err := s.getReports(r.Context(), currentLedger(r.Context()).ID, currentUser(r.Context()).ID)
	if err != nil {
		s.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rs)
}

// getReports is what a member gets of a ledger, saying whether they have an
// AI model to write it.
func (s *Server) getReports(ctx context.Context, ledgerID, userID int64) (store.Reports, error) {
	rs, err := s.opts.Store.GetReports(ctx, ledgerID, userID)
	rs.AIReady = s.aiReady(ctx, userID)
	return rs, err
}

// aiReady says whether a user has an AI model that can write their reports
// (the demo model can't).
func (s *Server) aiReady(ctx context.Context, userID int64) bool {
	_, cfg, err := s.llmClientFor(ctx, userID)
	return err == nil && cfg.Kind != "fake"
}

func (s *Server) handleSaveReports(w http.ResponseWriter, r *http.Request) {
	var req store.Reports
	if !decode(w, r, &req) {
		return
	}
	out, err := s.saveReports(r.Context(), currentLedger(r.Context()).ID, currentUser(r.Context()).ID, req, time.Now())
	if err != nil {
		writeError(w, http.StatusBadRequest, sentence(err))
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// saveReports saves what a member gets of a ledger. When today's time to
// send has passed, today's reports count as sent: turning one on, or moving
// the time to earlier, sends nothing until the next time comes round.
func (s *Server) saveReports(ctx context.Context, ledgerID, userID int64, r store.Reports, now time.Time) (store.Reports, error) {
	r, err := r.Clean()
	if err != nil {
		return r, err
	}
	// Turning AI on needs a model; one already on stays on, for when there's one again.
	if was, err := s.opts.Store.GetReports(ctx, ledgerID, userID); err != nil {
		return r, err
	} else if r.AI && !was.AI && !s.aiReady(ctx, userID) {
		return r, errors.New("choose an AI model in Settings first")
	}
	var sent map[string]string
	if day, ok := sendTimePassed(r, now); ok {
		sent = map[string]string{}
		for _, p := range store.ReportPeriods {
			sent[p] = day
		}
	}
	r, err = s.opts.Store.SaveReports(ctx, ledgerID, userID, r, sent)
	r.AIReady = s.aiReady(ctx, userID)
	return r, err
}

// sendTimePassed says whether the time reports go out has come today, in
// their time zone, and what day that is there.
func sendTimePassed(r store.Reports, now time.Time) (string, bool) {
	loc := r.Location()
	local := now.In(loc)
	h, m := 9, 0
	if hh, mm, ok := strings.Cut(r.Time, ":"); ok {
		h, _ = strconv.Atoi(hh)
		m, _ = strconv.Atoi(mm)
	}
	at := time.Date(local.Year(), local.Month(), local.Day(), h, m, 0, 0, loc)
	return local.Format("2006-01-02"), !local.Before(at)
}

// dueReports are the reports of a schedule to send now, and the day it is.
func dueReports(rs store.ReportSchedule, now time.Time) ([]string, string) {
	day, ok := sendTimePassed(rs.Reports, now)
	if !ok {
		return nil, day
	}
	local := now.In(rs.Location())
	var due []string
	for _, p := range store.ReportPeriods {
		if !rs.On(p) || rs.Sent[p] == day {
			continue
		}
		if (p == store.ReportWeekly && local.Weekday() != time.Monday) || (p == store.ReportMonthly && local.Day() != 1) {
			continue
		}
		due = append(due, p)
	}
	return due, day
}

func (s *Server) handleSendReport(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Period   string `json:"period"`
		Timezone string `json:"timezone"`
		Language string `json:"language"`
	}
	if !decode(w, r, &req) {
		return
	}
	sent, err := s.sendReportNow(r.Context(), currentUser(r.Context()), currentLedger(r.Context()), req.Period, req.Timezone, req.Language)
	if err != nil {
		writeError(w, http.StatusBadGateway, sentence(err))
		return
	}
	writeJSON(w, http.StatusOK, sent)
}

// reportSent is how sending a report went.
type reportSent struct {
	Report   *report         `json:"report"`
	Channels []store.Channel `json:"channels"` // those it went to, as they are after (a failure says why)
}

// sendReportNow sends a ledger's report for period to the user's channels
// for it, now, whatever the time.
func (s *Server) sendReportNow(ctx context.Context, u *store.User, l *store.Ledger, period, tz, lang string) (*reportSent, error) {
	rep, rs, err := s.ledgerReport(ctx, u, l, period, tz, lang, time.Now())
	if err != nil {
		return nil, err
	}
	if len(rs.Channels) == 0 {
		return nil, errors.New("choose a channel to send it to first")
	}
	out := &reportSent{Report: rep, Channels: []store.Channel{}}
	var errs []error
	for _, id := range rs.Channels {
		c, err := s.opts.Store.GetChannel(ctx, u.ID, id)
		if err != nil {
			continue
		}
		if err := s.deliver(ctx, c, rep); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", c.Name, err))
		}
		if c, err = s.opts.Store.GetChannel(ctx, u.ID, id); err == nil {
			out.Channels = append(out.Channels, *c)
		}
	}
	if len(errs) == len(rs.Channels) {
		return nil, errors.Join(errs...)
	}
	return out, nil
}

// ledgerReport is a ledger's report for period as it would be sent today,
// with the user's settings for it; tz and lang override those when given.
func (s *Server) ledgerReport(ctx context.Context, u *store.User, l *store.Ledger, period, tz, lang string, now time.Time) (*report, store.Reports, error) {
	rs, err := s.opts.Store.GetReports(ctx, l.ID, u.ID)
	if err != nil {
		return nil, rs, err
	}
	if tz != "" {
		rs.TZ = tz
	}
	if lang == "en" || lang == "zh" {
		rs.Language = lang
	}
	if period == "" {
		period = store.ReportDaily
	}
	rep, err := s.buildReport(ctx, u, l, period, reportLanguage(u, rs), rs.AI, now.In(rs.Location()))
	return rep, rs, err
}

// RunReports sends reports as they come due, checking every minute, until
// ctx ends.
func (s *Server) RunReports(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		s.sendDueReports(ctx, time.Now())
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (s *Server) sendDueReports(ctx context.Context, now time.Time) {
	st := s.opts.Store
	scheds, err := st.ReportSchedules(ctx)
	if err != nil {
		s.opts.Logger.Error("reports", "err", err)
		return
	}
	for _, rs := range scheds {
		due, day := dueReports(rs, now)
		for _, period := range due {
			// Marked first: a report goes out once, even if the server stops
			// halfway; a failed send shows on the channel instead.
			if ok, err := st.MarkReportSent(ctx, rs.LedgerID, rs.UserID, period, day); err != nil || !ok {
				continue
			}
			if err := s.sendReport(ctx, rs, period, now); err != nil {
				s.opts.Logger.Error("report", "ledger", rs.LedgerID, "user", rs.UserID, "period", period, "err", err)
			}
		}
	}
}

// sendReport sends a member's report of a ledger for period to their channels for it.
func (s *Server) sendReport(ctx context.Context, rs store.ReportSchedule, period string, now time.Time) error {
	st := s.opts.Store
	u, err := st.GetUser(ctx, rs.UserID)
	if err != nil {
		return err
	}
	l, err := st.GetLedger(ctx, rs.UserID, rs.LedgerID)
	if err != nil {
		return err
	}
	rep, err := s.buildReport(ctx, u, l, period, reportLanguage(u, rs.Reports), rs.AI, now.In(rs.Location()))
	if err != nil {
		return err
	}
	var errs []error
	for _, id := range rs.Channels {
		c, err := st.GetChannel(ctx, u.ID, id)
		if err != nil {
			continue
		}
		if err := s.deliver(ctx, c, rep); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// deliver sends a report on a channel and notes how it went.
func (s *Server) deliver(ctx context.Context, c *store.Channel, rep *report) error {
	err := s.post(ctx, c, rep)
	failure := ""
	if err != nil {
		failure = sentence(err)
	}
	if nerr := s.opts.Store.NoteChannel(context.WithoutCancel(ctx), c.ID, failure); nerr != nil {
		s.opts.Logger.Error("note channel", "err", nerr)
	}
	return err
}

// channelBody is what a kind of channel is sent for a report.
func channelBody(kind string, rep *report) ([]byte, http.Header, error) {
	h := http.Header{"Content-Type": {"application/json"}}
	full := rep.Title + "\n\n" + rep.Text
	var v any
	switch kind {
	case "slack":
		v = map[string]any{"text": full}
	case "discord":
		v = map[string]any{"content": cut(full, 2000)}
	case "feishu":
		v = map[string]any{"msg_type": "text", "content": map[string]any{"text": full}}
	case "dingtalk", "wecom":
		v = map[string]any{"msgtype": "text", "text": map[string]any{"content": full}}
	case "bark":
		v = map[string]any{"title": rep.Title, "body": rep.Text, "group": "Paylist"}
	case "ntfy":
		h.Set("Content-Type", "text/plain; charset=utf-8")
		h.Set("Title", mime.QEncoding.Encode("utf-8", rep.Title)) // ntfy shows it as the title
		return []byte(rep.Text), h, nil
	default:
		// Paylist's JSON: the report, its text the whole message (title
		// and all) as chat apps get it, for whatever passes it on.
		event := "report"
		if rep.Period == "test" {
			event = "test"
		}
		whole := *rep
		whole.Text = full
		v = struct {
			Event string `json:"event"`
			*report
		}{event, &whole}
	}
	b, err := json.Marshal(v)
	return b, h, err
}

// cut shortens s to at most n characters.
func cut(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

func (s *Server) post(ctx context.Context, c *store.Channel, rep *report) error {
	body, h, err := channelBody(c.Kind, rep)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, webhookTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.URL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	for _, hd := range c.Headers {
		req.Header.Set(hd.Name, hd.Value)
	}
	for k, v := range h {
		req.Header[k] = v
	}
	req.Header.Set("User-Agent", "Paylist")
	res, err := s.http.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return errors.New("it didn't answer in time")
		}
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return fmt.Errorf("couldn't reach it: %w", err)
	}
	defer res.Body.Close()
	got, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return fmt.Errorf("it answered %s%s", res.Status, snippet(got))
	}
	// Chat apps answer 200 with an error code of their own.
	var ans struct {
		Code    *int   `json:"code"`
		ErrCode *int   `json:"errcode"`
		Msg     string `json:"msg"`
		ErrMsg  string `json:"errmsg"`
	}
	if json.Unmarshal(got, &ans) == nil {
		if c.Kind == "feishu" && ans.Code != nil && *ans.Code != 0 {
			return fmt.Errorf("it answered %d: %s", *ans.Code, ans.Msg)
		}
		if (c.Kind == "dingtalk" || c.Kind == "wecom") && ans.ErrCode != nil && *ans.ErrCode != 0 {
			return fmt.Errorf("it answered %d: %s", *ans.ErrCode, ans.ErrMsg)
		}
	}
	return nil
}

func snippet(b []byte) string {
	t := strings.TrimSpace(string(b))
	if t == "" {
		return ""
	}
	return ": " + cut(t, 200)
}
