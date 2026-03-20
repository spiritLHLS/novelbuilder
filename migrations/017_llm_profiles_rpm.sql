-- 017_llm_profiles_rpm.sql
-- Add per-profile RPM (requests per minute) limit for LLM APIs.
-- 0 = unlimited (default, preserves existing behavior).
ALTER TABLE llm_profiles
    ADD COLUMN IF NOT EXISTS rpm_limit INT NOT NULL DEFAULT 0;
