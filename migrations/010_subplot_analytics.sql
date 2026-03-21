-- NovelBuilder Database Schema - Part 10: Subplot & Analytics
-- Subplot board, character emotional arcs, character interaction matrix, radar scan results

-- ============================================================
-- Subplot Board (A/B/C plot-line tracking)
-- ============================================================

CREATE TABLE subplots (
    id              UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id      UUID         NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    title           VARCHAR(200) NOT NULL,
    line_label      VARCHAR(10)  NOT NULL DEFAULT 'A',  -- A|B|C|D
    description     TEXT         NOT NULL DEFAULT '',
    status          VARCHAR(20)  NOT NULL DEFAULT 'active',  -- active|paused|resolved|stalled
    priority        INT          NOT NULL DEFAULT 3,
    start_chapter   INT,
    resolve_chapter INT,
    tags            TEXT[]       NOT NULL DEFAULT '{}',
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    CONSTRAINT ck_subplot_status CHECK (status IN ('active','paused','resolved','stalled'))
);

CREATE INDEX idx_subplots_project ON subplots(project_id);

CREATE TABLE subplot_checkpoints (
    id          UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    subplot_id  UUID         NOT NULL REFERENCES subplots(id) ON DELETE CASCADE,
    chapter_id  UUID         REFERENCES chapters(id) ON DELETE SET NULL,
    chapter_num INT,
    note        TEXT         NOT NULL DEFAULT '',
    progress    INT          NOT NULL DEFAULT 0  CHECK (progress BETWEEN 0 AND 100),
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_subplot_checkpoints_subplot ON subplot_checkpoints(subplot_id, chapter_num);

CREATE TRIGGER trg_subplots_updated_at BEFORE UPDATE ON subplots
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

-- ============================================================
-- Character Emotional Arcs (per-character per-chapter emotion tracking)
-- ============================================================

CREATE TABLE emotional_arc_entries (
    id              UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id      UUID         NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    character_id    UUID         NOT NULL REFERENCES characters(id) ON DELETE CASCADE,
    chapter_id      UUID         REFERENCES chapters(id) ON DELETE SET NULL,
    chapter_num     INT          NOT NULL,
    emotion         VARCHAR(50)  NOT NULL DEFAULT 'neutral',
    intensity       FLOAT        NOT NULL DEFAULT 0.5 CHECK (intensity BETWEEN 0 AND 1),
    note            TEXT         NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_emotional_arc_project ON emotional_arc_entries(project_id, chapter_num);
CREATE INDEX idx_emotional_arc_character ON emotional_arc_entries(character_id, chapter_num);
CREATE UNIQUE INDEX uq_emotional_arc_char_chapter ON emotional_arc_entries(character_id, chapter_num);

CREATE TRIGGER trg_emotional_arc_entries_updated_at BEFORE UPDATE ON emotional_arc_entries
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

-- ============================================================
-- Character Interaction Matrix (track who-met-who + info boundaries)
-- ============================================================

CREATE TABLE character_interactions (
    id              UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id      UUID         NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    char_a_id       UUID         NOT NULL REFERENCES characters(id) ON DELETE CASCADE,
    char_b_id       UUID         NOT NULL REFERENCES characters(id) ON DELETE CASCADE,
    first_meet_chapter INT,
    last_interact_chapter INT,
    relationship    VARCHAR(100) NOT NULL DEFAULT 'acquaintance',
    info_known_by_a JSONB        NOT NULL DEFAULT '[]',  -- facts char_a knows about char_b
    info_known_by_b JSONB        NOT NULL DEFAULT '[]',  -- facts char_b knows about char_a
    interaction_count INT        NOT NULL DEFAULT 1,
    notes           TEXT         NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_character_interaction UNIQUE (project_id, char_a_id, char_b_id),
    CONSTRAINT ck_char_order CHECK (char_a_id < char_b_id)  -- canonical ordering
);

CREATE INDEX idx_char_interactions_project ON character_interactions(project_id);
CREATE INDEX idx_char_interactions_char_a ON character_interactions(char_a_id);
CREATE INDEX idx_char_interactions_char_b ON character_interactions(char_b_id);

CREATE TRIGGER trg_character_interactions_updated_at BEFORE UPDATE ON character_interactions
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

-- ============================================================
-- Radar Scan Results (market trend analysis cache)
-- ============================================================

CREATE TABLE radar_scan_results (
    id          UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id  UUID         REFERENCES projects(id) ON DELETE CASCADE,
    genre       VARCHAR(50)  NOT NULL DEFAULT '',
    platform    VARCHAR(50)  NOT NULL DEFAULT 'general',
    result      JSONB        NOT NULL DEFAULT '{}',
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_radar_scan_results_project ON radar_scan_results(project_id, created_at DESC);
