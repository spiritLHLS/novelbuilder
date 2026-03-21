-- NovelBuilder Database Schema - Part 8: Content Tools
-- Prompt presets, glossary, book rules, chapter imports, fan-fiction settings

-- ============================================================
-- Prompt Presets (reusable prompt blocks)
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
-- Glossary / 术语表 (inject into chapter prompts)
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
