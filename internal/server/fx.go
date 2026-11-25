package server

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strings"
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

// inLedgerCurrency gives an amount paid in cur as the ledger's, at date's
// rate (today's for an empty date), and what was paid, to keep beside it.
// With no currency, or the ledger's own, the amount is the ledger's as it is
// and nothing more is kept: the agent's way to what the form does.
func (s *Server) inLedgerCurrency(ctx context.Context, l *store.Ledger, cur string, amount float64, date string) (float64, *float64, *string, error) {
	cur = strings.ToUpper(strings.TrimSpace(cur))
	if cur == "" || cur == l.Currency {
		return amount, nil, nil, nil
	}
	day := time.Now()
	if date != "" {
		t, err := store.ParseDate(date)
		if err != nil {
			return 0, nil, nil, err
		}
		day = t
	}
	rate, err := s.opts.FX.Get(ctx, cur, l.Currency, day)
	if err != nil {
		return 0, nil, nil, fmt.Errorf("couldn't convert %s to %s: %w", cur, l.Currency, err)
	}
	orig := amount
	return math.Round(amount*rate.Rate*100) / 100, &orig, &cur, nil
}
