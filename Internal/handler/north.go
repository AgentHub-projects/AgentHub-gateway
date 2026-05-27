package handler

import (
	"context"
	"errors"
	"log/slog"

	"gateway/Internal/session"

	acp "github.com/ironpark/go-acp"
)

var _ acp.Agent = (*NorthHandler)(nil)
var _ acp.SessionCreator = (*NorthHandler)(nil)
var _ acp.SessionLoader = (*NorthHandler)(nil)
var _ acp.SessionLister = (*NorthHandler)(nil)

// NorthHandler handles ACP requests from the user side and routes them to the
// current leader agent connection.
type NorthHandler struct {
	creator SessionCreator
	store   session.Store
	logger  *slog.Logger

	agentSelector   string
	sandboxSelector string
}

func NewNorthHandler(
	ctx context.Context,
	store session.Store,
	creator SessionCreator,
	agentSelector string,
	sandboxSelector string,
) *NorthHandler {
	return &NorthHandler{
		creator:         creator,
		store:           store,
		logger:          slog.Default().With("component", "north-handler"),
		agentSelector:   agentSelector,
		sandboxSelector: sandboxSelector,
	}
}

func (h *NorthHandler) Initialize(ctx context.Context, params *acp.InitializeRequest) (*acp.InitializeResponse, error) {
	return &acp.InitializeResponse{
		ProtocolVersion: acp.ProtocolVersion(acp.CurrentProtocolVersion),
		AgentInfo: &acp.Implementation{
			Name:    "agenthub-gateway",
			Version: "dev",
		},
		AgentCapabilities: &acp.AgentCapabilities{
			LoadSession: true,
			MCPCapabilities: &acp.MCPCapabilities{
				HTTP: true,
				SSE:  true,
			},
			SessionCapabilities: &acp.SessionCapabilities{
				List: &acp.SessionListCapabilities{},
			},
		},
	}, nil
}

func (h *NorthHandler) Authenticate(ctx context.Context, params *acp.AuthenticateRequest) (*acp.AuthenticateResponse, error) {
	panic("to do")
}

func (h *NorthHandler) NewSession(ctx context.Context, params *acp.NewSessionRequest) (*acp.NewSessionResponse, error) {
	connection := session.FromContext(ctx)
	if connection == nil {
		return nil, errors.New("no connection in context")
	}

	meta := session.NewMeta(params.Cwd, params.MCPServers, params.Meta)
	conn := h.creator.Create(connection)
	logger := h.logger.With("session", conn.NorthID)

	if err := h.store.CreateSession(ctx, conn.NorthID, meta); err != nil {
		logger.Error("persist session failed", "error", err)
		return nil, err
	}

	return &acp.NewSessionResponse{SessionID: conn.NorthID}, nil
}

func (h *NorthHandler) LoadSession(ctx context.Context, params *acp.LoadSessionRequest) (*acp.LoadSessionResponse, error) {
	connection := session.FromContext(ctx)
	if connection == nil {
		return nil, errors.New("no connection in context")
	}

	dbSession, err := h.store.GetSession(ctx, params.SessionID)
	if err != nil {
		h.logger.Error("get session failed", "session", params.SessionID, "error", err)
		return nil, err
	}

	sessionID := acp.SessionID(dbSession.SessionID)
	h.creator.CreateWithID(connection, sessionID)

	return &acp.LoadSessionResponse{}, nil
}

func (h *NorthHandler) ListSessions(ctx context.Context, params *acp.ListSessionsRequest) (*acp.ListSessionsResponse, error) {
	ids, err := h.store.ListSessions(ctx)
	if err != nil {
		h.logger.Error("list sessions failed", "error", err)
		return nil, err
	}

	sessions := make([]acp.SessionInfo, len(ids))
	for i, id := range ids {
		sessions[i] = acp.SessionInfo{SessionID: id}
	}

	return &acp.ListSessionsResponse{Sessions: sessions}, nil
}

func (h *NorthHandler) Prompt(ctx context.Context, params *acp.PromptRequest) (*acp.PromptResponse, error) {
	conn, err := h.creator.FindByNorth(params.SessionID)
	if err != nil {
		h.logger.Error("find session failed", "session", params.SessionID, "error", err)
		return nil, err
	}
	logger := h.logger.With("session", conn.NorthID)

	leaderConn, err := h.leaderConn(ctx, conn)
	if err != nil {
		logger.Error("connect leader failed", "error", err)
		return nil, err
	}

	params.SessionID = conn.NorthID
	resp, err := leaderConn.Prompt(ctx, params)
	if err != nil {
		logger.Error("prompt failed", "error", err)
		_ = leaderConn.Close()
		return nil, err
	}

	return resp, nil
}

func (h *NorthHandler) Cancel(ctx context.Context, params *acp.CancelNotification) error {
	conn, err := h.creator.FindByNorth(params.SessionID)
	if err != nil {
		h.logger.Error("find session failed", "session", params.SessionID, "error", err)
		return err
	}
	if conn.LeaderConn == nil {
		return nil
	}

	params.SessionID = conn.NorthID
	return conn.LeaderConn.Cancel(ctx, params)
}

func (h *NorthHandler) SetSessionMode(ctx context.Context, params *acp.SetSessionModeRequest) (*acp.SetSessionModeResponse, error) {
	return nil, acp.ErrMethodNotFound(acp.AgentMethods.SessionSetMode)
}

func (h *NorthHandler) SetSessionConfigOption(ctx context.Context, params *acp.SetSessionConfigOptionRequest) (*acp.SetSessionConfigOptionResponse, error) {
	return nil, acp.ErrMethodNotFound(acp.AgentMethods.SessionSetConfigOption)
}

func (h *NorthHandler) leaderConn(ctx context.Context, conn *session.Conn) (*acp.ClientSideConnection, error) {
	if conn.LeaderConn != nil {
		return conn.LeaderConn, nil
	}

	agentID, err := h.leaderAgentID(ctx, conn.NorthID)
	if err != nil {
		return nil, err
	}

	return h.creator.ConnectLeader(
		ctx,
		conn.NorthID,
		agentID,
		h.agentSelector,
		h.sandboxSelector,
	)
}

func (h *NorthHandler) leaderAgentID(ctx context.Context, sessionID acp.SessionID) (session.AgentID, error) {
	dbSession, err := h.store.GetSession(ctx, sessionID)
	if err != nil {
		return "", err
	}
	if dbSession.AgentID == nil || *dbSession.AgentID == "" {
		return "", acp.ErrInvalidParams(nil, "agentId is required")
	}
	return session.AgentID(*dbSession.AgentID), nil
}
