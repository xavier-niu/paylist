package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sort"
	"strings"
)

const defaultOpenAIBase = "https://api.openai.com/v1"

// openAIClient speaks the OpenAI Chat Completions protocol with tool calling,
// which OpenAI, DeepSeek, OpenRouter, Qwen, Moonshot, Ollama and most gateways
// implement.
type openAIClient struct {
	cfg  Config
	http *http.Client
}

func (c *openAIClient) url() string {
	base := strings.TrimRight(c.cfg.BaseURL, "/")
	if base == "" {
		base = defaultOpenAIBase
	}
	if strings.HasSuffix(base, "/chat/completions") {
		return base
	}
	return base + "/chat/completions"
}

type oaiFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type oaiToolCall struct {
	Index    int         `json:"index"`
	ID       string      `json:"id,omitempty"`
	Type     string      `json:"type,omitempty"`
	Function oaiFunction `json:"function"`
}

type oaiMessage struct {
	Role       string        `json:"role"`
	Content    *string       `json:"content"`
	ToolCalls  []oaiToolCall `json:"tool_calls,omitempty"`
	ToolCallID string        `json:"tool_call_id,omitempty"`
}

func str(s string) *string { return &s }

func (c *openAIClient) messages(system string, msgs []Message) []oaiMessage {
	out := []oaiMessage{{Role: "system", Content: str(system)}}
	for _, m := range msgs {
		switch m.Role {
		case "user":
			out = append(out, oaiMessage{Role: "user", Content: str(m.Text)})
		case "assistant":
			om := oaiMessage{Role: "assistant"}
			if m.Text != "" || len(m.ToolCalls) == 0 {
				om.Content = str(m.Text)
			}
			for i, tc := range m.ToolCalls {
				om.ToolCalls = append(om.ToolCalls, oaiToolCall{Index: i, ID: tc.ID, Type: "function",
					Function: oaiFunction{Name: tc.Name, Arguments: string(inputOrEmpty(tc.Input))}})
			}
			out = append(out, om)
		case "tool":
			for _, r := range m.Results {
				content := r.Content
				if r.IsError {
					content = "Error: " + content
				}
				out = append(out, oaiMessage{Role: "tool", ToolCallID: r.ID, Content: str(content)})
			}
		}
	}
	return out
}

func (c *openAIClient) Step(ctx context.Context, system string, msgs []Message, tools []Tool, onText func(string) error) (Message, Stop, error) {
	defs := make([]map[string]any, 0, len(tools))
	for _, t := range tools {
		defs = append(defs, map[string]any{"type": "function", "function": map[string]any{
			"name": t.Name, "description": t.Description, "parameters": t.Schema,
		}})
	}
	body := map[string]any{"model": c.cfg.Model, "messages": c.messages(system, msgs), "stream": true}
	if len(defs) > 0 {
		body["tools"] = defs
	}
	buf, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url(), bytes.NewReader(buf))
	if err != nil {
		return Message{}, "", errorf(http.StatusBadRequest, "Invalid base URL: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	if c.cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	}
	if strings.Contains(c.cfg.BaseURL, "openrouter.ai") {
		req.Header.Set("X-Title", "Paylist")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return Message{}, "", ctx.Err()
		}
		return Message{}, "", errorf(http.StatusBadGateway, "Could not reach the provider at %s.", c.url())
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return Message{}, "", httpError(resp.StatusCode, b)
	}

	var text strings.Builder
	calls := map[int]*oaiToolCall{}
	finish := ""
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		data, ok := strings.CutPrefix(line, "data:")
		if !ok {
			continue
		}
		data = strings.TrimSpace(data)
		if data == "[DONE]" {
			break
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content   string        `json:"content"`
					ToolCalls []oaiToolCall `json:"tool_calls"`
				} `json:"delta"`
				FinishReason *string `json:"finish_reason"`
			} `json:"choices"`
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}
		if chunk.Error != nil {
			return Message{}, "", errorf(http.StatusBadGateway, "The provider reported an error: %s", chunk.Error.Message)
		}
		for _, ch := range chunk.Choices {
			if d := ch.Delta.Content; d != "" {
				text.WriteString(d)
				if err := onText(d); err != nil {
					return Message{}, "", err
				}
			}
			for _, tc := range ch.Delta.ToolCalls {
				cur, ok := calls[tc.Index]
				if !ok {
					cur = &oaiToolCall{Index: tc.Index}
					calls[tc.Index] = cur
				}
				if tc.ID != "" {
					cur.ID = tc.ID
				}
				if tc.Function.Name != "" {
					cur.Function.Name += tc.Function.Name
				}
				cur.Function.Arguments += tc.Function.Arguments
			}
			if ch.FinishReason != nil && *ch.FinishReason != "" {
				finish = *ch.FinishReason
			}
		}
	}
	if err := sc.Err(); err != nil {
		if ctx.Err() != nil {
			return Message{}, "", ctx.Err()
		}
		if !errors.Is(err, io.EOF) {
			return Message{}, "", errorf(http.StatusBadGateway, "The provider's reply broke off: %v", err)
		}
	}

	out := Message{Role: "assistant", Provider: "openai", Text: strings.TrimSpace(text.String())}
	idx := make([]int, 0, len(calls))
	for i := range calls {
		idx = append(idx, i)
	}
	sort.Ints(idx)
	for n, i := range idx {
		tc := calls[i]
		if tc.ID == "" {
			tc.ID = "call_" + string(rune('a'+n))
		}
		out.ToolCalls = append(out.ToolCalls, ToolCall{ID: tc.ID, Name: tc.Function.Name, Input: json.RawMessage(validJSON(tc.Function.Arguments))})
	}
	switch {
	case finish == "length":
		return out, StopMaxTokens, nil
	case finish == "content_filter":
		return out, StopRefusal, nil
	case len(out.ToolCalls) > 0:
		return out, StopToolUse, nil
	}
	return out, StopEnd, nil
}

// validJSON returns s if it is JSON, and a JSON string holding it otherwise, so
// a malformed tool call still stores (and fails validation at the tool).
func validJSON(s string) string {
	if strings.TrimSpace(s) == "" {
		return "{}"
	}
	if json.Valid([]byte(s)) {
		return s
	}
	b, _ := json.Marshal(s)
	return string(b)
}
