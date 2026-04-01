package main

import (
	"bytes"
	"testing"
)

func TestWriteStreamChunkWritesContentWithoutExtraNewline(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	writeStreamChunk(&stdout, &stderr, "content", "hello")

	if got := stdout.String(); got != "hello" {
		t.Fatalf("expected raw content output, got %q", got)
	}
	if got := stderr.String(); got != "" {
		t.Fatalf("expected no stderr output, got %q", got)
	}
}

func TestWriteStreamChunkFormatsErrorsOnStderr(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	writeStreamChunk(&stdout, &stderr, "error", "boom")

	if got := stdout.String(); got != "" {
		t.Fatalf("expected no stdout output, got %q", got)
	}
	if got := stderr.String(); got != "[error] boom\n" {
		t.Fatalf("unexpected stderr output: %q", got)
	}
}

func TestLooksLikeTranscript(t *testing.T) {
	if !looksLikeTranscript("[thinking] hello") {
		t.Fatal("expected transcript marker to be detected")
	}
	if looksLikeTranscript("plain answer only") {
		t.Fatal("did not expect plain output to be treated as transcript")
	}
}
