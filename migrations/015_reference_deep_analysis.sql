-- 015_reference_deep_analysis.sql
-- Chunked background analysis of reference novels:
-- extraction of characters / world settings / outline / glossary / foreshadowings
-- into project tables.  Consolidated from ex-migrations 016 and 019.

-- ── 1. Progress tracking for multi-chunk analysis jobs ─────────────────────
CREATE TABLE IF NOT EXISTS reference_analysis_jobs (
    id            UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    ref_id        UUID        NOT NULL REFERENCES reference_materials(id) ON DELETE CASCADE,
    project_id    UUID        NOT NULL,
    status        TEXT        NOT NULL DEFAULT 'pending'
                              CHECK (status IN ('pending','running','completed','failed','cancelled')),
    total_chunks  INT         NOT NULL DEFAULT 0,
    done_chunks   INT         NOT NULL DEFAULT 0,
    error_message TEXT,
    -- aggregated extraction results (written incrementally, merged on completion)
    extracted_characters   JSONB,   -- [{name,role,description,traits}]
    extracted_world        JSONB,   -- {setting,time_period,locations,magic_system,...}
    extracted_outline      JSONB,   -- [{level,title,summary}]
    -- Checkpoint storage for resumable analysis (consolidated from ex-migration 016).
    chunk_results          JSONB    NOT NULL DEFAULT '[]'::jsonb,
    -- Extended extraction results (consolidated from ex-migration 019).
    extracted_glossary       JSONB, -- [{term,definition,category}]
    extracted_foreshadowings JSONB, -- [{content,priority,related_characters}]
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_ref_analysis_jobs_ref   ON reference_analysis_jobs (ref_id);
CREATE INDEX IF NOT EXISTS idx_ref_analysis_jobs_proj  ON reference_analysis_jobs (project_id);
CREATE INDEX IF NOT EXISTS idx_ref_analysis_jobs_stat  ON reference_analysis_jobs (status);

-- Idempotent ADD COLUMN guards for existing databases that ran the original 015
-- before chunk_results / extracted_glossary / extracted_foreshadowings were added
-- (ex-migrations 016 and 019).
ALTER TABLE reference_analysis_jobs
    ADD COLUMN IF NOT EXISTS chunk_results           JSONB NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN IF NOT EXISTS extracted_glossary      JSONB,
    ADD COLUMN IF NOT EXISTS extracted_foreshadowings JSONB;

-- ── 2. Link reference_materials to its latest analysis job ─────────────────
ALTER TABLE reference_materials
    ADD COLUMN IF NOT EXISTS analysis_job_id UUID REFERENCES reference_analysis_jobs(id) ON DELETE SET NULL;

-- ── 3. Add reference_analyzer to the allowed agent types ───────────────────
-- The CHECK constraint on agent_model_routes needs to include the new type.
-- We drop & recreate the constraint to avoid compatibility issues across PG versions.
ALTER TABLE agent_model_routes
    DROP CONSTRAINT IF EXISTS agent_model_routes_agent_type_check;

ALTER TABLE agent_model_routes
    ADD CONSTRAINT agent_model_routes_agent_type_check
    CHECK (agent_type IN ('writer','auditor','planner','reviser','radar','moderator','reference_analyzer'));
