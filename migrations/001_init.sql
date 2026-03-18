-- NovelBuilder Complete Database Schema
-- PostgreSQL 16 with pgvector extension

CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
CREATE EXTENSION IF NOT EXISTS "vector";

-- ============================================================
-- Core Tables
-- ============================================================

CREATE TABLE projects (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    title           VARCHAR(300) NOT NULL,
    genre           VARCHAR(50),
    description     TEXT,
    status          VARCHAR(20) DEFAULT 'active',
    created_at      TIMESTAMP DEFAULT NOW(),
    updated_at      TIMESTAMP DEFAULT NOW()
);

CREATE TABLE reference_materials (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id      UUID REFERENCES projects(id) ON DELETE CASCADE,
    title           VARCHAR(200),
    author          VARCHAR(100),
    genre           VARCHAR(50),
    file_path       VARCHAR(500),
    style_layer     JSONB,
    narrative_layer JSONB,
    atmosphere_layer JSONB,
    migration_config JSONB,
    style_collection VARCHAR(100),
    vector_fingerprint VECTOR(1024),
    sample_texts    JSONB,
    status          VARCHAR(20) DEFAULT 'processing',
    created_at      TIMESTAMP DEFAULT NOW()
);

-- Quarantine zone (isolated schema)
CREATE SCHEMA IF NOT EXISTS quarantine_zone;

CREATE TABLE quarantine_zone.plot_elements (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    material_id     UUID NOT NULL,
    element_type    VARCHAR(30) NOT NULL,
    content         TEXT NOT NULL,
    vector          VECTOR(1024),
    created_at      TIMESTAMP DEFAULT NOW()
);

CREATE TABLE world_bibles (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id      UUID REFERENCES projects(id) ON DELETE CASCADE,
    content         JSONB NOT NULL DEFAULT '{}',
    migration_source UUID REFERENCES reference_materials(id),
    version         INT DEFAULT 1,
    created_at      TIMESTAMP DEFAULT NOW(),
    updated_at      TIMESTAMP DEFAULT NOW(),
    CONSTRAINT uq_world_bibles_project UNIQUE (project_id)
);

CREATE TABLE world_bible_constitutions (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id      UUID REFERENCES projects(id) ON DELETE CASCADE,
    immutable_rules JSONB NOT NULL DEFAULT '[]',
    mutable_rules   JSONB NOT NULL DEFAULT '[]',
    forbidden_anchors JSONB NOT NULL DEFAULT '[]',
    version         INT DEFAULT 1,
    created_at      TIMESTAMP DEFAULT NOW(),
    updated_at      TIMESTAMP DEFAULT NOW(),
    CONSTRAINT uq_world_bible_constitutions_project UNIQUE (project_id)
);

CREATE TABLE characters (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id      UUID REFERENCES projects(id) ON DELETE CASCADE,
    name            VARCHAR(100) NOT NULL,
    role_type       VARCHAR(30) DEFAULT 'supporting',
    profile         JSONB NOT NULL DEFAULT '{}',
    current_state   JSONB DEFAULT '{}',
    voice_collection VARCHAR(100),
    created_at      TIMESTAMP DEFAULT NOW(),
    updated_at      TIMESTAMP DEFAULT NOW()
);

CREATE TABLE outlines (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id      UUID REFERENCES projects(id) ON DELETE CASCADE,
    level           VARCHAR(20) NOT NULL,
    parent_id       UUID REFERENCES outlines(id),
    order_num       INT NOT NULL DEFAULT 0,
    title           VARCHAR(300),
    content         JSONB NOT NULL DEFAULT '{}',
    tension_target  FLOAT DEFAULT 0.5,
    created_at      TIMESTAMP DEFAULT NOW(),
    updated_at      TIMESTAMP DEFAULT NOW()
);

