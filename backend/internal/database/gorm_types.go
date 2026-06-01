package database

import (
	"database/sql/driver"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

type TextArray []string

func (TextArray) GormDataType() string {
	return "text_array"
}

func (TextArray) GormDBDataType(db *gorm.DB, _ *schema.Field) string {
	if db.Dialector.Name() == "postgres" {
		return "text[]"
	}
	return "text"
}

func (a TextArray) Value() (driver.Value, error) {
	if len(a) == 0 {
		return "{}", nil
	}
	escaped := make([]string, 0, len(a))
	for _, v := range a {
		v = strings.ReplaceAll(v, `\`, `\\`)
		v = strings.ReplaceAll(v, `"`, `\"`)
		escaped = append(escaped, `"`+v+`"`)
	}
	return "{" + strings.Join(escaped, ",") + "}", nil
}

func (a *TextArray) Scan(value interface{}) error {
	if value == nil {
		*a = nil
		return nil
	}
	raw, ok := value.(string)
	if !ok {
		if b, ok := value.([]byte); ok {
			raw = string(b)
		} else {
			return fmt.Errorf("scan text array: unsupported %T", value)
		}
	}
	raw = strings.Trim(raw, "{}")
	if raw == "" {
		*a = []string{}
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		out = append(out, strings.Trim(part, `"`))
	}
	*a = out
	return nil
}

type JSONB []byte

func (JSONB) GormDataType() string {
	return "json"
}

func (JSONB) GormDBDataType(db *gorm.DB, _ *schema.Field) string {
	if db.Dialector.Name() == "postgres" {
		return "jsonb"
	}
	return "text"
}

func (j JSONB) Value() (driver.Value, error) {
	if len(j) == 0 {
		return nil, nil
	}
	return string(j), nil
}

func (j *JSONB) Scan(value interface{}) error {
	if value == nil {
		*j = nil
		return nil
	}
	switch v := value.(type) {
	case []byte:
		*j = append((*j)[0:0], v...)
	case string:
		*j = append((*j)[0:0], v...)
	default:
		return fmt.Errorf("scan json: unsupported %T", value)
	}
	return nil
}

type PlotElementSchema struct {
	ID          string `gorm:"type:uuid;primaryKey"`
	MaterialID  string `gorm:"type:uuid;not null"`
	ElementType string `gorm:"type:varchar(30);not null"`
	Content     string `gorm:"type:text;not null"`
	Vector      string `gorm:"type:vector(1024)"`
	CreatedAt   *time.Time
}

func (PlotElementSchema) TableName() string { return "quarantine_zone.plot_elements" }

type PlotElementSQLiteSchema struct {
	ID          string `gorm:"type:uuid;primaryKey"`
	MaterialID  string `gorm:"type:uuid;not null"`
	ElementType string `gorm:"type:varchar(30);not null"`
	Content     string `gorm:"type:text;not null"`
	Vector      string `gorm:"type:vector(1024)"`
	CreatedAt   *time.Time
}

func (PlotElementSQLiteSchema) TableName() string { return "plot_elements" }
