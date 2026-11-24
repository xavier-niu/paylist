package server

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/xavier-niu/paylist/internal/store"
)

// MCP: the spending ledger as a Model Context Protocol server. External agents
// (Claude Code, Codex, Cursor, …) reach it at /mcp with an agent token; the
// agent page's assistant reaches the same tools through an in-memory session
// (agentchat.go).
//
//   POST /mcp    Authorization: Bearer plt_…   (Settings → Agent access)
//
// Streamable HTTP, without sessions: each request is a whole exchange,
// answered with JSON.

const mcpInstructions = `Paylist is the user's personal spending ledger. Each entry has a name, a type, an amount, a date (YYYY-MM-DD) and the card it was paid with; type and card are choices from lists of options. The ledger is kept month by month. get_profile gives the user's currency and today's date.

Read it with list_entries (one month, a date range, a type, a card or a name), get_stats (a month's totals by day, by type and by card, and the months before) and list_months (every month with entries). Change it with add_entries (several at once), update_entry and delete_entries. list_options shows the types and cards; manage_option creates, renames, recolors or deletes one.

When adding entries, reuse an existing type and card whose name fits before making new ones; when the user names something without its amount, type or card, suggest_entries shows how it was entered before. For a refund, use refund: it deletes the purchase it undoes, or records the refund as a negative amount when no purchase matches. Delete only what the user asked to delete.`

// The user whose ledger a tool works on.
type mcpCallerFunc func(req *mcp.CallToolRequest) (*store.User, error)

type mcpListIn struct {
	Month string `json:"month,omitempty" jsonschema:"the month to list, YYYY-MM; the current month when neither month nor from/to is given"`
	From  string `json:"from,omitempty" jsonschema:"first date to include, YYYY-MM-DD (instead of month)"`
	To    string `json:"to,omitempty" jsonschema:"last date to include, YYYY-MM-DD (instead of month)"`
	Type  string `json:"type,omitempty" jsonschema:"only entries of this type"`
	Card  string `json:"card,omitempty" jsonschema:"only entries paid with this card"`
	Query string `json:"query,omitempty" jsonschema:"only entries whose name contains this"`
	Limit int    `json:"limit,omitempty" jsonschema:"at most this many entries (default 500)"`
}

type mcpListOut struct {
	Entries []store.Entry `json:"entries" jsonschema:"the entries, oldest first"`
	Count   int           `json:"count"`
	Total   float64       `json:"total" jsonschema:"the sum of their amounts"`
	From    string        `json:"from,omitempty"`
	To      string        `json:"to,omitempty"`
}

type mcpNewEntry struct {
	Name   string  `json:"name" jsonschema:"what was paid for"`
	Amount float64 `json:"amount" jsonschema:"how much, in the user's currency; negative for a refund"`
	Date   string  `json:"date,omitempty" jsonschema:"YYYY-MM-DD; today when left out"`
	Type   string  `json:"type,omitempty" jsonschema:"a type option, such as Food; a new name creates the option"`
	Card   string  `json:"card,omitempty" jsonschema:"a card option, such as Cash; a new name creates the option"`
}

type mcpAddIn struct {
	Entries []mcpNewEntry `json:"entries" jsonschema:"the entries to add (1 to 500)"`
}

type mcpAddOut struct {
	Added []store.Entry `json:"added"`
}

type mcpUpdateIn struct {
	ID     int64    `json:"id" jsonschema:"the entry's id, from list_entries"`
	Name   *string  `json:"name,omitempty" jsonschema:"a new name"`
	Amount *float64 `json:"amount,omitempty" jsonschema:"a new amount"`
	Date   *string  `json:"date,omitempty" jsonschema:"a new date, YYYY-MM-DD"`
	Type   *string  `json:"type,omitempty" jsonschema:"a new type; empty for none"`
	Card   *string  `json:"card,omitempty" jsonschema:"a new card; empty for none"`
}

type mcpDeleteIn struct {
	IDs []int64 `json:"ids" jsonschema:"the ids of the entries to delete"`
}