CREATE TABLE foreshadowings (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id      UUID REFERENCES projects(id) ON DELETE CASCADE,
    content         TEXT NOT NULL,
    embed_chapter_id UUID,
    resolve_chapter_id UUID,
    embed_method    VARCHAR(100),
    resolve_method  VARCHAR(100),
    priority        SMALLINT DEFAULT 3,
    status          VARCHAR(20) DEFAULT 'planned',
    tags            TEXT[],
    created_at      TIMESTAMP DEFAULT NOW(),
    updated_at      TIMESTAMP DEFAULT NOW()
);

CREATE TABLE book_blueprints (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id      UUID REFERENCES projects(id) ON DELETE CASCADE,
    world_bible_ref UUID REFERENCES world_bibles(id),
    master_outline  JSONB NOT NULL DEFAULT '{}',
    relation_graph  JSONB NOT NULL DEFAULT '{}',
    global_timeline JSONB NOT NULL DEFAULT '[]',
    status          VARCHAR(20) DEFAULT 'draft',
    version         INT DEFAULT 1,
    review_comment  TEXT,
    created_at      TIMESTAMP DEFAULT NOW(),
    updated_at      TIMESTAMP DEFAULT NOW(),
    CONSTRAINT ck_blueprint_status CHECK (status IN ('draft', 'pending_review', 'approved', 'rejected'))
);

CREATE TABLE volumes (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id      UUID REFERENCES projects(id) ON DELETE CASCADE,
    volume_num      INT NOT NULL,
    title           VARCHAR(200),
    blueprint_id    UUID REFERENCES book_blueprints(id),
    status          VARCHAR(20) DEFAULT 'drafting',
    chapter_start   INT,
    chapter_end     INT,
    review_comment  TEXT,
    created_at      TIMESTAMP DEFAULT NOW(),
    updated_at      TIMESTAMP DEFAULT NOW(),
    CONSTRAINT ck_volumes_status CHECK (status IN ('drafting', 'pending_review', 'approved', 'rejected')),
    CONSTRAINT uq_volumes_project_volume UNIQUE (project_id, volume_num)
);

CREATE TABLE chapters (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id      UUID REFERENCES projects(id) ON DELETE CASCADE,
    volume_id       UUID REFERENCES volumes(id),
    chapter_num     INT NOT NULL,
    title           VARCHAR(200),
    content         TEXT DEFAULT '',
    word_count      INT DEFAULT 0,
    summary         TEXT DEFAULT '',
    gen_params      JSONB DEFAULT '{}',
    quality_report  JSONB DEFAULT '{}',
    originality_score FLOAT DEFAULT 0,
    status          VARCHAR(20) DEFAULT 'draft',
    version         INT DEFAULT 1,
    review_comment  TEXT,
    created_at      TIMESTAMP DEFAULT NOW(),
    updated_at      TIMESTAMP DEFAULT NOW(),
    CONSTRAINT ck_chapters_status CHECK (status IN ('draft', 'pending_review', 'approved', 'rejected', 'needs_recheck')),
    CONSTRAINT uq_chapters_project_chapter UNIQUE (project_id, chapter_num)
);

-- ============================================================
-- Workflow Tables
-- ============================================================

CREATE TABLE workflow_runs (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id      UUID REFERENCES projects(id) ON DELETE CASCADE,
    strict_review   BOOLEAN DEFAULT TRUE,
    current_step    VARCHAR(50) NOT NULL DEFAULT 'init',
    status          VARCHAR(20) DEFAULT 'running',
    created_at      TIMESTAMP DEFAULT NOW(),
    updated_at      TIMESTAMP DEFAULT NOW()
);

CREATE TABLE workflow_steps (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id          UUID REFERENCES workflow_runs(id) ON DELETE CASCADE,
    step_key        VARCHAR(50) NOT NULL,
    step_order      INT NOT NULL,
    gate_level      VARCHAR(20) NOT NULL,
    status          VARCHAR(20) NOT NULL DEFAULT 'pending',
    output_ref      UUID,
    snapshot_ref    UUID,
    review_comment  TEXT,
    version         INT DEFAULT 1,
    generated_at    TIMESTAMP,
    reviewed_at     TIMESTAMP,
    created_at      TIMESTAMP DEFAULT NOW(),
    CONSTRAINT ck_workflow_steps_status CHECK (status IN ('pending', 'generated', 'pending_review', 'approved', 'rejected', 'rolled_back', 'needs_recheck'))
);

