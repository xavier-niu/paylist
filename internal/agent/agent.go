// Package agent runs the agent page's assistant: a model (the user's own key)
// in a loop with the tools of Paylist's MCP server, reached through an MCP
// client session.
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/xavier-niu/paylist/internal/llm"
)

// MaxSteps bounds the model calls in one turn.
const MaxSteps = 30

// Event is something to show while a turn runs.
type Event struct {
	Type   string          `json:"type"` // "text", "tool_call", "tool_result" or "notice"
	Text   string          `json:"text,omitempty"`
	Call   *llm.ToolCall   `json:"call,omitempty"`
	Result *llm.ToolResult `json:"result,omitempty"`
}

// Tools lists the MCP server's tools in the form the model takes.
func Tools(ctx context.Context, session *mcp.ClientSession) ([]llm.Tool, error) {
	res, err := session.ListTools(ctx, nil)
	if err != nil {
		return nil, err
	}
	out := make([]llm.Tool, 0, len(res.Tools))
	for _, t := range res.Tools {
		schema := map[string]any{}
		if b, err := json.Marshal(t.InputSchema); err == nil {
			_ = json.Unmarshal(b, &schema)
		}
		if schema["type"] == nil {
			schema["type"] = "object"
		}
		out = append(out, llm.Tool{Name: t.Name, Description: t.Description, Schema: schema})
	}
	return out, nil
}

// Run continues the conversation in history, whose last message is the
// user's, until the model answers. It returns the conversation with the new
// messages; on an error, the ones so far, ready to continue from.
func Run(ctx context.Context, client llm.Client, session *mcp.ClientSession, system string, history []llm.Message, emit func(Event) error) ([]llm.Message, error) {
	tools, err := Tools(ctx, session)
	if err != nil {
		return history, fmt.Errorf("list the ledger's tools: %w", err)
	}
	for step := 0; ; step++ {
		if step == MaxSteps {
			return history, emit(Event{Type: "notice", Text: fmt.Sprintf("Stopped after %d steps. Say \"continue\" to let it go on.", MaxSteps)})
		}
		msg, stop, err := client.Step(ctx, system, history, tools, func(t string) error {
			return emit(Event{Type: "text", Text: t})
		})
		if err != nil {
			return history, err
		}
		history = append(history, msg)
		switch stop {
		case llm.StopMaxTokens:
			return Complete(history), emit(Event{Type: "notice", Text: "The reply reached the length limit and was cut short."})
		case llm.StopRefusal:
			return Complete(history), emit(Event{Type: "notice", Text: "The model declined to continue with this request."})
		case llm.StopEnd:
			return Complete(history), nil
		}
		results := make([]llm.ToolResult, 0, len(msg.ToolCalls))
		for _, tc := range msg.ToolCalls {
			call := tc
			if err := emit(Event{Type: "tool_call", Call: &call}); err != nil {
				return Complete(history), err
			}
			r := callTool(ctx, session, call)
			results = append(results, r)
			if err := emit(Event{Type: "tool_result", Result: &r}); err != nil {
				history = append(history, llm.Message{Role: "tool", Results: results})
				return Complete(history), err
			}
		}
		history = append(history, llm.Message{Role: "tool", Results: results})
	}
}

func callTool(ctx context.Context, session *mcp.ClientSession, tc llm.ToolCall) llm.ToolResult {
	r := llm.ToolResult{ID: tc.ID, Name: tc.Name}
	var args any = json.RawMessage(`{}`)
	if len(tc.Input) > 0 {
		args = tc.Input
	}
	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: tc.Name, Arguments: args})
	if err != nil {
		r.Content, r.IsError = err.Error(), true
		return r
	}
	var parts []string
	for _, c := range res.Content {
		if t, ok := c.(*mcp.TextContent); ok {
			parts = append(parts, t.Text)
		}
	}
	if len(parts) == 0 && res.StructuredContent != nil {
		b, _ := json.Marshal(res.StructuredContent)
		parts = append(parts, string(b))
	}
	r.Content, r.IsError = strings.Join(parts, "\n"), res.IsError
	if r.Content == "" {
		r.Content = "(no output)"
	}
	return r
}

// Complete makes a conversation safe to continue: tool calls left without
// results (the turn was stopped) get results saying they didn't run.
func Complete(history []llm.Message) []llm.Message {
	if n := len(history); n > 0 && history[n-1].Role == "assistant" && len(history[n-1].ToolCalls) > 0 {
		results := make([]llm.ToolResult, 0, len(history[n-1].ToolCalls))
		for _, tc := range history[n-1].ToolCalls {
			results = append(results, llm.ToolResult{ID: tc.ID, Name: tc.Name, Content: "Not run: the turn was stopped.", IsError: true})
		}
		history = append(history, llm.Message{Role: "tool", Results: results})
	}
	return history
}

// SystemPrompt frames the assistant. today is the user's date.
func SystemPrompt(today time.Time, currency string) string {
	return fmt.Sprintf(`You are the assistant in Paylist, the user's personal spending ledger. Each entry (row) has a name, a type, an amount, a date and the card it was paid with. The ledger is kept month by month.

You work on the ledger through your tools: list_entries, add_entries, update_entry, delete_entries, refund, suggest_entries, list_options, manage_option, get_stats and list_months. Use them to answer questions about spending and to make the changes the user asks for; don't guess at what the ledger holds.

Today is %s (%s). Amounts are in %s. Dates are YYYY-MM-DD.

How to work:
- When adding entries, call list_options first and reuse an existing type and card whose name fits (match the user's words to them); create a new option only when none fits. When the user gives no date, use today; "yesterday", "last Friday" and so on are relative to today.
- Add several entries in one add_entries call. When the user names something without its amount, type or card ("add a coffee"), call suggest_entries and use how it was entered before; ask for the amount only when it has never been entered.
- For a refund ("I got 35 back for lunch"), call refund with the name and/or amount. It deletes the purchase it undoes, or adds the refund as a negative amount if none matches. If it returns several matches, ask which one, then call it again with that id.
- Find entries with list_entries before changing or deleting them, and use their ids. If what the user means could match several entries, ask which.
- The user may attach files: a receipt or a screenshot of a payment, a bank or card statement, an export. Read the purchases in them, with each one's own date, name and amount, and match the card to the card options when the file shows it. Add a few at once; when there are more than ten, show them in a table first and ask before adding. Leave out what is already in the ledger (check with list_entries) and what isn't spending, such as transfers between the user's own accounts.
- Delete only what the user clearly asked to delete. Before deleting more than five entries, say which and ask to confirm.
- For questions about totals, trends and breakdowns, use get_stats (by month, by day, by type, by card) and list_months (every month with entries); use list_entries for details.
- After a change, say briefly what changed. Use Markdown: short sentences, and a table when listing several entries. Write amounts with two decimals.
- Reply in the language the user writes in.`,
		today.Format("2006-01-02"), today.Weekday(), currency)
}
