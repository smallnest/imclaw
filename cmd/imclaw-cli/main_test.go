package main

import (
	"bytes"
	"os"
	"testing"

	"github.com/smallnest/imclaw/internal/event"
	flag "github.com/spf13/pflag"
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
	var stderr bytes.Buffer

	writeStructuredEvent(&stdout, &stderr, event.Event{
		Type:    event.TypeThinking,
		Content: "hello",
	})

	if got := stdout.String(); got != "{\"type\":\"thinking\",\"content\":\"hello\"}\n" {
		t.Fatalf("unexpected parsed message output: %q", got)
	}
	if got := stderr.String(); got != "" {
		t.Fatalf("expected no stderr output, got %q", got)
	}
}

func TestShortFlagsAreRegistered(t *testing.T) {
	tests := map[string]string{
		"s": "server",
		"t": "token",
		"S": "session",
		"a": "agent",
		"C": "cwd",
		"v": "version",
	}

	for shorthand, expected := range tests {
		f := flag.CommandLine.ShorthandLookup(shorthand)
		if f == nil {
			t.Fatalf("missing shorthand -%s", shorthand)
		}
		if f.Name != expected {
			t.Fatalf("shorthand -%s mapped to %q, want %q", shorthand, f.Name, expected)
		}
	}
}

func TestHandleParsedResultFallsBackToFinalTranscriptWithoutStructuredEvents(t *testing.T) {
	*parseTranscript = true
	defer func() { *parseTranscript = false }()

	output := captureStdout(t, func() {
		ok := handleParsedResult(map[string]interface{}{
			"content": "[thinking] hello\nplain output",
		}, false)
		if !ok {
			t.Fatal("expected output to be produced")
		}
	})

	if !bytes.Contains([]byte(output), []byte(`"type": "thinking"`)) {
		t.Fatalf("missing thinking event in fallback output: %q", output)
	}
	if !bytes.Contains([]byte(output), []byte(`"content": "plain output"`)) {
		t.Fatalf("missing final output content in fallback output: %q", output)
	}
}

func TestHandleParsedResultSkipsTranscriptWhenStructuredEventsAlreadyStreamed(t *testing.T) {
	*parseTranscript = true
	defer func() { *parseTranscript = false }()

	output := captureStdout(t, func() {
		ok := handleParsedResult(map[string]interface{}{
			"content": "[thinking] hello\nplain output",
		}, true)
		if !ok {
			t.Fatal("expected function to report handled result")
		}
	})

	if output != "" {
		t.Fatalf("expected no duplicate transcript output, got %q", output)
	}
}

func TestNotificationMatchesRequest(t *testing.T) {
	tests := []struct {
		name   string
		params map[string]interface{}
		reqID  string
		want   bool
	}{
		{name: "matching id", params: map[string]interface{}{"id": "req-1"}, reqID: "req-1", want: true},
		{name: "different id", params: map[string]interface{}{"id": "req-2"}, reqID: "req-1", want: false},
		{name: "missing id tolerated", params: map[string]interface{}{"type": "content"}, reqID: "req-1", want: true},
	}

	for _, tt := range tests {
		if got := notificationMatchesRequest(tt.params, tt.reqID); got != tt.want {
			t.Fatalf("%s: got %v want %v", tt.name, got, tt.want)
		}
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe failed: %v", err)
	}
	os.Stdout = w
	defer func() {
		os.Stdout = oldStdout
	}()

	done := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(r)
		done <- buf.String()
	}()

	fn()

	_ = w.Close()
	return <-done
}
