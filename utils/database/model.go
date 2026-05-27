package database

import (
	"time"

	"gorm.io/datatypes"
)

type Agent struct {
	ID        string         `gorm:"column:id;primaryKey"`
	Type      string         `gorm:"column:type;not null"`
	Name      string         `gorm:"column:name;not null"`
	Config    datatypes.JSON `gorm:"column:config;type:jsonb"`
	CreatedAt time.Time      `gorm:"column:created_at;not null"`
}

type AgentGroup struct {
	ID        string         `gorm:"column:id;primaryKey"`
	Name      string         `gorm:"column:name;not null"`
	Type      string         `gorm:"column:type;not null"`
	Config    datatypes.JSON `gorm:"column:config;type:jsonb"`
	CreatedAt time.Time      `gorm:"column:created_at;not null"`
}

type AgentGroupAgent struct {
	GroupID string `gorm:"column:group_id;primaryKey"`
	AgentID string `gorm:"column:agent_id;primaryKey"`
}

type Session struct {
	SessionID    string         `gorm:"column:session_id;primaryKey"`
	ChatType     string         `gorm:"column:chat_type;not null;default:single"`
	AgentID      *string        `gorm:"column:agent_id"`
	AgentGroupID *string        `gorm:"column:agent_group_id"`
	Meta         datatypes.JSON `gorm:"column:meta;type:jsonb;not null"`
	CreatedAt    time.Time      `gorm:"column:created_at;not null"`
}
