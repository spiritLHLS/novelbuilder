-- NovelBuilder Migration 014: Reference book chapter management + download task tracking

-- Add source URL (consolidated from 013) and per-download fetch tracking fields
ALTER TABLE reference_materials
    ADD COLUMN IF NOT EXISTS source_url        VARCHAR(2000),
    ADD COLUMN IF NOT EXISTS fetch_status      VARCHAR(20) DEFAULT 'none',
    ADD COLUMN IF NOT EXISTS fetch_done        INT DEFAULT 0,
    ADD COLUMN IF NOT EXISTS fetch_total       INT DEFAULT 0,
    ADD COLUMN IF NOT EXISTS fetch_error       TEXT,
    ADD COLUMN IF NOT EXISTS fetch_site        VARCHAR(100),
    ADD COLUMN IF NOT EXISTS fetch_book_id     VARCHAR(200),
    ADD COLUMN IF NOT EXISTS fetch_chapter_ids JSONB DEFAULT '[]'::jsonb;

-- Individual chapter records for downloaded reference books
-- Chapters are stored in DB so users can search/delete individual ones.
CREATE TABLE IF NOT EXISTS reference_book_chapters (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    ref_id     UUID NOT NULL REFERENCES reference_materials(id) ON DELETE CASCADE,
    chapter_no INT NOT NULL,
    chapter_id VARCHAR(200) NOT NULL DEFAULT '',
    title      VARCHAR(500) NOT NULL DEFAULT '',
    content    TEXT NOT NULL DEFAULT '',
    word_count INT NOT NULL DEFAULT 0,
    is_deleted BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_ref_book_chapters_ref
    ON reference_book_chapters(ref_id)
    WHERE NOT is_deleted;

CREATE INDEX IF NOT EXISTS idx_ref_book_chapters_ref_all
    ON reference_book_chapters(ref_id, chapter_no);
