package server

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/xavier-niu/paylist/internal/store"
)

// Ledgers: each request on entries, types and cards, statistics and refunds
// works on one ledger, named by the X-Paylist-Ledger header (or ?ledger=, for
// links such as the CSV export); without one, the ledger used last.
//
//   GET    /api/ledgers                          the user's ledgers
//   POST   /api/ledgers                          {name, currency}: a new one
//   PUT    /api/ledgers/{lid}                    {name?, currency?}       owner
//   DELETE /api/ledgers/{lid}                                             owner
//   GET    /api/ledgers/{lid}/members                                     any member
//   POST   /api/ledgers/{lid}/members            {email, role}            owner
//   PUT    /api/ledgers/{lid}/members/{uid}      {role}                   owner
//   DELETE /api/ledgers/{lid}/members/{uid}      owner, or the member leaving

const ledgerHeader = "X-Paylist-Ledger"

const ledgerKey ctxKey = 100

func currentLedger(ctx context.Context) *store.Ledger {
	l, _ := ctx.Value(ledgerKey).(*store.Ledger)
	return l
}

// pickLedger finds the ledger to work on: the one asked for (an id, or for
// agents also a name), else the one used last, else the first; someone left
// without any gets a new personal one.
func (s *Server) pickLedger(ctx context.Context, u *store.User, want string) (*store.Ledger, error) {
	st := s.opts.Store
	want = strings.TrimSpace(want)
	if want != "" {
		if id, err := strconv.ParseInt(want, 10, 64); err == nil {
			return st.GetLedger(ctx, u.ID, id)
		}
		ls, err := st.ListLedgers(ctx, u.ID)
		if err != nil {
			return nil, err
		}
		for i := range ls {
			if strings.EqualFold(ls[i].Name, want) {
				return &ls[i], nil
			}
		}
		return nil, store.ErrNotMember
	}
	if u.Prefs.LedgerID != 0 {
		if l, err := st.GetLedger(ctx, u.ID, u.Prefs.LedgerID); err == nil {
			return l, nil
		}
	}
	ls, err := st.ListLedgers(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	if len(ls) == 0 {
		return st.CreateLedger(ctx, u.ID, "Personal", u.Prefs.Currency)
	}
	return &ls[0], nil
}

// withLedger runs a handler on the request's ledger; write says it changes it,
// which a viewer may not.
func (s *Server) withLedger(write bool, next http.HandlerFunc) http.HandlerFunc {
	return s.requireUser(func(w http.ResponseWriter, r *http.Request) {
		want := r.Header.Get(ledgerHeader)
		if want == "" {
			want = r.URL.Query().Get("ledger")
		}
		l, err := s.pickLedger(r.Context(), currentUser(r.Context()), want)
		if errors.Is(err, store.ErrNotMember) {
			writeError(w, http.StatusNotFound, "No such ledger.")
			return
		}
		if err != nil {
			s.internalError(w, err)
			return
		}
		if write && !l.CanWrite() {
			writeError(w, http.StatusForbidden, "You can only view this ledger.")
			return
		}
		// Changes made in this request are recorded as the user's.
		ctx := store.WithActor(context.WithValue(r.Context(), ledgerKey, l), currentUser(r.Context()).ID, "")
		next(w, r.WithContext(ctx))
	})
}

func (s *Server) handleListLedgers(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r.Context())
	cur, err := s.pickLedger(r.Context(), u, "")
	if err != nil {
		s.internalError(w, err)
		return
	}
	ls, err := s.opts.Store.ListLedgers(r.Context(), u.ID)
	if err != nil {
		s.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ledgers": ls, "current": cur.ID})
}

func (s *Server) handleCreateLedger(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r.Context())
	var req struct {
		Name     string `json:"name"`
		Currency string `json:"currency"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.Currency == "" {
		req.Currency = u.Prefs.Currency
	}
	l, err := s.opts.Store.CreateLedger(r.Context(), u.ID, req.Name, req.Currency)
	if err != nil {
		writeError(w, http.StatusBadRequest, sentence(err))
		return
	}
	writeJSON(w, http.StatusCreated, l)
}

// ledgerAt is the ledger in the path as the user sees it; owner says only its owner may go on.
func (s *Server) ledgerAt(w http.ResponseWriter, r *http.Request, owner bool) (*store.Ledger, bool) {
	id, err := strconv.ParseInt(r.PathValue("lid"), 10, 64)
	if err != nil {
		writeError(w, http.StatusNotFound, "No such ledger.")
		return nil, false
	}
	l, err := s.opts.Store.GetLedger(r.Context(), currentUser(r.Context()).ID, id)
	if errors.Is(err, store.ErrNotMember) {
		writeError(w, http.StatusNotFound, "No such ledger.")
		return nil, false
	}
	if err != nil {
		s.internalError(w, err)
		return nil, false
	}
	if owner && l.Role != store.RoleOwner {
		writeError(w, http.StatusForbidden, "Only the ledger's owner can do that.")
		return nil, false
	}
	return l, true
}

func (s *Server) handleUpdateLedger(w http.ResponseWriter, r *http.Request) {
	l, ok := s.ledgerAt(w, r, true)
	if !ok {
		return
	}
	var req struct {
		Name     *string `json:"name"`
		Currency *string `json:"currency"`
	}
	if !decode(w, r, &req) {
		return
	}
	if err := s.opts.Store.UpdateLedger(r.Context(), l.ID, req.Name, req.Currency); err != nil {
		writeError(w, http.StatusBadRequest, sentence(err))
		return
	}
	l, err := s.opts.Store.GetLedger(r.Context(), currentUser(r.Context()).ID, l.ID)
	if err != nil {
		s.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, l)
}

func (s *Server) handleDeleteLedger(w http.ResponseWriter, r *http.Request) {
	l, ok := s.ledgerAt(w, r, true)
	if !ok {
		return
	}
	if err := s.opts.Store.DeleteLedger(r.Context(), currentUser(r.Context()).ID, l.ID); err != nil {
		if errors.Is(err, store.ErrLastLedger) {
			writeError(w, http.StatusBadRequest, sentence(err))
			return
		}
		s.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleListLedgerMembers(w http.ResponseWriter, r *http.Request) {
	l, ok := s.ledgerAt(w, r, false)
	if !ok {
		return
	}
	ms, err := s.opts.Store.ListLedgerMembers(r.Context(), l.ID)
	if err != nil {
		s.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ms)
}

func (s *Server) handleAddLedgerMember(w http.ResponseWriter, r *http.Request) {
	l, ok := s.ledgerAt(w, r, true)
	if !ok {
		return
	}
	var req struct {
		Email string `json:"email"`
		Role  string `json:"role"`
	}
	if !decode(w, r, &req) {
		return
	}
	m, err := s.opts.Store.AddLedgerMember(r.Context(), l.ID, req.Email, req.Role)
	if err != nil {
		writeError(w, http.StatusBadRequest, sentence(err))
		return
	}
	writeJSON(w, http.StatusCreated, m)
}

func memberID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("uid"), 10, 64)
	if err != nil {
		writeError(w, http.StatusNotFound, "No such member.")
		return 0, false
	}
	return id, true
}

func (s *Server) handleSetLedgerMemberRole(w http.ResponseWriter, r *http.Request) {
	l, ok := s.ledgerAt(w, r, true)
	if !ok {
		return
	}
	uid, ok := memberID(w, r)
	if !ok {
		return
	}
	var req struct {
		Role string `json:"role"`
	}
	if !decode(w, r, &req) {
		return
	}
	if err := s.opts.Store.SetLedgerMemberRole(r.Context(), l.ID, uid, req.Role); err != nil {
		writeError(w, http.StatusBadRequest, sentence(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleRemoveLedgerMember: the owner removes someone, or a member leaves.
func (s *Server) handleRemoveLedgerMember(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r.Context())
	uid, ok := memberID(w, r)
	if !ok {
		return
	}
	l, ok := s.ledgerAt(w, r, uid != u.ID)
	if !ok {
		return
	}
	if err := s.opts.Store.RemoveLedgerMember(r.Context(), l.ID, uid); err != nil {
		writeError(w, http.StatusBadRequest, sentence(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
