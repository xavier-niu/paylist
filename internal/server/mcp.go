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

	"github.com/xavier-niu/paylist/internal/fx"
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

const mcpInstructions = `Paylist is the user's personal spending ledger. Each entry has a name, a type, an amount, a date (YYYY-MM-DD) and the card it was paid with; type and card are choices from lists of options. The ledger is kept month by month. The user may have several ledgers, their own and others shared with them (list_ledgers); every tool takes one by name or id, and without one uses the ledger last open in Paylist. A viewer can only read a ledger. get_profile gives a ledger's currency and today's date.

Read it with list_entries (one month, a date range, a type, a card or a name), get_stats (a month's totals by day, by type and by card, against the usual and the year before, and the months before), list_months (every month with entries) and get_history (who created or changed an entry, and what). Change it with add_entries (several at once), update_entry and delete_entries. list_options shows the types and cards, with when each was last used; manage_option creates, renames, recolors, deletes or reorders them. manage_ledger makes a ledger, or renames, re-currencies, deletes or leaves one; ledger_people shows who has one, and its owner shares it.

Amounts are in the ledger's currency. For one paid in another, give add_entries, update_entry or refund its currency: it's converted at that day's rate (get_exchange_rate shows rates) and what was paid is kept beside it. When adding entries, reuse an existing type and card whose name fits before making new ones; when the user names something without its amount, type or card, suggest_entries shows how it was entered before. For a refund, use refund: it deletes the purchase it undoes, takes part off one picked by id, or records the refund as a negative amount when none matches. Delete only what the user asked to delete.`

// The user a tool works for, and the ledger to use when a call names none
// (empty: the one they had open last).
type mcpCallerFunc func(req *mcp.CallToolRequest) (*store.User, string, error)

type mcpListIn struct {
	Ledger string `json:"ledger,omitempty" jsonschema:"the ledger, by name or id (list_ledgers shows them); the one last open in Paylist when left out"`
	Month  string `json:"month,omitempty" jsonschema:"the month to list, YYYY-MM; the current month when neither month nor from/to is given"`
	From   string `json:"from,omitempty" jsonschema:"first date to include, YYYY-MM-DD (instead of month)"`
	To     string `json:"to,omitempty" jsonschema:"last date to include, YYYY-MM-DD (instead of month)"`
	Type   string `json:"type,omitempty" jsonschema:"only entries of this type"`
	Card   string `json:"card,omitempty" jsonschema:"only entries paid with this card"`
	Query  string `json:"query,omitempty" jsonschema:"only entries whose name contains this"`
	Limit  int    `json:"limit,omitempty" jsonschema:"at most this many entries (default 500)"`
}

type mcpListOut struct {
	Entries []store.Entry `json:"entries" jsonschema:"the entries, oldest first"`
	Count   int           `json:"count"`
	Total   float64       `json:"total" jsonschema:"the sum of their amounts"`
	From    string        `json:"from,omitempty"`
	To      string        `json:"to,omitempty"`
}

type mcpNewEntry struct {
	Name     string  `json:"name" jsonschema:"what was paid for"`
	Amount   float64 `json:"amount" jsonschema:"how much, in the ledger's currency (or currency's); negative for a refund"`
	Currency string  `json:"currency,omitempty" jsonschema:"the currency the amount is in, when not the ledger's (an ISO code such as USD): it is converted at the entry's date and what was paid is kept beside it"`
	Date     string  `json:"date,omitempty" jsonschema:"YYYY-MM-DD; today when left out"`
	Type     string  `json:"type,omitempty" jsonschema:"a type option, such as Food; a new name creates the option"`
	Card     string  `json:"card,omitempty" jsonschema:"a card option, such as Cash; a new name creates the option"`
}

type mcpAddIn struct {
	Ledger  string        `json:"ledger,omitempty" jsonschema:"the ledger, by name or id (list_ledgers shows them); the one last open in Paylist when left out"`
	Entries []mcpNewEntry `json:"entries" jsonschema:"the entries to add (1 to 500)"`
}

type mcpAddOut struct {
	Added []store.Entry `json:"added"`
}

