package server

import (
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/xavier-niu/paylist/internal/llm"
	"github.com/xavier-niu/paylist/internal/store"
)

// ---------------------------------------------------------------- me, profile, password

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r.Context())
	model, err := s.opts.Store.GetModelSettings(r.Context(), u.ID)
	if err != nil {
		s.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": u, "model": model})
}

func (s *Server) handlePrefs(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r.Context())
	var req store.Prefs
	if !decode(w, r, &req) {
		return
	}
	p := u.Prefs
	if c := strings.ToUpper(strings.TrimSpace(req.Currency)); currencyRe.MatchString(c) {
		p.Currency = c
	}
	if req.Theme == "system" || req.Theme == "light" || req.Theme == "dark" {
		p.Theme = req.Theme
	}
	if req.Language == "system" || req.Language == "en" || req.Language == "zh" {
		p.Language = req.Language
	}
	p, err := s.opts.Store.UpdatePrefs(r.Context(), u.ID, p)
	if err != nil {
		s.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

var currencyRe = regexp.MustCompile(`^[A-Z]{3}$`)

func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r.Context())
	var req struct {
		Current string `json:"current"`
		New     string `json:"new"`
	}
	if !decode(w, r, &req) {
		return
	}
	if _, err := s.opts.Store.Authenticate(r.Context(), u.Email, req.Current); err != nil {
		writeError(w, http.StatusBadRequest, "Your current password is incorrect.")
		return
	}
	if err := s.opts.Store.SetPassword(r.Context(), u.ID, req.New, currentToken(r.Context())); err != nil {
		writeError(w, http.StatusBadRequest, sentence(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ---------------------------------------------------------------- model settings

type modelRequest struct {
	Preset  string  `json:"preset"`
	Kind    string  `json:"kind"`
	BaseURL string  `json:"base_url"`
	Model   string  `json:"model"`
	APIKey  *string `json:"api_key"` // omitted: keep the stored key; "": remove it
}

func (s *Server) handleSaveModel(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r.Context())
	var req modelRequest
	if !decode(w, r, &req) {
		return
	}
	switch req.Kind {
	case "openai", "anthropic":
	case "fake":
		if !s.opts.AllowFake {
			writeError(w, http.StatusBadRequest, "The fake provider is not enabled on this server.")
			return
		}
	default:
		writeError(w, http.StatusBadRequest, "Choose a provider.")
		return
	}
	conf := store.ProviderConf{Kind: req.Kind, BaseURL: strings.TrimSpace(req.BaseURL), Model: strings.TrimSpace(req.Model)}
	ms, err := s.opts.Store.SaveProvider(r.Context(), u.ID, req.Preset, conf, req.APIKey)
	if err != nil {
		writeError(w, http.StatusBadRequest, sentence(err))
		return
	}
	writeJSON(w, http.StatusOK, ms)
}

// llmClient builds a client from the signed-in user's saved provider and key.
func (s *Server) llmClient(r *http.Request) (llm.Client, llm.Config, error) {
	u := currentUser(r.Context())
	_, conf, key, err := s.opts.Store.ActiveProvider(r.Context(), u.ID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, llm.Config{}, &llm.Error{Msg: "Choose an AI model and add your API key in Settings first.", Status: http.StatusBadRequest}
	}
	if err != nil {
		return nil, llm.Config{}, &llm.Error{Msg: sentence(err), Status: http.StatusInternalServerError}
	}
	cfg := llm.Config{Kind: conf.Kind, BaseURL: conf.BaseURL, Model: conf.Model, APIKey: key}
	client, err := llm.New(cfg, s.opts.AllowFake, s.http)
	return client, cfg, err
}

// ---------------------------------------------------------------- admin: accounts

type userRequest struct {
	Email    string `json:"email"`
	Name     string `json:"name"`
	Password string `json:"password"`
	IsAdmin  bool   `json:"is_admin"`
}

func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.opts.Store.ListUsers(r.Context())
	if err != nil {
		s.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, users)
}

func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	var req userRequest
	if !decode(w, r, &req) {
		return
	}
	u, err := s.opts.Store.CreateUser(r.Context(), req.Email, req.Name, req.Password, req.IsAdmin)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, store.ErrEmailTaken) {
			status = http.StatusConflict
		}
		writeError(w, status, sentence(err))
		return
	}
	writeJSON(w, http.StatusCreated, u)
}

func pathUserID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusNotFound, "No such account.")
		return 0, false
	}
	return id, true
}

func (s *Server) handleResetPassword(w http.ResponseWriter, r *http.Request) {
	id, ok := pathUserID(w, r)
	if !ok {
		return
	}
	var req struct {
		Password string `json:"password"`
	}
	if !decode(w, r, &req) {
		return
	}
	keep := ""
	if id == currentUser(r.Context()).ID {
		keep = currentToken(r.Context())
	}
	if err := s.opts.Store.SetPassword(r.Context(), id, req.Password, keep); err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, store.ErrNotFound) {
			status = http.StatusNotFound
		}
		writeError(w, status, sentence(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	id, ok := pathUserID(w, r)
	if !ok {
		return
	}
	if id == currentUser(r.Context()).ID {
		writeError(w, http.StatusBadRequest, "You can't delete your own account while signed in to it.")
		return
	}
	if err := s.opts.Store.DeleteUser(r.Context(), id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "No such account.")
			return
		}
		s.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ---------------------------------------------------------------- agent tokens (MCP)

func (s *Server) handleListTokens(w http.ResponseWriter, r *http.Request) {
	list, err := s.opts.Store.ListAgentTokens(r.Context(), currentUser(r.Context()).ID)
	if err != nil {
		s.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleCreateToken(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if !decode(w, r, &req) {
		return
	}
	token, rec, err := s.opts.Store.CreateAgentToken(r.Context(), currentUser(r.Context()).ID, req.Name)
	if errors.Is(err, store.ErrTooManyTokens) {
		writeError(w, http.StatusBadRequest, sentence(err))
		return
	}
	if err != nil {
		s.internalError(w, err)
		return
	}
	// The only time the token is ever sent: the browser shows it once.
	writeJSON(w, http.StatusCreated, map[string]any{"token": token, "info": rec, "endpoint": s.publicURL(r) + "/mcp"})
}

func (s *Server) handleDeleteToken(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err == nil {
		err = s.opts.Store.DeleteAgentToken(r.Context(), currentUser(r.Context()).ID, id)
	}
	if err != nil {
		writeError(w, http.StatusNotFound, "That token doesn't exist.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
