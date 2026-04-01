package gateway

import (
	"strings"
	"testing"

	"github.com/smallnest/imclaw/internal/agent"
	"github.com/smallnest/imclaw/internal/event"
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

func TestBuildStructuredEventsPrefersNativeAgentEvents(t *testing.T) {
	parser := event.NewParser()
	chunk := agent.StreamChunk{
		Type:    "content",
		Content: "ignored transcript",
		Events:  []agent.Event{{Version: agent.EventProtocolVersion, Type: agent.TypeOutputDelta, Content: "hello"}},
	}

	events := buildStructuredEvents(parser, chunk)
	if len(events) != 1 {
		t.Fatalf("expected one native event, got %#v", events)
	}
	if events[0].Type != agent.TypeOutputDelta || events[0].Content != "hello" {
		t.Fatalf("unexpected native event: %#v", events[0])
	}
}

func TestBuildStructuredEventsFallsBackToTranscriptParser(t *testing.T) {
	parser := event.NewParser()

	events := buildStructuredEvents(parser, agent.StreamChunk{Type: "content", Content: "[thinking] hello\nworld\n"})
	if len(events) != 3 {
		t.Fatalf("expected three fallback events, got %#v", events)
	}
	if events[0].Type != agent.TypeThinkingStart {
		t.Fatalf("expected thinking_start, got %#v", events[0])
	}
	if events[1].Type != agent.TypeThinkingDelta || events[1].Content != "hello" {
		t.Fatalf("unexpected thinking_delta event: %#v", events[1])
	}
	if events[2].Type != agent.TypeThinkingEnd || events[2].Content != "hello" {
		t.Fatalf("unexpected thinking_end event: %#v", events[2])
	}

	flushed := flushStructuredEvents(parser, true)
	if len(flushed) != 2 {
		t.Fatalf("expected output_final and done, got %#v", flushed)
	}
	if flushed[0].Type != agent.TypeOutputFinal || flushed[0].Content != "world" {
		t.Fatalf("unexpected flushed output event: %#v", flushed[0])
	}
	if flushed[1].Type != agent.TypeDone {
		t.Fatalf("expected done event, got %#v", flushed[1])
	}
}

func TestBuildStructuredEventsIncludesFallbackErrors(t *testing.T) {
	parser := event.NewParser()

	events := buildStructuredEvents(parser, agent.StreamChunk{Type: "error", Content: "exit status 5"})
	if len(events) != 1 {
		t.Fatalf("expected one error event, got %#v", events)
	}
	if events[0].Type != agent.TypeError || events[0].Content != "exit status 5" {
		t.Fatalf("unexpected error event: %#v", events[0])
	}
}
