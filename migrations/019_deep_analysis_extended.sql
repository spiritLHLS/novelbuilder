-- 019_deep_analysis_extended.sql
-- Add glossary and foreshadowing extraction columns to analysis jobs.
-- These are filled during the merge phase of deep analysis.

ALTER TABLE reference_analysis_jobs
    ADD COLUMN IF NOT EXISTS extracted_glossary      JSONB,
    ADD COLUMN IF NOT EXISTS extracted_foreshadowings JSONB;
