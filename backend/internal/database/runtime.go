package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/novelbuilder/backend/internal/config"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

var ErrNoRows = sql.ErrNoRows

type CommandTag struct {
	rowsAffected int64
}

func (t CommandTag) RowsAffected() int64 {
	return t.rowsAffected
}

type Batch struct {
	QueuedQueries []QueuedQuery
}

type QueuedQuery struct {
	SQL       string
	Arguments []interface{}
}

func (b *Batch) Queue(query string, arguments ...interface{}) *QueuedQuery {
	q := QueuedQuery{SQL: query, Arguments: arguments}
	b.QueuedQueries = append(b.QueuedQueries, q)
	return &b.QueuedQueries[len(b.QueuedQueries)-1]
}

func (b *Batch) Len() int {
	if b == nil {
		return 0
	}
	return len(b.QueuedQueries)
}

// DB is the single runtime database entrypoint. It is backed by GORM so the
// same service code can run against PostgreSQL or SQLite while repository
// methods are gradually simplified around models.
type DB struct {
	gorm    *gorm.DB
	sqlDB   *sql.DB
	dialect string
	logger  *zap.Logger
}

type Row struct {
	row *sql.Row
}

type Rows struct {
	rows *sql.Rows
}

type Tx struct {
	db      *DB
	gorm    *gorm.DB
	dialect string
}

type BatchResults struct {
	db      executor
	ctx     context.Context
	dialect string
	batch   *Batch
	index   int
	err     error
}

type executor interface {
	Exec(ctx context.Context, query string, args ...interface{}) (CommandTag, error)
	Query(ctx context.Context, query string, args ...interface{}) (*Rows, error)
	QueryRow(ctx context.Context, query string, args ...interface{}) *Row
}