type mcpUpdateIn struct {
	Ledger   string   `json:"ledger,omitempty" jsonschema:"the ledger, by name or id (list_ledgers shows them); the one last open in Paylist when left out"`
	ID       int64    `json:"id" jsonschema:"the entry's id, from list_entries"`
	Name     *string  `json:"name,omitempty" jsonschema:"a new name"`
	Amount   *float64 `json:"amount,omitempty" jsonschema:"a new amount, in the ledger's currency (or currency's)"`
	Currency string   `json:"currency,omitempty" jsonschema:"the currency the new amount is in, when not the ledger's (an ISO code such as USD): it is converted at the entry's date and what was paid kept beside it; the ledger's own code forgets what was paid elsewhere"`
	Date     *string  `json:"date,omitempty" jsonschema:"a new date, YYYY-MM-DD"`
	Type     *string  `json:"type,omitempty" jsonschema:"a new type; empty for none"`
	Card     *string  `json:"card,omitempty" jsonschema:"a new card; empty for none"`
}

type mcpDeleteIn struct {
	Ledger string  `json:"ledger,omitempty" jsonschema:"the ledger, by name or id (list_ledgers shows them); the one last open in Paylist when left out"`
	IDs    []int64 `json:"ids" jsonschema:"the ids of the entries to delete"`
}

type mcpDeleteOut struct {
	Deleted int64 `json:"deleted" jsonschema:"how many entries were deleted"`
}

type mcpOptionIn struct {
	Ledger  string   `json:"ledger,omitempty" jsonschema:"the ledger, by name or id (list_ledgers shows them); the one last open in Paylist when left out"`
	Kind    string   `json:"kind" jsonschema:"type or card"`
	Action  string   `json:"action" jsonschema:"create, rename, recolor, delete or reorder"`
	Name    string   `json:"name" jsonschema:"the option's name (for create, the new option's)"`
	NewName string   `json:"new_name,omitempty" jsonschema:"rename: its new name, which every entry with it gets too"`
	Color   string   `json:"color,omitempty" jsonschema:"create or recolor: gray, brown, orange, yellow, green, blue, purple, pink or red"`
	Order   []string `json:"order,omitempty" jsonschema:"reorder: every type (or card) of the ledger, by name, in the order to offer them in; name is ignored"`
}

type mcpOptionOut struct {
	Option  *store.Option `json:"option,omitempty"`
	Cleared int64         `json:"cleared,omitempty" jsonschema:"delete: how many entries lost this option"`
}

type mcpSuggestIn struct {
	Ledger string `json:"ledger,omitempty" jsonschema:"the ledger, by name or id (list_ledgers shows them); the one last open in Paylist when left out"`
	Query  string `json:"query" jsonschema:"part of a name, such as coffee"`
	Limit  int    `json:"limit,omitempty" jsonschema:"at most this many names (default 6, up to 20)"`
}

type mcpSuggestOut struct {
	Suggestions []store.Suggestion `json:"suggestions" jsonschema:"names used before, each with its latest type, card, amount and date, and how many entries have it; names starting with the query first, then the most used"`
}

type mcpMonthsOut struct {
	Months []store.MonthTotal `json:"months" jsonschema:"every month with entries up to this one, newest first, with its total and count"`
}

type mcpRefundIn struct {
	Ledger   string   `json:"ledger,omitempty" jsonschema:"the ledger, by name or id (list_ledgers shows them); the one last open in Paylist when left out"`
	ID       int64    `json:"id,omitempty" jsonschema:"a purchase's id (from list_entries, or from the matches of an earlier call), to refund that one; with an amount below its own, only that part"`
	Name     string   `json:"name,omitempty" jsonschema:"the purchase's name, as entered (case doesn't matter)"`
	Amount   *float64 `json:"amount,omitempty" jsonschema:"how much came back, in the ledger's currency (or currency's)"`
	Currency string   `json:"currency,omitempty" jsonschema:"the currency the amount came back in, when not the ledger's (an ISO code such as USD): it is converted at the refund's date for matching and recording, and what came back is kept"`
	Type     string   `json:"type,omitempty" jsonschema:"the type to record the refund under if no purchase matches"`
	Card     string   `json:"card,omitempty" jsonschema:"the card the money came back to, if no purchase matches"`
	Date     string   `json:"date,omitempty" jsonschema:"YYYY-MM-DD, the refund's date if no purchase matches; today when left out"`
	New      bool     `json:"new,omitempty" jsonschema:"add it as a refund of its own (a negative amount) even if a purchase matches"`
}

