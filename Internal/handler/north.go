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

// NorthHandler handles ACP requests from the user side and routes them to the
// current leader agent connection.
type NorthHandler struct {
	creator SessionCreator

	agentSelector   string
	sandboxSelector string
}

func NewNorthHandler(
	ctx context.Context,
	creator SessionCreator,
	agentSelector string,
	sandboxSelector string,
) *NorthHandler {
	return &NorthHandler{
		creator:         creator,
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
	conn := h.creator.Create(connection)
	if err := h.creator.PrepareSandbox(ctx, conn.NorthID, h.sandboxSelector); err != nil {
		return nil, err
	}
	slog.Debug("session created", "component", "north-handler", "session", conn.NorthID, "agent", conn.LeaderAgentID)

	return &acp.NewSessionResponse{SessionID: conn.NorthID}, nil
}

func (h *NorthHandler) LoadSession(ctx context.Context, params *acp.LoadSessionRequest) (*acp.LoadSessionResponse, error) {
	connection := session.FromContext(ctx)
	if connection == nil {
		return nil, errors.New("no connection in context")
	}

	conn := h.creator.CreateWithID(connection, params.SessionID)
	if err := h.creator.PrepareSandbox(ctx, conn.NorthID, h.sandboxSelector); err != nil {
		return nil, err
	}
	slog.Debug("session loaded", "component", "north-handler", "session", conn.NorthID, "agent", conn.LeaderAgentID)

	return &acp.LoadSessionResponse{}, nil
}

func (h *NorthHandler) Prompt(ctx context.Context, params *acp.PromptRequest) (*acp.PromptResponse, error) {
	conn, err := h.creator.FindByNorth(params.SessionID)
	if err != nil {
		return nil, err
	}
	if conn.LeaderAgentID == "" {
		meta, err := session.NewMeta(nil, params.Meta)
		if err != nil {
			return nil, acp.ErrInvalidParams(nil, err.Error())
		}
		conn.LeaderAgentID = session.AgentID(meta.AgentID)
	}

	leaderConn, err := h.leaderConn(ctx, conn)
	if err != nil {
		slog.Error("connect leader failed", "component", "north-handler", "session", conn.NorthID, "error", err)
		return nil, err
	}

	params.SessionID = conn.NorthID
	resp, err := leaderConn.Prompt(ctx, params)
	if err != nil {
		slog.Error("prompt failed", "component", "north-handler", "session", conn.NorthID, "error", err)
		_ = leaderConn.Close()
		return nil, err
	}

	return resp, nil
}

func (h *NorthHandler) Cancel(ctx context.Context, params *acp.CancelNotification) error {
	conn, err := h.creator.FindByNorth(params.SessionID)
	if err != nil {
		slog.Error("find session failed", "component", "north-handler", "session", params.SessionID, "error", err)
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
	)
}

func (h *NorthHandler) leaderAgentID(ctx context.Context, sessionID acp.SessionID) (session.AgentID, error) {
	conn, err := h.creator.FindByNorth(sessionID)
	if err != nil {
		return "", err
	}
	if conn.LeaderAgentID == "" {
		return "", acp.ErrInvalidParams(nil, "agentId is required")
	}
	return conn.LeaderAgentID, nil
}
