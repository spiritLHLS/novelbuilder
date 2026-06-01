package database

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/novelbuilder/backend/internal/config"
	"go.uber.org/zap"
)

func TestAutoMigrateSQLite(t *testing.T) {
	db, err := NewGORM(config.DatabaseConfig{
		Driver:       "sqlite",
		SQLitePath:   filepath.Join(t.TempDir(), "novelbuilder.db"),
		MaxOpenConns: 4,
		MaxIdleConns: 1,
	}, zap.NewNop())
	if err != nil {
		t.Fatalf("NewGORM sqlite: %v", err)
	}
	if err := AutoMigrate(context.Background(), db, zap.NewNop()); err != nil {
		t.Fatalf("AutoMigrate sqlite: %v", err)
	}
}
