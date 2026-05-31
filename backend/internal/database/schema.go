package database

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
)

// EnsureRuntimeSchema applies small, idempotent schema guarantees required by
// the running binary. The full schema is still created by the SQL files on a
// fresh database; this function covers no-downtime upgrades when the container
// image changes but the Postgres volume is kept.
func EnsureRuntimeSchema(ctx context.Context, db *pgxpool.Pool, logger *zap.Logger) error {
	statements := []string{
		`CREATE EXTENSION IF NOT EXISTS "uuid-ossp"`,
		`CREATE EXTENSION IF NOT EXISTS "vector"`,
		`ALTER TABLE projects ADD COLUMN IF NOT EXISTS project_type VARCHAR(20) NOT NULL DEFAULT 'original'`,
		`ALTER TABLE projects ADD COLUMN IF NOT EXISTS continuation_ref_id UUID REFERENCES reference_materials(id) ON DELETE SET NULL`,
		`ALTER TABLE projects ADD COLUMN IF NOT EXISTS continuation_start_chapter INT NOT NULL DEFAULT 1`,
		`CREATE INDEX IF NOT EXISTS idx_projects_continuation_ref ON projects(continuation_ref_id) WHERE continuation_ref_id IS NOT NULL`,
		`CREATE UNIQUE INDEX IF NOT EXISTS uq_characters_project_name ON characters(project_id, name)`,
		`CREATE INDEX IF NOT EXISTS idx_ref_book_chapters_ref_all ON reference_book_chapters(ref_id, chapter_no)`,
		`CREATE INDEX IF NOT EXISTS idx_char_interactions_project ON character_interactions(project_id)`,
	}
	for _, stmt := range statements {
		if _, err := db.Exec(ctx, stmt); err != nil {
			trimmed := strings.Join(strings.Fields(stmt), " ")
			return fmt.Errorf("ensure runtime schema %q: %w", trimmed, err)
		}
	}
	if logger != nil {
		logger.Info("runtime schema ensured")
	}
	return nil
}
