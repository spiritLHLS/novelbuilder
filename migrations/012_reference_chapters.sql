-- 012_reference_chapters.sql
-- Reference book chapter storage + background deep-analysis job tracking.

-- ============================================================
-- Reference Book Chapters
-- Individual chapter records for downloaded reference books.
-- Stored in DB so users can browse/delete individual chapters.
-- ============================================================

CREATE TABLE reference_book_chapters (
    id         UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    ref_id     UUID         NOT NULL REFERENCES reference_materials(id) ON DELETE CASCADE,
    chapter_no INT          NOT NULL,
    chapter_id VARCHAR(200) NOT NULL DEFAULT '',
    title      VARCHAR(500) NOT NULL DEFAULT '',
    content    TEXT         NOT NULL DEFAULT '',
    word_count INT          NOT NULL DEFAULT 0,
    is_deleted BOOLEAN      NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_ref_book_chapters_ref
    ON reference_book_chapters(ref_id)
    WHERE NOT is_deleted;

CREATE INDEX idx_ref_book_chapters_ref_all
    ON reference_book_chapters(ref_id, chapter_no);

-- ============================================================
-- Reference Analysis Jobs
-- Chunked background analysis of reference novels:
-- extraction of characters / world settings / outline / glossary / foreshadowings.
-- ============================================================

CREATE TABLE reference_analysis_jobs (
    id                       UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    ref_id                   UUID        NOT NULL REFERENCES reference_materials(id) ON DELETE CASCADE,
    project_id               UUID        NOT NULL,
    status                   TEXT        NOT NULL DEFAULT 'pending'
                                         CHECK (status IN ('pending','running','completed','failed','cancelled')),
    total_chunks             INT         NOT NULL DEFAULT 0,
    done_chunks              INT         NOT NULL DEFAULT 0,
    error_message            TEXT,
    -- Aggregated extraction results (written incrementally, merged on completion)
    extracted_characters     JSONB,   -- [{name,role,description,traits}]
    extracted_world          JSONB,   -- {setting,time_period,locations,magic_system,...}
    extracted_outline        JSONB,   -- [{level,title,summary}]
    -- Checkpoint storage for resumable analysis
    chunk_results            JSONB       NOT NULL DEFAULT '[]'::jsonb,
    -- Extended extraction results
    extracted_glossary       JSONB,   -- [{term,definition,category}]
    extracted_foreshadowings JSONB,   -- [{content,priority,related_characters}]
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_ref_analysis_jobs_ref  ON reference_analysis_jobs(ref_id);
CREATE INDEX idx_ref_analysis_jobs_proj ON reference_analysis_jobs(project_id);
CREATE INDEX idx_ref_analysis_jobs_stat ON reference_analysis_jobs(status);

CREATE TRIGGER trg_ref_analysis_jobs_updated_at BEFORE UPDATE ON reference_analysis_jobs
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

-- ── Link reference_materials to its latest analysis job ────────────────────
ALTER TABLE reference_materials
    ADD COLUMN analysis_job_id UUID REFERENCES reference_analysis_jobs(id) ON DELETE SET NULL;

