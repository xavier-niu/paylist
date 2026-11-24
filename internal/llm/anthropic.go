package llm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go/v2"
	"github.com/anthropics/anthropic-sdk-go/v2/option"
	"github.com/anthropics/anthropic-sdk-go/v2/packages/param"
)

const DefaultAnthropicModel = "claude-opus-5-5"

// Models that accept the server-side refusal fallback (`fallbacks: "default"`).
var fallbackModels = map[string]bool{
	"claude-fable-5-2": true, "claude-fable-5-1": true, "claude-opus-5-5": true, "claude-opus-5": true, "claude-sonnet-5-5": true,
}

// Models that accept output_config.effort.
var effortModelRe = regexp.MustCompile(`^claude-(fable-5|mythos-5|opus-5|sonnet-5|haiku-5-5|opus-4-[5678]|sonnet-4-6)`)

type anthropicClient struct {
	cfg    Config
	model  string
	client anthropic.Client
}

func newAnthropic(cfg Config) *anthropicClient {
	opts := []option.RequestOption{
		option.WithAPIKey(cfg.APIKey),
		option.WithMaxRetries(2),
		option.WithRequestTimeout(10 * time.Minute),
	}
	if cfg.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(cfg.BaseURL))
	}
	model := cfg.Model
	if model == "" {
		model = DefaultAnthropicModel
	}
	return &anthropicClient{cfg: cfg, model: model, client: anthropic.NewClient(opts...)}
}

// official reports whether requests go to Anthropic's own API rather than a
// gateway, which may not know the newer request fields.
func (c *anthropicClient) official() bool {
	return c.cfg.BaseURL == "" || strings.Contains(c.cfg.BaseURL, "api.anthropic.com")
}

// requestOptions opts into the server-side refusal fallback where supported: on a
// safety decline, the API re-runs the request on a suitable fallback model.
func (c *anthropicClient) requestOptions() []option.RequestOption {
	if c.official() && fallbackModels[c.model] {
		return []option.RequestOption{
			option.WithHeader("anthropic-beta", "server-side-fallback-2026-07-01"),
			option.WithJSONSet("fallbacks", "default"),
		}
	}
	return nil
}

func (c *anthropicClient) tools(tools []Tool) []anthropic.ToolUnionParam {
	out := make([]anthropic.ToolUnionParam, 0, len(tools))
	for _, t := range tools {
		schema := anthropic.ToolInputSchemaParam{ExtraFields: map[string]any{}}
		for k, v := range t.Schema {
			switch k {
			case "type":
			case "properties":
				schema.Properties = v
			case "required":
				schema.Required = stringList(v)
			default:
				schema.ExtraFields[k] = v
			}
		}
		tp := anthropic.ToolParam{Name: t.Name, Description: anthropic.String(t.Description), InputSchema: schema}
		if c.official() {
			// Stream tool inputs as they're written; the MCP server validates them.
			tp.EagerInputStreaming = anthropic.Bool(true)
		}
		out = append(out, anthropic.ToolUnionParam{OfTool: &tp})
	}
	return out
}

