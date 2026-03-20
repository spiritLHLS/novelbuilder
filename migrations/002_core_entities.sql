-- NovelBuilder Database Schema - Part 2: Core Entity Tables
-- projects, reference materials, world bibles, characters, outlines

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
    chapter_words     INT NOT NULL DEFAULT 3000,
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
    source_url      VARCHAR(2000),
    fetch_status    VARCHAR(20) DEFAULT 'none',
    fetch_done      INT DEFAULT 0,
    fetch_total     INT DEFAULT 0,
    fetch_error     TEXT,
    fetch_site      VARCHAR(100),
    fetch_book_id   VARCHAR(200),
    fetch_chapter_ids JSONB DEFAULT '[]'::jsonb,
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
