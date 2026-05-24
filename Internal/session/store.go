package session

import (
	"context"

	acp "github.com/ironpark/go-acp"

	dbmodel "gateway/utils/database"
)

type Store interface {
	CreateSession(ctx context.Context, sessionID acp.SessionID, meta any) error
	SaveMessage(ctx context.Context, sessionID acp.SessionID, kind string, payload any) error
	LoadMessages(ctx context.Context, sessionID acp.SessionID) ([]dbmodel.Message, error)
	GetSession(ctx context.Context, sessionID acp.SessionID) (*dbmodel.Session, error)
	ListSessions(ctx context.Context) ([]acp.SessionID, error)
}
