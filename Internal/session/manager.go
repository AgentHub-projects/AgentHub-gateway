package session

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	acp "github.com/ironpark/go-acp"
	"resty.dev/v3"
)

var (
	ErrSessionNotFound         = errors.New("session not found")
	ErrAgentConnectionNotFound = errors.New("agent connection not found")
)

const agentConnectionStartWait = 10 * time.Millisecond
const agentPromptRequestTimeout = 5 * time.Second

const mockInitAgentType = "claude-code"

type agentPromptResponse struct {
	AgentID      int    `json:"agentId"`
	SystemPrompt string `json:"systemPrompt"`
}

// Manager owns the in-memory routing state for north sessions and agent sandboxes.
//
// A north connection owns the session lifetime. When the user disconnects, all
// tracked connections are closed and every claim with the session label is
// released.
type Manager struct {
	mu  sync.RWMutex
	ctx context.Context

	connByNorth map[acp.SessionID]*Conn

	resolver       Resolver
	connector      Connector
	backendAddress string

	north acp.Agent
	south acp.Client
}

func NewManager(ctx context.Context, resolver Resolver, connector Connector, backendAddress string) *Manager {
	return &Manager{
		resolver:       resolver,
		connector:      connector,
		backendAddress: backendAddress,
		ctx:            ctx,
		connByNorth:    make(map[acp.SessionID]*Conn),
	}
}

func (m *Manager) SetHandlers(north acp.Agent, south acp.Client) {
	m.north = north
	m.south = south
}

func (m *Manager) Create(connection *Conn) *Conn {
	return m.CreateWithID(connection, acp.SessionID(uuid.NewString()))
}

func (m *Manager) CreateWithID(connection *Conn, id acp.SessionID) *Conn {
	connection.NorthID = id
	if connection.WorkerConn == nil {
		connection.WorkerConn = make(map[AgentID]*acp.ClientSideConnection)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if current := m.connByNorth[id]; current != nil && current != connection {
		if current.NorthConn != nil && current.NorthConn != connection.NorthConn {
			_ = current.NorthConn.Close()
		}

		connection.LeaderConn = current.LeaderConn
		connection.WorkerConn = current.WorkerConn
		connection.sandbox = current.sandbox
		if current.LeaderAgentID != "" {
			connection.LeaderAgentID = current.LeaderAgentID
		}
		if connection.WorkerConn == nil {
			connection.WorkerConn = make(map[AgentID]*acp.ClientSideConnection)
		}
	}

	m.connByNorth[id] = connection
	return connection
}

func (m *Manager) FindByNorth(id acp.SessionID) (*Conn, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	conn, ok := m.connByNorth[id]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrSessionNotFound, id)
	}
	return conn, nil
}

func (m *Manager) FindAgentConnection(sessionID acp.SessionID, agentID AgentID) (*acp.ClientSideConnection, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	conn, ok := m.connByNorth[sessionID]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrSessionNotFound, sessionID)
	}
	agentConn := conn.WorkerConn[agentID]
	if agentConn == nil {
		return nil, fmt.Errorf("%w: session=%s agent=%s", ErrAgentConnectionNotFound, sessionID, agentID)
	}
	return agentConn, nil
}

// ServeNorth starts one north-side ACP connection for the transport.
func (m *Manager) ServeNorth(ctx context.Context, tr acp.Transport) error {
	northConn := acp.NewAgentSideConnection(m.north, nil, nil, acp.WithTransport(tr))
	connection := &Conn{
		NorthConn:  northConn,
		WorkerConn: make(map[AgentID]*acp.ClientSideConnection),
	}

	ctx = WithContext(ctx, connection)
	defer m.closeConnection(connection)

	return northConn.Start(ctx)
}

func (m *Manager) ConnectLeader(ctx context.Context, sessionID acp.SessionID, agentID AgentID, agentSelector string) (*acp.ClientSideConnection, error) {
	return m.connectAgentRole(ctx, sessionID, agentID, agentID, AgentRoleLeader, agentSelector)
}

func (m *Manager) ConnectWorker(ctx context.Context, sessionID acp.SessionID, agentID AgentID, agentSelector string) (*acp.ClientSideConnection, error) {
	leaderID, err := m.leaderAgentID(sessionID)
	if err != nil {
		return nil, err
	}

	return m.connectAgentRole(ctx, sessionID, agentID, leaderID, AgentRoleWorker, agentSelector)
}