CREATE TABLE workflow_reviews (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    step_id         UUID REFERENCES workflow_steps(id) ON DELETE CASCADE,
    action          VARCHAR(20) NOT NULL,
    operator        VARCHAR(20) DEFAULT 'admin',
    reason          TEXT,
    from_step_order INT,
    to_step_order   INT,
    created_at      TIMESTAMP DEFAULT NOW()
);

CREATE TABLE workflow_snapshots (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id          UUID REFERENCES workflow_runs(id) ON DELETE CASCADE,
    step_key        VARCHAR(50) NOT NULL,
    params          JSONB NOT NULL DEFAULT '{}',
    context_payload JSONB,
    output_payload  JSONB,
    quality_payload JSONB,
    created_at      TIMESTAMP DEFAULT NOW()
);

CREATE TABLE idempotency_keys (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    idempotency_key VARCHAR(128) NOT NULL,
    action          VARCHAR(200) NOT NULL,
    request_hash    VARCHAR(128),
    status_code     INT,
    response_body   JSONB,
    created_at      TIMESTAMP DEFAULT NOW(),
    UNIQUE (idempotency_key, action)
);

-- ============================================================
-- Originality & Plot Analysis
-- ============================================================

CREATE TABLE plot_graph_snapshots (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id      UUID REFERENCES projects(id) ON DELETE CASCADE,
    chapter_id      UUID REFERENCES chapters(id),
    graph_type      VARCHAR(20) NOT NULL,
    nodes           JSONB NOT NULL DEFAULT '[]',
    edges           JSONB NOT NULL DEFAULT '[]',
    created_at      TIMESTAMP DEFAULT NOW()
);

CREATE TABLE originality_audits (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    chapter_id      UUID REFERENCES chapters(id) ON DELETE CASCADE,
    semantic_similarity FLOAT DEFAULT 0,
    event_graph_distance FLOAT DEFAULT 0,
    role_overlap    FLOAT DEFAULT 0,
    suspicious_segments JSONB DEFAULT '[]',
    pass            BOOLEAN DEFAULT FALSE,
    created_at      TIMESTAMP DEFAULT NOW()
);

-- ============================================================
-- Vector Storage (using pgvector)
-- ============================================================

CREATE TABLE vector_store (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id      UUID REFERENCES projects(id) ON DELETE CASCADE,
    collection      VARCHAR(100) NOT NULL,
    content         TEXT NOT NULL,
    metadata        JSONB DEFAULT '{}',
    embedding       VECTOR(1024),
    source_type     VARCHAR(50) NOT NULL DEFAULT 'reference',
    source_id       VARCHAR(100),
    created_at      TIMESTAMP DEFAULT NOW()
);

CREATE INDEX idx_vector_store_collection ON vector_store(collection);
CREATE INDEX idx_vector_store_source_id ON vector_store(project_id, source_id);
CREATE INDEX idx_vector_store_embedding ON vector_store USING ivfflat (embedding vector_cosine_ops) WITH (lists = 100);

-- ============================================================
-- Indexes
-- ============================================================

