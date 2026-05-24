package socketio

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"time"

	acp "github.com/ironpark/go-acp"
	socketclient "github.com/zishang520/socket.io/clients/socket/v3"
)

const defaultSocketIOConnectTimeout = 1 * time.Minute

// Connector dials the south agent over Socket.IO.
type Connector struct{}

func (c *Connector) Connect(ctx context.Context, endpoint string) (acp.Transport, error) {
	connectCtx, cancel := context.WithTimeout(ctx, defaultSocketIOConnectTimeout)
	defer cancel()

	opts := socketclient.DefaultOptions()
	opts.SetAutoConnect(false)
	opts.SetForceNew(true)
	opts.SetMultiplex(false)
	opts.SetPath(SocketIOPath)
	opts.SetReconnection(false)
	opts.SetTimeout(defaultSocketIOConnectTimeout)

	socket, err := socketclient.Connect(socketIOEndpointURL(endpoint), opts)
	if err != nil {
		return nil, fmt.Errorf("prepare socket.io client: %w", err)
	}

	connectedCh := make(chan struct{}, 1)
	errCh := make(chan error, 1)

	if err := socket.On("connect", func(...any) {
		select {
		case connectedCh <- struct{}{}:
		default:
		}
	}); err != nil {
		socket.Close()
		return nil, fmt.Errorf("register socket.io connect handler: %w", err)
	}

	if err := socket.On("connect_error", func(args ...any) {
		select {
		case errCh <- socketIOConnectError(args...):
		default:
		}
	}); err != nil {
		socket.Close()
		return nil, fmt.Errorf("register socket.io connect_error handler: %w", err)
	}

	socket.Connect()

	select {
	case <-connectedCh:
		return NewTransport(ctx, NewSocketIOClient(socket)), nil
	case err := <-errCh:
		socket.Close()
		return nil, fmt.Errorf("connect socket.io: %w", err)
	case <-connectCtx.Done():
		socket.Close()
		return nil, fmt.Errorf("connect socket.io: %w", connectCtx.Err())
	}
}

func socketIOConnectError(args ...any) error {
	if len(args) == 0 {
		return errors.New("unknown socket.io error")
	}

	if err, ok := args[0].(error); ok {
		return err
	}

	if value, ok := args[0].(fmt.Stringer); ok {
		return errors.New(value.String())
	}

	return fmt.Errorf("%v", args[0])
}

func socketIOEndpointURL(endpoint string) string {
	return (&url.URL{
		Scheme: "http",
		Host:   endpoint,
		Path:   SocketIONamespace,
	}).String()
}
