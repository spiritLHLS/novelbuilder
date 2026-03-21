-- NovelBuilder Database Schema - Part 2: Story Structure
-- Foreshadowings, book blueprints, volumes, chapters

-- ============================================================
-- Foreshadowings
-- ============================================================

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

-- ============================================================
-- Book Blueprints
-- ============================================================

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

-- ============================================================
-- Volumes & Chapters
-- ============================================================

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
    input_tokens    INT NOT NULL DEFAULT 0,
    output_tokens   INT NOT NULL DEFAULT 0,
    status          VARCHAR(20) DEFAULT 'draft',
    version         INT DEFAULT 1,
    review_comment  TEXT,
    created_at      TIMESTAMP DEFAULT NOW(),
    updated_at      TIMESTAMP DEFAULT NOW(),
    CONSTRAINT ck_chapters_status CHECK (status IN ('draft', 'pending_review', 'approved', 'rejected', 'needs_recheck')),
    CONSTRAINT uq_chapters_project_chapter UNIQUE (project_id, chapter_num)
);

CREATE TABLE chapter_snapshots (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    chapter_id        UUID NOT NULL REFERENCES chapters(id) ON DELETE CASCADE,
    version           INT NOT NULL,
    title             VARCHAR(200),
    content           TEXT NOT NULL,
    word_count        INT DEFAULT 0,
    summary           TEXT DEFAULT '',
    quality_report    JSONB DEFAULT '{}',
    originality_score FLOAT DEFAULT 0,
    source            VARCHAR(40) NOT NULL DEFAULT 'manual',
    note              TEXT DEFAULT '',
    created_at        TIMESTAMP DEFAULT NOW()
);

CREATE INDEX idx_chapter_snapshots_chapter_created
    ON chapter_snapshots(chapter_id, created_at DESC);
