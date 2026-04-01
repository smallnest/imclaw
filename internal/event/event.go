package event

// Type defines the type of a stream event.
// Events are emitted in a structured format for downstream consumers.
type Type string

const (
	// Thinking events
	TypeThinking Type = "thinking" // Thinking content block

	// Tool events - granular tool lifecycle
	TypeToolStart Type = "tool_start" // Tool execution started: "ToolName (pending)"
	TypeToolInput Type = "tool_input" // Tool input parameters
	TypeToolEnd   Type = "tool_end"   // Tool execution completed: "ToolName (completed)"
	TypeToolError Type = "tool_error" // Tool execution failed

	// Output events
	TypeOutput Type = "output" // Final assistant output

	// Error event
	TypeError Type = "error" // Stream or agent error
)

// Event represents a structured event in the stream.
type Event struct {
	Type    Type   `json:"type"`
	Content string `json:"content,omitempty"`
	Name    string `json:"name,omitempty"`    // Tool name for tool events
	Input   string `json:"input,omitempty"`   // Tool input for tool_input/tool_end
	Output  string `json:"output,omitempty"`  // Tool output for tool_end
}

// IsTool returns true if the event is tool-related.
func (e Event) IsTool() bool {
	return e.Type == TypeToolStart || e.Type == TypeToolInput || e.Type == TypeToolEnd || e.Type == TypeToolError
}

// IsTerminal returns true if the event represents a terminal state.
func (e Event) IsTerminal() bool {
	return e.Type == TypeError || e.Type == TypeToolError
}
