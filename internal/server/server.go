// Package server exposes the HTTP API, the MCP server and the web UI.
package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/xavier-niu/paylist/internal/store"
)

const maxBodyBytes = 2 << 20 // 2 MiB

type Options struct {
	Store         *store.Store // users, sessions and their tables
	Static        fs.FS        // web UI files
	AllowFake     bool         // enable the offline "fake" model (demos and tests)
	SecureCookies bool         // mark the session cookie Secure (set when served over HTTPS)
	// PublicURL is how people reach the app (e.g. https://pay.example.com); shown
	// as the MCP address. Worked out from each request when empty.
	PublicURL string
	Logger    *slog.Logger
}

type Server struct {
	opts    Options
	http    *http.Client
	limiter *loginLimiter
}

func New(opts Options) *Server {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	return &Server{
		opts:    opts,
		limiter: newLoginLimiter(),
		// No overall timeout: agent replies stream. Contexts bound each request.
		http: &http.Client{Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			ResponseHeaderTimeout: 5 * time.Minute,
			IdleConnTimeout:       90 * time.Second,
			MaxIdleConnsPerHost:   16,
		}},
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	})
	mux.HandleFunc("GET /api/config", s.handleConfig)

	// Accounts
	mux.HandleFunc("POST /api/auth/login", s.handleLogin)
	mux.HandleFunc("POST /api/auth/logout", s.handleLogout)
	mux.HandleFunc("POST /api/auth/setup", s.handleSetup)
	mux.HandleFunc("GET /api/me", s.requireUser(s.handleMe))
	mux.HandleFunc("PUT /api/me/prefs", s.requireUser(s.handlePrefs))
	mux.HandleFunc("PUT /api/me/password", s.requireUser(s.handleChangePassword))
	mux.HandleFunc("PUT /api/me/model", s.requireUser(s.handleSaveModel))
	mux.HandleFunc("GET /api/me/tokens", s.requireUser(s.handleListTokens))
	mux.HandleFunc("POST /api/me/tokens", s.requireUser(s.handleCreateToken))
	mux.HandleFunc("DELETE /api/me/tokens/{id}", s.requireUser(s.handleDeleteToken))
	mux.HandleFunc("GET /api/admin/users", s.requireAdmin(s.handleListUsers))
	mux.HandleFunc("POST /api/admin/users", s.requireAdmin(s.handleCreateUser))
	mux.HandleFunc("PUT /api/admin/users/{id}/password", s.requireAdmin(s.handleResetPassword))
	mux.HandleFunc("DELETE /api/admin/users/{id}", s.requireAdmin(s.handleDeleteUser))

	// Ledgers, and who has them
	mux.HandleFunc("GET /api/ledgers", s.requireUser(s.handleListLedgers))
	mux.HandleFunc("POST /api/ledgers", s.requireUser(s.handleCreateLedger))
	mux.HandleFunc("PUT /api/ledgers/{lid}", s.requireUser(s.handleUpdateLedger))
	mux.HandleFunc("DELETE /api/ledgers/{lid}", s.requireUser(s.handleDeleteLedger))
	mux.HandleFunc("GET /api/ledgers/{lid}/members", s.requireUser(s.handleListLedgerMembers))
	mux.HandleFunc("POST /api/ledgers/{lid}/members", s.requireUser(s.handleAddLedgerMember))
	mux.HandleFunc("PUT /api/ledgers/{lid}/members/{uid}", s.requireUser(s.handleSetLedgerMemberRole))
	mux.HandleFunc("DELETE /api/ledgers/{lid}/members/{uid}", s.requireUser(s.handleRemoveLedgerMember))

	// A ledger's entries, types and cards, statistics and refunds (the ledger is the request's; true: it changes it)
	mux.HandleFunc("GET /api/entries", s.withLedger(false, s.handleListEntries))
	mux.HandleFunc("GET /api/entries/suggest", s.withLedger(false, s.handleSuggestEntries))
	mux.HandleFunc("POST /api/entries", s.withLedger(true, s.handleCreateEntry))
	mux.HandleFunc("POST /api/entries/delete", s.withLedger(true, s.handleDeleteEntries))
	mux.HandleFunc("PUT /api/entries/{id}", s.withLedger(true, s.handleUpdateEntry))
	mux.HandleFunc("DELETE /api/entries/{id}", s.withLedger(true, s.handleDeleteEntry))
	mux.HandleFunc("GET /api/export.csv", s.withLedger(false, s.handleExportCSV))
	mux.HandleFunc("GET /api/options", s.withLedger(false, s.handleListOptions))
	mux.HandleFunc("POST /api/options", s.withLedger(true, s.handleCreateOption))
	mux.HandleFunc("PUT /api/options/{id}", s.withLedger(true, s.handleUpdateOption))
	mux.HandleFunc("DELETE /api/options/{id}", s.withLedger(true, s.handleDeleteOption))
	mux.HandleFunc("GET /api/stats", s.withLedger(false, s.handleStats))
	mux.HandleFunc("GET /api/months", s.withLedger(false, s.handleMonths))
	mux.HandleFunc("GET /api/refund/matches", s.withLedger(false, s.handleRefundMatches))
	mux.HandleFunc("POST /api/refund", s.withLedger(true, s.handleRefund))

	// The agent page: conversations, and a turn run with the user's own key
	mux.HandleFunc("GET /api/chats", s.requireUser(s.handleListChats))
	mux.HandleFunc("GET /api/chats/{id}", s.requireUser(s.handleGetChat))
	mux.HandleFunc("DELETE /api/chats/{id}", s.requireUser(s.handleDeleteChat))
	mux.HandleFunc("POST /api/agent/chat", s.withLedger(false, s.handleAgentChat))

	// The table as an MCP server (Streamable HTTP), for agents that speak MCP.
	mcpHandler := s.mcpHandler()
	for _, method := range []string{"POST", "GET", "DELETE"} {
		mux.Handle(method+" /mcp", mcpHandler)
	}

	if s.opts.Static != nil {
		mux.Handle("GET /", http.FileServerFS(s.opts.Static))
	}
	return s.logRequests(csrfGuard(mux))
}

