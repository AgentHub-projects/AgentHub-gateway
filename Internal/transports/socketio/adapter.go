package socketio

import (
	socketclient "github.com/zishang520/socket.io/clients/socket/v3"
	"github.com/zishang520/socket.io/servers/socket/v3"
	"github.com/zishang520/socket.io/v3/pkg/types"
)

func NewSocketIOClient(socket *socketclient.Socket) SocketIO {
	return &socketIOAdapter{
		on: func(event string, listener func(...any)) error {
			return socket.On(types.EventName(event), listener)
		},
		emit: func(event string, args ...any) error {
			return socket.Emit(event, args...)
		},
		close: func() {
			if socket != nil {
				socket.Close()
			}
		},
	}
}

func NewSocketIOServer(socket *socket.Socket) SocketIO {
	return &socketIOAdapter{
		on: func(event string, listener func(...any)) error {
			return socket.On(event, listener)
		},
		emit: func(event string, args ...any) error {
			return socket.Emit(event, args...)
		},
		close: func() {
			if socket != nil && socket.Connected() {
				socket.Disconnect(true)
			}
		},
	}
}
