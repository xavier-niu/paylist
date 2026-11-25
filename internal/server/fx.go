package server

import (
	"errors"
	"net/http"
	"time"

	"github.com/xavier-niu/paylist/internal/fx"
	"github.com/xavier-niu/paylist/internal/store"
)

// handleFX: ?from=USD&to=CNY&date=YYYY-MM-DD, the rate to convert an amount
// paid in one currency to a ledger's, for that day (today's by default). The
// form converts with it, and the entry keeps only the converted amount.
func (s *Server) handleFX(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	day := time.Now()
	if d := q.Get("date"); d != "" {
		t, err := store.ParseDate(d)
		if err != nil {
			writeError(w, http.StatusBadRequest, sentence(err))
			return
		}
		day = t
	}
	rate, err := s.opts.FX.Get(r.Context(), q.Get("from"), q.Get("to"), day)
	switch {
	case errors.Is(err, fx.ErrUnknown):
		writeError(w, http.StatusNotFound, "No exchange rate is published for this currency.")
	case errors.Is(err, fx.ErrCode):
		writeError(w, http.StatusBadRequest, sentence(err))
	case err != nil:
		s.opts.Logger.Warn("exchange rate", "err", err)
		writeError(w, http.StatusBadGateway, "Couldn't get the exchange rate. Try again.")
	default:
		writeJSON(w, http.StatusOK, rate)
	}
}
