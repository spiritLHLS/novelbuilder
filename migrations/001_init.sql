-- NovelBuilder Complete Database Schema
-- PostgreSQL 16 with pgvector extension

CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
CREATE EXTENSION IF NOT EXISTS "vector";

-- ============================================================
-- Core Tables
-- ============================================================

CREATE TABLE projects (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    title             VARCHAR(300) NOT NULL,
    genre             VARCHAR(50),
    description       TEXT,
    style_description TEXT,
    target_words      INT NOT NULL DEFAULT 500000,
    status            VARCHAR(20) DEFAULT 'active',
    created_at        TIMESTAMP DEFAULT NOW(),
    updated_at        TIMESTAMP DEFAULT NOW()
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
    status          VARCHAR(20) DEFAULT 'draft',
    chapter_start   INT,
    chapter_end     INT,
    review_comment  TEXT,
    created_at      TIMESTAMP DEFAULT NOW(),
    updated_at      TIMESTAMP DEFAULT NOW(),
    CONSTRAINT ck_volumes_status CHECK (status IN ('draft', 'pending_review', 'approved', 'rejected')),
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

-- ============================================================
-- Change Propagation System (Re3 / PEARL-inspired)
-- ============================================================

-- Dependency graph: what each generated artifact was built from.
-- Populated asynchronously after every chapter generation.
CREATE TABLE content_dependencies (
    id             UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id     UUID        NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    dependent_type VARCHAR(30) NOT NULL,  -- 'chapter'
    dependent_id   UUID        NOT NULL,
    source_type    VARCHAR(30) NOT NULL,  -- 'world_bible','character','blueprint','outline'
    source_id      UUID        NOT NULL,
    created_at     TIMESTAMP   NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_content_dep UNIQUE (dependent_type, dependent_id, source_type, source_id)
);

CREATE INDEX idx_content_dep_source    ON content_dependencies(source_type, source_id);
CREATE INDEX idx_content_dep_dependent ON content_dependencies(dependent_type, dependent_id);
CREATE INDEX idx_content_dep_project   ON content_dependencies(project_id);

-- User-initiated change event (records old→new snapshot of any edited entity).
CREATE TABLE change_events (
    id             UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id     UUID        NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    entity_type    VARCHAR(30) NOT NULL,  -- 'character','world_bible','outline','foreshadowing','blueprint'
    entity_id      UUID        NOT NULL,
    change_summary TEXT        NOT NULL DEFAULT '',
    old_snapshot   JSONB,
    new_snapshot   JSONB,
    status         VARCHAR(20) NOT NULL DEFAULT 'pending',
    created_at     TIMESTAMP   NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMP   NOT NULL DEFAULT NOW(),
    CONSTRAINT ck_change_events_status CHECK (status IN ('pending','analyzed','patching','done','cancelled'))
);

CREATE INDEX idx_change_events_project ON change_events(project_id, created_at DESC);

-- AI-generated propagation plan for a change event.
CREATE TABLE patch_plans (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    change_event_id UUID        NOT NULL REFERENCES change_events(id) ON DELETE CASCADE,
    project_id      UUID        NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    impact_summary  TEXT        NOT NULL DEFAULT '',
    total_items     INT         NOT NULL DEFAULT 0,
    done_items      INT         NOT NULL DEFAULT 0,
    status          VARCHAR(20) NOT NULL DEFAULT 'ready',
    created_at      TIMESTAMP   NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMP   NOT NULL DEFAULT NOW(),
    CONSTRAINT ck_patch_plans_status CHECK (status IN ('ready','executing','done','cancelled'))
);

CREATE INDEX idx_patch_plans_event   ON patch_plans(change_event_id);
CREATE INDEX idx_patch_plans_project ON patch_plans(project_id);

-- Individual tasks within a patch plan (one per affected artifact).
CREATE TABLE patch_items (
    id                 UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    plan_id            UUID        NOT NULL REFERENCES patch_plans(id) ON DELETE CASCADE,
    item_type          VARCHAR(30) NOT NULL,  -- 'chapter','outline','foreshadowing'
    item_id            UUID        NOT NULL,
    item_order         INT         NOT NULL DEFAULT 0,
    impact_description TEXT        NOT NULL DEFAULT '',
    patch_instruction  TEXT        NOT NULL DEFAULT '',
    status             VARCHAR(20) NOT NULL DEFAULT 'pending',
    result_snapshot    JSONB,
    created_at         TIMESTAMP   NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMP   NOT NULL DEFAULT NOW(),
    CONSTRAINT ck_patch_items_status CHECK (status IN ('pending','approved','executing','done','skipped','failed'))
);

CREATE INDEX idx_patch_items_plan ON patch_items(plan_id, item_order);

CREATE TRIGGER trg_change_events_updated_at BEFORE UPDATE ON change_events
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
CREATE TRIGGER trg_patch_plans_updated_at BEFORE UPDATE ON patch_plans
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
CREATE TRIGGER trg_patch_items_updated_at BEFORE UPDATE ON patch_items
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

-- ============================================================
-- Prompt Presets (Ai-Novel feature: reusable prompt blocks)
-- ============================================================

CREATE TABLE prompt_presets (
    id          UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id  UUID         REFERENCES projects(id) ON DELETE CASCADE,  -- NULL = global preset
    name        VARCHAR(200) NOT NULL,
    description TEXT         NOT NULL DEFAULT '',
    category    VARCHAR(50)  NOT NULL DEFAULT 'general',
    content     TEXT         NOT NULL,
    variables   JSONB        NOT NULL DEFAULT '[]',  -- [{name, description, default}]
    is_global   BOOLEAN      NOT NULL DEFAULT FALSE,
    sort_order  INT          NOT NULL DEFAULT 0,
    created_at  TIMESTAMP    NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMP    NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_prompt_presets_project ON prompt_presets(project_id) WHERE project_id IS NOT NULL;
CREATE INDEX idx_prompt_presets_global  ON prompt_presets(is_global)  WHERE is_global = TRUE;
CREATE INDEX idx_prompt_presets_category ON prompt_presets(category);

CREATE TRIGGER trg_prompt_presets_updated_at BEFORE UPDATE ON prompt_presets
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

-- ============================================================
-- Glossary / 术语表 (Ai-Novel feature: inject into chapter prompts)
-- ============================================================

CREATE TABLE glossary_terms (
    id          UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id  UUID         NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    term        VARCHAR(200) NOT NULL,
    definition  TEXT         NOT NULL DEFAULT '',
    aliases     JSONB        NOT NULL DEFAULT '[]',  -- alternative names/spellings
    category    VARCHAR(50)  NOT NULL DEFAULT 'general',  -- character|place|skill|item|general
    created_at  TIMESTAMP    NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMP    NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_glossary_term_project UNIQUE (project_id, term)
);

CREATE INDEX idx_glossary_terms_project  ON glossary_terms(project_id);
CREATE INDEX idx_glossary_terms_category ON glossary_terms(project_id, category);

CREATE TRIGGER trg_glossary_terms_updated_at BEFORE UPDATE ON glossary_terms
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

-- ============================================================
-- Background Task Queue (Ai-Novel: rq_worker-style retry/cancel)
-- ============================================================

CREATE TABLE task_queue (
    id            UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id    UUID         REFERENCES projects(id) ON DELETE SET NULL,
    task_type     VARCHAR(100) NOT NULL,
    payload       JSONB        NOT NULL DEFAULT '{}',
    status        VARCHAR(20)  NOT NULL DEFAULT 'pending',
    priority      INT          NOT NULL DEFAULT 5,
    attempts      INT          NOT NULL DEFAULT 0,
    max_attempts  INT          NOT NULL DEFAULT 3,
    error_message TEXT         NOT NULL DEFAULT '',
    scheduled_at  TIMESTAMP    NOT NULL DEFAULT NOW(),
    started_at    TIMESTAMP,
    completed_at  TIMESTAMP,
    created_at    TIMESTAMP    NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMP    NOT NULL DEFAULT NOW(),
    CONSTRAINT ck_task_status CHECK (status IN ('pending','running','done','failed','cancelled'))
);

-- Partial index on pending tasks sorted for next-to-run query (FOR UPDATE SKIP LOCKED)
CREATE INDEX idx_task_queue_pending ON task_queue(priority DESC, scheduled_at ASC)
    WHERE status = 'pending';
CREATE INDEX idx_task_queue_project ON task_queue(project_id);

CREATE TRIGGER trg_task_queue_updated_at BEFORE UPDATE ON task_queue
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

-- ============================================================
-- Story Resource Ledger (InkOS: particle_ledger concept)
-- Tracks in-story items / currency / skills per character
-- ============================================================

CREATE TABLE story_resources (
    id          UUID          PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id  UUID          NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name        VARCHAR(200)  NOT NULL,
    category    VARCHAR(50)   NOT NULL DEFAULT 'item',  -- item|currency|skill|weapon|misc
    quantity    NUMERIC(15,2) NOT NULL DEFAULT 0,
    unit        VARCHAR(50)   NOT NULL DEFAULT '',
    description TEXT          NOT NULL DEFAULT '',
    holder      VARCHAR(200)  NOT NULL DEFAULT '',  -- character name or "party"
    created_at  TIMESTAMP     NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMP     NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_story_resources_project ON story_resources(project_id);

-- ============================================================
-- System Settings (replaces all config-file / env-var driven config)
-- ============================================================

CREATE TABLE system_settings (
    key        VARCHAR(100) PRIMARY KEY,
    value      TEXT         NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

-- ============================================================
-- Story Resource Ledger
-- ============================================================

CREATE TABLE story_resource_changes (
    id          UUID          PRIMARY KEY DEFAULT gen_random_uuid(),
    resource_id UUID          NOT NULL REFERENCES story_resources(id) ON DELETE CASCADE,
    chapter_id  UUID          REFERENCES chapters(id) ON DELETE SET NULL,
    delta       NUMERIC(15,2) NOT NULL DEFAULT 0,
    reason      TEXT          NOT NULL DEFAULT '',
    created_at  TIMESTAMP     NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_resource_changes_resource ON story_resource_changes(resource_id, created_at);
CREATE INDEX idx_resource_changes_chapter  ON story_resource_changes(chapter_id);

CREATE TRIGGER trg_story_resources_updated_at BEFORE UPDATE ON story_resources
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

-- ============================================================
-- Webhook Notifications (InkOS: notify on chapter/quality events)
-- ============================================================

CREATE TABLE notification_webhooks (
    id          UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id  UUID         NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    url         VARCHAR(500) NOT NULL,
    secret      VARCHAR(200) NOT NULL DEFAULT '',  -- HMAC-SHA256 signing key
    events      JSONB        NOT NULL DEFAULT '["chapter_generated","quality_failed","workflow_step"]',
    is_active   BOOLEAN      NOT NULL DEFAULT TRUE,
    created_at  TIMESTAMP    NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMP    NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_notification_webhooks_project ON notification_webhooks(project_id);

CREATE TRIGGER trg_notification_webhooks_updated_at BEFORE UPDATE ON notification_webhooks
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

-- ============================================================
-- Chapter Summary Cache (narrative-continuity retrieval)
-- ============================================================

CREATE TABLE chapter_summaries (
    id          UUID     PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id  UUID     REFERENCES projects(id) ON DELETE CASCADE,
    chapter_id  UUID     REFERENCES chapters(id) ON DELETE CASCADE,
    chapter_num INT      NOT NULL,
    summary     TEXT     NOT NULL DEFAULT '',
    qdrant_sync BOOLEAN  NOT NULL DEFAULT FALSE,
    created_at  TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMP NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_chapter_summaries_chapter UNIQUE (chapter_id)
);

CREATE INDEX idx_chapter_summaries_project ON chapter_summaries(project_id, chapter_num);
CREATE INDEX idx_chapter_summaries_nosync  ON chapter_summaries(project_id) WHERE qdrant_sync = FALSE;

CREATE TRIGGER trg_chapter_summaries_updated_at BEFORE UPDATE ON chapter_summaries
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

-- ============================================================
-- Graph Sync Log (tracks entities pushed to Neo4j)
-- ============================================================

CREATE TABLE graph_sync_log (
    id            UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id    UUID         NOT NULL,
    entity_type   VARCHAR(50)  NOT NULL,
    entity_id     UUID         NOT NULL,
    synced_at     TIMESTAMP    NOT NULL DEFAULT NOW(),
    neo4j_node_id VARCHAR(200),
    CONSTRAINT uq_graph_sync_log UNIQUE (entity_type, entity_id)
);

CREATE INDEX idx_graph_sync_log_project ON graph_sync_log(project_id, entity_type);

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
-- Multi-Dimension Chapter Audit Reports (33-dimension)
-- ============================================================

CREATE TABLE audit_reports (
    id             UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    chapter_id     UUID         NOT NULL REFERENCES chapters(id) ON DELETE CASCADE,
    project_id     UUID         NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    dimensions     JSONB        NOT NULL DEFAULT '{}',  -- {dim_name: {score, issues, passed}}
    overall_score  FLOAT        NOT NULL DEFAULT 0.0,
    passed         BOOLEAN      NOT NULL DEFAULT FALSE,
    ai_probability FLOAT        NOT NULL DEFAULT 0.0,
    issues         JSONB        NOT NULL DEFAULT '[]',
    revision_count INT          NOT NULL DEFAULT 0,
    created_at     TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_audit_reports_chapter ON audit_reports(chapter_id, created_at DESC);
CREATE INDEX idx_audit_reports_project ON audit_reports(project_id, created_at DESC);

-- ============================================================
-- Book Rules (style guide + writing rules + anti-AI wordlists)
-- One row per project.
-- ============================================================

CREATE TABLE book_rules (
    id               UUID    PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id       UUID    NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    rules_content    TEXT    NOT NULL DEFAULT '',
    style_guide      TEXT    NOT NULL DEFAULT '',
    anti_ai_wordlist JSONB   NOT NULL DEFAULT '[]',
    banned_patterns  JSONB   NOT NULL DEFAULT '[]',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_book_rules_project UNIQUE (project_id)
);

CREATE TRIGGER trg_book_rules_updated_at BEFORE UPDATE ON book_rules
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

-- ============================================================
-- Chapter Imports (paste-and-import existing novels)
-- Supports fan-fiction import via fanfic_mode.
-- ============================================================

CREATE TABLE chapter_imports (
    id                   UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id           UUID         NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    source_text          TEXT         NOT NULL,
    split_pattern        VARCHAR(200) NOT NULL DEFAULT '第.{1,4}[章节回]',
    fanfic_mode          VARCHAR(20),  -- NULL|canon|au|ooc|cp
    status               VARCHAR(20)  NOT NULL DEFAULT 'pending',
    total_chapters       INT          NOT NULL DEFAULT 0,
    processed_chapters   INT          NOT NULL DEFAULT 0,
    error_message        TEXT         NOT NULL DEFAULT '',
    reverse_engineered   JSONB        NOT NULL DEFAULT '{}',
    created_at           TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at           TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    CONSTRAINT ck_chapter_import_status CHECK (status IN ('pending','processing','completed','failed'))
);

CREATE INDEX idx_chapter_imports_project ON chapter_imports(project_id, created_at DESC);

CREATE TRIGGER trg_chapter_imports_updated_at BEFORE UPDATE ON chapter_imports
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

-- ============================================================
-- Fan Fiction Settings (per project)
-- ============================================================

ALTER TABLE projects
    ADD COLUMN fanfic_mode        VARCHAR(20),    -- NULL | canon | au | ooc | cp
    ADD COLUMN fanfic_source_text TEXT,           -- pasted source material
    ADD COLUMN auto_write_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN auto_write_interval INT    NOT NULL DEFAULT 60;  -- minutes
