package model

import "time"

// Channel health reuses GroupHealthStatus / GroupHealthAttemptStatus semantics.

type ChannelHealthSnapshot struct {
	ID           int                    `json:"id" gorm:"primaryKey"`
	ChannelID    int                    `json:"channel_id" gorm:"index:idx_channel_health_channel_started;not null"`
	ChannelName  string                 `json:"channel_name" gorm:"type:varchar(255);not null"`
	Status       GroupHealthStatus      `json:"status" gorm:"type:varchar(16);index:idx_channel_health_status_started;not null"`
	StartedAt    time.Time              `json:"started_at" gorm:"index:idx_channel_health_channel_started;not null"`
	FinishedAt   *time.Time             `json:"finished_at"`
	DurationMS   int64                  `json:"duration_ms" gorm:"not null;default:0"`
	Message      string                 `json:"message"`
	ModelCount   int                    `json:"model_count" gorm:"not null;default:0"`
	SuccessCount int                    `json:"success_count" gorm:"not null;default:0"`
	Attempts     []ChannelHealthAttempt `json:"attempts,omitempty" gorm:"foreignKey:SnapshotID"`
}

type ChannelHealthAttempt struct {
	ID           int                      `json:"id" gorm:"primaryKey"`
	SnapshotID   int                      `json:"snapshot_id" gorm:"index:idx_channel_health_attempt_snapshot;not null"`
	ChannelID    int                      `json:"channel_id" gorm:"not null"`
	ChannelName  string                   `json:"channel_name" gorm:"type:varchar(255);not null"`
	ChannelKeyID int                      `json:"channel_key_id" gorm:"not null;default:0"`
	KeyRemark    string                   `json:"key_remark"`
	ModelName    string                   `json:"model_name" gorm:"type:varchar(255);not null"`
	Status       GroupHealthAttemptStatus `json:"status" gorm:"type:varchar(16);not null"`
	HTTPStatus   int                      `json:"http_status" gorm:"not null;default:0"`
	DurationMS   int64                    `json:"duration_ms" gorm:"not null;default:0"`
	ErrorMessage string                   `json:"error_message"`
}

type ChannelHealthView struct {
	ChannelID   int                    `json:"channel_id"`
	ChannelName string                 `json:"channel_name"`
	Latest      *ChannelHealthSnapshot `json:"latest,omitempty"`
}