func (m *Manager) ResolveSandboxEndpoint(ctx context.Context, sessionID acp.SessionID) (string, error) {
	return m.sandboxEndpoint(sessionID)
}

func (m *Manager) PrepareSandbox(ctx context.Context, sessionID acp.SessionID, sandboxSelector string) error {
	m.mu.RLock()
	conn, ok := m.connByNorth[sessionID]
	if !ok {
		m.mu.RUnlock()
		return fmt.Errorf("%w: %s", ErrSessionNotFound, sessionID)
	}
	if conn.sandbox != "" {
		m.mu.RUnlock()
		return nil
	}
	m.mu.RUnlock()

	sandboxEndpoint, err := m.resolver.Resolve(ctx, sessionID, "workspace", sandboxSelector)
	if err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	conn = m.connByNorth[sessionID]
	if conn == nil {
		return fmt.Errorf("%w: %s", ErrSessionNotFound, sessionID)
	}
	conn.sandbox = endpoint(sandboxEndpoint)
	return nil
}

func (m *Manager) connectAgentRole(ctx context.Context, sessionID acp.SessionID, agentID, leaderID AgentID, role, agentSelector string) (*acp.ClientSideConnection, error) {
	sandboxEndpoint, err := m.sandboxEndpoint(sessionID)
	if err != nil {
		return nil, err
	}

	agentEndpoint, err := m.resolver.Resolve(m.ctx, sessionID, agentID, agentSelector)
	if err != nil {
		return nil, err
	}

	init, err := agentInit(ctx, sessionID, agentID, leaderID, role, sandboxEndpoint, m.backendAddress)
	if err != nil {
		return nil, err
	}
	if err := m.connector.Init(ctx, agentEndpoint, init); err != nil {
		return nil, err
	}

	agentConn, err := m.connectAgent(ctx, sessionID, agentID, agentEndpoint)
	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	if conn := m.connByNorth[sessionID]; conn != nil && conn.WorkerConn[agentID] == agentConn && role == AgentRoleLeader {
		conn.LeaderConn = agentConn
	}
	m.mu.Unlock()

	return agentConn, nil
}

func (m *Manager) leaderAgentID(sessionID acp.SessionID) (AgentID, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	conn := m.connByNorth[sessionID]
	if conn == nil {
		return "", fmt.Errorf("%w: %s", ErrSessionNotFound, sessionID)
	}
	if conn.LeaderAgentID != "" {
		return conn.LeaderAgentID, nil
	}
	return "", errors.New("missing leader agent id")
}

func (m *Manager) sandboxEndpoint(sessionID acp.SessionID) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	conn := m.connByNorth[sessionID]
	if conn == nil {
		return "", fmt.Errorf("%w: %s", ErrSessionNotFound, sessionID)
	}
	if conn.sandbox == "" {
		return "", errors.New("missing sandbox endpoint")
	}
	return string(conn.sandbox), nil
}

func agentInit(ctx context.Context, sessionID acp.SessionID, agentID, leaderID AgentID, role, sandboxEndpoint, address string) (AgentInit, error) {
	systemprompt := ""
	if role == AgentRoleWorker {
		endpoint := fmt.Sprintf("%s/api/agents/%s/prompt", address, agentID)
		var payload agentPromptResponse
		resp, err := resty.New().
			SetTimeout(agentPromptRequestTimeout).
			R().
			SetContext(ctx).
			SetResult(&payload).
			Get(endpoint)
		if err != nil {
			return AgentInit{}, fmt.Errorf("get agent prompt: %w", err)
		}
		if resp.IsError() {
			return AgentInit{}, fmt.Errorf("get agent prompt: %s returned %s: %s", endpoint, resp.Status(), strings.TrimSpace(resp.String()))
		}
		systemprompt = strings.TrimSpace(payload.SystemPrompt)
	}

	return AgentInit{
		AgentType:      mockInitAgentType,
		Role:           role,
		AgentID:        string(agentID),
		LeaderAgentID:  string(leaderID),
		SessionID:      string(sessionID),
		SandboxAddress: sandboxEndpoint,
		SystemPrompt:   systemprompt,
		MCPServers:     []acp.MCPServer{},
	}, nil
}

