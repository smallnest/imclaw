package gateway

import (
	"context"
	"strings"
	"testing"

	"github.com/smallnest/imclaw/internal/agent"
	"github.com/smallnest/imclaw/internal/event"
	"github.com/smallnest/imclaw/internal/session"
)

type stubAgent struct {
	ensureSessionID string
}

func (s stubAgent) Name() string { return "stub" }
func (s stubAgent) Type() string { return "stub" }
func (s stubAgent) CreateSession(ctx context.Context, sessionName string) (string, error) {
	return s.ensureSessionID, nil
}
func (s stubAgent) EnsureSession(ctx context.Context, sessionName string) (string, error) {
	return s.ensureSessionID, nil
}
func (s stubAgent) Prompt(ctx context.Context, sessionID, prompt string) (string, error) {
	return "", nil
}
func (s stubAgent) PromptWithOptions(ctx context.Context, sessionID, prompt string, opts *agent.PromptOptions) (string, error) {
	return "", nil
}
func (s stubAgent) PromptStream(ctx context.Context, sessionID, prompt string, opts *agent.PromptOptions) (<-chan agent.StreamChunk, error) {
	ch := make(chan agent.StreamChunk)
	close(ch)
	return ch, nil
}
func (s stubAgent) Close() error { return nil }

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

func TestFinalOutputShouldPreferStructuredOutputFinal(t *testing.T) {
	var fullContent strings.Builder
	var streamErr string
	var finalOutput string
	var sawFinalOutput bool
	parser := event.NewParser()

	chunks := []agent.StreamChunk{
		{
			Type:    "content",
			Content: "raw transcript that includes thinking and output",
			Events: []agent.Event{
				{Version: agent.EventProtocolVersion, Type: agent.TypeThinkingEnd, Content: "internal reasoning"},
				{Version: agent.EventProtocolVersion, Type: agent.TypeOutputFinal, Content: "1. 第一项\n2. 第二项"},
				{Version: agent.EventProtocolVersion, Type: agent.TypeDone},
			},
		},
	}

	for _, chunk := range chunks {
		applyStreamChunk(&fullContent, &streamErr, chunk)
		for _, evt := range buildStructuredEvents(parser, chunk) {
			if evt.Type == agent.TypeOutputFinal {
				finalOutput = evt.Content
				sawFinalOutput = true
			}
		}
	}

	finalContent := filterTranscriptMarkers(event.StripANSI(fullContent.String()))
	if sawFinalOutput {
		finalContent = filterTranscriptMarkers(event.StripANSI(finalOutput))
	}

	if streamErr != "" {
		t.Fatalf("unexpected stream error: %q", streamErr)
	}
	if finalContent != "1. 第一项\n2. 第二项" {
		t.Fatalf("expected structured output_final to win, got %q", finalContent)
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

func TestEnsureAgentSessionStoresInternalIDAndHandle(t *testing.T) {
	sessionMgr := session.NewManager()
	srv := NewServer(&Config{}, sessionMgr, agent.NewManager())
	sess := sessionMgr.Create(defaultSessionChannel, "", "sess-ensure", "claude")

	handle, err := srv.ensureAgentSession(sess, stubAgent{ensureSessionID: "acpx-123"}, "req-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if handle != sess.ID {
		t.Fatalf("expected prompt handle %q, got %q", sess.ID, handle)
	}

	updated, ok := sessionMgr.Get(defaultSessionChannel, sess.ID)
	if !ok {
		t.Fatal("expected session to be updated")
	}
	if updated.AgentSession != "acpx-123" {
		t.Fatalf("expected internal session id to be stored, got %#v", updated)
	}
	if updated.AgentSessionHandle != sess.ID {
		t.Fatalf("expected session handle to remain stable, got %#v", updated)
	}
}
