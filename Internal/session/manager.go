package session

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	acp "github.com/ironpark/go-acp"
)

var (
	ErrSessionNotFound         = errors.New("session not found")
	ErrConnectionNotFound      = errors.New("connection not found")
	ErrAgentConnectionNotFound = errors.New("agent connection not found")
)

const agentConnectionStartWait = 10 * time.Millisecond

const mockInitAgentType = "claude-code"

// Manager owns the in-memory routing state for north sessions and agent sandboxes.
//
// A north connection is transient: when the user disconnects, the north side is
// detached but the session entry and agent claims stay alive until Release/Close.
type Manager struct {
	mu  sync.RWMutex
	ctx context.Context

	connByID    map[string]*Conn
	connByNorth map[acp.SessionID]*Conn

	resolver  Resolver
	connector Connector

	north acp.Agent
	south acp.Client
}

func NewManager(ctx context.Context, resolver Resolver, connector Connector) *Manager {
	return &Manager{
		resolver:    resolver,
		connector:   connector,
		ctx:         ctx,
		connByID:    make(map[string]*Conn),
		connByNorth: make(map[acp.SessionID]*Conn),
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

	m.mu.Lock()
	defer m.mu.Unlock()

	if current := m.connByNorth[id]; current != nil && current != connection {
		delete(m.connByID, current.ConnID)
		if current.NorthConn != nil && current.NorthConn != connection.NorthConn {
			_ = current.NorthConn.Close()
		}

		current.ConnID = connection.ConnID
		current.NorthID = connection.NorthID
		current.NorthConn = connection.NorthConn
		if connection.LeaderAgentID != "" {
			current.LeaderAgentID = connection.LeaderAgentID
		}
		m.connByID[current.ConnID] = current
		return current
	}

	m.connByNorth[id] = connection
	m.connByID[connection.ConnID] = connection
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

func (m *Manager) FindConnection(id string) (*Conn, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	conn, ok := m.connByID[id]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrConnectionNotFound, id)
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

func (m *Manager) ResolveSandboxEndpoint(ctx context.Context, sessionID acp.SessionID) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	conn, ok := m.connByNorth[sessionID]
	if !ok {
		return "", fmt.Errorf("%w: %s", ErrSessionNotFound, sessionID)
	}
	return string(conn.sandbox), nil
}

// ServeNorth starts one north-side ACP connection for the transport.
func (m *Manager) ServeNorth(ctx context.Context, tr acp.Transport) error {
	northConn := acp.NewAgentSideConnection(m.north, nil, nil, acp.WithTransport(tr))
	connection := &Conn{
		ConnID:     uuid.NewString(),
		NorthConn:  northConn,
		WorkerConn: make(map[AgentID]*acp.ClientSideConnection),
	}

	m.mu.Lock()
	m.connByID[connection.ConnID] = connection
	m.mu.Unlock()

	ctx = WithContext(ctx, connection)
	defer m.closeConnection(connection.ConnID)

	return northConn.Start(ctx)
}

func (m *Manager) ConnectLeader(ctx context.Context, sessionID acp.SessionID, agentID AgentID, agentSelector, sandboxSelector string) (*acp.ClientSideConnection, error) {
	return m.connectAgentRole(ctx, sessionID, agentID, agentID, AgentRoleLeader, agentSelector, sandboxSelector)
}

func (m *Manager) ConnectWorker(ctx context.Context, sessionID acp.SessionID, agentID AgentID, agentSelector, sandboxSelector string) (*acp.ClientSideConnection, error) {
	leaderID, err := m.leaderAgentID(sessionID)
	if err != nil {
		return nil, err
	}

	return m.connectAgentRole(ctx, sessionID, agentID, leaderID, AgentRoleWorker, agentSelector, sandboxSelector)
}

func (m *Manager) connectAgentRole(ctx context.Context, sessionID acp.SessionID, agentID, leaderID AgentID, role, agentSelector, sandboxSelector string) (*acp.ClientSideConnection, error) {
	sandboxEndpoint, err := m.resolver.Resolve(m.ctx, sessionID, leaderID, sandboxSelector)
	if err != nil {
		return nil, err
	}

	agentEndpoint, err := m.resolver.Resolve(m.ctx, sessionID, agentID, agentSelector)
	if err != nil {
		return nil, err
	}

	if err := m.connector.Init(ctx, agentEndpoint, agentInit(sessionID, agentID, leaderID, role, sandboxEndpoint)); err != nil {
		return nil, err
	}

	agentConn, err := m.connectAgent(ctx, sessionID, agentID, agentEndpoint)
	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	if conn := m.connByNorth[sessionID]; conn != nil && conn.WorkerConn[agentID] == agentConn && role == AgentRoleLeader {
		conn.LeaderConn = agentConn
		conn.sandbox = endpoint(sandboxEndpoint)
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
	for agentID, agentConn := range conn.WorkerConn {
		if agentConn != nil && agentConn == conn.LeaderConn {
			return agentID, nil
		}
	}
	return "", errors.New("missing leader agent connection")
}

func agentInit(sessionID acp.SessionID, agentID, leaderID AgentID, role, sandboxEndpoint string) AgentInit {
	return AgentInit{
		AgentType:      mockInitAgentType,
		Role:           role,
		AgentID:        string(agentID),
		LeaderAgentID:  string(leaderID),
		SessionID:      string(sessionID),
		SandboxAddress: sandboxEndpoint,
		MCPServers:     []acp.MCPServer{},
	}
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

// ReleaseByID releases one agent claim under a session.
func (m *Manager) ReleaseByID(sessionID acp.SessionID, agentID AgentID) error {
	var agentConn *acp.ClientSideConnection

	m.mu.Lock()
	if conn := m.connByNorth[sessionID]; conn != nil {
		agentConn = conn.WorkerConn[agentID]
		delete(conn.WorkerConn, agentID)
		if conn.LeaderConn == agentConn {
			conn.LeaderConn = nil
		}
	}
	m.mu.Unlock()

	var errs []error
	if agentConn != nil {
		errs = append(errs, agentConn.Close())
	}
	if err := m.resolver.Release(m.ctx, sessionID, agentID); err != nil {
		errs = append(errs, fmt.Errorf("release agent claim session=%s agent=%s: %w", sessionID, agentID, err))
	}
	return errors.Join(errs...)
}

func (m *Manager) Release(sessionID acp.SessionID) error {
	m.mu.Lock()
	conn := m.connByNorth[sessionID]
	if conn == nil {
		m.mu.Unlock()
		return nil
	}

	delete(m.connByNorth, sessionID)
	delete(m.connByID, conn.ConnID)
	agentIDs := connAgentIDs(conn)
	m.mu.Unlock()

	errs := []error{conn.Close()}
	errs = append(errs, m.releaseAgents(sessionID, agentIDs)...)
	return errors.Join(errs...)
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
	conns := make([]*Conn, 0, len(m.connByNorth)+len(m.connByID))
	seen := make(map[*Conn]struct{}, len(m.connByNorth)+len(m.connByID))
	for _, conn := range m.connByNorth {
		if _, ok := seen[conn]; ok {
			continue
		}
		seen[conn] = struct{}{}
		conns = append(conns, conn)
	}
	for _, conn := range m.connByID {
		if _, ok := seen[conn]; ok {
			continue
		}
		seen[conn] = struct{}{}
		conns = append(conns, conn)
	}

	m.connByID = make(map[string]*Conn)
	m.connByNorth = make(map[acp.SessionID]*Conn)
	m.mu.Unlock()

	var errs []error
	for _, conn := range conns {
		errs = append(errs, conn.Close())
		errs = append(errs, m.releaseAgents(conn.NorthID, connAgentIDs(conn))...)
	}
	return errors.Join(errs...)
}

func (m *Manager) closeConnection(connectionID string) error {
	m.mu.Lock()
	conn := m.connByID[connectionID]
	if conn == nil {
		m.mu.Unlock()
		return nil
	}

	delete(m.connByID, connectionID)
	northConn := conn.NorthConn
	if conn.ConnID == connectionID {
		conn.ConnID = ""
		conn.NorthConn = nil
	}
	m.mu.Unlock()

	if northConn != nil {
		return northConn.Close()
	}
	return nil
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

func (m *Manager) releaseAgents(sessionID acp.SessionID, agentIDs []AgentID) []error {
	errs := make([]error, 0, len(agentIDs))
	for _, agentID := range agentIDs {
		if err := m.resolver.Release(m.ctx, sessionID, agentID); err != nil {
			errs = append(errs, fmt.Errorf("release agent claim session=%s agent=%s: %w", sessionID, agentID, err))
		}
	}
	return errs
}

func connAgentIDs(conn *Conn) []AgentID {
	agentIDs := make([]AgentID, 0, len(conn.WorkerConn))
	for agentID := range conn.WorkerConn {
		agentIDs = append(agentIDs, agentID)
	}
	return agentIDs
}
