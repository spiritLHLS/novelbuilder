-- Add unique constraint for chapter-level outlines to support upsert
CREATE UNIQUE INDEX IF NOT EXISTS idx_outlines_project_level_order
ON outlines(project_id, level, order_num)
WHERE level = 'chapter';
