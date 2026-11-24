package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"
)

// Agent chats are the conversations on the agent page. The messages are kept
// as the agent package writes them (JSON), so a conversation can be shown
// again and continued.

const maxChats = 200 // per user; the oldest go first

type ChatSummary struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type Chat struct {
	ChatSummary
	Messages json.RawMessage `json:"messages"`
}

func chatTitle(title string) string {
	title = strings.Join(strings.Fields(title), " ")
	if utf8.RuneCountInString(title) > 80 {
		title = string([]rune(title)[:79]) + "…"
	}
	if title == "" {
		title = "New chat"
	}
	return title
}

// NewChatID returns a random id for a new conversation.
func NewChatID() string { return randomHex(8) }

func (s *Store) ListChats(ctx context.Context, userID int64) ([]ChatSummary, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, title, created_at, updated_at FROM agent_chats
		WHERE user_id = ? ORDER BY updated_at DESC, id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ChatSummary{}
	for rows.Next() {
		var c ChatSummary
		if err := rows.Scan(&c.ID, &c.Title, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) GetChat(ctx context.Context, userID int64, id string) (*Chat, error) {
	var c Chat
	var msgs string
	err := s.db.QueryRowContext(ctx, `SELECT id, title, messages, created_at, updated_at FROM agent_chats WHERE user_id = ? AND id = ?`,
		userID, id).Scan(&c.ID, &c.Title, &msgs, &c.CreatedAt, &c.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	c.Messages = json.RawMessage(msgs)
	return &c, nil
}

// SaveChat creates or replaces a conversation. The title is kept from the
// first save.
func (s *Store) SaveChat(ctx context.Context, userID int64, id, title string, messages json.RawMessage) error {
	ts := now()
	_, err := s.db.ExecContext(ctx, `INSERT INTO agent_chats (id, user_id, title, messages, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (user_id, id) DO UPDATE SET messages = excluded.messages, updated_at = excluded.updated_at`,
		id, userID, chatTitle(title), string(messages), ts, ts)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `DELETE FROM agent_chats WHERE user_id = ? AND id NOT IN
		(SELECT id FROM agent_chats WHERE user_id = ? ORDER BY updated_at DESC LIMIT ?)`, userID, userID, maxChats)
	return err
}

func (s *Store) DeleteChat(ctx context.Context, userID int64, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM agent_chats WHERE user_id = ? AND id = ?`, userID, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