func NewPool(cfg config.DatabaseConfig, logger *zap.Logger) (*DB, error) {
	gormDB, err := NewGORM(cfg, logger)
	if err != nil {
		return nil, err
	}
	sqlDB, err := gormDB.DB()
	if err != nil {
		return nil, fmt.Errorf("get sql database: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := sqlDB.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("ping database: %w", err)
	}
	db := &DB{
		gorm:    gormDB,
		sqlDB:   sqlDB,
		dialect: gormDB.Dialector.Name(),
		logger:  logger,
	}
	if logger != nil {
		logger.Info("database connected", zap.String("driver", db.dialect))
	}
	return db, nil
}

func (db *DB) GORM() *gorm.DB {
	return db.gorm
}

func (db *DB) DriverName() string {
	return db.dialect
}

func (db *DB) Close() {
	if db != nil && db.sqlDB != nil {
		_ = db.sqlDB.Close()
	}
}

func (db *DB) Ping(ctx context.Context) error {
	if db == nil || db.sqlDB == nil {
		return errors.New("database is not initialized")
	}
	return db.sqlDB.PingContext(ctx)
}

func (db *DB) Exec(ctx context.Context, query string, args ...interface{}) (CommandTag, error) {
	return execGORM(ctx, db.gorm, db.dialect, query, args...)
}

func (db *DB) QueryRow(ctx context.Context, query string, args ...interface{}) *Row {
	query, args = normalizeSQL(db.dialect, query, args...)
	return &Row{row: db.gorm.WithContext(ctx).Raw(query, args...).Row()}
}

func (db *DB) Query(ctx context.Context, query string, args ...interface{}) (*Rows, error) {
	query, args = normalizeSQL(db.dialect, query, args...)
	rows, err := db.gorm.WithContext(ctx).Raw(query, args...).Rows()
	if err != nil {
		return nil, mapSQLError(err)
	}
	return &Rows{rows: rows}, nil
}

func (db *DB) Begin(ctx context.Context) (*Tx, error) {
	tx := db.gorm.WithContext(ctx).Begin()
	if tx.Error != nil {
		return nil, tx.Error
	}
	return &Tx{db: db, gorm: tx, dialect: db.dialect}, nil
}

func (db *DB) SendBatch(ctx context.Context, batch *Batch) *BatchResults {
	return &BatchResults{db: db, ctx: ctx, dialect: db.dialect, batch: batch}
}

func (tx *Tx) Exec(ctx context.Context, query string, args ...interface{}) (CommandTag, error) {
	return execGORM(ctx, tx.gorm, tx.dialect, query, args...)
}

func (tx *Tx) QueryRow(ctx context.Context, query string, args ...interface{}) *Row {
	query, args = normalizeSQL(tx.dialect, query, args...)
	return &Row{row: tx.gorm.WithContext(ctx).Raw(query, args...).Row()}
}

func (tx *Tx) Query(ctx context.Context, query string, args ...interface{}) (*Rows, error) {
	query, args = normalizeSQL(tx.dialect, query, args...)
	rows, err := tx.gorm.WithContext(ctx).Raw(query, args...).Rows()
	if err != nil {
		return nil, mapSQLError(err)
	}
	return &Rows{rows: rows}, nil
}

func (tx *Tx) SendBatch(ctx context.Context, batch *Batch) *BatchResults {
	return &BatchResults{db: tx, ctx: ctx, dialect: tx.dialect, batch: batch}
}

func (tx *Tx) Commit(ctx context.Context) error {
	return tx.gorm.WithContext(ctx).Commit().Error
}

func (tx *Tx) Rollback(ctx context.Context) error {
	return tx.gorm.WithContext(ctx).Rollback().Error
}

func (r *Row) Scan(dest ...interface{}) error {
	if r == nil || r.row == nil {
		return ErrNoRows
	}
	return mapSQLError(r.row.Scan(dest...))
}

func (r *Rows) Next() bool {
	return r != nil && r.rows != nil && r.rows.Next()
}

func (r *Rows) Scan(dest ...interface{}) error {
	if r == nil || r.rows == nil {
		return errors.New("rows closed")
	}
	return mapSQLError(r.rows.Scan(dest...))
}

func (r *Rows) Close() {
	if r != nil && r.rows != nil {
		_ = r.rows.Close()
	}
}

func (r *Rows) Err() error {
	if r == nil || r.rows == nil {
		return nil
	}
	return mapSQLError(r.rows.Err())
}

func (br *BatchResults) Exec() (CommandTag, error) {
	if br.err != nil {
		return CommandTag{}, br.err
	}
	if br.batch == nil || br.index >= len(br.batch.QueuedQueries) {
		return CommandTag{}, nil
	}
	q := br.batch.QueuedQueries[br.index]
	br.index++
	tag, err := br.db.Exec(br.ctx, q.SQL, q.Arguments...)
	if err != nil {
		br.err = err
	}
	return tag, err
}

func (br *BatchResults) Query() (*Rows, error) {
	if br.err != nil {
		return nil, br.err
	}
	if br.batch == nil || br.index >= len(br.batch.QueuedQueries) {
		return nil, ErrNoRows
	}
	q := br.batch.QueuedQueries[br.index]
	br.index++
	rows, err := br.db.Query(br.ctx, q.SQL, q.Arguments...)
	if err != nil {
		br.err = err
	}
	return rows, err
}

func (br *BatchResults) QueryRow() *Row {
	if br.err != nil || br.batch == nil || br.index >= len(br.batch.QueuedQueries) {
		return &Row{}
	}
	q := br.batch.QueuedQueries[br.index]
	br.index++
	return br.db.QueryRow(br.ctx, q.SQL, q.Arguments...)
}

func (br *BatchResults) Close() error {
	return br.err
}

func execGORM(ctx context.Context, db *gorm.DB, dialect string, query string, args ...interface{}) (CommandTag, error) {
	query, args = normalizeSQL(dialect, query, args...)
	if isSQLiteNoop(dialect, query) {
		return CommandTag{}, nil
	}
	result := db.WithContext(ctx).Exec(query, args...)
	if result.Error != nil {
		return CommandTag{}, mapSQLError(result.Error)
	}
	return CommandTag{rowsAffected: result.RowsAffected}, nil
}

func mapSQLError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) || errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrNoRows
	}
	return err
}

var (
	sqlitePlaceholderRe      = regexp.MustCompile(`\$[0-9]+`)
	sqliteAnyRe              = regexp.MustCompile(`=\s*ANY\(\$([0-9]+)(?:::[a-zA-Z0-9_\[\]]+)?\)`)
	sqliteCastRe             = regexp.MustCompile(`::[a-zA-Z0-9_\[\]]+`)
	sqliteOnConstraintRe     = regexp.MustCompile(`(?i)ON\s+CONFLICT\s+ON\s+CONSTRAINT\s+[a-zA-Z0-9_]+\s+DO\s+NOTHING`)
	sqliteForUpdateRe        = regexp.MustCompile(`(?i)\s+FOR\s+UPDATE(\s+SKIP\s+LOCKED)?`)
	sqliteIntervalMinutesRe  = regexp.MustCompile(`(?i)NOW\(\)\s*-\s*INTERVAL\s+'([0-9]+)\s+minutes?'`)
	sqliteJsonTextContainRe  = regexp.MustCompile(`(?i)([a-zA-Z0-9_\.]+)\s*@>\s*to_jsonb\(\$([0-9]+)(?:::[a-zA-Z0-9_\[\]]+)?\)`)
	sqliteJsonParamContainRe = regexp.MustCompile(`(?i)([a-zA-Z0-9_\.]+)\s*@>\s*to_jsonb\(\?\)`)
)