CREATE INDEX idx_workflow_steps_run_order ON workflow_steps(run_id, step_order);
CREATE INDEX idx_workflow_steps_status ON workflow_steps(status);
CREATE INDEX idx_chapters_project_status ON chapters(project_id, status);
CREATE INDEX idx_volumes_project_status ON volumes(project_id, status);
CREATE INDEX idx_outlines_project ON outlines(project_id);
CREATE INDEX idx_characters_project ON characters(project_id);
CREATE INDEX idx_foreshadowings_project ON foreshadowings(project_id);
CREATE INDEX idx_reference_materials_project ON reference_materials(project_id);
CREATE INDEX idx_world_bibles_project ON world_bibles(project_id);
CREATE INDEX idx_world_bible_constitutions_project ON world_bible_constitutions(project_id);
CREATE INDEX idx_book_blueprints_project ON book_blueprints(project_id);
CREATE INDEX idx_chapters_project_num ON chapters(project_id, chapter_num);

-- Deferred FK constraints for foreshadowings (chapters table created later)
ALTER TABLE foreshadowings
    ADD CONSTRAINT fk_foreshadowings_embed_chapter
        FOREIGN KEY (embed_chapter_id) REFERENCES chapters(id) ON DELETE SET NULL,
    ADD CONSTRAINT fk_foreshadowings_resolve_chapter
        FOREIGN KEY (resolve_chapter_id) REFERENCES chapters(id) ON DELETE SET NULL;

-- ============================================================
-- Triggers: Chapter Sequence Guard
-- ============================================================

CREATE OR REPLACE FUNCTION guard_chapter_sequence()
RETURNS TRIGGER AS $$
DECLARE
    prev_status VARCHAR(20);
    bp_status VARCHAR(20);
BEGIN
    -- Check blueprint is approved
    SELECT bb.status INTO bp_status
    FROM book_blueprints bb
    WHERE bb.project_id = NEW.project_id
    ORDER BY bb.created_at DESC
    LIMIT 1;

    IF bp_status IS DISTINCT FROM 'approved' THEN
        RAISE EXCEPTION 'WF_001: book blueprint not approved';
    END IF;

    -- Check previous chapter is approved
    IF NEW.chapter_num > 1 THEN
        SELECT status INTO prev_status
        FROM chapters
        WHERE project_id = NEW.project_id
          AND chapter_num = NEW.chapter_num - 1
        LIMIT 1;

        IF prev_status IS DISTINCT FROM 'approved' THEN
            RAISE EXCEPTION 'WF_002: previous chapter not approved';
        END IF;
    END IF;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_guard_chapter_sequence
BEFORE INSERT ON chapters
FOR EACH ROW
EXECUTE FUNCTION guard_chapter_sequence();

-- ============================================================
-- Updated_at auto-update trigger
-- ============================================================

CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_projects_updated_at BEFORE UPDATE ON projects FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
CREATE TRIGGER trg_world_bibles_updated_at BEFORE UPDATE ON world_bibles FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
CREATE TRIGGER trg_world_bible_constitutions_updated_at BEFORE UPDATE ON world_bible_constitutions FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
CREATE TRIGGER trg_characters_updated_at BEFORE UPDATE ON characters FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
CREATE TRIGGER trg_chapters_updated_at BEFORE UPDATE ON chapters FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
CREATE TRIGGER trg_volumes_updated_at BEFORE UPDATE ON volumes FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
CREATE TRIGGER trg_foreshadowings_updated_at BEFORE UPDATE ON foreshadowings FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
CREATE TRIGGER trg_book_blueprints_updated_at BEFORE UPDATE ON book_blueprints FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

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
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        VARCHAR(100) NOT NULL UNIQUE,
    provider    VARCHAR(50)  NOT NULL DEFAULT 'openai',
    base_url    VARCHAR(500) NOT NULL DEFAULT 'https://api.openai.com/v1',
    api_key     TEXT         NOT NULL,
    model_name  VARCHAR(200) NOT NULL,
    max_tokens  INT          NOT NULL DEFAULT 8192,
    temperature FLOAT        NOT NULL DEFAULT 0.7,
    is_default  BOOLEAN      NOT NULL DEFAULT FALSE,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX idx_llm_profiles_single_default ON llm_profiles (is_default) WHERE (is_default = TRUE);
CREATE INDEX idx_llm_profiles_is_default ON llm_profiles (is_default);
