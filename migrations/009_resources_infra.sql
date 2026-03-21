-- NovelBuilder Database Schema - Part 9: Resources & Infrastructure
-- Story resource ledger, system settings, webhooks, summaries, sync log, audit reports

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
-- System Settings (replaces all config-file / env-var driven config)
-- ============================================================

CREATE TABLE system_settings (
    key        VARCHAR(100) PRIMARY KEY,
    value      TEXT         NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

-- ============================================================
-- Webhook Notifications (notify on chapter/quality events)
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
