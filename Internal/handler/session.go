package handler

import (
	"context"

	"gateway/Internal/session"

	acp "github.com/ironpark/go-acp"
)

type SessionManager interface {
	FindByNorth(id acp.SessionID) (*session.Conn, error)
	FindAgentConnection(sessionID acp.SessionID, agentID session.AgentID) (*acp.ClientSideConnection, error)
	ResolveSandboxEndpoint(ctx context.Context, sessionID acp.SessionID) (string, error)
}

type SessionCreator interface {
	SessionManager

	Create(connection *session.Conn) *session.Conn
	CreateWithID(connection *session.Conn, id acp.SessionID) *session.Conn
	ConnectLeader(ctx context.Context, sessionID acp.SessionID, agentID session.AgentID, agentSelector, sandboxSelector string) (*acp.ClientSideConnection, error)
}