func (m *Manager) connectAgent(ctx context.Context, sessionID acp.SessionID, agentID AgentID, address string) (*acp.ClientSideConnection, error) {
	m.mu.RLock()
	conn, ok := m.connByNorth[sessionID]
	if !ok {
		m.mu.RUnlock()
		return nil, fmt.Errorf("%w: %s", ErrSessionNotFound, sessionID)
	}
	if existing := conn.WorkerConn[agentID]; existing != nil {
		m.mu.RUnlock()
		return existing, nil
	}
	m.mu.RUnlock()

	tr, err := m.connector.Connect(m.ctx, address)
	if err != nil {
		return nil, err
	}

	var agentConn *acp.ClientSideConnection
	handedOff := false
	defer func() {
		if handedOff {
			return
		}
		if agentConn != nil {
			_ = agentConn.Close()
			return
		}
		_ = tr.Close()
	}()

	agentConn = acp.NewClientSideConnection(m.south, nil, nil, acp.WithTransport(tr))

	m.mu.Lock()
	current, ok := m.connByNorth[sessionID]
	if !ok {
		m.mu.Unlock()
		return nil, fmt.Errorf("%w: %s", ErrSessionNotFound, sessionID)
	}
	if existing := current.WorkerConn[agentID]; existing != nil {
		m.mu.Unlock()
		return existing, nil
	}
	current.WorkerConn[agentID] = agentConn
	handedOff = true
	m.mu.Unlock()

	go func() {
		defer func() {
			if err := agentConn.Close(); err != nil {
				slog.Warn("close agent connection failed", "session", sessionID, "agent", agentID, "error", err)
			}
		}()

		if err := agentConn.Start(WithAgentID(m.ctx, agentID)); err != nil && !errors.Is(err, context.Canceled) {
			slog.Warn("agent connection exited with error", "session", sessionID, "agent", agentID, "error", err)
		}
		slog.Info("agent connection closed", "session", sessionID, "agent", agentID)
		m.clearAgentConnection(sessionID, agentID, agentConn)
	}()

	// go-acp requires Start to run before the first outbound request but does
	// not expose a readiness signal, so give the loop one scheduling window.
	time.Sleep(agentConnectionStartWait)

	return agentConn, nil
}

// List returns all in-memory north session IDs.
func (m *Manager) List() []acp.SessionID {
	m.mu.RLock()
	defer m.mu.RUnlock()

	ids := make([]acp.SessionID, 0, len(m.connByNorth))
	for id := range m.connByNorth {
		ids = append(ids, id)
	}
	return ids
}

// Close closes every connection and releases every tracked claim.
func (m *Manager) Close() error {
	m.mu.Lock()
	conns := make([]*Conn, 0, len(m.connByNorth))
	for _, conn := range m.connByNorth {
		conns = append(conns, conn)
	}

	m.connByNorth = make(map[acp.SessionID]*Conn)
	m.mu.Unlock()

	var errs []error
	for _, conn := range conns {
		errs = append(errs, m.closeAndReleaseSession(conn.NorthID, conn))
	}
	return errors.Join(errs...)
}

func (m *Manager) closeConnection(conn *Conn) error {
	if conn == nil {
		return nil
	}
	if conn.NorthID == "" {
		if conn.NorthConn != nil {
			return conn.NorthConn.Close()
		}
		return nil
	}

	m.mu.Lock()
	if current := m.connByNorth[conn.NorthID]; current != conn {
		m.mu.Unlock()
		return nil
	}

	delete(m.connByNorth, conn.NorthID)
	m.mu.Unlock()

	slog.Info("north connection closed, release session claims", "session", conn.NorthID)
	return m.closeAndReleaseSession(conn.NorthID, conn)
}

func (m *Manager) clearAgentConnection(sessionID acp.SessionID, agentID AgentID, expected *acp.ClientSideConnection) {
	m.mu.Lock()
	defer m.mu.Unlock()

	conn := m.connByNorth[sessionID]
	if conn == nil {
		return
	}
	if conn.WorkerConn[agentID] == expected {
		conn.WorkerConn[agentID] = nil
	}
	if conn.LeaderConn == expected {
		conn.LeaderConn = nil
	}
}

func (m *Manager) closeAndReleaseSession(sessionID acp.SessionID, conn *Conn) error {
	return errors.Join(conn.Close(), m.resolver.Release(m.ctx, sessionID))
}
