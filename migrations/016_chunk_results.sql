-- 016_chunk_results.sql
-- Add per-chunk result storage so that a cancelled / failed analysis job
-- can be resumed from its last checkpoint rather than restarting from scratch.

ALTER TABLE reference_analysis_jobs
    ADD COLUMN IF NOT EXISTS chunk_results JSONB NOT NULL DEFAULT '[]'::jsonb;
