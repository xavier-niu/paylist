package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/xavier-niu/paylist/internal/agent"
	"github.com/xavier-niu/paylist/internal/llm"
	"github.com/xavier-niu/paylist/internal/store"
)

// The agent page: the user talks to a model (their own provider and key) that
// works on their table through Paylist's MCP tools. Each turn connects an MCP
// client to a server for this user over an in-memory transport, so the
// assistant has exactly the tools external agents get at /mcp.

const (
	maxAgentMessageChars = 8000
	maxChatHistory       = 400 // messages in one conversation

	// Files sent with a message: receipts, screenshots, statements, exports.
	maxAttachments    = 5
	maxImageBytes     = 5 << 20  // what Claude takes for one image
	maxFileBytes      = 10 << 20 // a PDF or a text file
	maxAgentTurnBytes = 40 << 20 // the request, files included (base64 adds a third)
)

var chatIDRe = regexp.MustCompile(`^[a-f0-9]{8,64}$`)

func (s *Server) handleListChats(w http.ResponseWriter, r *http.Request) {
	chats, err := s.opts.Store.ListChats(r.Context(), currentUser(r.Context()).ID)
	if err != nil {
		s.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, chats)
}

func (s *Server) handleGetChat(w http.ResponseWriter, r *http.Request) {
	c, err := s.opts.Store.GetChat(r.Context(), currentUser(r.Context()).ID, r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "No such conversation.")
		return
	}
	if err != nil {
		s.internalError(w, err)
		return
	}
	var msgs []llm.Message
	_ = json.Unmarshal(c.Messages, &msgs)
	for i := range msgs {
		msgs[i].Raw = nil // the provider's own copy: not needed to show it
	}
	writeJSON(w, http.StatusOK, map[string]any{"chat": c.ChatSummary, "messages": msgs})
}

