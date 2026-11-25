package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// fakeClient stands in for a model in demos and tests (PAYLIST_DEV_FAKE=1). It
// understands "add <name> <amount> [type] [card]", which it turns into an
// add_entries call, and "wait <seconds>", which takes that long to answer (a
// slow model, to watch a turn run on); anything else gets this month's
// statistics through get_stats. After a tool call it shows the result.
type fakeClient struct{}

var (
	fakeAddRe  = regexp.MustCompile(`(?i)^\s*add\s+(.+?)\s+(-?\d+(?:\.\d+)?)(?:\s+(\S+))?(?:\s+(\S+))?\s*$`)
	fakeWaitRe = regexp.MustCompile(`(?i)^\s*wait\s+(\d{1,2})\s*$`)
)

func (fakeClient) Step(ctx context.Context, _ string, msgs []Message, tools []Tool, onText func(string) error) (Message, Stop, error) {
	if len(msgs) == 0 {
		return Message{}, "", errorf(400, "Nothing to answer.")
	}
	say := func(text string) (Message, Stop, error) {
		for _, part := range strings.SplitAfter(text, " ") {
			if err := onText(part); err != nil {
				return Message{}, "", err
			}
		}
		return Message{Role: "assistant", Provider: "fake", Text: text}, StopEnd, nil
	}
	call := func(text, name string, input any) (Message, Stop, error) {
		has := false
		for _, t := range tools {
			has = has || t.Name == name
		}
		if !has {
			return say(fmt.Sprintf("I'd use the %s tool, but it isn't available.", name))
		}
		if err := onText(text); err != nil {
			return Message{}, "", err
		}
		in, _ := json.Marshal(input)
		id := fmt.Sprintf("fake_%d", time.Now().UnixNano())
		return Message{Role: "assistant", Provider: "fake", Text: text,
			ToolCalls: []ToolCall{{ID: id, Name: name, Input: in}}}, StopToolUse, nil
	}

	last := msgs[len(msgs)-1]
	switch last.Role {
	case "tool":
		var b strings.Builder
		b.WriteString("Done. (This is the demo model: connect a real one in Settings for real answers.)\n\n")
		for _, r := range last.Results {
			content := r.Content
			if len(content) > 1500 {
				content = content[:1500] + "…"
			}
			status := "returned"
			if r.IsError {
				status = "failed"
			}
			fmt.Fprintf(&b, "`%s` %s:\n\n```json\n%s\n```\n", r.Name, status, content)
		}
		return say(b.String())
	case "user":
		if n := len(last.Attachments); n > 0 {
			return say(fmt.Sprintf("I got %d file(s), but the demo model can't read files. Connect a real model in Settings and it will read them.", n))
		}
		if m := fakeWaitRe.FindStringSubmatch(last.Text); m != nil {
			n, _ := strconv.Atoi(m[1])
			n = min(n, 60)
			var b strings.Builder
			for i := 1; i <= n; i++ {
				select {
				case <-ctx.Done():
					return Message{Role: "assistant", Provider: "fake", Text: b.String()}, "", ctx.Err()
				case <-time.After(time.Second):
				}
				word := fmt.Sprintf("%d… ", i)
				b.WriteString(word)
				if err := onText(word); err != nil {
					return Message{}, "", err
				}
			}
			return say(fmt.Sprintf("Waited %d seconds.", n))
		}
		if m := fakeAddRe.FindStringSubmatch(last.Text); m != nil {
			amount, _ := strconv.ParseFloat(m[2], 64)
			entry := map[string]any{"name": m[1], "amount": amount, "date": time.Now().Format("2006-01-02")}
			if m[3] != "" {
				entry["type"] = m[3]
			}
			if m[4] != "" {
				entry["card"] = m[4]
			}
			return call("Adding it to your ledger.", "add_entries", map[string]any{"entries": []any{entry}})
		}
		return call("Let me look at this month.", "get_stats", map[string]any{})
	}
	return say("I'm not sure what to do next.")
}
