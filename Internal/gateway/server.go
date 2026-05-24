package gateway

import (
	"context"
	"errors"
	"gateway/Internal/transports/socketio"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"gateway/Internal/session"

	acp "github.com/ironpark/go-acp"
	"github.com/zishang520/socket.io/servers/socket/v3"
)

type Server struct {
	http.Handler

	rootCtx      context.Context
	cancel       context.CancelFunc
	socketServer *socket.Server

	manager *session.Manager
	store   session.Store
}

func NewServer(ctx context.Context, manager *session.Manager, store session.Store) *Server {
	rootCtx, cancel := context.WithCancel(ctx)
	socketServer := socket.NewServer(nil, socket.DefaultServerOptions())

	s := &Server{
		rootCtx:      rootCtx,
		cancel:       cancel,
		socketServer: socketServer,
		manager:      manager,
		store:        store,
	}

	acp := socketServer.Of(socketio.SocketIONamespace, nil)
	_ = acp.On("connection", func(clients ...any) {
		client := clients[0].(*socket.Socket)

		cancelCtx, cancel := context.WithCancel(ctx)
		if err := client.On("disconnect", func(args ...any) {
			slog.Info("north socket disconnected")
			cancel()
		}); err != nil {
			slog.Error("north disconnect handler failed", "error", err)
		}
		s.handleSocket(cancelCtx, cancel, client)
	})

	mux := http.NewServeMux()
	socketHandler := socketServer.ServeHandler(nil)
	mux.Handle(socketio.SocketIOPath, socketHandler)
	mux.Handle(socketio.SocketIOPath+"/", socketHandler)
	mux.HandleFunc("/filesystem/", s.handleHTTP)
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	s.Handler = mux

	return s
}

func (s *Server) handleSocket(ctx context.Context, cancel context.CancelFunc, server *socket.Socket) {
	go func() {
		defer cancel()
		tr := socketio.NewTransport(ctx, socketio.NewSocketIOServer(server))
		if err := s.manager.ServeNorth(ctx, tr); err != nil {
			slog.Warn("serve north connection failed", "error", err)
		}
		slog.Info("serve north connection closed")
	}()
}

func (s *Server) Close() error {
	s.cancel()
	if s.socketServer != nil {
		s.socketServer.Close(nil)
	}
	return s.manager.Close()
}

func (s *Server) handleHTTP(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	sessionID := query.Get("sessionId")
	if sessionID == "" {
		http.Error(w, "session id required", http.StatusBadRequest)
		return
	}

	if s.store == nil {
		slog.Error("session store not configured")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	dbSession, err := s.store.GetSession(r.Context(), acp.SessionID(sessionID))
	if err != nil {
		if errors.Is(err, session.ErrSessionNotFound) {
			http.Error(w, "no session found", http.StatusNotFound)
			return
		}

		slog.Error("get session failed", "error", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	endpoint, err := s.manager.ResolveSandboxEndpoint(r.Context(), acp.SessionID(dbSession.SessionID))
	if err != nil {
		if errors.Is(err, session.ErrSessionNotFound) {
			http.Error(w, "no session found", http.StatusNotFound)
			return
		}

		slog.Error("resolve sandbox endpoint failed", "error", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if endpoint == "" {
		slog.Error("empty sandbox endpoint")
		http.Error(w, "no session endpoint found", http.StatusBadGateway)
		return
	}

	targetURL, err := proxyTargetURL(endpoint)
	if err != nil {
		slog.Error("invalid sandbox endpoint", "endpoint", endpoint, "error", err)
		http.Error(w, http.StatusText(http.StatusBadGateway), http.StatusBadGateway)
		return
	}

	proxy := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(targetURL)
			query := pr.Out.URL.Query()
			query.Set("sessionId", dbSession.SessionID)
			pr.Out.URL.RawQuery = query.Encode()
		},
		ErrorHandler: func(rw http.ResponseWriter, req *http.Request, proxyErr error) {
			slog.Error("proxy sandbox request failed", "error", proxyErr)
			http.Error(rw, http.StatusText(http.StatusBadGateway), http.StatusBadGateway)
		},
	}

	proxy.ServeHTTP(w, r)
}

func proxyTargetURL(address string) (*url.URL, error) {
	if !strings.Contains(address, "://") {
		address = "http://" + address
	}
	parsed, err := url.Parse(address)
	if err != nil {
		return nil, err
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return nil, errors.New("missing endpoint scheme or host")
	}
	return parsed, nil
}
