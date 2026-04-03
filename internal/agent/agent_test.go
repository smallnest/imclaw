package agent

import (
	"context"
	"strings"
	"testing"
)

func collectStream(t *testing.T, stream <-chan StreamChunk) []StreamChunk {
	t.Helper()

	var chunks []StreamChunk
	for chunk := range stream {
		chunks = append(chunks, chunk)
	}
	return chunks
}

func TestBuildPromptArgsUsesResolvedPolicy(t *testing.T) {
	opts := &PromptOptions{
		PermissionPreset: "full-auto",
		DeniedTools:      "Write",
		Timeout:          12,
	}
	policy, err := resolvePromptPolicy(opts)
	if err != nil {
		t.Fatalf("resolvePromptPolicy() error = %v", err)
	}

	args, timeout, format := buildPromptArgs("codex", "sess-1", "hello", opts, policy, false)
	if timeout != 12 {
		t.Fatalf("timeout = %d, want 12", timeout)
	}
	if format != "text" {
		t.Fatalf("format = %q, want text", format)
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--approve-all") {
		t.Fatalf("missing approve-all flag in %q", joined)
	}
	if strings.Contains(joined, "--allowed-tools Bash,Edit,Glob,Grep,LS,MultiEdit,NotebookEdit,Read,TodoWrite,WebFetch,WebSearch,Write") || strings.Contains(joined, ",Write,") || strings.Contains(joined, ",Write ") || strings.Contains(joined, " Write,") {
		t.Fatalf("denied tool leaked into args: %q", joined)
	}
}

func TestAnnotatePermissionErrorIncludesPolicySummary(t *testing.T) {
	policy, err := resolvePromptPolicy(&PromptOptions{PermissionPreset: "safe-readonly"})
	if err != nil {
		t.Fatalf("resolvePromptPolicy() error = %v", err)
	}

	msg := annotatePermissionError("exit status 5", policy)
	if !strings.Contains(msg, "permission policy denied request") {
		t.Fatalf("unexpected message: %q", msg)
	}
	if !strings.Contains(msg, "preset=safe-readonly") {
		t.Fatalf("missing preset summary: %q", msg)
	}
}

func TestRunCommandStreamReportsErrorAfterContent(t *testing.T) {
	a := &ACPXAgent{command: "/bin/sh"}

	stream, err := a.runCommandStream(context.Background(), 5, nil, "-c", "printf foo; exit 5")
	if err != nil {
		t.Fatalf("runCommandStream returned error: %v", err)
	}

	chunks := collectStream(t, stream)
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d: %#v", len(chunks), chunks)
	}
	if chunks[0].Type != "content" || chunks[0].Content != "foo" {
		t.Fatalf("unexpected first chunk: %#v", chunks[0])
	}
	if chunks[1].Type != "error" {
		t.Fatalf("expected final error chunk, got %#v", chunks[1])
	}
	if len(chunks[1].Events) != 3 {
		t.Fatalf("expected output_delta, output_final, and error events, got %#v", chunks[1].Events)
	}
	if chunks[1].Events[2].Type != TypeError {
		t.Fatalf("expected terminal error event, got %#v", chunks[1].Events)
	}
}

func TestRunCommandStreamPreservesPartialLineWithoutNewline(t *testing.T) {
	a := &ACPXAgent{command: "/bin/sh"}

	stream, err := a.runCommandStream(context.Background(), 5, nil, "-c", "printf partial")
	if err != nil {
		t.Fatalf("runCommandStream returned error: %v", err)
	}

	chunks := collectStream(t, stream)
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d: %#v", len(chunks), chunks)
	}
	if chunks[0].Type != "content" || chunks[0].Content != "partial" {
		t.Fatalf("unexpected content chunk: %#v", chunks[0])
	}
	if chunks[1].Type != "done" {
		t.Fatalf("expected done chunk, got %#v", chunks[1])
	}
	if len(chunks[1].Events) != 3 {
		t.Fatalf("expected output_delta, output_final, and done events, got %#v", chunks[1].Events)
	}
	if chunks[1].Events[1].Type != TypeOutputFinal || chunks[1].Events[1].Content != "partial" {
		t.Fatalf("unexpected output_final event: %#v", chunks[1].Events[1])
	}
	if chunks[1].Events[2].Type != TypeDone {
		t.Fatalf("expected done event, got %#v", chunks[1].Events[2])
	}
}

func TestProtocolParserEmitsToolLifecycleAndTerminalEvents(t *testing.T) {
	parser := NewProtocolParser()

	events := parser.Feed("[thinking] plan\n[tool] Read (pending)\n  path=/tmp\n[tool] Read (completed)\n  ok\nanswer\n")
	flushed := parser.Flush()
	events = append(events, flushed...)

	want := []EventType{
		TypeThinkingStart,
		TypeThinkingDelta,
		TypeThinkingEnd,
		TypeToolStart,
		TypeToolInput,
		TypeToolOutput,
		TypeToolEnd,
		TypeOutputDelta,
		TypeOutputFinal,
	}
	if len(events) != len(want) {
		t.Fatalf("expected %d events, got %d: %#v", len(want), len(events), events)
	}
	for i, typ := range want {
		if events[i].Type != typ {
			t.Fatalf("event %d type = %q, want %q (%#v)", i, events[i].Type, typ, events[i])
		}
		if events[i].Version != EventProtocolVersion {
			t.Fatalf("event %d missing version: %#v", i, events[i])
		}
	}
	if events[6].Name != "Read" || events[6].Input != "path=/tmp" || events[6].Output != "ok" {
		t.Fatalf("unexpected tool_end payload: %#v", events[6])
	}
	if events[8].Content != "answer" {
		t.Fatalf("unexpected output_final content: %#v", events[8])
	}
}
