package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"

	"gateway/Internal/session"
	"gateway/pkg/constants"

	acp "github.com/ironpark/go-acp"
)

const defaultAgentID = "leader"

var _ acp.Agent = (*NorthHandler)(nil)
var _ acp.SessionCreator = (*NorthHandler)(nil)
var _ acp.SessionLoader = (*NorthHandler)(nil)
var _ acp.SessionLister = (*NorthHandler)(nil)
var _ acp.ExtMethodHandler = (*NorthHandler)(nil)

// NorthHandler handles ACP requests from the user side and routes them to the
// current leader agent connection.
type NorthHandler struct {
	creator SessionCreator
	store   session.Store
	logger  *slog.Logger

	defaultAgentID          session.AgentID
	leaderTemplateSelector  string
	sandboxTemplateSelector string
}

func NewNorthHandler(ctx context.Context, store session.Store, creator SessionCreator) *NorthHandler {
	return &NorthHandler{
		creator:                 creator,
		store:                   store,
		logger:                  slog.Default().With("component", "north-handler"),
		defaultAgentID:          session.AgentID(defaultAgentID),
		leaderTemplateSelector:  constants.AgentLabel,
		sandboxTemplateSelector: constants.SandboxLabel,
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
	return &acp.AuthenticateResponse{}, nil
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
		logger.Warn("persist session failed", "error", err)
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
	conn := h.creator.CreateWithID(connection, sessionID)

	records, err := h.store.LoadMessages(ctx, sessionID)
	if err != nil {
		h.logger.Error("load messages failed", "session", sessionID, "error", err)
		return nil, err
	}

	for _, rec := range records {
		if rec.Kind != acp.ClientMethods.SessionUpdate {
			continue
		}

		var update acp.SessionNotification
		if err := json.Unmarshal(rec.Payload, &update); err != nil {
			h.logger.Warn("skip malformed replay message", "session", sessionID, "message", rec.ID, "error", err)
			continue
		}

		update.SessionID = sessionID
		if err := conn.NorthConn.SessionUpdate(ctx, &update); err != nil {
			h.logger.Error("replay session update failed", "session", sessionID, "message", rec.ID, "error", err)
			return nil, err
		}
	}

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

	if err := h.store.SaveMessage(ctx, conn.NorthID, acp.AgentMethods.SessionPrompt, params); err != nil {
		logger.Error("persist prompt failed", "error", err)
		return nil, err
	}

	leaderConn, err := h.leaderConn(ctx, conn, params.Meta)
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
	conn, err := h.creator.FindByNorth(params.SessionID)
	if err != nil {
		return nil, err
	}
	if conn.LeaderConn == nil {
		return &acp.SetSessionModeResponse{}, nil
	}

	params.SessionID = conn.NorthID
	return conn.LeaderConn.SetSessionMode(ctx, params)
}

func (h *NorthHandler) SetSessionConfigOption(ctx context.Context, params *acp.SetSessionConfigOptionRequest) (*acp.SetSessionConfigOptionResponse, error) {
	conn, err := h.creator.FindByNorth(params.SessionID)
	if err != nil {
		return nil, err
	}
	if conn.LeaderConn == nil {
		return &acp.SetSessionConfigOptionResponse{}, nil
	}

	params.SessionID = conn.NorthID
	return conn.LeaderConn.SetSessionConfigOption(ctx, params)
}

func (h *NorthHandler) ExtMethod(ctx context.Context, method string, params json.RawMessage) (any, error) {
	raw := make(map[string]any)
	if err := json.Unmarshal(params, &raw); err != nil {
		return nil, err
	}

	sessionID, _ := raw["sessionId"].(string)
	if sessionID == "" {
		return nil, errors.New("missing session id")
	}

	conn, err := h.creator.FindByNorth(acp.SessionID(sessionID))
	if err != nil {
		return nil, err
	}

	agentConn := conn.LeaderConn
	if agentID := session.MetaString(raw, session.MetaAgentID); agentID != "" {
		agentConn, err = h.creator.FindAgentConnection(conn.NorthID, session.AgentID(agentID))
		if err != nil {
			return nil, err
		}
	}
	if agentConn == nil {
		return nil, errors.New("agent connection not established")
	}

	raw["sessionId"] = string(conn.NorthID)
	return agentConn.ExtMethod(ctx, method, raw)
}

func (h *NorthHandler) leaderConn(ctx context.Context, conn *session.Conn, meta map[string]any) (*acp.ClientSideConnection, error) {
	if conn.LeaderConn != nil {
		return conn.LeaderConn, nil
	}

	route, err := h.resolveRoute(ctx, conn.NorthID, meta)
	if err != nil {
		return nil, err
	}

	return h.creator.ConnectLeader(
		ctx,
		conn.NorthID,
		route.agentID,
		route.leaderTemplateSelector,
		route.sandboxTemplateSelector,
	)
}

type route struct {
	agentID                 session.AgentID
	leaderTemplateSelector  string
	sandboxTemplateSelector string
}

func (h *NorthHandler) resolveRoute(ctx context.Context, sessionID acp.SessionID, meta map[string]any) (route, error) {
	var r route

	if dbSession, err := h.store.GetSession(ctx, sessionID); err == nil {
		if dbSession.AgentID != nil {
			r.agentID = session.AgentID(*dbSession.AgentID)
		}

		var stored session.Meta
		if err := json.Unmarshal(dbSession.Meta, &stored); err == nil {
			r.apply(stored)
		}
	}

	r.apply(session.NewMeta("", nil, meta))

	if r.agentID == "" {
		r.agentID = h.defaultAgentID
	}
	if r.leaderTemplateSelector == "" {
		r.leaderTemplateSelector = h.leaderTemplateSelector
	}
	if r.sandboxTemplateSelector == "" {
		r.sandboxTemplateSelector = h.sandboxTemplateSelector
	}

	return r, nil
}

func (r *route) apply(meta session.Meta) {
	if meta.AgentID != "" {
		r.agentID = session.AgentID(meta.AgentID)
	}
	if meta.LeaderTemplateSelector != "" {
		r.leaderTemplateSelector = meta.LeaderTemplateSelector
	}
	if meta.SandboxTemplateSelector != "" {
		r.sandboxTemplateSelector = meta.SandboxTemplateSelector
	}
}
