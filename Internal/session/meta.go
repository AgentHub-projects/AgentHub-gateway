package session

import (
	"fmt"

	acp "github.com/ironpark/go-acp"
)

const (
	ChatTypeSingle = "single"
	ChatTypeGroup  = "group"

	MetaAgentID      = "agentId"
	MetaAgentGroupID = "agentGroupId"
)

type Meta struct {
	MCPServers   []acp.MCPServer `json:"mcpServers,omitempty"`
	AgentID      string          `json:"agentId,omitempty"`
	AgentGroupID string          `json:"agentGroupId,omitempty"`
	ChatType     string          `json:"chatType"`
}

func NewMeta(mcpServers []acp.MCPServer, raw map[string]any) (Meta, error) {
	meta := Meta{
		MCPServers:   mcpServers,
		AgentID:      MetaString(raw, MetaAgentID),
		AgentGroupID: MetaString(raw, MetaAgentGroupID),
	}

	switch {
	case meta.AgentID != "" && meta.AgentGroupID != "":
		return Meta{}, fmt.Errorf("_meta.%s and _meta.%s are mutually exclusive", MetaAgentID, MetaAgentGroupID)
	case meta.AgentID != "":
		meta.ChatType = ChatTypeSingle
	case meta.AgentGroupID != "":
		meta.ChatType = ChatTypeGroup
	default:
		return Meta{}, fmt.Errorf("_meta.%s or _meta.%s is required", MetaAgentID, MetaAgentGroupID)
	}

	return meta, nil
}

func MetaString(raw map[string]any, key string) string {
	if raw == nil {
		return ""
	}
	value, _ := raw[key].(string)
	return value
}
