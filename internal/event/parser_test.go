package event

import (
	"testing"
)

func TestParserEmitsToolStartAndToolEnd(t *testing.T) {
	p := NewParser()

	raw := `[tool] Read (pending)
  input: {"path": "/tmp/test"}

[tool] Read (completed)
  output: "hello world"

[thinking] Let me analyze this...`

	events := p.Feed(raw)
	events = append(events, p.Flush()...)

	// Should emit: tool_start, tool_end, thinking
	// Note: blank lines between markers create output events, so we filter
	var toolStart, toolEnd, thinking bool
	for _, e := range events {
		switch e.Type {
		case TypeToolStart:
			toolStart = true
			if e.Name != "Read" {
				t.Errorf("expected tool name 'Read', got %q", e.Name)
			}
		case TypeToolEnd:
			toolEnd = true
			if e.Name != "Read" {
				t.Errorf("expected tool name 'Read', got %q", e.Name)
			}
		case TypeThinking:
			thinking = true
		}
	}

	if !toolStart {
		t.Error("missing tool_start event")
	}
	if !toolEnd {
		t.Error("missing tool_end event")
	}
	if !thinking {
		t.Error("missing thinking event")
	}
}

func TestParserHandlesToolError(t *testing.T) {
	p := NewParser()

	raw := `[tool] Write (pending)
  input: {"path": "/root/file"}

[tool] Write (error)
  error: permission denied`

	events := p.Feed(raw)
	events = append(events, p.Flush()...)

	// Should emit: tool_start, tool_error
	var hasToolStart, hasToolError bool
	for _, e := range events {
		switch e.Type {
		case TypeToolStart:
			hasToolStart = true
			if e.Name != "Write" {
				t.Errorf("expected tool name 'Write', got %q", e.Name)
			}
		case TypeToolError:
			hasToolError = true
			if e.Name != "Write" {
				t.Errorf("expected tool name 'Write', got %q", e.Name)
			}
		}
	}

	if !hasToolStart {
		t.Error("missing tool_start event")
	}
	if !hasToolError {
		t.Error("missing tool_error event")
	}
}

func TestParserHandlesOutputBlocks(t *testing.T) {
	p := NewParser()

	raw := `[thinking] Analyzing...

The answer is 42.

[tool] Bash (pending)
  input: {"cmd": "echo test"}

[tool] Bash (completed)
  output: test

Done!`

	events := p.Feed(raw)
	events = append(events, p.Flush()...)

	// Should have: thinking, output, tool_start, tool_end, output
	var hasThinking, hasOutput, hasToolStart, hasToolEnd bool
	for _, e := range events {
		switch e.Type {
		case TypeThinking:
			hasThinking = true
		case TypeOutput:
			hasOutput = true
		case TypeToolStart:
			hasToolStart = true
		case TypeToolEnd:
			hasToolEnd = true
		}
	}

	if !hasThinking {
		t.Error("missing thinking event")
	}
	if !hasOutput {
		t.Error("missing output event")
	}
	if !hasToolStart {
		t.Error("missing tool_start event")
	}
	if !hasToolEnd {
		t.Error("missing tool_end event")
	}
}

func TestParserIncrementalFeeding(t *testing.T) {
	p := NewParser()

	// Feed in small chunks
	events := p.Feed("[tool] Read (pending)\n")
	if len(events) != 1 || events[0].Type != TypeToolStart {
		t.Fatalf("expected tool_start after first chunk, got %#v", events)
	}
	if events[0].Name != "Read" {
		t.Fatalf("expected tool name 'Read', got %q", events[0].Name)
	}

	// Feed tool input - indented lines are collected, no events emitted
	events = p.Feed("  input: {\"path\": \"/tmp\"}\n")
	if len(events) != 0 {
		t.Fatalf("expected no events for tool input, got %#v", events)
	}

	// Feed completion marker with output
	events = p.Feed("[tool] Read (completed)\n  output: test\n")
	t.Logf("After completion chunk: %d events: %#v", len(events), events)

	// Feed a newline to trigger tool_end
	events = append(events, p.Feed("\n")...)
	events = append(events, p.Flush()...)
	t.Logf("After newline and flush: %d events: %#v", len(events), events)

	// Find tool_end event
	var foundToolEnd bool
	for _, e := range events {
		if e.Type == TypeToolEnd {
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

func TestParserIgnoresStatusMarkers(t *testing.T) {
	p := NewParser()

	raw := `[acpx] session abc123
[client] initialize (running)
[done] end_turn`

	events := p.Feed(raw)
	events = append(events, p.Flush()...)

	// Should not emit any events for status markers
	if len(events) != 0 {
		t.Fatalf("expected no events for status markers, got %d: %#v", len(events), events)
	}
}

func TestParserStripsANSIEscapes(t *testing.T) {
	p := NewParser()

	raw := "\x1b[1m[thinking]\x1b[0m hello\nworld"

	events := p.Feed(raw)
	events = append(events, p.Flush()...)

	if len(events) == 0 {
		t.Fatal("expected at least one event")
	}

	if events[0].Type != TypeThinking {
		t.Fatalf("expected thinking event, got %s", events[0].Type)
	}

	// Content should not contain ANSI escapes
	if events[0].Content != "hello" {
		t.Fatalf("unexpected content: %q", events[0].Content)
	}
}

func TestEventIsTool(t *testing.T) {
	tests := []struct {
		event    Event
		expected bool
	}{
		{Event{Type: TypeToolStart}, true},
		{Event{Type: TypeToolEnd}, true},
		{Event{Type: TypeToolInput}, true},
		{Event{Type: TypeToolError}, true},
		{Event{Type: TypeThinking}, false},
		{Event{Type: TypeOutput}, false},
		{Event{Type: TypeError}, false},
	}

	for _, tt := range tests {
		if got := tt.event.IsTool(); got != tt.expected {
			t.Errorf("Event{Type: %s}.IsTool() = %v, want %v", tt.event.Type, got, tt.expected)
		}
	}
}

func TestEventIsTerminal(t *testing.T) {
	tests := []struct {
		event    Event
		expected bool
	}{
		{Event{Type: TypeError}, true},
		{Event{Type: TypeToolError}, true},
		{Event{Type: TypeToolEnd}, false},
		{Event{Type: TypeOutput}, false},
	}

	for _, tt := range tests {
		if got := tt.event.IsTerminal(); got != tt.expected {
			t.Errorf("Event{Type: %s}.IsTerminal() = %v, want %v", tt.event.Type, got, tt.expected)
		}
	}
}
