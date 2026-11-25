package server

import (
	"encoding/csv"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/xavier-niu/paylist/internal/store"
)

// ---------------------------------------------------------------- entries

// entryFilter reads ?month=YYYY-MM (or ?from=&to=), ?type=, ?card= and ?q=.
// With neither month nor dates, it is the current month.
func entryFilter(r *http.Request) (store.EntryFilter, string, error) {
	q := r.URL.Query()
	f := store.EntryFilter{From: q.Get("from"), To: q.Get("to"), Type: q.Get("type"), Card: q.Get("card"), Query: q.Get("q")}
	month := q.Get("month")
	if month == "" && f.From == "" && f.To == "" {
		month = time.Now().Format("2006-01")
	}
	if month != "" {
		from, to, err := store.MonthRange(month)
		if err != nil {
			return f, "", err
		}
		f.From, f.To = from, to
	}
	for _, d := range []string{f.From, f.To} {
		if d != "" {
			if _, err := store.ParseDate(d); err != nil {
				return f, "", err
			}
		}
	}
	return f, month, nil
}

func (s *Server) handleListEntries(w http.ResponseWriter, r *http.Request) {
	f, month, err := entryFilter(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, sentence(err))
		return
	}
	entries, err := s.opts.Store.ListEntries(r.Context(), currentLedger(r.Context()).ID, f)
	if err != nil {
		s.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"month": month, "entries": entries})
}

// handleSuggestEntries: ?q= part of a name; past purchases whose name holds it, one per name.
func (s *Server) handleSuggestEntries(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit == 0 {
		limit = 6
	}
	out, err := s.opts.Store.SuggestEntries(r.Context(), currentLedger(r.Context()).ID, r.URL.Query().Get("q"), limit)
	if err != nil {
		s.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleCreateEntry(w http.ResponseWriter, r *http.Request) {
	var in store.EntryInput
	if !decode(w, r, &in) {
		return
	}
	out, err := s.opts.Store.CreateEntries(r.Context(), currentLedger(r.Context()).ID, []store.EntryInput{in})
	if err != nil {
		writeError(w, http.StatusBadRequest, sentence(err))
		return
	}
	writeJSON(w, http.StatusCreated, out[0])
}

func pathID(w http.ResponseWriter, r *http.Request, what string) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusNotFound, "No such "+what+".")
		return 0, false
	}
	return id, true
}

func (s *Server) handleUpdateEntry(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "entry")
	if !ok {
		return
	}
	var in store.EntryInput
	if !decode(w, r, &in) {
		return
	}
	e, err := s.opts.Store.UpdateEntry(r.Context(), currentLedger(r.Context()).ID, id, in)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "No such entry.")
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, sentence(err))
		return
	}
	writeJSON(w, http.StatusOK, e)
}

func (s *Server) handleDeleteEntry(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "entry")
	if !ok {
		return
	}
	n, err := s.opts.Store.DeleteEntries(r.Context(), currentLedger(r.Context()).ID, []int64{id})
	if err != nil {
		s.internalError(w, err)
		return
	}
	if n == 0 {
		writeError(w, http.StatusNotFound, "No such entry.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": n})
}

func (s *Server) handleDeleteEntries(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDs []int64 `json:"ids"`
	}
	if !decode(w, r, &req) {
		return
	}
	n, err := s.opts.Store.DeleteEntries(r.Context(), currentLedger(r.Context()).ID, req.IDs)
	if err != nil {
		writeError(w, http.StatusBadRequest, sentence(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": n})
}

// handleExportCSV downloads the entries the filter selects (a month by default).
func (s *Server) handleExportCSV(w http.ResponseWriter, r *http.Request) {
	f, month, err := entryFilter(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, sentence(err))
		return
	}
	entries, err := s.opts.Store.ListEntries(r.Context(), currentLedger(r.Context()).ID, f)
	if err != nil {
		s.internalError(w, err)
		return
	}
	name := "paylist"
	if month != "" {
		name += "-" + month
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.csv"`, name))
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"Name", "Type", "Amount", "Date", "Card", "Original amount", "Original currency"})
	for _, e := range entries {
		orig := ""
		if e.OrigAmount != nil {
			orig = strconv.FormatFloat(*e.OrigAmount, 'f', 2, 64)
		}
		_ = cw.Write([]string{csvSafe(e.Name), csvSafe(e.Type), strconv.FormatFloat(e.Amount, 'f', 2, 64), e.Date, csvSafe(e.Card), orig, e.OrigCurrency})
	}
	cw.Flush()
}

// csvSafe keeps a spreadsheet from reading a cell as a formula.
func csvSafe(v string) string {
	if v != "" && strings.ContainsRune("=+-@\t\r", rune(v[0])) {
		return "'" + v
	}
	return v
}

// ---------------------------------------------------------------- options