type mcpHistoryIn struct {
	Ledger string `json:"ledger,omitempty" jsonschema:"the ledger, by name or id (list_ledgers shows them); the one last open in Paylist when left out"`
	ID     int64  `json:"id" jsonschema:"the entry's id, from list_entries"`
}
type mcpHistoryOut struct {
	Events []store.EntryEvent `json:"events"`
}
type mcpRateIn struct {
	From string `json:"from" jsonschema:"the currency to convert from, an ISO code such as USD"`
	To   string `json:"to" jsonschema:"the currency to convert to, such as the ledger's (get_profile)"`
	Date string `json:"date,omitempty" jsonschema:"YYYY-MM-DD; today when left out"`
}
type mcpManageLedgerIn struct {
	Action   string `json:"action" jsonschema:"create, rename, currency, delete or leave"`
	Ledger   string `json:"ledger,omitempty" jsonschema:"the ledger to rename, change, delete or leave, by name or id; the one last open in Paylist when left out"`
	Name     string `json:"name,omitempty" jsonschema:"create: the new ledger's name; rename: its new name"`
	Currency string `json:"currency,omitempty" jsonschema:"create or currency: an ISO code such as CNY or USD (create: the user's usual one when left out)"`
}
type mcpManageLedgerOut struct {
	Ledger *store.Ledger `json:"ledger,omitempty"`
	Done   bool          `json:"done,omitempty"` // deleted, or left
}
type mcpPeopleIn struct {
	Ledger string `json:"ledger,omitempty" jsonschema:"the ledger, by name or id; the one last open in Paylist when left out"`
	Action string `json:"action,omitempty" jsonschema:"list (the default), invite, set_role or remove"`
	Email  string `json:"email,omitempty" jsonschema:"invite, set_role or remove: the person's Paylist account email"`
	Role   string `json:"role,omitempty" jsonschema:"invite or set_role: editor (changes it like the owner) or viewer (only reads it)"`
}
type mcpPeopleOut struct {
	Ledger string               `json:"ledger"`
	People []store.LedgerMember `json:"people"`
}

type mcpProfileOut struct {
	Name     string `json:"name,omitempty"`
	Ledger   string `json:"ledger" jsonschema:"the ledger this answer is about"`
	Role     string `json:"role" jsonschema:"the user's role in it: owner, editor or viewer (a viewer can only read)"`
	Currency string `json:"currency" jsonschema:"the ISO 4217 code the ledger's amounts are in, such as CNY or USD"`
	Language string `json:"language" jsonschema:"en, zh, or system (the browser's)"`
	Today    string `json:"today" jsonschema:"today's date, YYYY-MM-DD"`
	Weekday  string `json:"weekday"`
}

// mcpLedgerIn is a tool's input that is only which ledger.
type mcpLedgerIn struct {
	Ledger string `json:"ledger,omitempty" jsonschema:"the ledger, by name or id (list_ledgers shows them); the one last open in Paylist when left out"`
}

type mcpLedgersOut struct {
	Ledgers []store.Ledger `json:"ledgers" jsonschema:"the user's ledgers: their own and those shared with them, with the user's role in each (owner, editor or viewer; a viewer can only read)"`
}

type mcpStatsIn struct {
	Ledger string `json:"ledger,omitempty" jsonschema:"the ledger, by name or id (list_ledgers shows them); the one last open in Paylist when left out"`
	Month  string `json:"month,omitempty" jsonschema:"the month, YYYY-MM; the current month when left out"`
	Months int    `json:"months,omitempty" jsonschema:"how many months of monthly totals to include, up to this one (1 to 36, default 12)"`
}

