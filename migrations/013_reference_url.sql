-- Migration 013: add source_url to reference_materials
-- Tracks the originating URL when a reference book is imported from the network.
ALTER TABLE reference_materials
    ADD COLUMN IF NOT EXISTS source_url VARCHAR(2000);