func stringList(v any) []string {
	switch l := v.(type) {
	case []string:
		return l
	case []any:
		out := make([]string, 0, len(l))
		for _, x := range l {
			if s, ok := x.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

// history turns the conversation into Claude's messages. Claude's own earlier
// replies go back exactly as they came (thinking blocks included).
func (c *anthropicClient) history(msgs []Message) ([]anthropic.MessageParam, error) {
	out := make([]anthropic.MessageParam, 0, len(msgs))
	add := func(p anthropic.MessageParam) {
		// Consecutive user turns (a tool result, then a new question) become one.
		if n := len(out); n > 0 && out[n-1].OfUser != nil && p.OfUser != nil {
			out[n-1].OfUser.Content = append(out[n-1].OfUser.Content, p.OfUser.Content...)
			return
		}
		out = append(out, p)
	}
	for _, m := range msgs {
		switch m.Role {
		case "user":
			add(anthropic.NewUserMessage(anthropic.NewTextBlock(m.Text)))
		case "tool":
			blocks := make([]anthropic.ContentBlockParamUnion, 0, len(m.Results))
			for _, r := range m.Results {
				blocks = append(blocks, anthropic.NewToolResultBlock(r.ID, r.Content, r.IsError))
			}
			add(anthropic.NewUserMessage(blocks...))
		case "assistant":
			if m.Provider == "anthropic" && json.Valid(m.Raw) {
				// Sent byte for byte as Claude wrote it.
				p := param.Override[anthropic.AssistantMessageParam](json.RawMessage(m.Raw))
				add(anthropic.MessageParam{OfAssistant: &p})
				continue
			}
			var blocks []anthropic.ContentBlockParamUnion
			if m.Text != "" {
				blocks = append(blocks, anthropic.NewTextBlock(m.Text))
			}
			for _, tc := range m.ToolCalls {
				blocks = append(blocks, anthropic.NewToolUseBlock(tc.ID, inputOrEmpty(tc.Input), tc.Name))
			}
			if len(blocks) == 0 {
				blocks = append(blocks, anthropic.NewTextBlock("(no reply)"))
			}
			add(anthropic.NewAssistantMessage(blocks...))
		}
	}
	return out, nil
}

func (c *anthropicClient) Step(ctx context.Context, system string, msgs []Message, tools []Tool, onText func(string) error) (Message, Stop, error) {
	history, err := c.history(msgs)
	if err != nil {
		return Message{}, "", err
	}
	params := anthropic.MessageNewParams{
		Model:     c.model,
		MaxTokens: 32000,
		System:    []anthropic.TextBlockParam{{Text: system}},
		Messages:  history,
		Tools:     c.tools(tools),
	}
	if effortModelRe.MatchString(c.model) {
		params.OutputConfig = anthropic.OutputConfigParam{Effort: anthropic.OutputConfigEffortMedium}
	}
	if c.official() {
		// Cache the tools, system prompt and conversation so far: each step of
		// the loop resends them.
		params.CacheControl = anthropic.CacheControlEphemeralParam{}
	}
	stream := c.client.Messages.NewStreaming(ctx, params, c.requestOptions()...)
	defer stream.Close()

	message := anthropic.Message{}
	for stream.Next() {
		event := stream.Current()
		if err := message.Accumulate(event); err != nil {
			return Message{}, "", errorf(http.StatusBadGateway, "Could not read Claude's reply: %v", err)
		}
		if delta, ok := event.AsAny().(anthropic.ContentBlockDeltaEvent); ok {
			if td, ok := delta.Delta.AsAny().(anthropic.TextDelta); ok && td.Text != "" {
				if err := onText(td.Text); err != nil {
					return Message{}, "", err
				}
			}
		}
	}
	if err := stream.Err(); err != nil {
		return Message{}, "", anthropicError(ctx, err)
	}

	out := Message{Role: "assistant", Provider: "anthropic"}
	var texts []string
	for _, block := range message.Content {
		switch b := block.AsAny().(type) {
		case anthropic.TextBlock:
			texts = append(texts, b.Text)
		case anthropic.ToolUseBlock:
			out.ToolCalls = append(out.ToolCalls, ToolCall{ID: b.ID, Name: b.Name, Input: json.RawMessage(b.JSON.Input.Raw())})
		}
	}
	out.Text = strings.Join(texts, "\n\n")
	if raw, err := json.Marshal(message.ToParam()); err == nil {
		out.Raw = raw
	}
	switch message.StopReason {
	case anthropic.StopReasonToolUse:
		return out, StopToolUse, nil
	case anthropic.StopReasonMaxTokens:
		return out, StopMaxTokens, nil
	case anthropic.StopReasonRefusal:
		return out, StopRefusal, nil
	}
	return out, StopEnd, nil
}

func anthropicError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	var apiErr *anthropic.Error
	if errors.As(err, &apiErr) {
		switch apiErr.StatusCode {
		case 401, 403:
			return errorf(http.StatusUnauthorized, "Anthropic rejected the API key (%d). Check it in Settings.", apiErr.StatusCode)
		case 404:
			return errorf(http.StatusBadRequest, "Model or endpoint not found. Check the model name and base URL in Settings.")
		case 429:
			return errorf(http.StatusTooManyRequests, "Rate limited by Anthropic. Try again shortly.")
		default:
			msg := err.Error()
			if len(msg) > 400 {
				msg = msg[:400] + "…"
			}
			return errorf(http.StatusBadGateway, "Anthropic error %d: %s", apiErr.StatusCode, msg)
		}
	}
	return errorf(http.StatusBadGateway, "Could not reach the Anthropic API.")
}
