package gateway

import (
	"strings"
	"testing"

	"github.com/smallnest/imclaw/internal/agent"
)

func TestApplyStreamChunkAggregatesContentWithoutDoneDuplication(t *testing.T) {
	var fullContent strings.Builder
	var streamErr string

	applyStreamChunk(&fullContent, &streamErr, agent.StreamChunk{Type: "content", Content: "foo"})
	applyStreamChunk(&fullContent, &streamErr, agent.StreamChunk{Type: "done", Content: "foo"})

	if got := fullContent.String(); got != "foo" {
		t.Fatalf("expected content to avoid done duplication, got %q", got)
	}
	if streamErr != "" {
		t.Fatalf("expected no stream error, got %q", streamErr)
	}
}

func TestApplyStreamChunkCapturesErrorSeparately(t *testing.T) {
	var fullContent strings.Builder
	var streamErr string

	applyStreamChunk(&fullContent, &streamErr, agent.StreamChunk{Type: "content", Content: "foo"})
	applyStreamChunk(&fullContent, &streamErr, agent.StreamChunk{Type: "error", Content: "exit status 5"})

	if got := fullContent.String(); got != "foo" {
		t.Fatalf("unexpected content aggregation: %q", got)
	}
	if streamErr != "exit status 5" {
		t.Fatalf("unexpected stream error: %q", streamErr)
	}
}
