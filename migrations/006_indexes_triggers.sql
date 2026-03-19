-- NovelBuilder Database Schema - Part 6: Indexes, Core Functions & Triggers

-- ============================================================
-- General Indexes
-- ============================================================

CREATE INDEX idx_workflow_steps_run_order ON workflow_steps(run_id, step_order);
CREATE INDEX idx_workflow_steps_status ON workflow_steps(status);
CREATE INDEX idx_chapters_project_status ON chapters(project_id, status);
CREATE INDEX idx_volumes_project_status ON volumes(project_id, status);
CREATE INDEX idx_outlines_project ON outlines(project_id);
CREATE INDEX idx_characters_project ON characters(project_id);
CREATE INDEX idx_foreshadowings_project ON foreshadowings(project_id);
CREATE INDEX idx_reference_materials_project ON reference_materials(project_id);
CREATE INDEX idx_world_bibles_project ON world_bibles(project_id);
CREATE INDEX idx_world_bible_constitutions_project ON world_bible_constitutions(project_id);
CREATE INDEX idx_book_blueprints_project ON book_blueprints(project_id);
CREATE INDEX idx_chapters_project_num ON chapters(project_id, chapter_num);

-- Deferred FK constraints for foreshadowings (chapters table created later)
ALTER TABLE foreshadowings
    ADD CONSTRAINT fk_foreshadowings_embed_chapter
        FOREIGN KEY (embed_chapter_id) REFERENCES chapters(id) ON DELETE SET NULL,
    ADD CONSTRAINT fk_foreshadowings_resolve_chapter
        FOREIGN KEY (resolve_chapter_id) REFERENCES chapters(id) ON DELETE SET NULL;

-- ============================================================
-- Triggers: Chapter Sequence Guard
-- ============================================================

CREATE OR REPLACE FUNCTION guard_chapter_sequence()
RETURNS TRIGGER AS $$
DECLARE
    prev_status VARCHAR(20);
    bp_status VARCHAR(20);
BEGIN
    -- Allow bulk-import / data-migration operations to bypass this guard
    IF current_setting('app.bypass_sequence_guard', true) = 'true' THEN
        RETURN NEW;
    END IF;

    -- Check blueprint is approved
    SELECT bb.status INTO bp_status
    FROM book_blueprints bb
    WHERE bb.project_id = NEW.project_id
    ORDER BY bb.created_at DESC
    LIMIT 1;

    IF bp_status IS DISTINCT FROM 'approved' THEN
        RAISE EXCEPTION 'WF_001: book blueprint not approved';
    END IF;

    -- Check previous chapter is approved
    IF NEW.chapter_num > 1 THEN
        SELECT status INTO prev_status
        FROM chapters
        WHERE project_id = NEW.project_id
          AND chapter_num = NEW.chapter_num - 1
        LIMIT 1;

        IF prev_status IS DISTINCT FROM 'approved' THEN
            RAISE EXCEPTION 'WF_002: previous chapter not approved';
        END IF;
    END IF;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_guard_chapter_sequence
BEFORE INSERT ON chapters
FOR EACH ROW
EXECUTE FUNCTION guard_chapter_sequence();

-- ============================================================
-- Updated_at auto-update trigger (used by all subsequent tables too)
-- ============================================================

CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_projects_updated_at BEFORE UPDATE ON projects FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
CREATE TRIGGER trg_world_bibles_updated_at BEFORE UPDATE ON world_bibles FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
CREATE TRIGGER trg_world_bible_constitutions_updated_at BEFORE UPDATE ON world_bible_constitutions FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
CREATE TRIGGER trg_characters_updated_at BEFORE UPDATE ON characters FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
CREATE TRIGGER trg_chapters_updated_at BEFORE UPDATE ON chapters FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
CREATE TRIGGER trg_volumes_updated_at BEFORE UPDATE ON volumes FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
CREATE TRIGGER trg_foreshadowings_updated_at BEFORE UPDATE ON foreshadowings FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
CREATE TRIGGER trg_book_blueprints_updated_at BEFORE UPDATE ON book_blueprints FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
