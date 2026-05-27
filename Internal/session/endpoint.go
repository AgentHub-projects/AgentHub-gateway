package session

import (
	"context"

	"github.com/ironpark/go-acp"
)

const (
	AgentRoleLeader = "leader"
	AgentRoleWorker = "worker"
)

type Resolver interface {
	Resolve(ctx context.Context, sessionID acp.SessionID, agentID AgentID, templateSelector string) (string, error)
	Release(ctx context.Context, sessionID acp.SessionID, agentID AgentID) error
}

type Connector interface {
	Connect(ctx context.Context, endpoint string) (acp.Transport, error)
	Init(ctx context.Context, endpoint string, init AgentInit) error
}

type AgentInit struct {
	AgentType      string          `json:"agentType,omitempty"`
	Role           string          `json:"role"`
	AgentID        string          `json:"agentId"`
	LeaderAgentID  string          `json:"leaderAgentId"`
	SessionID      string          `json:"sessionId"`
	Cwd            string          `json:"cwd"`
	SandboxAddress string          `json:"sandboxAddress"`
	SystemPrompt   string          `json:"systemPrompt,omitempty"`
	MCPServers     []acp.MCPServer `json:"mcpServers"`
}