// newMCPServer builds the MCP server whose tools work on the ledger of the user
// caller returns.
// newMCPServer serves the tools to one caller; via says through what ("agent"
// for the agent page, "mcp" for outside agents), for entries' history.
func (s *Server) newMCPServer(caller mcpCallerFunc, via string) *mcp.Server {
	srv := mcp.NewServer(&mcp.Implementation{Name: "paylist", Title: "Paylist", Version: "1.0.0"},
		&mcp.ServerOptions{Instructions: mcpInstructions})
	yes, no := true, false
	st := s.opts.Store
	// ledger is the ledger a call works on, if the user has it and, to change
	// it, may; and the call's context, its changes recorded as the user's.
	ledger := func(ctx context.Context, req *mcp.CallToolRequest, want string, write bool) (context.Context, *store.Ledger, error) {
		u, def, err := caller(req)
		if err != nil {
			return ctx, nil, err
		}
		ctx = store.WithActor(ctx, u.ID, via)
		if strings.TrimSpace(want) == "" {
			want = def
		}
		l, err := s.pickLedger(ctx, u, want)
		if errors.Is(err, store.ErrNotMember) {
			return ctx, nil, fmt.Errorf("you have no ledger %q; list_ledgers shows yours", want)
		}
		if err != nil {
			return ctx, nil, err
		}
		if write && !l.CanWrite() {
			return ctx, nil, fmt.Errorf("you can only view the ledger %q", l.Name)
		}
		return ctx, l, nil
	}

	mcp.AddTool(srv, &mcp.Tool{
		Name: "list_entries",
		Description: "List entries of the spending ledger, oldest first, with their count and total: one month (the current one by default) " +
			"or a date range, optionally only one type, one card or names containing a query.",
		Annotations: &mcp.ToolAnnotations{Title: "List entries", ReadOnlyHint: true, OpenWorldHint: &no},
	}, func(ctx context.Context, req *mcp.CallToolRequest, in mcpListIn) (*mcp.CallToolResult, mcpListOut, error) {
		ctx, l, err := ledger(ctx, req, in.Ledger, false)
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
		entries, err := st.ListEntries(ctx, l.ID, f)
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
		ctx, l, err := ledger(ctx, req, in.Ledger, true)
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
			amount, orig, cur, err := s.inLedgerCurrency(ctx, l, e.Currency, e.Amount, e.Date)
			if err != nil {
				return nil, mcpAddOut{}, err
			}
			ins = append(ins, store.EntryInput{Name: &e.Name, Amount: &amount, Date: &e.Date, Type: &e.Type, Card: &e.Card, OrigAmount: orig, OrigCurrency: cur})
		}
		added, err := st.CreateEntries(ctx, l.ID, ins)
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
		ctx, l, err := ledger(ctx, req, in.Ledger, true)
		if err != nil {
			return nil, store.Entry{}, err
		}
		change := store.EntryInput{Name: in.Name, Amount: in.Amount, Date: in.Date, Type: in.Type, Card: in.Card}
		if cur := strings.TrimSpace(in.Currency); cur != "" {
			// An amount in a currency, converted at the entry's (new) date.
			if in.Amount == nil {
				return nil, store.Entry{}, fmt.Errorf("give the amount in %s too", cur)
			}
			old, err := st.GetEntry(ctx, l.ID, in.ID)
			if err != nil {
				return nil, store.Entry{}, fmt.Errorf("there is no entry %d", in.ID)
			}
			date := old.Date
			if in.Date != nil {
				date = *in.Date
			}
			amount, orig, origCur, err := s.inLedgerCurrency(ctx, l, cur, *in.Amount, date)
			if err != nil {
				return nil, store.Entry{}, err
			}
			none := ""
			if origCur == nil {
				origCur = &none // the ledger's own: nothing paid elsewhere
			}
			change.Amount, change.OrigAmount, change.OrigCurrency = &amount, orig, origCur
		}
		e, err := st.UpdateEntry(ctx, l.ID, in.ID, change)
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
		ctx, l, err := ledger(ctx, req, in.Ledger, true)
		if err != nil {
			return nil, mcpDeleteOut{}, err
		}
		n, err := st.DeleteEntries(ctx, l.ID, in.IDs)
		if err != nil {
			return nil, mcpDeleteOut{}, errors.New(sentence(err))
		}
		return nil, mcpDeleteOut{Deleted: n}, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_options",
		Description: "List the options for an entry's type and card, in the order they're offered in, with their colors, how many entries use each and when one last did (last_used). One unused for twelve months is inactive: the app offers it only when searched for.",
		Annotations: &mcp.ToolAnnotations{Title: "List options", ReadOnlyHint: true, OpenWorldHint: &no},
	}, func(ctx context.Context, req *mcp.CallToolRequest, in mcpLedgerIn) (*mcp.CallToolResult, store.Options, error) {
		ctx, l, err := ledger(ctx, req, in.Ledger, false)
		if err != nil {
			return nil, store.Options{}, err
		}
		opts, err := st.ListOptions(ctx, l.ID)
		return nil, opts, err
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name: "manage_option",
		Description: "Create, rename, recolor, delete or reorder the type or card options. Renaming renames it on every entry; " +
			"deleting clears it from the entries that had it; reorder sets the order they're offered in, given every one of the kind by name.",
		Annotations: &mcp.ToolAnnotations{Title: "Manage an option", DestructiveHint: &yes, OpenWorldHint: &no},
	}, func(ctx context.Context, req *mcp.CallToolRequest, in mcpOptionIn) (*mcp.CallToolResult, mcpOptionOut, error) {
		ctx, l, err := ledger(ctx, req, in.Ledger, true)
		if err != nil {
			return nil, mcpOptionOut{}, err
		}
		if in.Action == "reorder" {
			opts, err := st.ListOptions(ctx, l.ID)
			if err != nil {
				return nil, mcpOptionOut{}, err
			}
			list := opts.Types
			if in.Kind == store.KindCard {
				list = opts.Cards
			}
			byName := map[string]int64{}
			for _, o := range list {
				byName[strings.ToLower(o.Name)] = o.ID
			}
			ids := make([]int64, 0, len(in.Order))
			for _, n := range in.Order {
				id, ok := byName[strings.ToLower(strings.TrimSpace(n))]
				if !ok {
					return nil, mcpOptionOut{}, fmt.Errorf("there is no %s option called %q", in.Kind, n)
				}
				ids = append(ids, id)
			}
			if err := st.ReorderOptions(ctx, l.ID, in.Kind, ids); err != nil {
				return nil, mcpOptionOut{}, errors.New(sentence(err))
			}
			return nil, mcpOptionOut{}, nil
		}
		if in.Action == "create" {
			o, err := st.CreateOption(ctx, l.ID, in.Kind, in.Name, in.Color)
			if err != nil {
				return nil, mcpOptionOut{}, errors.New(sentence(err))
			}
			return nil, mcpOptionOut{Option: o}, nil
		}
		o, err := st.FindOption(ctx, l.ID, in.Kind, in.Name)
		if errors.Is(err, store.ErrNotFound) {
			return nil, mcpOptionOut{}, fmt.Errorf("there is no %s option called %q", in.Kind, in.Name)
		}
		if err != nil {
			return nil, mcpOptionOut{}, err
		}
		switch in.Action {
		case "rename":
			o, err = st.UpdateOption(ctx, l.ID, o.ID, &in.NewName, nil)
		case "recolor":
			o, err = st.UpdateOption(ctx, l.ID, o.ID, nil, &in.Color)
		case "delete":
			var cleared int64
			if _, cleared, err = st.DeleteOption(ctx, l.ID, o.ID); err == nil {
				return nil, mcpOptionOut{Cleared: cleared}, nil
			}
		default:
			return nil, mcpOptionOut{}, fmt.Errorf("action is %q: use create, rename, recolor, delete or reorder", in.Action)
		}
		if err != nil {
			return nil, mcpOptionOut{}, errors.New(sentence(err))
		}
		return nil, mcpOptionOut{Option: o}, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name: "get_stats",
		Description: "Spending statistics for a month (the current one by default): its total, count, daily average, " +
			"every day's total, totals by type and by card, the totals of the months before it, and each type's total in each of those months (type_monthly). " +
			"For month over month, compare to_date with prev_to_date: in the current month they cover day 1 to compare_day of each month; otherwise the whole months. " +
			"For what's unusual, compare to_date with avg_to_date, and by_type with type_avg: the average month over the avg_months months before, counted the same way. " +
			"year has the month's year: its total and count, to_date against prev_to_date (up to compare_date, against the same day a year earlier, in the current year), every month of it and of the year before, and its totals by type.",
		Annotations: &mcp.ToolAnnotations{Title: "Get statistics", ReadOnlyHint: true, OpenWorldHint: &no},
	}, func(ctx context.Context, req *mcp.CallToolRequest, in mcpStatsIn) (*mcp.CallToolResult, store.Stats, error) {
		ctx, l, err := ledger(ctx, req, in.Ledger, false)
		if err != nil {
			return nil, store.Stats{}, err
		}
		if in.Month == "" {
			in.Month = time.Now().Format("2006-01")
		}
		if in.Months == 0 {
			in.Months = 12
		}
		stats, err := st.GetStats(ctx, l.ID, in.Month, in.Months, time.Now())
		if err != nil {
			return nil, store.Stats{}, errors.New(sentence(err))
		}
		return nil, *stats, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_months",
		Description: "Every month that has entries, up to the current one, newest first, with its total and count.",
		Annotations: &mcp.ToolAnnotations{Title: "List months", ReadOnlyHint: true, OpenWorldHint: &no},
	}, func(ctx context.Context, req *mcp.CallToolRequest, in mcpLedgerIn) (*mcp.CallToolResult, mcpMonthsOut, error) {
		ctx, l, err := ledger(ctx, req, in.Ledger, false)
		if err != nil {
			return nil, mcpMonthsOut{}, err
		}
		months, err := st.AllMonths(ctx, l.ID, time.Now().Format("2006-01"))
		return nil, mcpMonthsOut{Months: months}, err
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name: "suggest_entries",
		Description: "Names used before that contain the query, one per name with how it was last entered (type, card, amount, date) " +
			"and how often. Use it to fill in what the user leaves out when adding something they've bought before.",
		Annotations: &mcp.ToolAnnotations{Title: "Suggest entries", ReadOnlyHint: true, OpenWorldHint: &no},
	}, func(ctx context.Context, req *mcp.CallToolRequest, in mcpSuggestIn) (*mcp.CallToolResult, mcpSuggestOut, error) {
		ctx, l, err := ledger(ctx, req, in.Ledger, false)
		if err != nil {
			return nil, mcpSuggestOut{}, err
		}
		if in.Limit <= 0 {
			in.Limit = 6
		}
		out, err := st.SuggestEntries(ctx, l.ID, in.Query, in.Limit)
		return nil, mcpSuggestOut{Suggestions: out}, err
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name: "refund",
		Description: "Record a refund the way the ledger's Refund form does. Give the purchase's id, or its name and amount " +
			"(with the type, card and date, in case nothing matches): a purchase from the last 120 days with the same name and amount " +
			"is deleted and returned as refunded; if several match, nothing changes and they come back as matches, so call again with " +
			"the right id (ask the user if unsure); if none match, the refund is added as a negative amount and returned as added. " +
			"For part of a purchase (50 back on 100), give its id and the amount that came back: the purchase goes down by it and is returned as reduced. " +
			"With new, the refund is added as its own negative amount whatever matches.",
		Annotations: &mcp.ToolAnnotations{Title: "Refund", DestructiveHint: &yes, OpenWorldHint: &no},
	}, func(ctx context.Context, req *mcp.CallToolRequest, in mcpRefundIn) (*mcp.CallToolResult, store.RefundResult, error) {
		ctx, l, err := ledger(ctx, req, in.Ledger, true)
		if err != nil {
			return nil, store.RefundResult{}, err
		}
		refund := store.RefundInput{ID: in.ID, Name: in.Name, Amount: in.Amount, Type: in.Type, Card: in.Card, Date: in.Date, New: in.New}
		if in.Amount != nil && strings.TrimSpace(in.Currency) != "" {
			amount, orig, cur, err := s.inLedgerCurrency(ctx, l, in.Currency, *in.Amount, in.Date)
			if err != nil {
				return nil, store.RefundResult{}, err
			}
			refund.Amount = &amount
			if cur != nil {
				refund.OrigAmount, refund.OrigCurrency = orig, *cur
			}
		}
		res, err := st.Refund(ctx, l.ID, refund, time.Now())
		if errors.Is(err, store.ErrNotFound) {
			return nil, store.RefundResult{}, fmt.Errorf("there is no entry %d", in.ID)
		}
		if err != nil {
			return nil, store.RefundResult{}, errors.New(sentence(err))
		}
		return nil, *res, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_ledgers",
		Description: "The user's ledgers: their own and those shared with them, with their role in each. Other tools take a ledger by name or id; without one they use the ledger last open in Paylist.",
		Annotations: &mcp.ToolAnnotations{Title: "List ledgers", ReadOnlyHint: true, OpenWorldHint: &no},
	}, func(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, mcpLedgersOut, error) {
		u, _, err := caller(req)
		if err != nil {
			return nil, mcpLedgersOut{}, err
		}
		ls, err := st.ListLedgers(ctx, u.ID)
		return nil, mcpLedgersOut{Ledgers: ls}, err
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_profile",
		Description: "The ledger's name, currency and the user's role in it, the user's language, and today's date: what amounts are in and what \"today\" means.",
		Annotations: &mcp.ToolAnnotations{Title: "Get the profile", ReadOnlyHint: true, OpenWorldHint: &no},
	}, func(ctx context.Context, req *mcp.CallToolRequest, in mcpLedgerIn) (*mcp.CallToolResult, mcpProfileOut, error) {
		u, _, err := caller(req)
		if err != nil {
			return nil, mcpProfileOut{}, err
		}
		ctx, l, err := ledger(ctx, req, in.Ledger, false)
		if err != nil {
			return nil, mcpProfileOut{}, err
		}
		now := time.Now()
		return nil, mcpProfileOut{Name: u.Name, Ledger: l.Name, Role: l.Role, Currency: l.Currency, Language: u.Prefs.Language, Today: now.Format("2006-01-02"), Weekday: now.Weekday().String()}, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_history",
		Description: "An entry's history, oldest first: who created it and with what, then each change (every field's before and after, amounts in the ledger's currency) and each part refunded, with who did it, through what (\"\" for the app, \"agent\", \"mcp\") and when.",
		Annotations: &mcp.ToolAnnotations{Title: "Get an entry's history", ReadOnlyHint: true, OpenWorldHint: &no},
	}, func(ctx context.Context, req *mcp.CallToolRequest, in mcpHistoryIn) (*mcp.CallToolResult, mcpHistoryOut, error) {
		ctx, l, err := ledger(ctx, req, in.Ledger, false)
		if err != nil {
			return nil, mcpHistoryOut{}, err
		}
		h, err := st.EntryHistory(ctx, l.ID, in.ID)
		if errors.Is(err, store.ErrNotFound) {
			return nil, mcpHistoryOut{}, fmt.Errorf("there is no entry %d", in.ID)
		}
		return nil, mcpHistoryOut{Events: h}, err
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_exchange_rate",
		Description: "How much one unit of a currency is worth in another on a day (today's by default): the European Central Bank's reference rate, or for currencies it doesn't publish, today's from ExchangeRate-API. Amounts paid in another currency needn't be converted by hand: add_entries, update_entry and refund take a currency.",
		Annotations: &mcp.ToolAnnotations{Title: "Get an exchange rate", ReadOnlyHint: true, OpenWorldHint: &yes},
	}, func(ctx context.Context, req *mcp.CallToolRequest, in mcpRateIn) (*mcp.CallToolResult, fx.Rate, error) {
		if _, _, err := caller(req); err != nil {
			return nil, fx.Rate{}, err
		}
		day := time.Now()
		if in.Date != "" {
			t, err := store.ParseDate(in.Date)
			if err != nil {
				return nil, fx.Rate{}, err
			}
			day = t
		}
		r, err := s.opts.FX.Get(ctx, in.From, in.To, day)
		return nil, r, err
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name: "manage_ledger",
		Description: "Make a ledger of the user's own (create, with a name and a currency), or, as its owner, rename one, change its currency (amounts aren't converted) " +
			"or delete it with all its entries (never the user's last); or leave one shared with them. Confirm with the user before deleting.",
		Annotations: &mcp.ToolAnnotations{Title: "Manage a ledger", DestructiveHint: &yes, OpenWorldHint: &no},
	}, func(ctx context.Context, req *mcp.CallToolRequest, in mcpManageLedgerIn) (*mcp.CallToolResult, mcpManageLedgerOut, error) {
		u, _, err := caller(req)
		if err != nil {
			return nil, mcpManageLedgerOut{}, err
		}
		if in.Action == "create" {
			cur := strings.TrimSpace(in.Currency)
			if cur == "" {
				cur = u.Prefs.Currency
			}
			l, err := st.CreateLedger(ctx, u.ID, in.Name, cur)
			if err != nil {
				return nil, mcpManageLedgerOut{}, errors.New(sentence(err))
			}
			return nil, mcpManageLedgerOut{Ledger: l}, nil
		}
		_, l, err := ledger(ctx, req, in.Ledger, false)
		if err != nil {
			return nil, mcpManageLedgerOut{}, err
		}
		owner := l.Role == store.RoleOwner
		if !owner && in.Action != "leave" {
			return nil, mcpManageLedgerOut{}, fmt.Errorf("only the owner of %q can %s it", l.Name, in.Action)
		}
		switch in.Action {
		case "rename":
			err = st.UpdateLedger(ctx, l.ID, &in.Name, nil)
		case "currency":
			err = st.UpdateLedger(ctx, l.ID, nil, &in.Currency)
		case "delete":
			err = st.DeleteLedger(ctx, u.ID, l.ID)
		case "leave":
			if owner {
				return nil, mcpManageLedgerOut{}, fmt.Errorf("the owner can't leave %q; delete it instead", l.Name)
			}
			err = st.RemoveLedgerMember(ctx, l.ID, u.ID)
		default:
			return nil, mcpManageLedgerOut{}, fmt.Errorf("action is %q: use create, rename, currency, delete or leave", in.Action)
		}
		if err != nil {
			return nil, mcpManageLedgerOut{}, errors.New(sentence(err))
		}
		if in.Action == "delete" || in.Action == "leave" {
			return nil, mcpManageLedgerOut{Done: true}, nil
		}
		l, err = st.GetLedger(ctx, u.ID, l.ID)
		return nil, mcpManageLedgerOut{Ledger: l}, err
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name: "ledger_people",
		Description: "The people who have a ledger and their roles (owner, editor, viewer). Its owner can also share it with someone by the email of their Paylist account " +
			"on this server (invite, as editor or viewer), change their role, or remove them.",
		Annotations: &mcp.ToolAnnotations{Title: "A ledger's people", DestructiveHint: &yes, OpenWorldHint: &no},
	}, func(ctx context.Context, req *mcp.CallToolRequest, in mcpPeopleIn) (*mcp.CallToolResult, mcpPeopleOut, error) {
		_, l, err := ledger(ctx, req, in.Ledger, false)
		if err != nil {
			return nil, mcpPeopleOut{}, err
		}
		action := in.Action
		if action == "" {
			action = "list"
		}
		if action != "list" {
			if l.Role != store.RoleOwner {
				return nil, mcpPeopleOut{}, fmt.Errorf("only the owner of %q can share it", l.Name)
			}
			switch action {
			case "invite":
				_, err = st.AddLedgerMember(ctx, l.ID, in.Email, in.Role)
			case "set_role", "remove":
				var who *store.LedgerMember
				ms, lerr := st.ListLedgerMembers(ctx, l.ID)
				if lerr != nil {
					return nil, mcpPeopleOut{}, lerr
				}
				for i := range ms {
					if strings.EqualFold(ms[i].Email, strings.TrimSpace(in.Email)) {
						who = &ms[i]
					}
				}
				if who == nil {
					return nil, mcpPeopleOut{}, fmt.Errorf("%s doesn't have %q", in.Email, l.Name)
				}
				if action == "set_role" {
					err = st.SetLedgerMemberRole(ctx, l.ID, who.UserID, in.Role)
				} else {
					err = st.RemoveLedgerMember(ctx, l.ID, who.UserID)
				}
			default:
				return nil, mcpPeopleOut{}, fmt.Errorf("action is %q: use list, invite, set_role or remove", action)
			}
			if err != nil {
				return nil, mcpPeopleOut{}, errors.New(sentence(err))
			}
		}
		ms, err := st.ListLedgerMembers(ctx, l.ID)
		return nil, mcpPeopleOut{Ledger: l.Name, People: ms}, err
	})
	return srv
}

// mcpHandler serves /mcp. Requests without a valid agent token get 401.
func (s *Server) mcpHandler() http.Handler {
	srv := s.newMCPServer(func(req *mcp.CallToolRequest) (*store.User, string, error) {
		if info := req.Extra.TokenInfo; info != nil {
			if u, ok := info.Extra["user"].(*store.User); ok {
				return u, "", nil
			}
		}
		return nil, "", errors.New("send an agent token: Authorization: Bearer plt_…")
	}, "mcp")
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
