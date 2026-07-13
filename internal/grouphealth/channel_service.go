package grouphealth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"gorm.io/gorm"
)

var ErrChannelHealthAlreadyRunning = errors.New("channel health check already running")

type ChannelHealthRepository interface {
	CreateRunningSnapshot(ctx context.Context, channel model.Channel) (*model.ChannelHealthSnapshot, error)
	AppendAttempt(ctx context.Context, snapshotID int, attempt model.ChannelHealthAttempt) error
	FinishSnapshot(ctx context.Context, snapshotID int, status model.GroupHealthStatus, durationMS int64, message string, modelCount, successCount int, finishedAt time.Time) error
	GetLatestSnapshotByChannelID(ctx context.Context, channelID int) (*model.ChannelHealthSnapshot, error)
	GetRunningSnapshotByChannelID(ctx context.Context, channelID int) (*model.ChannelHealthSnapshot, error)
	GetChannelHealthViewByID(ctx context.Context, channelID int) (*model.ChannelHealthView, error)
}

type ChannelService struct {
	repo   ChannelHealthRepository
	prober *Prober
}

var channelRunLocks sync.Map

func NewChannelService(repo ChannelHealthRepository, prober *Prober) *ChannelService {
	if repo == nil {
		repo = op.NewChannelHealthRepository()
	}
	if prober == nil {
		prober = NewProber()
	}
	return &ChannelService{
		repo:   repo,
		prober: prober,
	}
}

func lockChannel(channelID int) func() {
	value, _ := channelRunLocks.LoadOrStore(channelID, &sync.Mutex{})
	lock := value.(*sync.Mutex)
	lock.Lock()
	return func() {
		lock.Unlock()
	}
}

// SplitChannelModelNames merges model + custom_model (comma-separated) into unique names.
func SplitChannelModelNames(values ...string) []string {
	seen := make(map[string]struct{})
	result := make([]string, 0)
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			name := strings.TrimSpace(part)
			if name == "" {
				continue
			}
			if _, ok := seen[name]; ok {
				continue
			}
			seen[name] = struct{}{}
			result = append(result, name)
		}
	}
	return result
}

// FilterChannelModelNames keeps only selected models that exist on the channel.
// Empty selected means "all channel models". Selected-only unknowns are dropped.
// If selection is non-empty but none match, returns empty slice (caller decides).
func FilterChannelModelNames(available []string, selected []string) []string {
	if len(selected) == 0 {
		return append([]string(nil), available...)
	}
	avail := make(map[string]struct{}, len(available))
	for _, name := range available {
		avail[name] = struct{}{}
	}
	seen := make(map[string]struct{})
	result := make([]string, 0, len(selected))
	for _, raw := range selected {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		if _, ok := avail[name]; !ok {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		result = append(result, name)
	}
	return result
}

// RunChannelHealth probes models on a channel.
// selectedModels empty => probe all models configured on the channel.
// selectedModels non-empty => only probe those that exist on the channel.
func (s *ChannelService) RunChannelHealth(ctx context.Context, channelID int, selectedModels ...string) error {
	unlock := lockChannel(channelID)
	defer unlock()

	if _, err := s.repo.GetRunningSnapshotByChannelID(ctx, channelID); err == nil {
		return ErrChannelHealthAlreadyRunning
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}

	channel, err := op.ChannelGet(channelID, ctx)
	if err != nil {
		return err
	}

	snapshot, err := s.repo.CreateRunningSnapshot(ctx, *channel)
	if err != nil {
		return err
	}

	allModels := SplitChannelModelNames(channel.Model, channel.CustomModel)
	modelNames := FilterChannelModelNames(allModels, selectedModels)
	usedKey := channel.GetChannelKey()
	successCount := 0
	attemptedCount := 0
	message := "all models failed"

	if usedKey.ID == 0 || strings.TrimSpace(usedKey.ChannelKey) == "" {
		if len(modelNames) == 0 {
			finishedAt := time.Now()
			return s.repo.FinishSnapshot(ctx, snapshot.ID, model.GroupHealthStatusFailed, finishedAt.Sub(snapshot.StartedAt).Milliseconds(), "no available key and no models", 0, 0, finishedAt)
		}
		for _, modelName := range modelNames {
			attemptedCount++
			if err := s.repo.AppendAttempt(ctx, snapshot.ID, model.ChannelHealthAttempt{
				ChannelID:    channel.ID,
				ChannelName:  channel.Name,
				ModelName:    modelName,
				Status:       model.GroupHealthAttemptStatusFailed,
				ErrorMessage: "no available key",
			}); err != nil {
				return err
			}
		}
		finishedAt := time.Now()
		return s.repo.FinishSnapshot(ctx, snapshot.ID, model.GroupHealthStatusFailed, finishedAt.Sub(snapshot.StartedAt).Milliseconds(), "no available key", len(modelNames), 0, finishedAt)
	}

	if len(allModels) == 0 {
		finishedAt := time.Now()
		return s.repo.FinishSnapshot(ctx, snapshot.ID, model.GroupHealthStatusFailed, finishedAt.Sub(snapshot.StartedAt).Milliseconds(), "channel has no models", 0, 0, finishedAt)
	}
	if len(modelNames) == 0 {
		finishedAt := time.Now()
		return s.repo.FinishSnapshot(ctx, snapshot.ID, model.GroupHealthStatusFailed, finishedAt.Sub(snapshot.StartedAt).Milliseconds(), "no matching models selected", 0, 0, finishedAt)
	}

	for _, modelName := range modelNames {
		result := s.prober.RunCandidate(ctx, *channel, usedKey, modelName)
		attemptedCount++
		attempt := model.ChannelHealthAttempt{
			ChannelID:    channel.ID,
			ChannelName:  channel.Name,
			ChannelKeyID: usedKey.ID,
			KeyRemark:    usedKey.Remark,
			ModelName:    modelName,
			HTTPStatus:   result.HTTPStatus,
			DurationMS:   result.DurationMS,
			ErrorMessage: result.ErrorMessage,
		}
		if result.Success {
			attempt.Status = model.GroupHealthAttemptStatusSuccess
			successCount++
		} else {
			attempt.Status = model.GroupHealthAttemptStatusFailed
		}
		if err := s.repo.AppendAttempt(ctx, snapshot.ID, attempt); err != nil {
			return err
		}
	}

	finalStatus := model.GroupHealthStatusFailed
	switch {
	case successCount == 0:
		message = "all models failed"
	case successCount == attemptedCount:
		finalStatus = model.GroupHealthStatusSuccess
		message = fmt.Sprintf("all %d models succeeded", successCount)
	default:
		finalStatus = model.GroupHealthStatusPartial
		message = fmt.Sprintf("%d/%d models succeeded", successCount, attemptedCount)
	}

	finishedAt := time.Now()
	durationMS := finishedAt.Sub(snapshot.StartedAt).Milliseconds()
	return s.repo.FinishSnapshot(ctx, snapshot.ID, finalStatus, durationMS, message, len(modelNames), successCount, finishedAt)
}

func (s *ChannelService) GetChannelHealthViewByID(ctx context.Context, channelID int) (*model.ChannelHealthView, error) {
	return s.repo.GetChannelHealthViewByID(ctx, channelID)
}

func (s *ChannelService) GetRunningSnapshotByChannelID(ctx context.Context, channelID int) (*model.ChannelHealthSnapshot, error) {
	return s.repo.GetRunningSnapshotByChannelID(ctx, channelID)
}
