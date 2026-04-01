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
}
