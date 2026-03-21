-- NovelBuilder Database Schema - Part 4: Plot Analysis & Vector Store

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
