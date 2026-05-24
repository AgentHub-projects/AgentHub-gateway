package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	acp "github.com/ironpark/go-acp"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"gateway/Internal/session"
	dbmodel "gateway/utils/database"
)

type DB struct {
	client *gorm.DB
}

var _ session.Store = (*DB)(nil)

func NewDB(client *gorm.DB) *DB {
	return &DB{client: client}
}

// CreateSession persists a new session record.
func (s *DB) CreateSession(ctx context.Context, sessionID acp.SessionID, meta any) error {
	raw, err := marshalJSON(meta)
	if err != nil {
		return fmt.Errorf("marshal session meta: %w", err)
	}

	record := &dbmodel.Session{
		SessionID: string(sessionID),
		ChatType:  session.ChatTypeSingle,
		Meta:      raw,
	}
	if sessionMeta, ok := meta.(session.Meta); ok {
		record.ChatType = sessionMeta.ChatType
		if record.ChatType == "" {
			record.ChatType = session.ChatTypeSingle
		}
		if sessionMeta.AgentID != "" {
			agentID := sessionMeta.AgentID
			record.AgentID = &agentID
		}
		if sessionMeta.AgentGroupID != "" {
			agentGroupID := sessionMeta.AgentGroupID
			record.AgentGroupID = &agentGroupID
		}
	}
	if err := s.client.WithContext(ctx).Create(record).Error; err != nil {
		return fmt.Errorf("create session: %w", err)
	}

	return nil
}

// SaveMessage appends a message to a session.
func (s *DB) SaveMessage(ctx context.Context, sessionID acp.SessionID, kind string, payload any) error {
	raw, err := marshalJSON(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	record := &dbmodel.Message{
		SessionID: string(sessionID),
		Kind:      normalizeMessageKind(kind),
		Payload:   raw,
	}
	if err := s.client.WithContext(ctx).Create(record).Error; err != nil {
		return fmt.Errorf("save message: %w", err)
	}

	return nil
}

// LoadMessages returns all messages for a session ordered by insert order.
func (s *DB) LoadMessages(ctx context.Context, sessionID acp.SessionID) ([]dbmodel.Message, error) {
	var messages []dbmodel.Message
	if err := s.client.WithContext(ctx).
		Where("session_id = ?", sessionID).
		Order("id ASC").
		Find(&messages).Error; err != nil {
		return nil, fmt.Errorf("load messages: %w", err)
	}

	for i := range messages {
		messages[i].Kind = normalizeMessageKind(messages[i].Kind)
	}

	return messages, nil
}

func normalizeMessageKind(kind string) string {
	switch kind {
	case "prompt":
		return acp.AgentMethods.SessionPrompt
	case "session_update":
		return acp.ClientMethods.SessionUpdate
	default:
		return kind
	}
}

// GetSession loads a session by ID.
func (s *DB) GetSession(ctx context.Context, sessionID acp.SessionID) (*dbmodel.Session, error) {
	var record dbmodel.Session
	err := s.client.WithContext(ctx).
		Where("session_id = ?", sessionID).
		Take(&record).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("%w: %s", session.ErrSessionNotFound, sessionID)
		}
		return nil, fmt.Errorf("get session: %w", err)
	}

	return &record, nil
}

// ListSessions returns all session IDs.
func (s *DB) ListSessions(ctx context.Context) ([]acp.SessionID, error) {
	var sessionIDs []string
	if err := s.client.WithContext(ctx).
		Model(&dbmodel.Session{}).
		Order("created_at ASC").
		Pluck("session_id", &sessionIDs).Error; err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}

	ids := make([]acp.SessionID, len(sessionIDs))
	for i, sessionID := range sessionIDs {
		ids[i] = acp.SessionID(sessionID)
	}

	return ids, nil
}

func marshalJSON(value any) (datatypes.JSON, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}

	return datatypes.JSON(raw), nil
}
