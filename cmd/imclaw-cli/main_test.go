package main

import (
	"bytes"
	"testing"

	"github.com/smallnest/imclaw/internal/transcript"
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

func TestShouldSuggestApproveAll(t *testing.T) {
	*approveAll = false
	*approveReads = true
	*denyAll = false

	if !shouldSuggestApproveAll("Agent error: exit status 5") {
		t.Fatal("expected approve-all hint for exit status 5")
	}
	if !shouldSuggestApproveAll("User refused permission to run tool") {
		t.Fatal("expected approve-all hint for permission refusal")
	}
	if shouldSuggestApproveAll("plain network timeout") {
		t.Fatal("did not expect approve-all hint for unrelated error")
	}
}

func TestPrintCLIErrorIncludesHint(t *testing.T) {
	*approveAll = false
	*approveReads = true
	*denyAll = false

	var stderr bytes.Buffer
	printCLIError(&stderr, "Agent error: exit status 5")

	got := stderr.String()
	if !bytes.Contains([]byte(got), []byte("Error: Agent error: exit status 5\n")) {
		t.Fatalf("missing main error line: %q", got)
	}
	if !bytes.Contains([]byte(got), []byte("Retry with --approve-all")) {
		t.Fatalf("missing approve-all hint: %q", got)
	}
}

func TestWriteParsedMessageOutputsJSONLine(t *testing.T) {
	var stdout bytes.Buffer

	writeParsedMessage(&stdout, transcript.Message{
		Type:    transcript.MessageThinking,
		Content: "hello",
	})

	if got := stdout.String(); got != "{\"type\":\"thinking\",\"content\":\"hello\"}\n" {
		t.Fatalf("unexpected parsed message output: %q", got)
	}
}
