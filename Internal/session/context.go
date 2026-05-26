package session

import "context"

type ctxKeyConn struct{}
type ctxKeyAgentID struct{}

func WithContext(ctx context.Context, conn *Conn) context.Context {
	return context.WithValue(ctx, ctxKeyConn{}, conn)
}

func FromContext(ctx context.Context) *Conn {
	conn, _ := ctx.Value(ctxKeyConn{}).(*Conn)
	return conn
}

func WithAgentID(ctx context.Context, agentID AgentID) context.Context {
	return context.WithValue(ctx, ctxKeyAgentID{}, agentID)
}

func AgentIDFromContext(ctx context.Context) AgentID {
	agentID, _ := ctx.Value(ctxKeyAgentID{}).(AgentID)
	return agentID
}
