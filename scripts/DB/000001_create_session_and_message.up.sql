CREATE TABLE IF NOT EXISTS agents (
    id TEXT PRIMARY KEY,
    type TEXT NOT NULL,
    name TEXT NOT NULL,
    config JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS agent_groups (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    type TEXT NOT NULL,
    config JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS agent_group_agents (
    group_id TEXT NOT NULL,
    agent_id TEXT NOT NULL,

    PRIMARY KEY (group_id, agent_id),

    CONSTRAINT agent_group_agents_group_id_key
        FOREIGN KEY (group_id)
        REFERENCES agent_groups (id)
        ON DELETE CASCADE,

    CONSTRAINT agent_group_agents_agent_id_key
        FOREIGN KEY (agent_id)
        REFERENCES agents (id)
        ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS sessions (
    session_id TEXT PRIMARY KEY,
    chat_type TEXT NOT NULL DEFAULT 'single',
    agent_id TEXT,
    agent_group_id TEXT,
    meta JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT sessions_agent_id_key
        FOREIGN KEY (agent_id)
        REFERENCES agents (id)
        ON DELETE RESTRICT,

    CONSTRAINT sessions_agent_group_id_key
        FOREIGN KEY (agent_group_id)
        REFERENCES agent_groups (id)
        ON DELETE RESTRICT,

    CONSTRAINT sessions_chat_type_check
        CHECK (chat_type IN ('single', 'group')),

    CONSTRAINT sessions_chat_target_check
        CHECK (
            (chat_type = 'single' AND agent_id IS NOT NULL AND agent_group_id IS NULL)
            OR
            (chat_type = 'group' AND agent_id IS NULL AND agent_group_id IS NOT NULL)
        )
);

CREATE TABLE IF NOT EXISTS messages (
    id BIGSERIAL PRIMARY KEY,
    session_id TEXT NOT NULL,
    kind TEXT NOT NULL,
    payload JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT messages_session_id_key
        FOREIGN KEY (session_id)
        REFERENCES sessions (session_id)
        ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_sessions_created_at
    ON sessions (created_at);

CREATE INDEX IF NOT EXISTS idx_sessions_agent_id
    ON sessions (agent_id);

CREATE INDEX IF NOT EXISTS idx_sessions_agent_group_id
    ON sessions (agent_group_id);

CREATE INDEX IF NOT EXISTS idx_agent_group_agents_agent_id
    ON agent_group_agents (agent_id);

CREATE INDEX IF NOT EXISTS idx_messages_created_at
    ON messages (created_at);

CREATE INDEX IF NOT EXISTS idx_messages_session_id_created_at
    ON messages (session_id, created_at);
