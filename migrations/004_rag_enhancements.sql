-- Migration 004: RAG enhancements
-- Adds source tracking columns to vector_store and sample_texts cache to reference_materials

-- Add source tracking so individual reference's vectors can be deleted/rebuilt efficiently
ALTER TABLE vector_store
    ADD COLUMN IF NOT EXISTS source_type VARCHAR(50) NOT NULL DEFAULT 'reference',
    ADD COLUMN IF NOT EXISTS source_id   VARCHAR(100);

CREATE INDEX IF NOT EXISTS idx_vector_store_source_id
    ON vector_store(project_id, source_id);

-- Cache the extracted text samples so rebuild does not require re-reading raw files
ALTER TABLE reference_materials
    ADD COLUMN IF NOT EXISTS sample_texts JSONB;
