-- NovelBuilder Database Schema - Part 6: AI & LLM Tables
-- Agent review sessions, LLM profiles, model routing, agent sessions

-- ============================================================
-- Agent Review Tables
-- ============================================================

CREATE TABLE agent_review_sessions (
    id           UUID PRIMARY KEY,
    project_id   UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    review_scope VARCHAR(50) NOT NULL DEFAULT 'full',
    target_id    VARCHAR(255),
    status       VARCHAR(50) NOT NULL DEFAULT 'running',
    rounds       INTEGER NOT NULL DEFAULT 3,
    consensus    TEXT NOT NULL DEFAULT '',
    issues       JSONB NOT NULL DEFAULT '[]',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMPTZ
);

CREATE INDEX idx_agent_review_sessions_project_id ON agent_review_sessions(project_id);
CREATE INDEX idx_agent_review_sessions_status ON agent_review_sessions(status);

CREATE TABLE agent_review_messages (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id UUID NOT NULL REFERENCES agent_review_sessions(id) ON DELETE CASCADE,
    round      INTEGER NOT NULL,
    agent_role VARCHAR(100) NOT NULL,
    agent_name VARCHAR(100) NOT NULL,
    content    TEXT NOT NULL,
    tags       JSONB NOT NULL DEFAULT '[]',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_agent_review_messages_session_id ON agent_review_messages(session_id);

-- ============================================================
-- LLM Profiles
-- ============================================================

CREATE TABLE llm_profiles (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    name            VARCHAR(100) NOT NULL UNIQUE,
    provider        VARCHAR(50)  NOT NULL DEFAULT 'openai',
    base_url        VARCHAR(500) NOT NULL DEFAULT 'https://api.openai.com/v1',
    api_key         TEXT         NOT NULL,
    model_name      VARCHAR(200) NOT NULL,
    max_tokens      INT          NOT NULL DEFAULT 8192,
    temperature     FLOAT        NOT NULL DEFAULT 0.7,
    is_default      BOOLEAN      NOT NULL DEFAULT FALSE,
    -- Per-profile RPM limit (0 = unlimited). Column defined in CREATE TABLE.
    rpm_limit       INT          NOT NULL DEFAULT 0,
    -- Parameter omission flags and API style selector. Column defined in CREATE TABLE.
    omit_max_tokens  BOOLEAN     NOT NULL DEFAULT false,
    omit_temperature BOOLEAN     NOT NULL DEFAULT false,
    -- api_style: 'chat_completions' (default) or 'responses' (OpenAI Responses API).
    api_style        VARCHAR(50) NOT NULL DEFAULT 'chat_completions',
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_llm_profiles_single_default ON llm_profiles (is_default) WHERE (is_default = TRUE);
CREATE INDEX IF NOT EXISTS idx_llm_profiles_is_default ON llm_profiles (is_default);

-- Idempotent ADD COLUMN guards for existing databases that ran the original 007
-- before rpm_limit / omit_* / api_style columns were added .
ALTER TABLE llm_profiles
    ADD COLUMN IF NOT EXISTS rpm_limit        INT         NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS omit_max_tokens  BOOLEAN     NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS omit_temperature BOOLEAN     NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS api_style        VARCHAR(50) NOT NULL DEFAULT 'chat_completions';

-- ============================================================
-- Per-Agent Model Routing
-- Different agent types (writer, auditor, planner, etc.) can use different LLM profiles.
-- project_id = NULL means global default for that agent type.
-- ============================================================

CREATE TABLE agent_model_routes (
    id             UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_type     VARCHAR(50)  NOT NULL,  -- writer|auditor|planner|reviser|radar|moderator
    llm_profile_id UUID         REFERENCES llm_profiles(id) ON DELETE SET NULL,
    project_id     UUID         REFERENCES projects(id) ON DELETE CASCADE,
    created_at     TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

-- Partial unique indexes to handle NULL project_id correctly
CREATE UNIQUE INDEX uq_agent_route_project ON agent_model_routes(agent_type, project_id) WHERE project_id IS NOT NULL;
CREATE UNIQUE INDEX uq_agent_route_global ON agent_model_routes(agent_type) WHERE project_id IS NULL;
CREATE INDEX idx_agent_model_routes_project ON agent_model_routes(project_id);

CREATE TRIGGER trg_agent_model_routes_updated_at BEFORE UPDATE ON agent_model_routes
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

-- ============================================================
-- Agent Sessions (LangGraph session tracking)
-- ============================================================

CREATE TABLE agent_sessions (
    id            UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id    UUID         REFERENCES projects(id) ON DELETE CASCADE,
    task_type     VARCHAR(50)  NOT NULL DEFAULT 'generate_chapter',
    status        VARCHAR(20)  NOT NULL DEFAULT 'running',
    session_key   VARCHAR(200),
    input_params  JSONB        DEFAULT '{}',
    result        JSONB,
    error_msg     TEXT,
    quality_score FLOAT,
    created_at    TIMESTAMP    NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMP    NOT NULL DEFAULT NOW(),
    CONSTRAINT ck_agent_session_status CHECK (status IN ('running', 'done', 'error'))
);

CREATE INDEX idx_agent_sessions_project ON agent_sessions(project_id, created_at DESC);
CREATE INDEX idx_agent_sessions_status  ON agent_sessions(status) WHERE status = 'running';

CREATE TRIGGER trg_agent_sessions_updated_at BEFORE UPDATE ON agent_sessions
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