func (s *Server) handleDeleteChat(w http.ResponseWriter, r *http.Request) {
	if err := s.opts.Store.DeleteChat(r.Context(), currentUser(r.Context()).ID, r.PathValue("id")); err != nil {
		writeError(w, http.StatusNotFound, "No such conversation.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// attachmentTypes are the files the agent takes, by MIME type.
var attachmentTypes = map[string]bool{
	"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true,
	"application/pdf": true,
	"text/plain":      true, "text/csv": true, "text/tab-separated-values": true, "text/markdown": true, "application/json": true,
}

// checkAttachments vets the files sent with a message: what they are, how big,
// and for text, that it is text.
func checkAttachments(in []llm.Attachment) ([]llm.Attachment, error) {
	if len(in) > maxAttachments {
		return nil, fmt.Errorf("Send at most %d files at a time.", maxAttachments)
	}
	out := make([]llm.Attachment, 0, len(in))
	for _, a := range in {
		a.Name = strings.TrimSpace(a.Name)
		if a.Name == "" {
			a.Name = "file"
		}
		if utf8.RuneCountInString(a.Name) > 200 {
			a.Name = string([]rune(a.Name)[:200])
		}
		a.Type = strings.ToLower(strings.TrimSpace(strings.SplitN(a.Type, ";", 2)[0]))
		if !attachmentTypes[a.Type] {
			return nil, fmt.Errorf("%s: the agent takes images (PNG, JPEG, GIF, WebP), PDFs, and text files (TXT, CSV, TSV, Markdown, JSON).", a.Name)
		}
		raw, err := base64.StdEncoding.DecodeString(a.Data)
		if err != nil || len(raw) == 0 {
			return nil, fmt.Errorf("%s: the file couldn't be read.", a.Name)
		}
		limit := maxFileBytes
		if a.Kind() == "image" {
			limit = maxImageBytes
		}
		if len(raw) > limit {
			return nil, fmt.Errorf("%s is larger than %d MB.", a.Name, limit>>20)
		}
		if a.Kind() == "text" && !utf8.Valid(raw) {
			return nil, fmt.Errorf("%s isn't a text file.", a.Name)
		}
		out = append(out, a)
	}
	return out, nil
}

// handleAgentChat runs one turn: {chat_id?, message, attachments?, today?} → NDJSON events:
//
//	{"type":"chat","id","title"}            the conversation (new or continued)
//	{"type":"text","text"}                  a fragment of the reply
//	{"type":"tool_call","call"}             a tool the model called…
//	{"type":"tool_result","result"}         …and what it returned
//	{"type":"notice","text"}                the reply was cut short, declined, …
//	{"type":"error","error","status"}       the turn failed
//	{"type":"done"}
func (s *Server) handleAgentChat(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r.Context())
	var req struct {
		ChatID      string           `json:"chat_id"`
		Message     string           `json:"message"`
		Attachments []llm.Attachment `json:"attachments"`
		Today       string           `json:"today"`
	}
	if !decodeUpTo(w, r, &req, maxAgentTurnBytes) {
		return
	}
	req.Message = strings.TrimSpace(req.Message)
	files, err := checkAttachments(req.Attachments)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if (req.Message == "" && len(files) == 0) || utf8.RuneCountInString(req.Message) > maxAgentMessageChars {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("Write a message of 1–%d characters.", maxAgentMessageChars))
		return
	}
	client, _, err := s.llmClient(r)
	if err != nil {
		writeLLMError(w, err)
		return
	}

	var history []llm.Message
	id, title := req.ChatID, req.Message
	if title == "" {
		title = files[0].Name
	}
	if id != "" && !chatIDRe.MatchString(id) {
		writeError(w, http.StatusNotFound, "No such conversation. Start a new one.")
		return
	}
	if id != "" {
		c, err := s.opts.Store.GetChat(r.Context(), u.ID, id)
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "No such conversation. Start a new one.")
			return
		}
		if err != nil {
			s.internalError(w, err)
			return
		}
		if err := json.Unmarshal(c.Messages, &history); err != nil {
			s.internalError(w, err)
			return
		}
		title = c.Title
	} else {
		id = store.NewChatID()
	}
	if len(history) >= maxChatHistory {
		writeError(w, http.StatusBadRequest, "This conversation is very long. Start a new one.")
		return
	}
	history = append(agent.Complete(history), llm.Message{Role: "user", Text: req.Message, Attachments: files})

	today := time.Now()
	if t, err := store.ParseDate(req.Today); err == nil {
		today = t
	}

	session, closeSession, err := s.agentSession(r.Context(), u, currentLedger(r.Context()))
	if err != nil {
		s.internalError(w, err)
		return
	}
	defer closeSession()

	nd := newNDJSON(w)
	_ = nd.send(map[string]any{"type": "chat", "id": id, "title": title})
	// Save the question at once, so the conversation is there even if the
	// turn fails.
	save := func(h []llm.Message) {
		b, err := json.Marshal(h)
		if err == nil {
			err = s.opts.Store.SaveChat(context.WithoutCancel(r.Context()), u.ID, id, title, b)
		}
		if err != nil {
			s.opts.Logger.Error("save agent chat", "err", err)
		}
	}
	save(history)

	l := currentLedger(r.Context())
	system := agent.SystemPrompt(today, l.Name, l.Currency, l.Role)
	history, err = agent.Run(r.Context(), client, session, system, history, func(e agent.Event) error {
		return nd.send(e)
	})
	save(agent.Complete(history))
	if err != nil && r.Context().Err() == nil {
		status := http.StatusBadGateway
		var le *llm.Error
		if errors.As(err, &le) {
			status = le.Status
		}
		_ = nd.send(map[string]any{"type": "error", "error": err.Error(), "status": status})
	}
	_ = nd.send(map[string]any{"type": "done"})
}

// agentSession connects an MCP client to a server whose tools work on u's table.
func (s *Server) agentSession(ctx context.Context, u *store.User, l *store.Ledger) (*mcp.ClientSession, func(), error) {
	// The agent works on the ledger open on the page, unless asked about another.
	def := strconv.FormatInt(l.ID, 10)
	srv := s.newMCPServer(func(*mcp.CallToolRequest) (*store.User, string, error) { return u, def, nil })
	clientT, serverT := mcp.NewInMemoryTransports()
	ss, err := srv.Connect(ctx, serverT, nil)
	if err != nil {
		return nil, nil, err
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "paylist-agent", Version: "1.0.0"}, nil)
	cs, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		ss.Close()
		return nil, nil, err
	}
	return cs, func() { cs.Close(); ss.Close() }, nil
}

func writeLLMError(w http.ResponseWriter, err error) {
	var le *llm.Error
	if errors.As(err, &le) {
		writeError(w, le.Status, le.Msg)
		return
	}
	writeError(w, http.StatusBadGateway, err.Error())
}