type mcpDeleteOut struct {
	Deleted int64 `json:"deleted" jsonschema:"how many entries were deleted"`
}

type mcpOptionIn struct {
	Kind    string `json:"kind" jsonschema:"type or card"`
	Action  string `json:"action" jsonschema:"create, rename, recolor or delete"`
	Name    string `json:"name" jsonschema:"the option's name (for create, the new option's)"`
	NewName string `json:"new_name,omitempty" jsonschema:"rename: its new name, which every entry with it gets too"`
	Color   string `json:"color,omitempty" jsonschema:"create or recolor: gray, brown, orange, yellow, green, blue, purple, pink or red"`
}

type mcpOptionOut struct {
	Option  *store.Option `json:"option,omitempty"`
	Cleared int64         `json:"cleared,omitempty" jsonschema:"delete: how many entries lost this option"`
}

type mcpSuggestIn struct {
	Query string `json:"query" jsonschema:"part of a name, such as coffee"`
	Limit int    `json:"limit,omitempty" jsonschema:"at most this many names (default 6, up to 20)"`
}

type mcpSuggestOut struct {
	Suggestions []store.Suggestion `json:"suggestions" jsonschema:"names used before, each with its latest type, card, amount and date, and how many entries have it; names starting with the query first, then the most used"`
}

type mcpMonthsOut struct {
	Months []store.MonthTotal `json:"months" jsonschema:"every month with entries up to this one, newest first, with its total and count"`
}

type mcpRefundIn struct {
	ID     int64    `json:"id,omitempty" jsonschema:"a purchase's id (from list_entries, or from the matches of an earlier call), to refund that one"`
	Name   string   `json:"name,omitempty" jsonschema:"what was refunded, or part of its name"`
	Amount *float64 `json:"amount,omitempty" jsonschema:"how much came back"`
	Date   string   `json:"date,omitempty" jsonschema:"YYYY-MM-DD, for a refund that has to be added because no purchase matches; today when left out"`
}

type mcpProfileOut struct {
	Name     string `json:"name,omitempty"`
	Currency string `json:"currency" jsonschema:"the ISO 4217 code amounts are in, such as CNY or USD"`
	Language string `json:"language" jsonschema:"en, zh, or system (the browser's)"`
	Today    string `json:"today" jsonschema:"today's date, YYYY-MM-DD"`
	Weekday  string `json:"weekday"`
}

type mcpStatsIn struct {
	Month  string `json:"month,omitempty" jsonschema:"the month, YYYY-MM; the current month when left out"`
	Months int    `json:"months,omitempty" jsonschema:"how many months of monthly totals to include, up to this one (1 to 36, default 12)"`
}

