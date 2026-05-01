-- Migration 016: Continuation Mode for Reference Books
-- Adds project_type and continuation_ref_id to support continuing imported reference books.

ALTER TABLE projects
    ADD COLUMN project_type         VARCHAR(20) NOT NULL DEFAULT 'original',  -- original | continuation
    ADD COLUMN continuation_ref_id  UUID REFERENCES reference_materials(id) ON DELETE SET NULL;

-- Index for quick lookup of continuation projects
CREATE INDEX idx_projects_continuation_ref ON projects(continuation_ref_id) WHERE continuation_ref_id IS NOT NULL;

-- Store the last synced chapter number for continuation projects
-- so the system knows where reference chapters end and generated chapters begin.
ALTER TABLE projects
    ADD COLUMN continuation_start_chapter INT NOT NULL DEFAULT 1;
-- continuation_start_chapter: chapter number from which AI-generated content starts.
-- Chapters before this are from the reference book.
