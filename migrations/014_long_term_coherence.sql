-- NovelBuilder Database Schema - Part 14: Long-term Coherence & Foreshadowing Enhancement
-- Adds: volume arc summaries, chapter similarity tracking, auto-generated foreshadowings,
--        entity provenance tracking, genre compliance flags

-- ============================================================
-- Volume Arc Summaries (running compressed summary per volume for long-term coherence)
-- ============================================================

CREATE TABLE volume_arc_summaries (
    id              UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id      UUID         NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    volume_id       UUID         NOT NULL REFERENCES volumes(id) ON DELETE CASCADE,
    summary         TEXT         NOT NULL DEFAULT '',
    key_events      TEXT         NOT NULL DEFAULT '',       -- semicolon-separated key plot events so far
    unresolved_threads TEXT      NOT NULL DEFAULT '',       -- unresolved threads carried forward
    last_chapter_num INT         NOT NULL DEFAULT 0,       -- last chapter incorporated into this summary
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    UNIQUE(project_id, volume_id)
);

CREATE INDEX idx_volume_arc_summaries_project ON volume_arc_summaries(project_id);

CREATE TRIGGER trg_volume_arc_summaries_updated_at BEFORE UPDATE ON volume_arc_summaries
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

-- ============================================================
-- Chapter Similarity Log (detect and track repetitive content across chapters)
-- ============================================================

CREATE TABLE chapter_similarity_log (
    id              UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id      UUID         NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    chapter_id      UUID         NOT NULL REFERENCES chapters(id) ON DELETE CASCADE,
    similar_chapter_id UUID      NOT NULL REFERENCES chapters(id) ON DELETE CASCADE,
    similarity_score FLOAT       NOT NULL DEFAULT 0,       -- 0.0-1.0 cosine similarity
    similar_segments JSONB       NOT NULL DEFAULT '[]',    -- [{segment_a, segment_b, score}]
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    CONSTRAINT ck_similarity_different CHECK (chapter_id <> similar_chapter_id)
);

CREATE INDEX idx_chapter_similarity_project ON chapter_similarity_log(project_id, chapter_id);

-- ============================================================
-- Entity Provenance (track where characters/items first appear and their justification)
-- ============================================================

CREATE TABLE entity_provenance (
    id              UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id      UUID         NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    entity_type     VARCHAR(30)  NOT NULL,                 -- character|item|weapon|skill|location
    entity_name     VARCHAR(200) NOT NULL,
    first_chapter_id UUID        REFERENCES chapters(id) ON DELETE SET NULL,
    first_chapter_num INT,
    source_type     VARCHAR(30)  NOT NULL DEFAULT 'outlined', -- outlined|world_bible|emergent|untracked
    source_detail   TEXT         NOT NULL DEFAULT '',       -- e.g. "大纲事件2: 获得神剑"
    is_justified    BOOLEAN      NOT NULL DEFAULT true,    -- false = flagged for review
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    UNIQUE(project_id, entity_type, entity_name)
);

CREATE INDEX idx_entity_provenance_project ON entity_provenance(project_id);

-- ============================================================
-- Foreshadowing auto-generation tracking
-- ============================================================

ALTER TABLE foreshadowings ADD COLUMN IF NOT EXISTS origin VARCHAR(30) NOT NULL DEFAULT 'manual';
-- origin: manual | blueprint | auto_extracted | reference
-- Tracks whether a foreshadowing was user-created, from blueprint generation,
-- auto-extracted from chapter content, or imported from reference material

ALTER TABLE foreshadowings ADD COLUMN IF NOT EXISTS cross_volume BOOLEAN NOT NULL DEFAULT false;
-- Marks foreshadowings that span across volumes

-- ============================================================
-- Genre compliance audit log
-- ============================================================

ALTER TABLE chapters ADD COLUMN IF NOT EXISTS genre_compliance_score FLOAT NOT NULL DEFAULT 1.0;
-- 0.0-1.0 score for genre consistency, updated during audit

ALTER TABLE chapters ADD COLUMN IF NOT EXISTS genre_violations JSONB NOT NULL DEFAULT '[]';
-- Array of detected genre violations: [{term, context, severity}]