// newMCPServer builds the MCP server whose tools work on the ledger of the user
// caller returns.
func (s *Server) newMCPServer(caller mcpCallerFunc) *mcp.Server {
	srv := mcp.NewServer(&mcp.Implementation{Name: "paylist", Title: "Paylist", Version: "1.0.0"},
		&mcp.ServerOptions{Instructions: mcpInstructions})
	yes, no := true, false
	st := s.opts.Store

	mcp.AddTool(srv, &mcp.Tool{
		Name: "list_entries",
		Description: "List entries of the spending ledger, oldest first, with their count and total: one month (the current one by default) " +
			"or a date range, optionally only one type, one card or names containing a query.",
		Annotations: &mcp.ToolAnnotations{Title: "List entries", ReadOnlyHint: true, OpenWorldHint: &no},
	}, func(ctx context.Context, req *mcp.CallToolRequest, in mcpListIn) (*mcp.CallToolResult, mcpListOut, error) {
		u, err := caller(req)
		if err != nil {
			return nil, mcpListOut{}, err
		}
		f := store.EntryFilter{From: in.From, To: in.To, Type: in.Type, Card: in.Card, Query: in.Query, Limit: in.Limit}
		if in.Month == "" && f.From == "" && f.To == "" {
			in.Month = time.Now().Format("2006-01")
		}
		if in.Month != "" {
			if f.From, f.To, err = store.MonthRange(in.Month); err != nil {
				return nil, mcpListOut{}, errors.New(sentence(err))
			}
		}
		if f.Limit <= 0 || f.Limit > 500 {
			f.Limit = 500
		}
		entries, err := st.ListEntries(ctx, u.ID, f)
		if err != nil {
			return nil, mcpListOut{}, err
		}
		out := mcpListOut{Entries: entries, Count: len(entries), From: f.From, To: f.To}
		var cents int64
		for _, e := range entries {
			cents += int64(math.Round(e.Amount * 100))
		}
		out.Total = float64(cents) / 100
		return nil, out, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name: "add_entries",
		Description: "Add entries to the spending ledger, all in one go (if one is invalid, none is added). " +
			"A type or card that isn't an option yet becomes one, so reuse existing names (list_options) where they fit.",
		Annotations: &mcp.ToolAnnotations{Title: "Add entries", DestructiveHint: &no, OpenWorldHint: &no},
	}, func(ctx context.Context, req *mcp.CallToolRequest, in mcpAddIn) (*mcp.CallToolResult, mcpAddOut, error) {
		u, err := caller(req)
		if err != nil {
			return nil, mcpAddOut{}, err
		}
		today := time.Now().Format("2006-01-02")
		ins := make([]store.EntryInput, 0, len(in.Entries))
		for _, e := range in.Entries {
			e := e
			if strings.TrimSpace(e.Date) == "" {
				e.Date = today
			}
			ins = append(ins, store.EntryInput{Name: &e.Name, Amount: &e.Amount, Date: &e.Date, Type: &e.Type, Card: &e.Card})
		}
		added, err := st.CreateEntries(ctx, u.ID, ins)
		if err != nil {
			return nil, mcpAddOut{}, errors.New(sentence(err))
		}
		return nil, mcpAddOut{Added: added}, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "update_entry",
		Description: "Change an entry of the spending ledger: only the fields given change. An empty type or card clears it.",
		Annotations: &mcp.ToolAnnotations{Title: "Update an entry", DestructiveHint: &yes, IdempotentHint: true, OpenWorldHint: &no},
	}, func(ctx context.Context, req *mcp.CallToolRequest, in mcpUpdateIn) (*mcp.CallToolResult, store.Entry, error) {
		u, err := caller(req)
		if err != nil {
			return nil, store.Entry{}, err
		}
		e, err := st.UpdateEntry(ctx, u.ID, in.ID, store.EntryInput{Name: in.Name, Amount: in.Amount, Date: in.Date, Type: in.Type, Card: in.Card})
		if errors.Is(err, store.ErrNotFound) {
			return nil, store.Entry{}, fmt.Errorf("there is no entry %d", in.ID)
		}
		if err != nil {
			return nil, store.Entry{}, errors.New(sentence(err))
		}
		return nil, *e, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "delete_entries",
		Description: "Delete entries from the spending ledger by id. This can't be undone.",
		Annotations: &mcp.ToolAnnotations{Title: "Delete entries", DestructiveHint: &yes, IdempotentHint: true, OpenWorldHint: &no},
	}, func(ctx context.Context, req *mcp.CallToolRequest, in mcpDeleteIn) (*mcp.CallToolResult, mcpDeleteOut, error) {
		u, err := caller(req)
		if err != nil {
			return nil, mcpDeleteOut{}, err
		}
		n, err := st.DeleteEntries(ctx, u.ID, in.IDs)
		if err != nil {
			return nil, mcpDeleteOut{}, errors.New(sentence(err))
		}
		return nil, mcpDeleteOut{Deleted: n}, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_options",
		Description: "List the options for an entry's type and card, with their colors and how many entries use each.",
		Annotations: &mcp.ToolAnnotations{Title: "List options", ReadOnlyHint: true, OpenWorldHint: &no},
	}, func(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, store.Options, error) {
		u, err := caller(req)
		if err != nil {
			return nil, store.Options{}, err
		}
		opts, err := st.ListOptions(ctx, u.ID)
		return nil, opts, err
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name: "manage_option",
		Description: "Create, rename, recolor or delete a type or card option. Renaming renames it on every entry; " +
			"deleting clears it from the entries that had it.",
		Annotations: &mcp.ToolAnnotations{Title: "Manage an option", DestructiveHint: &yes, OpenWorldHint: &no},
	}, func(ctx context.Context, req *mcp.CallToolRequest, in mcpOptionIn) (*mcp.CallToolResult, mcpOptionOut, error) {
		u, err := caller(req)
		if err != nil {
			return nil, mcpOptionOut{}, err
		}
		if in.Action == "create" {
			o, err := st.CreateOption(ctx, u.ID, in.Kind, in.Name, in.Color)
			if err != nil {
				return nil, mcpOptionOut{}, errors.New(sentence(err))
			}
			return nil, mcpOptionOut{Option: o}, nil
		}
		o, err := st.FindOption(ctx, u.ID, in.Kind, in.Name)
		if errors.Is(err, store.ErrNotFound) {
			return nil, mcpOptionOut{}, fmt.Errorf("there is no %s option called %q", in.Kind, in.Name)
		}
		if err != nil {
			return nil, mcpOptionOut{}, err
		}
		switch in.Action {
		case "rename":
			o, err = st.UpdateOption(ctx, u.ID, o.ID, &in.NewName, nil)
		case "recolor":
			o, err = st.UpdateOption(ctx, u.ID, o.ID, nil, &in.Color)
		case "delete":
			var cleared int64
			if _, cleared, err = st.DeleteOption(ctx, u.ID, o.ID); err == nil {
				return nil, mcpOptionOut{Cleared: cleared}, nil
			}
		default:
			return nil, mcpOptionOut{}, fmt.Errorf("action is %q: use create, rename, recolor or delete", in.Action)
		}
		if err != nil {
			return nil, mcpOptionOut{}, errors.New(sentence(err))
		}
		return nil, mcpOptionOut{Option: o}, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name: "get_stats",
		Description: "Spending statistics for a month (the current one by default): its total, count, daily average, " +
			"every day's total, totals by type and by card, and the totals of the months before it. For month over month, compare " +
			"to_date with prev_to_date: in the current month they cover day 1 to compare_day of each month; otherwise the whole months.",
		Annotations: &mcp.ToolAnnotations{Title: "Get statistics", ReadOnlyHint: true, OpenWorldHint: &no},
	}, func(ctx context.Context, req *mcp.CallToolRequest, in mcpStatsIn) (*mcp.CallToolResult, store.Stats, error) {
		u, err := caller(req)
		if err != nil {
			return nil, store.Stats{}, err
		}
		if in.Month == "" {
			in.Month = time.Now().Format("2006-01")
		}
		if in.Months == 0 {
			in.Months = 12
		}
		stats, err := st.GetStats(ctx, u.ID, in.Month, in.Months, time.Now())
		if err != nil {
			return nil, store.Stats{}, errors.New(sentence(err))
		}
		return nil, *stats, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_months",
		Description: "Every month that has entries, up to the current one, newest first, with its total and count.",
		Annotations: &mcp.ToolAnnotations{Title: "List months", ReadOnlyHint: true, OpenWorldHint: &no},
	}, func(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, mcpMonthsOut, error) {
		u, err := caller(req)
		if err != nil {
			return nil, mcpMonthsOut{}, err
		}
		months, err := st.AllMonths(ctx, u.ID, time.Now().Format("2006-01"))
		return nil, mcpMonthsOut{Months: months}, err
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name: "suggest_entries",
		Description: "Names used before that contain the query, one per name with how it was last entered (type, card, amount, date) " +
			"and how often. Use it to fill in what the user leaves out when adding something they've bought before.",
		Annotations: &mcp.ToolAnnotations{Title: "Suggest entries", ReadOnlyHint: true, OpenWorldHint: &no},
	}, func(ctx context.Context, req *mcp.CallToolRequest, in mcpSuggestIn) (*mcp.CallToolResult, mcpSuggestOut, error) {
		u, err := caller(req)
		if err != nil {
			return nil, mcpSuggestOut{}, err
		}
		if in.Limit <= 0 {
			in.Limit = 6
		}
		out, err := st.SuggestEntries(ctx, u.ID, in.Query, in.Limit)
		return nil, mcpSuggestOut{Suggestions: out}, err
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name: "refund",
		Description: "Record a refund the way the ledger's Refund button does. Give the purchase's id, or its name and/or amount: " +
			"a purchase from the last 120 days that matches (the name contains it, the amount is the same) is deleted and returned as refunded; " +
			"if several match, nothing changes and they come back as matches, so call again with the right id (ask the user if unsure); " +
			"if none match, the refund is added as a negative amount (this needs the amount) and returned as added.",
		Annotations: &mcp.ToolAnnotations{Title: "Refund", DestructiveHint: &yes, OpenWorldHint: &no},
	}, func(ctx context.Context, req *mcp.CallToolRequest, in mcpRefundIn) (*mcp.CallToolResult, store.RefundResult, error) {
		u, err := caller(req)
		if err != nil {
			return nil, store.RefundResult{}, err
		}
		res, err := st.Refund(ctx, u.ID, store.RefundInput{ID: in.ID, Name: in.Name, Amount: in.Amount, Date: in.Date}, time.Now())
		if errors.Is(err, store.ErrNotFound) {
			return nil, store.RefundResult{}, fmt.Errorf("there is no entry %d", in.ID)
		}
		if err != nil {
			return nil, store.RefundResult{}, errors.New(sentence(err))
		}
		return nil, *res, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_profile",
		Description: "The user's currency and language, and today's date: what amounts are in and what \"today\" means.",
		Annotations: &mcp.ToolAnnotations{Title: "Get the profile", ReadOnlyHint: true, OpenWorldHint: &no},
	}, func(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, mcpProfileOut, error) {
		u, err := caller(req)
		if err != nil {
			return nil, mcpProfileOut{}, err
		}
		now := time.Now()
		return nil, mcpProfileOut{Name: u.Name, Currency: u.Prefs.Currency, Language: u.Prefs.Language, Today: now.Format("2006-01-02"), Weekday: now.Weekday().String()}, nil
	})
	return srv
}

