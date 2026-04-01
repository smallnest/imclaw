package agent

import (
	"context"
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

func TestRunCommandStreamReportsErrorAfterContent(t *testing.T) {
	a := &ACPXAgent{command: "/bin/sh"}

	stream, err := a.runCommandStream(context.Background(), 5, "-c", "printf foo; exit 5")
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

	stream, err := a.runCommandStream(context.Background(), 5, "-c", "printf partial")
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
