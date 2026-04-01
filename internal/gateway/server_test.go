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

func TestBuildStructuredEventsFromTranscriptChunks(t *testing.T) {
	parser := event.NewParser()

	events := buildStructuredEvents(parser, agent.StreamChunk{Type: "content", Content: "[thinking] hello\nworld\n"}, false)
	if len(events) != 1 {
		t.Fatalf("expected one structured event, got %#v", events)
	}
	if events[0].Type != event.TypeThinking || events[0].Content != "hello" {
		t.Fatalf("unexpected thinking event: %#v", events[0])
	}

	events = buildStructuredEvents(parser, agent.StreamChunk{}, true)
	if len(events) != 1 {
		t.Fatalf("expected one flushed event, got %#v", events)
	}
	if events[0].Type != event.TypeOutput || events[0].Content != "world" {
		t.Fatalf("unexpected flushed output event: %#v", events[0])
	}
}

func TestBuildStructuredEventsIncludesErrors(t *testing.T) {
	parser := event.NewParser()

	events := buildStructuredEvents(parser, agent.StreamChunk{Type: "error", Content: "exit status 5"}, false)
	if len(events) != 1 {
		t.Fatalf("expected one error event, got %#v", events)
	}
	if events[0].Type != event.TypeError || events[0].Content != "exit status 5" {
		t.Fatalf("unexpected error event: %#v", events[0])
	}
}

func TestBuildStructuredEventsParsesToolLifecycle(t *testing.T) {
	parser := event.NewParser()

	// Tool start
	events := buildStructuredEvents(parser, agent.StreamChunk{Type: "content", Content: "[tool] Read (pending)\n"}, false)
	if len(events) != 1 || events[0].Type != event.TypeToolStart {
		t.Fatalf("expected tool_start event, got %#v", events)
	}
	if events[0].Name != "Read" {
		t.Fatalf("expected tool name 'Read', got %q", events[0].Name)
	}

	// Tool input
	events = buildStructuredEvents(parser, agent.StreamChunk{Type: "content", Content: "  input: {\"path\": \"/tmp\"}\n"}, false)
	if len(events) != 0 {
		t.Fatalf("expected no events for tool input, got %#v", events)
	}

	// Tool end
	events = buildStructuredEvents(parser, agent.StreamChunk{Type: "content", Content: "[tool] Read (completed)\n  output: hello\n"}, false)
	// After completion, we collect output, then flush
	events = append(events, buildStructuredEvents(parser, agent.StreamChunk{}, true)...)

	var foundToolEnd bool
	for _, e := range events {
		if e.Type == event.TypeToolEnd {
			foundToolEnd = true
			if e.Name != "Read" {
				t.Errorf("expected tool name 'Read', got %q", e.Name)
			}
		}
	}
	if !foundToolEnd {
		t.Errorf("expected tool_end event, got %#v", events)
	}
}
