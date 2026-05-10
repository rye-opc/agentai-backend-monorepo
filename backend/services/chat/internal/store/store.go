package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("not found")

type Store struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

type Conversation struct {
	ID        string
	UserID    string
	Title     string
	Pinned    bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

type Message struct {
	ID             string
	ConversationID string
	UserID         string
	Role           string
	Content        string
	AudioRef       *string
	RunID          *string
	CreatedAt      time.Time
}

func (s *Store) CreateConversation(ctx context.Context, userID string, title string) (Conversation, error) {
	var c Conversation
	if err := s.pool.QueryRow(ctx, `
		INSERT INTO conversations (user_id, title) VALUES ($1, $2)
		RETURNING id, user_id, title, pinned, created_at, updated_at
	`, userID, title).Scan(&c.ID, &c.UserID, &c.Title, &c.Pinned, &c.CreatedAt, &c.UpdatedAt); err != nil {
		return Conversation{}, err
	}
	return c, nil
}

func (s *Store) ListConversations(
	ctx context.Context,
	userID string,
	limit int,
	cursorCreatedAtUnixMs *int64,
	cursorID *string,
) (items []Conversation, nextCreatedAtUnixMs *int64, nextID *string, err error) {
	limitPlusOne := limit + 1

	var rows pgx.Rows
	if cursorCreatedAtUnixMs != nil && cursorID != nil && *cursorID != "" {
		rows, err = s.pool.Query(ctx, `
			SELECT id, user_id, title, pinned, created_at, updated_at
			FROM conversations
			WHERE user_id = $1
			  AND (created_at, id) < (to_timestamp($2 / 1000.0), $3::uuid)
			ORDER BY created_at DESC, id DESC
			LIMIT $4
		`, userID, *cursorCreatedAtUnixMs, *cursorID, limitPlusOne)
	} else {
		rows, err = s.pool.Query(ctx, `
			SELECT id, user_id, title, pinned, created_at, updated_at
			FROM conversations
			WHERE user_id = $1
			ORDER BY created_at DESC, id DESC
			LIMIT $2
		`, userID, limitPlusOne)
	}
	if err != nil {
		return nil, nil, nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var c Conversation
		if err := rows.Scan(&c.ID, &c.UserID, &c.Title, &c.Pinned, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, nil, nil, err
		}
		items = append(items, c)
	}
	if rows.Err() != nil {
		return nil, nil, nil, rows.Err()
	}

	if len(items) <= limit {
		return items, nil, nil, nil
	}

	items = items[:limit]
	last := items[len(items)-1]
	ms := last.CreatedAt.UnixMilli()
	nextCreatedAtUnixMs = &ms
	nextID = &last.ID
	return items, nextCreatedAtUnixMs, nextID, nil
}

func (s *Store) CreateMessage(
	ctx context.Context,
	userID string,
	conversationID string,
	role string,
	content string,
	audioRef *string,
) (Message, error) {
	var m Message

	row := s.pool.QueryRow(ctx, `
		WITH conv AS (
			SELECT id
			FROM conversations
			WHERE id = $1 AND user_id = $2
		)
		INSERT INTO messages (conversation_id, user_id, role, content, audio_ref)
		SELECT conv.id, $2, $3, $4, $5
		FROM conv
		RETURNING id, conversation_id, user_id, role, content, audio_ref, run_id, created_at
	`, conversationID, userID, role, content, audioRef)

	if err := row.Scan(&m.ID, &m.ConversationID, &m.UserID, &m.Role, &m.Content, &m.AudioRef, &m.RunID, &m.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Message{}, ErrNotFound
		}
		return Message{}, err
	}

	return m, nil
}

func (s *Store) ListMessages(
	ctx context.Context,
	userID string,
	conversationID string,
	limit int,
	cursorCreatedAtUnixMs *int64,
	cursorID *string,
) (items []Message, nextCreatedAtUnixMs *int64, nextID *string, err error) {
	limitPlusOne := limit + 1

	var rows pgx.Rows
	if cursorCreatedAtUnixMs != nil && cursorID != nil && *cursorID != "" {
		rows, err = s.pool.Query(ctx, `
			SELECT m.id, m.conversation_id, m.user_id, m.role, m.content, m.audio_ref, m.run_id, m.created_at
			FROM messages m
			JOIN conversations c ON c.id = m.conversation_id
			WHERE m.conversation_id = $1
			  AND c.user_id = $2
			  AND (m.created_at, m.id) < (to_timestamp($3 / 1000.0), $4::uuid)
			ORDER BY m.created_at DESC, m.id DESC
			LIMIT $5
		`, conversationID, userID, *cursorCreatedAtUnixMs, *cursorID, limitPlusOne)
	} else {
		rows, err = s.pool.Query(ctx, `
			SELECT m.id, m.conversation_id, m.user_id, m.role, m.content, m.audio_ref, m.run_id, m.created_at
			FROM messages m
			JOIN conversations c ON c.id = m.conversation_id
			WHERE m.conversation_id = $1
			  AND c.user_id = $2
			ORDER BY m.created_at DESC, m.id DESC
			LIMIT $3
		`, conversationID, userID, limitPlusOne)
	}
	if err != nil {
		return nil, nil, nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.ID, &m.ConversationID, &m.UserID, &m.Role, &m.Content, &m.AudioRef, &m.RunID, &m.CreatedAt); err != nil {
			return nil, nil, nil, err
		}
		items = append(items, m)
	}
	if rows.Err() != nil {
		return nil, nil, nil, rows.Err()
	}

	if len(items) <= limit {
		return items, nil, nil, nil
	}

	items = items[:limit]
	last := items[len(items)-1]
	ms := last.CreatedAt.UnixMilli()
	nextCreatedAtUnixMs = &ms
	nextID = &last.ID
	return items, nextCreatedAtUnixMs, nextID, nil
}
