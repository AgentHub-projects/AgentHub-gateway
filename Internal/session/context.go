package session

import "context"

type ctxKeyConn struct{}

func WithContext(ctx context.Context, conn *Conn) context.Context {
	return context.WithValue(ctx, ctxKeyConn{}, conn)
}

func FromContext(ctx context.Context) *Conn {
	conn, _ := ctx.Value(ctxKeyConn{}).(*Conn)
	return conn
}
