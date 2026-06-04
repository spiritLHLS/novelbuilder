package config

import "testing"

func TestLoadNormalizesDatabasePoolSettings(t *testing.T) {
	t.Setenv("ADMIN_PASSWORD", "test-admin-password")
	t.Setenv("DB_MAX_OPEN_CONNS", "1")
	t.Setenv("DB_MAX_IDLE_CONNS", "0")
	t.Setenv("DB_CONN_MAX_LIFETIME_MINUTES", "999")

	cfg := Load()
	if cfg.Database.MaxOpenConns != 20 {
		t.Fatalf("MaxOpenConns = %d, want 20", cfg.Database.MaxOpenConns)
	}
	if cfg.Database.MaxIdleConns != 5 {
		t.Fatalf("MaxIdleConns = %d, want 5", cfg.Database.MaxIdleConns)
	}
	if cfg.Database.ConnMaxLifetimeMinutes != 60 {
		t.Fatalf("ConnMaxLifetimeMinutes = %d, want 60", cfg.Database.ConnMaxLifetimeMinutes)
	}
}

func TestLoadDoesNotDefaultDatabasePassword(t *testing.T) {
	t.Setenv("ADMIN_PASSWORD", "test-admin-password")
	t.Setenv("DB_PASSWORD", "")

	cfg := Load()
	if cfg.Database.Password != "" {
		t.Fatalf("Database.Password = %q, want empty default", cfg.Database.Password)
	}
}
