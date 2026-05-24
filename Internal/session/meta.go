package session

import acp "github.com/ironpark/go-acp"

const (
	ChatTypeSingle = "single"
	ChatTypeGroup  = "group"

	DefaultSandboxCwd = "/workspace"

	MetaAgentID                 = "agentId"
	MetaAgentGroupID            = "agentGroupId"
	MetaChatType                = "chatType"
	MetaSandboxCwd              = "sandboxCwd"
	MetaLeaderTemplateSelector  = "leaderTemplateSelector"
	MetaSandboxTemplateSelector = "sandboxTemplateSelector"
)

type Meta struct {
	Cwd                     string          `json:"cwd"`
	MCPServers              []acp.MCPServer `json:"mcpServers,omitempty"`
	AgentID                 string          `json:"agentId,omitempty"`
	AgentGroupID            string          `json:"agentGroupId,omitempty"`
	ChatType                string          `json:"chatType,omitempty"`
	LeaderTemplateSelector  string          `json:"leaderTemplateSelector,omitempty"`
	SandboxTemplateSelector string          `json:"sandboxTemplateSelector,omitempty"`
}

func NewMeta(cwd string, mcpServers []acp.MCPServer, raw map[string]any) Meta {
	if sandboxCwd := MetaString(raw, MetaSandboxCwd); sandboxCwd != "" {
		cwd = sandboxCwd
	} else {
		cwd = DefaultSandboxCwd
	}

	meta := Meta{
		Cwd:                     cwd,
		MCPServers:              mcpServers,
		AgentID:                 MetaString(raw, MetaAgentID),
		AgentGroupID:            MetaString(raw, MetaAgentGroupID),
		ChatType:                MetaString(raw, MetaChatType),
		LeaderTemplateSelector:  MetaString(raw, MetaLeaderTemplateSelector),
		SandboxTemplateSelector: MetaString(raw, MetaSandboxTemplateSelector),
	}
	if meta.ChatType == "" {
		meta.ChatType = ChatTypeSingle
		if meta.AgentGroupID != "" {
			meta.ChatType = ChatTypeGroup
		}
	}
	return meta
}

func MetaString(raw map[string]any, key string) string {
	if raw == nil {
		return ""
	}
	value, _ := raw[key].(string)
	return value
}
