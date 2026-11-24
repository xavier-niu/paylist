// Package llm runs the agent's model with the user's own key.
//
// Provider kinds:
//   - "anthropic": Claude via the official Anthropic Go SDK.
//   - "openai":    any OpenAI-compatible Chat Completions API with tool calling —
//     OpenAI, DeepSeek, OpenRouter, Qwen, Moonshot, Ollama, LiteLLM, vLLM, ...
//   - "fake":      an offline stand-in for demos and tests (only when enabled).
//
// A conversation is kept in a provider-neutral form ([]Message), so the user can
// switch providers between turns. Claude's own replies also keep their native
// form (Raw), so its thinking blocks go back to it unchanged.
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

type Config struct {
	Kind    string `json:"kind"`
	BaseURL string `json:"base_url"`
	APIKey  string `json:"-"`
	Model   string `json:"model"`
}

// Tool is a tool the model may call, with its input's JSON Schema.
type Tool struct {
	Name        string
	Description string
	Schema      map[string]any
}

type ToolCall struct {
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

type ToolResult struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Content string `json:"content"`
	IsError bool   `json:"is_error,omitempty"`
}

// Message is one turn of a conversation:
//   - "user":      Text
//   - "assistant": Text and ToolCalls; Provider and Raw hold the provider's own
//     form of the message, replayed as-is to the same provider
//   - "tool":      Results, one for each of the previous message's ToolCalls
type Message struct {
	Role      string          `json:"role"`
	Text      string          `json:"text,omitempty"`
	ToolCalls []ToolCall      `json:"tool_calls,omitempty"`
	Results   []ToolResult    `json:"results,omitempty"`
	Provider  string          `json:"provider,omitempty"`
	Raw       json.RawMessage `json:"raw,omitempty"`
}

// Stop says why the model stopped.
type Stop string

const (
	StopEnd       Stop = "end"        // finished its reply
	StopToolUse   Stop = "tool_use"   // wants the results of its ToolCalls
	StopMaxTokens Stop = "max_tokens" // ran out of output tokens
	StopRefusal   Stop = "refusal"    // declined
)

type Client interface {
	// Step sends the conversation and returns the model's next message,
	// calling onText with each fragment of its text as it arrives.
	Step(ctx context.Context, system string, history []Message, tools []Tool, onText func(string) error) (Message, Stop, error)
}

// Error is a user-facing provider error with an HTTP-ish status.
type Error struct {
	Msg    string
	Status int
}

func (e *Error) Error() string { return e.Msg }

func errorf(status int, format string, args ...any) *Error {
	return &Error{Msg: fmt.Sprintf(format, args...), Status: status}
}

// New returns a client for cfg. The fake client is only allowed when allowFake is set.
func New(cfg Config, allowFake bool, httpClient *http.Client) (Client, error) {
	cfg.BaseURL = strings.TrimSpace(cfg.BaseURL)
	cfg.Model = strings.TrimSpace(cfg.Model)
	cfg.APIKey = strings.TrimSpace(cfg.APIKey)
	switch cfg.Kind {
	case "anthropic":
		if cfg.APIKey == "" {
			return nil, errorf(http.StatusUnauthorized, "Add your Anthropic API key in Settings first.")
		}
		return newAnthropic(cfg), nil
	case "openai":
		if cfg.Model == "" {
			return nil, errorf(http.StatusBadRequest, "Set a model name in Settings first.")
		}
		return &openAIClient{cfg: cfg, http: httpClient}, nil
	case "fake":
		if allowFake {
			return fakeClient{}, nil
		}
	}
	return nil, errorf(http.StatusBadRequest, "Unknown provider kind %q.", cfg.Kind)
}

func httpError(status int, body []byte) *Error {
	detail := strings.Join(strings.Fields(string(bytes.ToValidUTF8(body, nil))), " ")
	if len(detail) > 400 {
		detail = detail[:400] + "…"
	}
	switch {
	case status == 401 || status == 403:
		return errorf(http.StatusUnauthorized, "Authentication failed (%d). Check your API key. %s", status, detail)
	case status == 429:
		return errorf(http.StatusTooManyRequests, "Rate limited by the provider (429). %s", detail)
	case status == 404:
		return errorf(http.StatusBadRequest, "Endpoint or model not found (404). Check the base URL and model. %s", detail)
	default:
		return errorf(http.StatusBadGateway, "The provider returned HTTP %d. %s", status, detail)
	}
}

// inputOrEmpty is a tool call's input, or {} when it has none.
func inputOrEmpty(in json.RawMessage) json.RawMessage {
	if len(bytes.TrimSpace(in)) == 0 {
		return json.RawMessage(`{}`)
	}
	return in
}
