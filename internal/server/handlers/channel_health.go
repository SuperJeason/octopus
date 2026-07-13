package handlers

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/bestruirui/octopus/internal/grouphealth"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/server/middleware"
	"github.com/bestruirui/octopus/internal/server/resp"
	"github.com/bestruirui/octopus/internal/server/router"
	"github.com/bestruirui/octopus/internal/utils/safe"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

var defaultChannelHealthService = grouphealth.NewChannelService(nil, nil)

type channelHealthRunRequest struct {
	Models []string `json:"models"`
}

func init() {
	router.NewGroupRouter("/api/v1/channel/health").
		Use(middleware.Auth()).
		AddRoute(
			router.NewRoute("/:id", http.MethodGet).
				Handle(getChannelHealth),
		).
		AddRoute(
			router.NewRoute("/:id/run", http.MethodPost).
				Handle(runChannelHealth),
		)
}

func ensureChannelHealthEnabled(c *gin.Context) bool {
	enabled, err := op.SettingGetBool(model.SettingKeyGroupHealthEnabled)
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return false
	}
	if !enabled {
		resp.Error(c, http.StatusForbidden, "group health checks are disabled")
		return false
	}
	return true
}

func getChannelHealth(c *gin.Context) {
	if !ensureChannelHealthEnabled(c) {
		return
	}
	channelID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		resp.InvalidParam(c)
		return
	}
	view, err := defaultChannelHealthService.GetChannelHealthViewByID(c.Request.Context(), channelID)
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	resp.Success(c, view)
}

func parseChannelHealthRunRequest(c *gin.Context) ([]string, error) {
	if c.Request.Body == nil || c.Request.ContentLength == 0 {
		return nil, nil
	}
	var req channelHealthRunRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, nil
		}
		return nil, err
	}
	models := make([]string, 0, len(req.Models))
	seen := make(map[string]struct{}, len(req.Models))
	for _, raw := range req.Models {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		models = append(models, name)
	}
	return models, nil
}

func runChannelHealth(c *gin.Context) {
	if !ensureChannelHealthEnabled(c) {
		return
	}
	channelID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		resp.InvalidParam(c)
		return
	}

	// Ensure channel exists before accepting the job.
	if _, err := op.ChannelGet(channelID, c.Request.Context()); err != nil {
		resp.Error(c, http.StatusNotFound, err.Error())
		return
	}

	selectedModels, err := parseChannelHealthRunRequest(c)
	if err != nil {
		resp.InvalidParam(c)
		return
	}

	running, err := defaultChannelHealthService.GetRunningSnapshotByChannelID(c.Request.Context(), channelID)
	if err == nil {
		c.JSON(http.StatusConflict, resp.ResponseStruct{
			Code:    http.StatusConflict,
			Message: grouphealth.ErrChannelHealthAlreadyRunning.Error(),
			Data:    running,
		})
		return
	}
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}

	modelsCopy := append([]string(nil), selectedModels...)
	safe.Go("channel-health-run", func() {
		runCtx := context.Background()
		_ = defaultChannelHealthService.RunChannelHealth(runCtx, channelID, modelsCopy...)
	})

	c.JSON(http.StatusAccepted, resp.ResponseStruct{
		Code:    http.StatusAccepted,
		Message: "accepted",
		Data: gin.H{
			"channel_id": channelID,
			"models":     selectedModels,
		},
	})
}
