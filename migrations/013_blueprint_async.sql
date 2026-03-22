-- NovelBuilder Database Schema - Part 13: Async Blueprint Generation
-- Extends book_blueprints to support asynchronous AI generation with status tracking.

-- Drop the old strict status constraint and add 'generating' and 'failed' states.
ALTER TABLE book_blueprints
    DROP CONSTRAINT IF EXISTS ck_blueprint_status;

ALTER TABLE book_blueprints
    ADD CONSTRAINT ck_blueprint_status
    CHECK (status IN ('generating', 'draft', 'failed', 'pending_review', 'approved', 'rejected'));

-- Column to store error details when status = 'failed'.
ALTER TABLE book_blueprints
    ADD COLUMN IF NOT EXISTS error_message TEXT;
