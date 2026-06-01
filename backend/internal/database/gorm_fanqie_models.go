package database

import "time"

type FanqieUploadSchema struct {
	ID              string `gorm:"type:uuid;primaryKey"`
	ProjectID       string `gorm:"type:uuid;not null;uniqueIndex:uq_fanqie_upload;index"`
	ChapterID       string `gorm:"type:uuid;not null;uniqueIndex:uq_fanqie_upload"`
	FanqieChapterID string `gorm:"type:text;not null;default:''"`
	Status          string `gorm:"type:text;not null;default:pending;index"`
	ErrorMessage    string `gorm:"type:text;not null;default:''"`
	UploadedAt      *time.Time
	CreatedAt       *time.Time
	UpdatedAt       *time.Time
}

func (FanqieUploadSchema) TableName() string { return "fanqie_uploads" }
