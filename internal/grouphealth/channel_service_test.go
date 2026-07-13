package grouphealth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
)

func TestRunChannelHealthProbesAllModels(t *testing.T) {
	ctx := setupGroupHealthTestDB(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl","object":"chat.completion","choices":[]}`))
	}))
	defer server.Close()

	channel := &model.Channel{
		Name:        "channel-health-all",
		Type:        outbound.OutboundTypeOpenAIChat,
		Enabled:     true,
		BaseUrls:    []model.BaseUrl{{URL: server.URL + "/v1"}},
		Model:       "model-a,model-b",
		CustomModel: "model-c",
		Keys:        []model.ChannelKey{{Enabled: true, ChannelKey: "sk-test", Remark: "main"}},
	}
	if err := op.ChannelCreate(channel, ctx); err != nil {
		t.Fatalf("ChannelCreate failed: %v", err)
	}

	// Enable group health setting so path is realistic (service itself does not check it).
	if err := op.SettingSetString(model.SettingKeyGroupHealthEnabled, "true"); err != nil {
		t.Fatalf("enable group health: %v", err)
	}

	service := NewChannelService(op.NewChannelHealthRepository(), &Prober{CandidateTimeout: 5 * time.Second})
	if err := service.RunChannelHealth(ctx, channel.ID); err != nil {
		t.Fatalf("RunChannelHealth failed: %v", err)
	}

	view, err := service.GetChannelHealthViewByID(ctx, channel.ID)
	if err != nil {
		t.Fatalf("GetChannelHealthViewByID failed: %v", err)
	}
	if view.Latest == nil {
		t.Fatal("expected latest snapshot")
	}
	if view.Latest.Status != model.GroupHealthStatusSuccess {
		t.Fatalf("expected success, got %s message=%s", view.Latest.Status, view.Latest.Message)
	}
	if view.Latest.ModelCount != 3 || view.Latest.SuccessCount != 3 {
		t.Fatalf("expected 3/3, got model_count=%d success_count=%d", view.Latest.ModelCount, view.Latest.SuccessCount)
	}
	if len(view.Latest.Attempts) != 3 {
		t.Fatalf("expected 3 attempts, got %d", len(view.Latest.Attempts))
	}
}

func TestFilterChannelModelNames(t *testing.T) {
	available := []string{"a", "b", "c"}
	if got := FilterChannelModelNames(available, nil); len(got) != 3 {
		t.Fatalf("nil selection should keep all, got %#v", got)
	}
	got := FilterChannelModelNames(available, []string{"c", "x", "a", "a"})
	if len(got) != 2 || got[0] != "c" || got[1] != "a" {
		t.Fatalf("unexpected filter result: %#v", got)
	}
	if got := FilterChannelModelNames(available, []string{"missing"}); len(got) != 0 {
		t.Fatalf("expected empty for no match, got %#v", got)
	}
}

func TestRunChannelHealthSelectedModelsOnly(t *testing.T) {
	ctx := setupGroupHealthTestDB(t)

	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(body)
		bodyStr := string(body)
		for _, model := range []string{"model-a", "model-b", "model-c"} {
			if strings.Contains(bodyStr, model) {
				seen = append(seen, model)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl","object":"chat.completion","choices":[]}`))
	}))
	defer server.Close()

	channel := &model.Channel{
		Name:        "channel-health-selected",
		Type:        outbound.OutboundTypeOpenAIChat,
		Enabled:     true,
		BaseUrls:    []model.BaseUrl{{URL: server.URL + "/v1"}},
		Model:       "model-a,model-b",
		CustomModel: "model-c",
		Keys:        []model.ChannelKey{{Enabled: true, ChannelKey: "sk-test"}},
	}
	if err := op.ChannelCreate(channel, ctx); err != nil {
		t.Fatalf("ChannelCreate failed: %v", err)
	}

	service := NewChannelService(op.NewChannelHealthRepository(), &Prober{CandidateTimeout: 5 * time.Second})
	if err := service.RunChannelHealth(ctx, channel.ID, "model-b", "missing", "model-c"); err != nil {
		t.Fatalf("RunChannelHealth failed: %v", err)
	}

	view, err := service.GetChannelHealthViewByID(ctx, channel.ID)
	if err != nil {
		t.Fatalf("GetChannelHealthViewByID failed: %v", err)
	}
	if view.Latest == nil || view.Latest.ModelCount != 2 || len(view.Latest.Attempts) != 2 {
		t.Fatalf("expected only selected models, got %#v", view.Latest)
	}
	names := []string{view.Latest.Attempts[0].ModelName, view.Latest.Attempts[1].ModelName}
	if names[0] != "model-b" || names[1] != "model-c" {
		t.Fatalf("unexpected attempt order/names: %#v", names)
	}
}

func TestRunChannelHealthPartialFailure(t *testing.T) {
	ctx := setupGroupHealthTestDB(t)

	call := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call++
		if call == 1 {
			http.Error(w, `{"error":"fail"}`, http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl","object":"chat.completion","choices":[]}`))
	}))
	defer server.Close()

	channel := &model.Channel{
		Name:     "channel-health-partial",
		Type:     outbound.OutboundTypeOpenAIChat,
		Enabled:  true,
		BaseUrls: []model.BaseUrl{{URL: server.URL + "/v1"}},
		Model:    "bad-model,good-model",
		Keys:     []model.ChannelKey{{Enabled: true, ChannelKey: "sk-test"}},
	}
	if err := op.ChannelCreate(channel, ctx); err != nil {
		t.Fatalf("ChannelCreate failed: %v", err)
	}

	service := NewChannelService(op.NewChannelHealthRepository(), &Prober{CandidateTimeout: 5 * time.Second})
	if err := service.RunChannelHealth(ctx, channel.ID); err != nil {
		t.Fatalf("RunChannelHealth failed: %v", err)
	}

	view, err := service.GetChannelHealthViewByID(ctx, channel.ID)
	if err != nil {
		t.Fatalf("GetChannelHealthViewByID failed: %v", err)
	}
	if view.Latest == nil || view.Latest.Status != model.GroupHealthStatusPartial {
		t.Fatalf("expected partial, got %#v", view.Latest)
	}
	if view.Latest.SuccessCount != 1 || view.Latest.ModelCount != 2 {
		t.Fatalf("expected 1/2, got %d/%d", view.Latest.SuccessCount, view.Latest.ModelCount)
	}
}
