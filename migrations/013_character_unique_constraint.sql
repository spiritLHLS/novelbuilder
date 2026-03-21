-- 013_character_unique_constraint.sql
-- Add missing UNIQUE (project_id, name) constraint on characters table.
-- This constraint is required by ON CONFLICT (project_id, name) clauses used
-- in import_service.go and reference_deep_analysis_service.go.
--
-- Step 1: Remove duplicates keeping the earliest-created row per (project_id, name).
-- In practice duplicates should not exist; this guard makes the migration safe even
-- on production instances that triggered the error.
DELETE FROM characters
WHERE id IN (
    SELECT id
    FROM (
        SELECT id,
               ROW_NUMBER() OVER (PARTITION BY project_id, name ORDER BY created_at) AS rn
        FROM characters
    ) ranked
    WHERE rn > 1
);

-- Step 2: Create the unique index (idempotent via IF NOT EXISTS).
-- A unique index satisfies ON CONFLICT column-list clauses the same as a UNIQUE constraint.
CREATE UNIQUE INDEX IF NOT EXISTS uq_characters_project_name
    ON characters (project_id, name);