func normalizeSQL(dialect string, query string, args ...interface{}) (string, []interface{}) {
	if dialect != "sqlite" {
		return query, args
	}
	q := query
	q = sqliteIntervalMinutesRe.ReplaceAllString(q, "datetime('now', '-$1 minutes')")
	q = replaceSQLiteAny(q)
	q = replaceSQLiteJSONContains(q)
	q = sqliteOnConstraintRe.ReplaceAllString(q, "ON CONFLICT DO NOTHING")
	q = sqliteForUpdateRe.ReplaceAllString(q, "")
	q = strings.ReplaceAll(q, "NOW()", "CURRENT_TIMESTAMP")
	q = strings.ReplaceAll(q, "now()", "CURRENT_TIMESTAMP")
	q = strings.ReplaceAll(q, "ILIKE", "LIKE")
	q = strings.ReplaceAll(q, "ilike", "LIKE")
	q = strings.ReplaceAll(q, "char_length(", "length(")
	q = strings.ReplaceAll(q, "LEAST(", "min(")
	q = strings.ReplaceAll(q, "least(", "min(")
	q = strings.ReplaceAll(q, "E'\\n'", "char(10)")
	q = strings.ReplaceAll(q, "jsonb_each(ar.dimensions) AS d(key, val)", "json_each(ar.dimensions) AS d")
	q = strings.ReplaceAll(q, "(val->>'passed')::boolean = FALSE", "COALESCE(json_extract(d.value, '$.passed'), 0) = 0")
	q = strings.ReplaceAll(q, "position(EXCLUDED.relationship in character_interactions.relationship) > 0", "instr(character_interactions.relationship, EXCLUDED.relationship) > 0")
	q = strings.ReplaceAll(q, "profile = characters.profile || EXCLUDED.profile", "profile = json_patch(COALESCE(characters.profile, '{}'), COALESCE(EXCLUDED.profile, '{}'))")
	q = strings.ReplaceAll(q, "content    = EXCLUDED.content || world_bibles.content", "content    = json_patch(COALESCE(world_bibles.content, '{}'), COALESCE(EXCLUDED.content, '{}'))")
	q = strings.ReplaceAll(q, "content = COALESCE(content, '{}') || $1::jsonb", "content = json_patch(COALESCE(content, '{}'), $1)")
	q = sqliteCastRe.ReplaceAllString(q, "")
	q = sqliteJsonParamContainRe.ReplaceAllString(q, "$1 LIKE '%' || ? || '%'")
	q, args = expandSQLitePlaceholders(q, args)
	return q, args
}

func replaceSQLiteAny(query string) string {
	return sqliteAnyRe.ReplaceAllStringFunc(query, func(match string) string {
		parts := sqliteAnyRe.FindStringSubmatch(match)
		if len(parts) < 2 {
			return match
		}
		return "IN $" + parts[1]
	})
}

func replaceSQLiteJSONContains(query string) string {
	return sqliteJsonTextContainRe.ReplaceAllStringFunc(query, func(match string) string {
		parts := sqliteJsonTextContainRe.FindStringSubmatch(match)
		if len(parts) < 3 {
			return match
		}
		return parts[1] + " LIKE '%' || $" + parts[2] + " || '%'"
	})
}

func expandSQLitePlaceholders(query string, args []interface{}) (string, []interface{}) {
	expanded := make([]interface{}, 0, len(args))
	q := sqlitePlaceholderRe.ReplaceAllStringFunc(query, func(match string) string {
		var index int
		for _, ch := range match[1:] {
			index = index*10 + int(ch-'0')
		}
		if index > 0 && index <= len(args) {
			expanded = append(expanded, args[index-1])
		}
		return "?"
	})
	if len(expanded) == 0 {
		return q, args
	}
	return q, expanded
}

func isSQLiteNoop(dialect string, query string) bool {
	if dialect != "sqlite" {
		return false
	}
	trimmed := strings.ToUpper(strings.TrimSpace(query))
	return strings.HasPrefix(trimmed, "CREATE EXTENSION") ||
		strings.HasPrefix(trimmed, "CREATE SCHEMA") ||
		strings.HasPrefix(trimmed, "GRANT ") ||
		strings.HasPrefix(trimmed, "ALTER DEFAULT PRIVILEGES") ||
		strings.HasPrefix(trimmed, "SET LOCAL ")
}
