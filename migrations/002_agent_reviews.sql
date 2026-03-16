-- Agent review sessions table
CREATE TABLE IF NOT EXISTS agent_review_sessions (
    id          UUID PRIMARY KEY,
    project_id  UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    review_scope VARCHAR(50) NOT NULL DEFAULT 'full',
    target_id   VARCHAR(255),
    status      VARCHAR(50) NOT NULL DEFAULT 'running',
    rounds      INTEGER NOT NULL DEFAULT 3,
    consensus   TEXT NOT NULL DEFAULT '',
    issues      JSONB NOT NULL DEFAULT '[]',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_agent_review_sessions_project_id ON agent_review_sessions(project_id);
CREATE INDEX IF NOT EXISTS idx_agent_review_sessions_status ON agent_review_sessions(status);

-- Individual messages stored separately for efficient streaming access
CREATE TABLE IF NOT EXISTS agent_review_messages (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id  UUID NOT NULL REFERENCES agent_review_sessions(id) ON DELETE CASCADE,
    round       INTEGER NOT NULL,
    agent_role  VARCHAR(100) NOT NULL,
    agent_name  VARCHAR(100) NOT NULL,
    content     TEXT NOT NULL,
    tags        JSONB NOT NULL DEFAULT '[]',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_agent_review_messages_session_id ON agent_review_messages(session_id);