// mcpHandler serves /mcp. Requests without a valid agent token get 401.
func (s *Server) mcpHandler() http.Handler {
	srv := s.newMCPServer(func(req *mcp.CallToolRequest) (*store.User, error) {
		if info := req.Extra.TokenInfo; info != nil {
			if u, ok := info.Extra["user"].(*store.User); ok {
				return u, nil
			}
		}
		return nil, errors.New("send an agent token: Authorization: Bearer plt_…")
	})
	h := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv }, &mcp.StreamableHTTPOptions{
		Stateless:    true,
		JSONResponse: true,
		// Its DNS-rebinding guard turns away requests that reach a local address
		// under another name, which is every request behind a reverse proxy on
		// the same machine. This server wants a token on every request, which a
		// page in someone's browser doesn't have.
		DisableLocalhostProtection: true,
		MaxRequestBodyBytes:        maxBodyBytes,
	})
	return auth.RequireBearerToken(s.verifyAgentToken, &auth.RequireBearerTokenOptions{AllowMissingExpiration: true})(h)
}

// verifyAgentToken accepts the user's agent tokens; the user goes along with
// the request to the tools.
func (s *Server) verifyAgentToken(ctx context.Context, token string, _ *http.Request) (*auth.TokenInfo, error) {
	u, err := s.opts.Store.UserByAgentToken(ctx, strings.TrimSpace(token))
	if errors.Is(err, store.ErrNotFound) {
		return nil, fmt.Errorf("%w: this agent token is invalid or has been revoked; create one in Settings → Agent access", auth.ErrInvalidToken)
	}
	if err != nil {
		return nil, err
	}
	return &auth.TokenInfo{UserID: strconv.FormatInt(u.ID, 10), Extra: map[string]any{"user": u}}, nil
}
