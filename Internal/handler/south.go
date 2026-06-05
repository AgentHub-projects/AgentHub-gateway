package handler

import (
	"context"
	"encoding/json"
	"errors"

	"gateway/Internal/session"

	acp "github.com/ironpark/go-acp"
)

var _ acp.Client = (*SouthHandler)(nil)
var _ acp.ExtMethodHandler = (*SouthHandler)(nil)

// SouthHandler handles callbacks from agent-side connections. Session updates
// and permission prompts go north. Workspace tools are intentionally not
// implemented here; agents must use adaptor-registered sandbox tools instead.
type SouthHandler struct {
	manager       SessionManager
	agentSelector string
}

func NewSouthHandler(ctx context.Context, manager SessionManager, agentSelector string) *SouthHandler {
	return &SouthHandler{
		manager:       manager,
		agentSelector: agentSelector,
	}
}

func (h *SouthHandler) SessionUpdate(ctx context.Context, params *acp.SessionNotification) error {
	conn, err := h.northConn(params.SessionID)
	if err != nil {
		return err
	}

	params.SessionID = conn.NorthID
	if agentID := session.AgentIDFromContext(ctx); agentID != "" {
		if params.Meta == nil {
			params.Meta = make(map[string]any)
		}
		params.Meta[session.MetaAgentID] = string(agentID)
	}

	return conn.NorthConn.SessionUpdate(ctx, params)
}

func (h *SouthHandler) RequestPermission(ctx context.Context, params *acp.RequestPermissionRequest) (*acp.RequestPermissionResponse, error) {
	conn, err := h.northConn(params.SessionID)
	if err != nil {
		return nil, err
	}
	params.SessionID = conn.NorthID
	return conn.NorthConn.RequestPermission(ctx, params)
}

func (h *SouthHandler) ReadTextFile(ctx context.Context, params *acp.ReadTextFileRequest) (*acp.ReadTextFileResponse, error) {
	return nil, acp.ErrMethodNotFound(acp.ClientMethods.FSReadTextFile)
}

func (h *SouthHandler) WriteTextFile(ctx context.Context, params *acp.WriteTextFileRequest) (*acp.WriteTextFileResponse, error) {
	return nil, acp.ErrMethodNotFound(acp.ClientMethods.FSWriteTextFile)
}

func (h *SouthHandler) CreateTerminal(ctx context.Context, params *acp.CreateTerminalRequest) (*acp.CreateTerminalResponse, error) {
	return nil, acp.ErrMethodNotFound(acp.ClientMethods.TerminalCreate)
}

func (h *SouthHandler) TerminalOutput(ctx context.Context, params *acp.TerminalOutputRequest) (*acp.TerminalOutputResponse, error) {
	return nil, acp.ErrMethodNotFound(acp.ClientMethods.TerminalOutput)
}

func (h *SouthHandler) ReleaseTerminal(ctx context.Context, params *acp.ReleaseTerminalRequest) (*acp.ReleaseTerminalResponse, error) {
	return nil, acp.ErrMethodNotFound(acp.ClientMethods.TerminalRelease)
}

func (h *SouthHandler) WaitForTerminalExit(ctx context.Context, params *acp.WaitForTerminalExitRequest) (*acp.WaitForTerminalExitResponse, error) {
	return nil, acp.ErrMethodNotFound(acp.ClientMethods.TerminalWaitForExit)
}

func (h *SouthHandler) KillTerminalCommand(ctx context.Context, params *acp.KillTerminalRequest) (*acp.KillTerminalResponse, error) {
	return nil, acp.ErrMethodNotFound(acp.ClientMethods.TerminalKill)
}

func (h *SouthHandler) ExtMethod(ctx context.Context, method string, params json.RawMessage) (any, error) {
	raw := make(map[string]any)
	if err := json.Unmarshal(params, &raw); err != nil {
		return nil, err
	}

	sessionID, _ := raw["sessionId"].(string)
	if sessionID == "" {
		return nil, errors.New("missing session id")
	}

	conn, err := h.northConn(acp.SessionID(sessionID))
	if err != nil {
		return nil, err
	}

	raw["sessionId"] = string(conn.NorthID)
	if agentID := session.MetaString(raw, session.MetaAgentID); agentID != "" {
		targetAgentID := session.AgentID(agentID)
		agentConn, err := h.manager.FindAgentConnection(conn.NorthID, targetAgentID)
		if errors.Is(err, session.ErrAgentConnectionNotFound) {
			agentConn, err = h.manager.ConnectWorker(ctx, conn.NorthID, targetAgentID, h.agentSelector)
		}
		if err != nil {
			return nil, err
		}
		return agentConn.ExtMethod(ctx, method, raw)
	}

	return conn.NorthConn.ExtMethod(ctx, method, raw)
}

func (h *SouthHandler) northConn(sessionID acp.SessionID) (*session.Conn, error) {
	conn, err := h.manager.FindByNorth(sessionID)
	if err != nil {
		return nil, err
	}
	if conn.NorthConn == nil {
		return nil, errors.New("missing north connection")
	}
	return conn, nil
}
