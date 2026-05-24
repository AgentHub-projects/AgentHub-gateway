package session

import (
	"context"

	"github.com/ironpark/go-acp"
)

type Resolver interface {
	Resolve(ctx context.Context, sessionID acp.SessionID, agentID AgentID, templateSelector string) (string, error)
	Release(ctx context.Context, sessionID acp.SessionID, agentID AgentID) error
}

type Connector interface {
	Connect(ctx context.Context, endpoint string) (acp.Transport, error)
}