func (s *Server) handleListOptions(w http.ResponseWriter, r *http.Request) {
	opts, err := s.opts.Store.ListOptions(r.Context(), currentLedger(r.Context()).ID)
	if err != nil {
		s.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, opts)
}

func (s *Server) handleCreateOption(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Kind  string `json:"kind"`
		Name  string `json:"name"`
		Color string `json:"color"`
	}
	if !decode(w, r, &req) {
		return
	}
	o, err := s.opts.Store.CreateOption(r.Context(), currentLedger(r.Context()).ID, req.Kind, req.Name, req.Color)
	if err != nil {
		writeError(w, http.StatusBadRequest, sentence(err))
		return
	}
	writeJSON(w, http.StatusCreated, o)
}

func (s *Server) handleUpdateOption(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "option")
	if !ok {
		return
	}
	var req struct {
		Name  *string `json:"name"`
		Color *string `json:"color"`
	}
	if !decode(w, r, &req) {
		return
	}
	o, err := s.opts.Store.UpdateOption(r.Context(), currentLedger(r.Context()).ID, id, req.Name, req.Color)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "No such option.")
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, sentence(err))
		return
	}
	writeJSON(w, http.StatusOK, o)
}

// handleReorderOptions: {kind, ids}, the ledger's types (or cards) in their new order.
func (s *Server) handleReorderOptions(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Kind string  `json:"kind"`
		IDs  []int64 `json:"ids"`
	}
	if !decode(w, r, &req) {
		return
	}
	if err := s.opts.Store.ReorderOptions(r.Context(), currentLedger(r.Context()).ID, req.Kind, req.IDs); err != nil {
		writeError(w, http.StatusBadRequest, sentence(err))
		return
	}
	out, err := s.opts.Store.ListOptions(r.Context(), currentLedger(r.Context()).ID)
	if err != nil {
		s.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleDeleteOption(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "option")
	if !ok {
		return
	}
	_, cleared, err := s.opts.Store.DeleteOption(r.Context(), currentLedger(r.Context()).ID, id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "No such option.")
		return
	}
	if err != nil {
		s.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"cleared": cleared})
}

// ---------------------------------------------------------------- statistics

// handleStats: ?month=YYYY-MM (default this month), ?months= of history (default 12).
func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	month := q.Get("month")
	if month == "" {
		month = time.Now().Format("2006-01")
	}
	months := 12
	if v := q.Get("months"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			writeError(w, http.StatusBadRequest, "months must be a number from 1 to 36.")
			return
		}
		months = n
	}
	today := time.Now()
	if t, err := store.ParseDate(q.Get("today")); err == nil {
		today = t // the browser's date, which can differ from the server's
	}
	st, err := s.opts.Store.GetStats(r.Context(), currentLedger(r.Context()).ID, month, months, today)
	if err != nil {
		writeError(w, http.StatusBadRequest, sentence(err))
		return
	}
	writeJSON(w, http.StatusOK, st)
}

// handleMonths lists every month with entries, newest first, up to ?upto=YYYY-MM
// (the browser's month; later, future-dated months are left out).
func (s *Server) handleMonths(w http.ResponseWriter, r *http.Request) {
	upTo := r.URL.Query().Get("upto")
	if upTo == "" {
		upTo = time.Now().Format("2006-01")
	}
	months, err := s.opts.Store.AllMonths(r.Context(), currentLedger(r.Context()).ID, upTo)
	if err != nil {
		writeError(w, http.StatusBadRequest, sentence(err))
		return
	}
	writeJSON(w, http.StatusOK, months)
}

// requestToday is the browser's date (?today= or "today" in the body), which
// can differ from the server's.
func requestToday(d string) time.Time {
	if t, err := store.ParseDate(d); err == nil {
		return t
	}
	return time.Now()
}

// handleRefundMatches: ?name= and/or ?amount=, the purchases a refund would undo.
func (s *Server) handleRefundMatches(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	var amount *float64
	if v := strings.TrimSpace(q.Get("amount")); v != "" {
		n, err := strconv.ParseFloat(v, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "The amount must be a number.")
			return
		}
		amount = &n
	}
	out, err := s.opts.Store.RefundMatches(r.Context(), currentLedger(r.Context()).ID, q.Get("name"), amount, requestToday(q.Get("today")), 5)
	if err != nil {
		writeError(w, http.StatusBadRequest, sentence(err))
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// handleRefund applies a refund: {id} for a picked purchase, or {name, amount}.
func (s *Server) handleRefund(w http.ResponseWriter, r *http.Request) {
	var req struct {
		store.RefundInput
		Today string `json:"today"`
	}
	if !decode(w, r, &req) {
		return
	}
	res, err := s.opts.Store.Refund(r.Context(), currentLedger(r.Context()).ID, req.RefundInput, requestToday(req.Today))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "No such entry.")
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, sentence(err))
		return
	}
	writeJSON(w, http.StatusOK, res)
}
