package op

import (
	"context"
	"errors"
	"time"

	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"gorm.io/gorm"
)

type ChannelHealthRepository struct{}

func NewChannelHealthRepository() *ChannelHealthRepository {
	return &ChannelHealthRepository{}
}

func (r *ChannelHealthRepository) CreateRunningSnapshot(ctx context.Context, channel model.Channel) (*model.ChannelHealthSnapshot, error) {
	snapshot := &model.ChannelHealthSnapshot{
		ChannelID:   channel.ID,
		ChannelName: channel.Name,
		Status:      model.GroupHealthStatusRunning,
		StartedAt:   time.Now(),
	}
	if err := db.GetDB().WithContext(ctx).Create(snapshot).Error; err != nil {
		return nil, err
	}
	return snapshot, nil
}

func (r *ChannelHealthRepository) AppendAttempt(ctx context.Context, snapshotID int, attempt model.ChannelHealthAttempt) error {
	attempt.SnapshotID = snapshotID
	return db.GetDB().WithContext(ctx).Create(&attempt).Error
}

func (r *ChannelHealthRepository) FinishSnapshot(ctx context.Context, snapshotID int, status model.GroupHealthStatus, durationMS int64, message string, modelCount, successCount int, finishedAt time.Time) error {
	return db.GetDB().WithContext(ctx).
		Model(&model.ChannelHealthSnapshot{}).
		Where("id = ?", snapshotID).
		Updates(map[string]any{
			"status":        status,
			"finished_at":   finishedAt,
			"duration_ms":   durationMS,
			"message":       message,
			"model_count":   modelCount,
			"success_count": successCount,
		}).Error
}

func (r *ChannelHealthRepository) GetLatestSnapshotByChannelID(ctx context.Context, channelID int) (*model.ChannelHealthSnapshot, error) {
	var snapshot model.ChannelHealthSnapshot
	err := db.GetDB().WithContext(ctx).
		Preload("Attempts", func(tx *gorm.DB) *gorm.DB {
			return tx.Order("id ASC")
		}).
		Where("channel_id = ?", channelID).
		Order("id DESC").
		First(&snapshot).Error
	if err != nil {
		return nil, err
	}
	return &snapshot, nil
}

func (r *ChannelHealthRepository) GetRunningSnapshotByChannelID(ctx context.Context, channelID int) (*model.ChannelHealthSnapshot, error) {
	var snapshot model.ChannelHealthSnapshot
	err := db.GetDB().WithContext(ctx).
		Where("channel_id = ? AND status = ?", channelID, model.GroupHealthStatusRunning).
		Order("id DESC").
		First(&snapshot).Error
	if err != nil {
		return nil, err
	}
	return &snapshot, nil
}

func (r *ChannelHealthRepository) GetChannelHealthViewByID(ctx context.Context, channelID int) (*model.ChannelHealthView, error) {
	channel, err := ChannelGet(channelID, ctx)
	if err != nil {
		return nil, err
	}
	view := &model.ChannelHealthView{
		ChannelID:   channel.ID,
		ChannelName: channel.Name,
	}
	snapshot, err := r.GetLatestSnapshotByChannelID(ctx, channelID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return view, nil
		}
		return nil, err
	}
	view.Latest = snapshot
	return view, nil
}
