package llm

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go/v2"
)

// Claude's reply goes back to it exactly as it came: thinking blocks (with
// their signatures), text and tool calls.
func TestAnthropicReplayKeepsReply(t *testing.T) {
	var m anthropic.Message
	if err := json.Unmarshal([]byte(`{"id":"msg_1","type":"message","role":"assistant","model":"claude-opus-5-5",
		"content":[
			{"type":"thinking","thinking":"","signature":"sig-abc"},
			{"type":"text","text":"Adding it."},
			{"type":"tool_use","id":"toolu_1","name":"add_entries","input":{"entries":[{"name":"Coffee","amount":12.5}]}}
		],
		"stop_reason":"tool_use","usage":{"input_tokens":1,"output_tokens":1}}`), &m); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(m.ToParam())
	if err != nil {
		t.Fatal(err)
	}
	c := newAnthropic(Config{Kind: "anthropic", APIKey: "k"})
	history, err := c.history([]Message{
		{Role: "user", Text: "add coffee 12.5"},
		{Role: "assistant", Provider: "anthropic", Raw: raw, Text: "Adding it.", ToolCalls: []ToolCall{{ID: "toolu_1", Name: "add_entries"}}},
		{Role: "tool", Results: []ToolResult{{ID: "toolu_1", Name: "add_entries", Content: "ok"}}},
		{Role: "user", Text: "thanks"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 3 {
		t.Fatalf("%d messages, want 3 (the tool result and the next question merged)", len(history))
	}
	again, _ := json.Marshal(history[1])
	if string(again) != string(raw) {
		t.Errorf("replayed reply changed:\n%s\n%s", raw, again)
	}
	for _, want := range []string{`"signature":"sig-abc"`, `"type":"tool_use"`, `"Coffee"`} {
		if !strings.Contains(string(again), want) {
			t.Errorf("replayed reply lacks %s: %s", want, again)
		}
	}
	user, _ := json.Marshal(history[2])
	if !strings.Contains(string(user), `"tool_result"`) || !strings.Contains(string(user), `"thanks"`) {
		t.Errorf("merged user turn: %s", user)
	}
}

// A reply from another provider is rebuilt from its text and tool calls.
func TestAnthropicReplayOtherProvider(t *testing.T) {
	c := newAnthropic(Config{Kind: "anthropic", APIKey: "k"})
	history, err := c.history([]Message{
		{Role: "user", Text: "hi"},
		{Role: "assistant", Provider: "openai", Text: "Looking.", ToolCalls: []ToolCall{{ID: "call_1", Name: "get_stats", Input: json.RawMessage(`{}`)}}},
		{Role: "tool", Results: []ToolResult{{ID: "call_1", Name: "get_stats", Content: "{}"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(history[1])
	if !strings.Contains(string(b), `"id":"call_1"`) || !strings.Contains(string(b), `"name":"get_stats"`) {
		t.Errorf("rebuilt reply: %s", b)
	}
}

func TestOpenAIMessages(t *testing.T) {
	c := &openAIClient{cfg: Config{Model: "m"}}
	msgs := c.messages("sys", []Message{
		{Role: "user", Text: "hi"},
		{Role: "assistant", ToolCalls: []ToolCall{{ID: "c1", Name: "list_entries", Input: nil}}},
		{Role: "tool", Results: []ToolResult{{ID: "c1", Content: "bad", IsError: true}}},
	})
	b, _ := json.Marshal(msgs)
	for _, want := range []string{`"role":"system"`, `"arguments":"{}"`, `"tool_call_id":"c1"`, `"content":"Error: bad"`, `"content":null`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("missing %s in %s", want, b)
		}
	}
}

func TestAttachments(t *testing.T) {
	b64 := func(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }
	msg := Message{Role: "user", Text: "add these", Attachments: []Attachment{
		{Name: "receipt.png", Type: "image/png", Data: b64("png bytes")},
		{Name: "statement.pdf", Type: "application/pdf", Data: b64("%PDF-1.7")},
		{Name: "export.csv", Type: "text/csv", Data: b64("name,amount\nCoffee,18\n")},
	}}

	// Claude: an image, two documents (the CSV as text), then the question.
	history, err := newAnthropic(Config{Kind: "anthropic", APIKey: "k"}).history([]Message{msg})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(history[0])
	got := string(b)
	for _, want := range []string{`"type":"image"`, `"media_type":"image/png"`, `"application/pdf"`, `"title":"statement.pdf"`, `"media_type":"text/plain"`, `name,amount\nCoffee,18\n`} {
		if !strings.Contains(got, want) {
			t.Errorf("Claude: missing %s in %s", want, got)
		}
	}
	if strings.Index(got, `"text":"add these"`) < strings.Index(got, `"type":"image"`) {
		t.Errorf("Claude: the question should follow the files: %s", got)
	}

	// OpenAI-compatible: a list of parts.
	msgs := (&openAIClient{cfg: Config{Model: "m"}}).messages("sys", []Message{msg, {Role: "user", Text: "plain"}})
	b, _ = json.Marshal(msgs)
	got = string(b)
	for _, want := range []string{`"type":"image_url"`, `"url":"data:image/png;base64,`, `"type":"file"`, `"filename":"statement.pdf"`, `File export.csv:`, `"text":"add these"`, `"content":"plain"`} {
		if !strings.Contains(got, want) {
			t.Errorf("OpenAI: missing %s in %s", want, got)
		}
	}
}