func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		if strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/mcp" {
			s.opts.Logger.Info("request", "method", r.Method, "path", r.URL.Path, "dur", time.Since(start).Round(time.Millisecond))
		}
	})
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"allow_fake":  s.opts.AllowFake,
		"needs_setup": s.needsSetup(r),
		"colors":      store.Colors,
	})
}

// needsSetup reports whether no accounts exist yet, so the sign-in page offers
// first-run setup instead.
func (s *Server) needsSetup(r *http.Request) bool {
	n, err := s.opts.Store.CountUsers(r.Context())
	return err == nil && n == 0
}

// publicURL is how people reach this server.
func (s *Server) publicURL(r *http.Request) string {
	if s.opts.PublicURL != "" {
		return strings.TrimRight(s.opts.PublicURL, "/")
	}
	scheme := "http"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s", scheme, r.Host)
}

// ---------------------------------------------------------------- helpers

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	return decodeUpTo(w, r, v, maxBodyBytes)
}

// decodeUpTo is decode for a body of up to limit bytes.
func decodeUpTo(w http.ResponseWriter, r *http.Request, v any, limit int64) bool {
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			writeError(w, http.StatusRequestEntityTooLarge, "Request is too large.")
		} else {
			writeError(w, http.StatusBadRequest, "Invalid JSON body.")
		}
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{"error": msg})
}

// sentence capitalises an error for display ("password must be…" -> "Password must be….").
func sentence(err error) string {
	msg := err.Error()
	if msg == "" {
		return msg
	}
	msg = strings.ToUpper(msg[:1]) + msg[1:]
	if !strings.HasSuffix(msg, ".") && !strings.HasSuffix(msg, "?") {
		msg += "."
	}
	return msg
}

// ndjson streams newline-delimited JSON, flushing after each line.
type ndjson struct {
	w       http.ResponseWriter
	rc      *http.ResponseController
	started bool
}

func newNDJSON(w http.ResponseWriter) *ndjson {
	return &ndjson{w: w, rc: http.NewResponseController(w)}
}

func (n *ndjson) send(v any) error {
	if !n.started {
		n.w.Header().Set("Content-Type", "application/x-ndjson; charset=utf-8")
		n.w.Header().Set("Cache-Control", "no-store")
		n.w.Header().Set("X-Accel-Buffering", "no")
		n.w.WriteHeader(http.StatusOK)
		n.started = true
	}
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if _, err := n.w.Write(append(b, '\n')); err != nil {
		return err
	}
	return n.rc.Flush()
}
