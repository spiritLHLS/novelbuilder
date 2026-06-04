package database

import (
	"strings"
	"testing"
)

func TestNormalizeSQLForSQLitePostgresCompatibility(t *testing.T) {
	query, args := normalizeSQL("sqlite", `
		SELECT id
		FROM chapters
		WHERE project_id = ANY($1::uuid[])
		  AND title ILIKE $2
		  AND metadata @> to_jsonb($3::text)
		  AND updated_at > NOW() - INTERVAL '15 minutes'
		FOR UPDATE SKIP LOCKED
	`, []string{"p1", "p2"}, "%intro%", "hero")

	for _, forbidden := range []string{"$1", "$2", "$3", "::", "ILIKE", "NOW()", "FOR UPDATE", "SKIP LOCKED", "INTERVAL"} {
		if strings.Contains(query, forbidden) {
			t.Fatalf("normalized sqlite query still contains %q: %s", forbidden, query)
		}
	}
	for _, expected := range []string{"project_id IN ?", "title LIKE ?", "metadata LIKE '%' || ? || '%'", "datetime('now', '-15 minutes')"} {
		if !strings.Contains(query, expected) {
			t.Fatalf("normalized sqlite query missing %q: %s", expected, query)
		}
	}
	if len(args) != 3 {
		t.Fatalf("normalized args length = %d, want 3", len(args))
	}
}

func TestNormalizeSQLLeavesPostgresQueriesUntouched(t *testing.T) {
	original := `SELECT id FROM tasks WHERE id = ANY($1::uuid[]) FOR UPDATE SKIP LOCKED`
	query, args := normalizeSQL("postgres", original, []string{"t1"})
	if query != original {
		t.Fatalf("postgres query changed: %s", query)
	}
	if len(args) != 1 {
		t.Fatalf("postgres args length = %d, want 1", len(args))
	}
}
