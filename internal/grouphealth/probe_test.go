package grouphealth

import (
	"context"
	"strings"
	"testing"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
)

func TestResolveProbeMessage(t *testing.T) {
	if got := ResolveProbeMessage(""); got != "ping" {
		t.Fatalf("empty should fallback to ping, got %q", got)
	}
	if got := ResolveProbeMessage("  hi  "); got != "hi" {
		t.Fatalf("expected trimmed hi, got %q", got)
	}
	long := strings.Repeat("a", 600)
	if got := ResolveProbeMessage(long); len([]rune(got)) != 500 {
		t.Fatalf("expected 500 runes, got %d", len([]rune(got)))
	}
}

func TestBuildProbeInternalRequestUsesCustomMessage(t *testing.T) {
	req := buildProbeInternalRequest(outbound.OutboundTypeOpenAIChat, "gpt-test", "hello probe")
	if req == nil || len(req.Messages) == 0 || req.Messages[0].Content.Content == nil {
		t.Fatal("expected message content")
	}
	if *req.Messages[0].Content.Content != "hello probe" {
		t.Fatalf("expected custom probe message, got %q", *req.Messages[0].Content.Content)
	}
}

func TestBuildProbeRequestForResponses(t *testing.T) {
	channel := &model.Channel{
		Type:     outbound.OutboundTypeOpenAIResponse,
		BaseUrls: []model.BaseUrl{{URL: "https://example.com/v1"}},
	}
	usedKey := &model.ChannelKey{ID: 1, ChannelKey: "sk-test"}

	req, err := buildProbeRequest(context.Background(), channel, usedKey, "gpt-5.4")
	if err != nil {
		t.Fatalf("buildProbeRequest returned error: %v", err)
	}
	if req.URL.Path != "/v1/responses" {
		t.Fatalf("expected /v1/responses, got %s", req.URL.Path)
	}
}

func TestBuildProbeRequestForEmbeddings(t *testing.T) {
	channel := &model.Channel{
		Type:     outbound.OutboundTypeOpenAIEmbedding,
		BaseUrls: []model.BaseUrl{{URL: "https://example.com/v1"}},
	}
	usedKey := &model.ChannelKey{ID: 1, ChannelKey: "sk-test"}

	req, err := buildProbeRequest(context.Background(), channel, usedKey, "text-embedding-3-large")
	if err != nil {
		t.Fatalf("buildProbeRequest returned error: %v", err)
	}
	if req.URL.Path != "/v1/embeddings" {
		t.Fatalf("expected /v1/embeddings, got %s", req.URL.Path)
	}
}

func TestSplitChannelModelNames(t *testing.T) {
	got := SplitChannelModelNames("a,b", "b, c")
	if len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Fatalf("unexpected models: %#v", got)
	}
}
