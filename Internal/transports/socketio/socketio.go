package socketio

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/ironpark/go-acp"
)

const (
	// SocketIONamespace is the ACP namespace exposed over Socket.IO.
	SocketIONamespace = "/acp"
	// SocketIOPath is the Engine.IO endpoint path used by Socket.IO.
	SocketIOPath = "/socket.io"

	acpMessageEvent  = "acp:message"
	readEnqueueLimit = 30 * time.Second
)

// SocketIO is the minimal Socket.IO surface the ACP transport needs.
type SocketIO interface {
	On(string, func(...any)) error
	Emit(string, ...any) error
	Close()
}

type socketIOAdapter struct {
	on    func(string, func(...any)) error
	emit  func(string, ...any) error
	close func()
}

func (a *socketIOAdapter) On(event string, listener func(...any)) error {
	if a == nil || a.on == nil {
		return io.EOF
	}

	return a.on(event, listener)
}

func (a *socketIOAdapter) Emit(event string, args ...any) error {
	if a == nil || a.emit == nil {
		return io.EOF
	}

	return a.emit(event, args...)
}

func (a *socketIOAdapter) Close() {
	if a != nil && a.close != nil {
		a.close()
	}
}

type socketIOTransport struct {
	socket SocketIO
	logger *slog.Logger

	readCh    chan json.RawMessage
	writeMu   sync.Mutex
	closeCh   chan struct{}
	closeOnce sync.Once
}

func NewTransport(ctx context.Context, socket SocketIO) acp.Transport {
	t := &socketIOTransport{
		socket:  socket,
		logger:  slog.Default(),
		readCh:  make(chan json.RawMessage, 32),
		closeCh: make(chan struct{}),
	}

	if t.socket != nil {
		_ = t.socket.On(acpMessageEvent, func(args ...any) {
			msg, err := decodePayload(args...)
			if err != nil {
				t.logger.Warn("decode socket.io payload failed", "error", err)
				_ = t.Close()
				return
			}

			enqueueCtx, cancel := context.WithTimeout(ctx, readEnqueueLimit)
			defer cancel()

			select {
			case t.readCh <- msg:
			case <-t.closeCh:
			case <-enqueueCtx.Done():
				t.logger.Warn("enqueue socket.io message timed out, closing transport")
				_ = t.Close()
			}
		})
		_ = t.socket.On("disconnect", func(...any) {
			_ = t.Close()
		})
	}

	return t
}

func (t *socketIOTransport) ReadMessage(ctx context.Context) (json.RawMessage, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-t.closeCh:
		return nil, io.EOF
	case msg := <-t.readCh:
		t.logger.Info("read message", "msg", string(msg))

		return msg, nil
	}
}

func (t *socketIOTransport) WriteMessage(ctx context.Context, data json.RawMessage) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.closeCh:
		return io.EOF
	default:
	}

	t.writeMu.Lock()
	defer t.writeMu.Unlock()

	if t.socket == nil {
		return io.EOF
	}

	t.logger.Info("write message", "msg", string(data))

	return t.socket.Emit(acpMessageEvent, data)
}

func (t *socketIOTransport) Close() error {
	t.closeOnce.Do(func() {
		close(t.closeCh)
		if t.socket != nil {
			t.socket.Close()
		}
	})
	return nil
}

func decodePayload(args ...any) (json.RawMessage, error) {
	if len(args) == 0 {
		return nil, errors.New("missing socket.io payload")
	}

	switch value := args[0].(type) {
	case json.RawMessage:
		return value, nil
	case []byte:
		return value, nil
	case string:
		return []byte(value), nil
	default:
		return json.Marshal(value)
	}
}
