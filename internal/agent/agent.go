package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os/exec"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/creack/pty"
	"github.com/smallnest/imclaw/internal/metrics"
	"github.com/smallnest/imclaw/internal/permission"
)

// StreamChunk represents a chunk of streaming output.
type StreamChunk struct {
	Type    string  `json:"type"`             // "content", "error", "done"
	Content string  `json:"content"`          // The content of the chunk
	Events  []Event `json:"events,omitempty"` // Native agent events for this chunk
}

// EventProtocolVersion identifies the agent event schema version.
const EventProtocolVersion = "v1"

// EventType identifies the type of an agent-native stream event.
type EventType string

const (
	TypeThinkingStart EventType = "thinking_start"
	TypeThinkingDelta EventType = "thinking_delta"
	TypeThinkingEnd   EventType = "thinking_end"
	TypeToolStart     EventType = "tool_start"
	TypeToolInput     EventType = "tool_input"
	TypeToolOutput    EventType = "tool_output"
	TypeToolEnd       EventType = "tool_end"
	TypeOutputDelta   EventType = "output_delta"
	TypeOutputFinal   EventType = "output_final"
	TypeError         EventType = "error"
	TypeDone          EventType = "done"
)

// Event is the source-of-truth agent event forwarded to gateway clients.
type Event struct {
	Version string    `json:"version"`
	Type    EventType `json:"type"`
	Content string    `json:"content,omitempty"`
	Name    string    `json:"name,omitempty"`
	Input   string    `json:"input,omitempty"`
	Output  string    `json:"output,omitempty"`
}

// Agent represents an AI agent
type Agent interface {
	// Name returns the agent name
	Name() string

	// Type returns the agent type (claude, codex, etc.)
	Type() string

	// CreateSession creates a new agent session
	CreateSession(ctx context.Context, sessionName string) (string, error)

	// EnsureSession ensures a session exists
	EnsureSession(ctx context.Context, sessionName string) (string, error)

	// Prompt sends a prompt to the agent
	Prompt(ctx context.Context, sessionID, prompt string) (string, error)

	// PromptWithOptions sends a prompt with options
	PromptWithOptions(ctx context.Context, sessionID, prompt string, opts *PromptOptions) (string, error)

	// PromptStream sends a prompt and streams the response
	PromptStream(ctx context.Context, sessionID, prompt string, opts *PromptOptions) (<-chan StreamChunk, error)

	// Close closes the agent
	Close() error
}

// PromptOptions contains all options for a prompt
type PromptOptions struct {
	// Permissions
	Permissions string

	// Output format
	Format string

	// Working directory
	Cwd string

	// Auth policy
	AuthPolicy string

	// Non-interactive permissions
	NonInteractivePerms string

	// Suppress reads
	SuppressReads bool

	// Model
	Model string

	// Permission preset
	PermissionPreset string

	// Allowed tools
	AllowedTools string

	// Denied tools
	DeniedTools string

	// Max turns
	MaxTurns int

	// Prompt retries
	PromptRetries int

	// Timeout
	Timeout int

	// TTL
	TTL int
}

// Manager manages agents
type Manager struct {
	mu     sync.RWMutex
	agents map[string]Agent // agentType -> Agent
}

// NewManager creates a new agent manager
func NewManager() *Manager {
	return &Manager{
		agents: make(map[string]Agent),
	}
}

// GetOrCreate gets an agent by type, creates if not exists
func (m *Manager) GetOrCreate(agentType string) Agent {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Default to claude if not specified
	if agentType == "" {
		agentType = "claude"
	}

	if ag, ok := m.agents[agentType]; ok {
		return ag
	}

	// Create new agent
	ag := NewACPXAgent(AgentConfig{
		Name:    agentType,
		Type:    agentType,
		Command: "acpx",
	})
	m.agents[agentType] = ag
	log.Printf("[agent] Created agent: %s", agentType)

	return ag
}

// Get gets an agent by type
func (m *Manager) Get(agentType string) (Agent, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if agentType == "" {
		agentType = "claude"
	}

	ag, ok := m.agents[agentType]
	return ag, ok
}

// Supported agent types (coding agents from acpx)
var supportedAgents = []string{
	"claude",
	"codex",
	"cursor",
	"copilot",
	"gemini",
	"qwen",
	"kimi",
	"kiro",
	"droid",
	"iflow",
	"kilocode",
	"opencode",
	"qoder",
	"trae",
	"pi",
	"openclaw",
}

// SupportedAgents returns all supported agent types
func SupportedAgents() []string {
	return supportedAgents
}

// List lists all agent types (created agents)
func (m *Manager) List() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	types := make([]string, 0, len(m.agents))
	for t := range m.agents {
		types = append(types, t)
	}
	return types
}

// Close closes all agents
func (m *Manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var errs []error
	for _, agent := range m.agents {
		if err := agent.Close(); err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("errors closing agents: %v", errs)
	}
	return nil
}

// AgentConfig represents agent configuration
type AgentConfig struct {
	Name    string
	Type    string
	Command string
}

// ACPXAgent represents an acpx-based agent
type ACPXAgent struct {
	name      string
	agentType string
	command   string
}

// NewACPXAgent creates a new acpx agent
func NewACPXAgent(cfg AgentConfig) *ACPXAgent {
	return &ACPXAgent{
		name:      cfg.Name,
		agentType: cfg.Type,
		command:   cfg.Command,
	}
}

// Name returns the agent name
func (a *ACPXAgent) Name() string {
	return a.name
}

// Type returns the agent type
func (a *ACPXAgent) Type() string {
	return a.agentType
}

// CreateSession creates a new agent session
func (a *ACPXAgent) CreateSession(ctx context.Context, sessionName string) (string, error) {
	args := []string{a.agentType, "sessions", "new", "--name", sessionName}
	log.Printf("[acpx] Creating session: %s", sessionName)

	output, err := a.runCommandRaw(ctx, 300, args...)
	if err != nil {
		return "", err
	}

	output = strings.TrimSpace(output)
	log.Printf("[acpx] Session created, output: %s", output)

	// Try to parse JSON format first
	var result struct {
		SessionID string `json:"sessionId"`
	}

	lines := strings.Split(output, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "{") {
			if err := parseJSON(line, &result); err == nil && result.SessionID != "" {
				log.Printf("[acpx] Parsed session ID from JSON: %s", result.SessionID)
				return result.SessionID, nil
			}
		}
	}

	// Try to parse: [acpx] created session <name> (<id>)
	// or: <id>    (created)
	for _, line := range lines {
		line = strings.TrimSpace(line)

		if strings.Contains(line, "created session") {
			start := strings.LastIndex(line, "(")
			end := strings.LastIndex(line, ")")
			if start != -1 && end != -1 && end > start {
				id := strings.TrimSpace(line[start+1 : end])
				if id != "" && !strings.Contains(id, " ") {
					log.Printf("[acpx] Parsed session ID from created line: %s", id)
					return id, nil
				}
			}
		}

		if strings.HasSuffix(line, "(created)") {
			id := strings.TrimSpace(strings.TrimSuffix(line, "(created)"))
			if id != "" && !strings.Contains(id, " ") && !strings.HasPrefix(id, "[") {
				log.Printf("[acpx] Parsed session ID from created status: %s", id)
				return id, nil
			}
		}
	}

	log.Printf("[acpx] No sessionId in output, using session name: %s", sessionName)
	return sessionName, nil
}

// EnsureSession ensures a session exists
func (a *ACPXAgent) EnsureSession(ctx context.Context, sessionName string) (string, error) {
	args := []string{a.agentType, "sessions", "ensure", "--name", sessionName}
	log.Printf("[acpx] Ensuring session: %s", sessionName)

	output, err := a.runCommandRaw(ctx, 300, args...)
	if err != nil {
		return "", err
	}

	output = strings.TrimSpace(output)
	log.Printf("[acpx] Session ensured, output: %s", output)

	// Try to parse JSON format first: {"sessionId": "xxx"}
	var result struct {
		SessionID string `json:"sessionId"`
	}

	lines := strings.Split(output, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "{") {
			if err := parseJSON(line, &result); err == nil && result.SessionID != "" {
				log.Printf("[acpx] Parsed session ID from JSON: %s", result.SessionID)
				return result.SessionID, nil
			}
		}
	}

	// Try to parse: [acpx] created session <name> (<id>)
	// or: <id>    (created)
	for _, line := range lines {
		line = strings.TrimSpace(line)

		// Format: [acpx] created session name (id)
		if strings.Contains(line, "created session") {
			// Extract ID from parentheses: "name (id)"
			start := strings.LastIndex(line, "(")
			end := strings.LastIndex(line, ")")
			if start != -1 && end != -1 && end > start {
				id := strings.TrimSpace(line[start+1 : end])
				if id != "" && !strings.Contains(id, " ") {
					log.Printf("[acpx] Parsed session ID from created line: %s", id)
					return id, nil
				}
			}
		}

		// Format: <id>    (created)
		if strings.HasSuffix(line, "(created)") {
			id := strings.TrimSpace(strings.TrimSuffix(line, "(created)"))
			if id != "" && !strings.Contains(id, " ") && !strings.HasPrefix(id, "[") {
				log.Printf("[acpx] Parsed session ID from created status: %s", id)
				return id, nil
			}
		}
	}

	// If no session ID found, return the session name
	log.Printf("[acpx] No sessionId in output, using session name: %s", sessionName)
	return sessionName, nil
}

// runCommandRaw executes command and returns raw output without post-processing
func (a *ACPXAgent) runCommandRaw(ctx context.Context, timeout int, args ...string) (string, error) {
	if timeout == 0 {
		timeout = 300
	}

	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	fullCmd := fmt.Sprintf("%s %s", a.command, strings.Join(args, " "))
	log.Printf("[acpx] Executing: %s", fullCmd)

	cmd := exec.CommandContext(ctx, a.command, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("command failed: %v, output: %s", err, string(output))
	}

	return string(output), nil
}

// Prompt sends a prompt to the agent
func (a *ACPXAgent) Prompt(ctx context.Context, sessionID, prompt string) (string, error) {
	return a.PromptWithOptions(ctx, sessionID, prompt, &PromptOptions{
		Format: "text",
	})
}

// PromptWithOptions sends a prompt with options
func (a *ACPXAgent) PromptWithOptions(ctx context.Context, sessionID, prompt string, opts *PromptOptions) (string, error) {
	if opts == nil {
		opts = &PromptOptions{}
	}

	return a.doPrompt(ctx, sessionID, prompt, opts)
}

// PromptStream sends a prompt and streams the response
func (a *ACPXAgent) PromptStream(ctx context.Context, sessionID, prompt string, opts *PromptOptions) (<-chan StreamChunk, error) {
	if opts == nil {
		opts = &PromptOptions{}
	}

	return a.doPromptStream(ctx, sessionID, prompt, opts)
}

func (a *ACPXAgent) doPrompt(ctx context.Context, sessionID, prompt string, opts *PromptOptions) (string, error) {
	policy, err := resolvePromptPolicy(opts)
	if err != nil {
		return "", err
	}

	args, timeout, format := buildPromptArgs(a.agentType, sessionID, prompt, opts, policy, false)

	log.Printf("[acpx] Sending prompt to session %s (%s, format=%s)", sessionID, policy.Summary(), format)
	log.Printf("[acpx] Prompt: %s", truncate(prompt, 200))

	start := time.Now()
	response, err := a.runCommand(ctx, timeout, args...)
	metrics.Default().Latency(metrics.AgentExecDuration).Since(start)
	if err != nil {
		annotated := annotatePermissionError(err.Error(), policy)
		if isPermissionError(err.Error()) {
			metrics.Default().Counter(metrics.PermissionDenials).Inc()
			metrics.LogEvent("permission.denied", sessionID, "", map[string]interface{}{
				"agent":  a.agentType,
				"policy": policy.Summary(),
			})
		}
		return "", fmt.Errorf("%s", annotated)
	}
	return response, nil
}

// doPromptStream executes the prompt and streams the output
func (a *ACPXAgent) doPromptStream(ctx context.Context, sessionID, prompt string, opts *PromptOptions) (<-chan StreamChunk, error) {
	policy, err := resolvePromptPolicy(opts)
	if err != nil {
		return nil, err
	}

	args, timeout, _ := buildPromptArgs(a.agentType, sessionID, prompt, opts, policy, true)

	log.Printf("[acpx] Streaming prompt to session %s (%s)", sessionID, policy.Summary())
	log.Printf("[acpx] Prompt: %s", truncate(prompt, 200))

	streamStart := time.Now()
	ch, err := a.runCommandStream(ctx, timeout, policy, args...)
	if err != nil {
		if isPermissionError(err.Error()) {
			metrics.Default().Counter(metrics.PermissionDenials).Inc()
			metrics.LogEvent("permission.denied", sessionID, "", map[string]interface{}{
				"agent":  a.agentType,
				"policy": policy.Summary(),
			})
		}
		metrics.Default().Counter(metrics.AgentExecFailures).Inc()
		return nil, err
	}

	// Wrap the channel to track duration when stream completes
	wrappedCh := make(chan StreamChunk, 200)
	go func() {
		defer close(wrappedCh)
		for chunk := range ch {
			wrappedCh <- chunk
		}
		metrics.Default().Latency(metrics.AgentExecDuration).Since(streamStart)
	}()

	return wrappedCh, nil
}

func resolvePromptPolicy(opts *PromptOptions) (*permission.ResolvedPolicy, error) {
	if opts == nil {
		opts = &PromptOptions{}
	}
	return permission.Resolve(permission.Policy{
		PresetName:          opts.PermissionPreset,
		Permissions:         opts.Permissions,
		AllowedTools:        opts.AllowedTools,
		DeniedTools:         opts.DeniedTools,
		AuthPolicy:          opts.AuthPolicy,
		NonInteractivePerms: opts.NonInteractivePerms,
	})
}

func buildPromptArgs(agentType, sessionID, prompt string, opts *PromptOptions, policy *permission.ResolvedPolicy, streaming bool) ([]string, int, string) {
	args := []string{}

	if opts.Cwd != "" {
		args = append(args, "--cwd", opts.Cwd)
	}
	if policy.AuthPolicy != "" {
		args = append(args, "--auth-policy", policy.AuthPolicy)
	}
	if policy.Permissions != "" {
		switch policy.Permissions {
		case "approve-all":
			args = append(args, "--approve-all")
		case "approve-reads":
			args = append(args, "--approve-reads")
		case "deny-all":
			args = append(args, "--deny-all")
		}
	}
	if policy.NonInteractivePerms != "" {
		args = append(args, "--non-interactive-permissions", policy.NonInteractivePerms)
	}

	format := opts.Format
	if format == "" {
		format = "text"
	}
	args = append(args, "--format", format)

	if opts.SuppressReads {
		args = append(args, "--suppress-reads")
	}
	if opts.Model != "" {
		args = append(args, "--model", opts.Model)
	}
	if allowed := policy.AllowedToolsCSV(); allowed != "" {
		args = append(args, "--allowed-tools", allowed)
	}
	if opts.MaxTurns > 0 {
		args = append(args, "--max-turns", fmt.Sprintf("%d", opts.MaxTurns))
	}
	if opts.PromptRetries > 0 {
		args = append(args, "--prompt-retries", fmt.Sprintf("%d", opts.PromptRetries))
	}

	timeout := 300
	if opts.Timeout > 0 {
		timeout = opts.Timeout
		args = append(args, "--timeout", fmt.Sprintf("%d", opts.Timeout))
	}
	if opts.TTL > 0 {
		args = append(args, "--ttl", fmt.Sprintf("%d", opts.TTL))
	}

	args = append(args, agentType, "-s", sessionID, prompt)
	return args, timeout, format
}

func annotatePermissionError(message string, policy *permission.ResolvedPolicy) string {
	if policy == nil {
		return message
	}
	if isPermissionError(message) {
		return fmt.Sprintf("permission policy denied request (%s): %s", policy.Summary(), message)
	}
	return message
}

func isPermissionError(message string) bool {
	lower := strings.ToLower(message)
	return strings.Contains(lower, "permission") || strings.Contains(lower, "exit status 5") || strings.Contains(lower, "refused")
}

// runCommandStream executes command and streams the output
func (a *ACPXAgent) runCommandStream(ctx context.Context, timeout int, policy *permission.ResolvedPolicy, args ...string) (<-chan StreamChunk, error) {
	if timeout == 0 {
		timeout = 300
	}

	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)

	// Log the command being executed
	fullCmd := fmt.Sprintf("%s %s", a.command, strings.Join(args, " "))
	log.Printf("[acpx] Executing (stream): %s", fullCmd)

	cmd := exec.CommandContext(ctx, a.command, args...)

	// acpx only emits true incremental output when attached to a PTY.
	ptmx, err := pty.Start(cmd)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to start PTY command: %w", err)
	}

	// Create output channel with larger buffer
	outputCh := make(chan StreamChunk, 200)

	// Goroutine to read and stream output
	go func() {
		defer close(outputCh)
		defer cancel()
		defer ptmx.Close()

		// Ensure process is killed on exit
		defer func() {
			if cmd.Process != nil && cmd.ProcessState == nil {
				_ = cmd.Process.Kill()
			}
		}()

		// Read stdout in raw chunks so output can stream even without newlines.
		reader := bufio.NewReader(ptmx)
		var accumulatedContent strings.Builder
		parser := NewProtocolParser()
		buf := make([]byte, 1024)

		for {
			n, err := reader.Read(buf)
			if n > 0 {
				chunk := string(buf[:n])
				accumulatedContent.WriteString(chunk)
				events := parser.Feed(chunk)

				select {
				case outputCh <- StreamChunk{Type: "content", Content: chunk, Events: events}:
				case <-ctx.Done():
					return
				}
			}

			if err != nil {
				if err != io.EOF {
					log.Printf("[acpx] stdout error: %v", err)
				}
				break
			}
		}

		// Wait for command to complete
		waitErr := cmd.Wait()
		flushEvents := parser.Flush()

		// Send done or error (with context check to avoid deadlock)
		if waitErr != nil {
			message := annotatePermissionError(waitErr.Error(), policy)
			events := append(flushEvents, Event{Version: EventProtocolVersion, Type: TypeError, Content: message})
			select {
			case outputCh <- StreamChunk{Type: "error", Content: message, Events: events}:
			case <-ctx.Done():
			}
		} else {
			events := append(flushEvents, Event{Version: EventProtocolVersion, Type: TypeDone})
			select {
			case outputCh <- StreamChunk{Type: "done", Events: events}:
			case <-ctx.Done():
			}
		}

		log.Printf("[acpx] Stream completed, total bytes: %d", accumulatedContent.Len())
	}()

	return outputCh, nil
}

func newEvent(typ EventType) Event {
	return Event{Version: EventProtocolVersion, Type: typ}
}

type protocolState string

const (
	stateIdle       protocolState = ""
	stateThinking   protocolState = "thinking"
	stateToolInput  protocolState = "tool_input"
	stateToolOutput protocolState = "tool_output"
	stateOutput     protocolState = "output"
)

// ProtocolParser converts transcript text into agent-native events.
type ProtocolParser struct {
	buf bytes.Buffer

	state protocolState

	thinkingBuf bytes.Buffer
	outputBuf   bytes.Buffer

	toolName   string
	toolInput  bytes.Buffer
	toolOutput bytes.Buffer
}

// NewProtocolParser creates a parser for agent-native events.
func NewProtocolParser() *ProtocolParser {
	return &ProtocolParser{}
}

// Feed consumes a stream chunk and emits any completed events.
func (p *ProtocolParser) Feed(chunk string) []Event {
	chunk = normalizeProtocolChunk(chunk)
	if chunk == "" {
		return nil
	}

	p.buf.WriteString(chunk)

	var events []Event
	for {
		line, found := p.readLine()
		if !found {
			break
		}
		events = append(events, p.processLine(line)...)
	}

	return events
}

// Flush emits buffered state as final events.
func (p *ProtocolParser) Flush() []Event {
	var events []Event

	if p.buf.Len() > 0 {
		events = append(events, p.processLine(p.buf.String())...)
		p.buf.Reset()
	}

	switch p.state {
	case stateThinking:
		events = append(events, p.flushThinking()...)
	case stateToolInput, stateToolOutput:
		events = append(events, p.flushTool()...)
	case stateOutput:
		events = append(events, p.flushOutput()...)
	}

	p.state = stateIdle
	return events
}

func (p *ProtocolParser) readLine() (string, bool) {
	data := p.buf.Bytes()
	idx := bytes.IndexByte(data, '\n')
	if idx < 0 {
		return "", false
	}
	line := string(data[:idx])
	p.buf.Next(idx + 1)
	return line, true
}

func (p *ProtocolParser) processLine(line string) []Event {
	var events []Event

	if markerType, content, isMarker := parseProtocolMarker(line); isMarker {
		switch markerType {
		case "thinking":
			events = append(events, p.endActiveBlock()...)
			events = append(events, newEvent(TypeThinkingStart))
			p.state = stateThinking
			if content != "" {
				p.appendLine(&p.thinkingBuf, content)
				events = append(events, Event{Version: EventProtocolVersion, Type: TypeThinkingDelta, Content: content})
			}
		case "tool":
			events = append(events, p.processToolMarker(content)...)
		case "done":
			events = append(events, p.endActiveBlock()...)
		default:
			events = append(events, p.endActiveBlock()...)
		}
		return events
	}

	switch p.state {
	case stateThinking:
		if line == "" || startsWithProtocolWhitespace(line) {
			p.appendLine(&p.thinkingBuf, line)
			events = append(events, Event{Version: EventProtocolVersion, Type: TypeThinkingDelta, Content: line})
			return events
		}
		events = append(events, p.flushThinking()...)
		p.state = stateOutput
		p.appendLine(&p.outputBuf, line)
		events = append(events, Event{Version: EventProtocolVersion, Type: TypeOutputDelta, Content: line})
	case stateToolInput:
		if line == "" || startsWithProtocolWhitespace(line) {
			p.appendLine(&p.toolInput, line)
			events = append(events, Event{Version: EventProtocolVersion, Type: TypeToolInput, Name: p.toolName, Input: line})
			return events
		}
		events = append(events, p.flushTool()...)
		p.state = stateOutput
		p.appendLine(&p.outputBuf, line)
		events = append(events, Event{Version: EventProtocolVersion, Type: TypeOutputDelta, Content: line})
	case stateToolOutput:
		if line == "" || startsWithProtocolWhitespace(line) {
			p.appendLine(&p.toolOutput, line)
			events = append(events, Event{Version: EventProtocolVersion, Type: TypeToolOutput, Name: p.toolName, Output: line})
			return events
		}
		events = append(events, p.flushTool()...)
		p.state = stateOutput
		p.appendLine(&p.outputBuf, line)
		events = append(events, Event{Version: EventProtocolVersion, Type: TypeOutputDelta, Content: line})
	case stateOutput:
		p.appendLine(&p.outputBuf, line)
		events = append(events, Event{Version: EventProtocolVersion, Type: TypeOutputDelta, Content: line})
	default:
		if strings.TrimSpace(line) == "" {
			return nil
		}
		p.state = stateOutput
		p.appendLine(&p.outputBuf, line)
		events = append(events, Event{Version: EventProtocolVersion, Type: TypeOutputDelta, Content: line})
	}

	return events
}

func (p *ProtocolParser) processToolMarker(content string) []Event {
	content = strings.TrimSpace(content)
	var events []Event

	switch {
	case strings.HasSuffix(content, "(pending)"):
		events = append(events, p.endActiveBlock()...)
		p.toolName = strings.TrimSpace(strings.TrimSuffix(content, "(pending)"))
		p.toolInput.Reset()
		p.toolOutput.Reset()
		p.state = stateToolInput
		return append(events, Event{Version: EventProtocolVersion, Type: TypeToolStart, Name: p.toolName})
	case strings.HasSuffix(content, "(completed)"):
		if p.state == stateThinking || p.state == stateOutput {
			events = append(events, p.endActiveBlock()...)
		}
		if p.toolName == "" {
			p.toolName = strings.TrimSpace(strings.TrimSuffix(content, "(completed)"))
		}
		p.state = stateToolOutput
		return events
	case strings.HasSuffix(content, "(error)"):
		if p.state == stateThinking || p.state == stateOutput {
			events = append(events, p.endActiveBlock()...)
		}
		name := strings.TrimSpace(strings.TrimSuffix(content, "(error)"))
		if p.toolName == "" {
			p.toolName = name
		}
		evt := Event{
			Version: EventProtocolVersion,
			Type:    TypeError,
			Name:    p.toolName,
			Input:   strings.TrimSpace(p.toolInput.String()),
			Output:  strings.TrimSpace(p.toolOutput.String()),
			Content: "tool execution failed",
		}
		p.resetTool()
		p.state = stateIdle
		return append(events, evt)
	default:
		events = append(events, p.endActiveBlock()...)
		return append(events, Event{Version: EventProtocolVersion, Type: TypeToolStart, Name: content})
	}
}

func (p *ProtocolParser) endActiveBlock() []Event {
	switch p.state {
	case stateThinking:
		return p.flushThinking()
	case stateToolInput, stateToolOutput:
		return p.flushTool()
	case stateOutput:
		return p.flushOutput()
	default:
		return nil
	}
}

func (p *ProtocolParser) flushThinking() []Event {
	content := trimProtocolBlankLines(p.thinkingBuf.String())
	p.thinkingBuf.Reset()
	p.state = stateIdle

	if content == "" {
		return []Event{newEvent(TypeThinkingEnd)}
	}

	return []Event{{Version: EventProtocolVersion, Type: TypeThinkingEnd, Content: content}}
}

func (p *ProtocolParser) flushTool() []Event {
	if p.toolName == "" {
		p.resetTool()
		p.state = stateIdle
		return nil
	}

	evt := Event{
		Version: EventProtocolVersion,
		Type:    TypeToolEnd,
		Name:    p.toolName,
		Input:   strings.TrimSpace(p.toolInput.String()),
		Output:  strings.TrimSpace(p.toolOutput.String()),
	}
	p.resetTool()
	p.state = stateIdle
	return []Event{evt}
}

func (p *ProtocolParser) flushOutput() []Event {
	content := trimProtocolBlankLines(p.outputBuf.String())
	p.outputBuf.Reset()
	p.state = stateIdle

	if content == "" {
		return nil
	}

	// Filter out any remaining transcript markers
	content = filterOutputMarkers(content)

	return []Event{{Version: EventProtocolVersion, Type: TypeOutputFinal, Content: content}}
}

func (p *ProtocolParser) resetTool() {
	p.toolName = ""
	p.toolInput.Reset()
	p.toolOutput.Reset()
}

func (p *ProtocolParser) appendLine(buf *bytes.Buffer, line string) {
	if buf.Len() > 0 {
		buf.WriteByte('\n')
	}
	buf.WriteString(line)
}

func parseProtocolMarker(line string) (string, string, bool) {
	if len(line) == 0 || line[0] != '[' {
		return "", "", false
	}

	end := strings.IndexByte(line, ']')
	if end < 1 {
		return "", "", false
	}

	markerType := line[1:end]
	switch markerType {
	case "thinking", "tool", "done", "client", "acpx":
	default:
		return "", "", false
	}

	return markerType, strings.TrimPrefix(line[end+1:], " "), true
}

func normalizeProtocolChunk(raw string) string {
	raw = stripANSI(raw)
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	raw = strings.ReplaceAll(raw, "\r", "\n")
	return raw
}

func stripANSI(s string) string {
	if strings.IndexByte(s, '') < 0 {
		return s
	}

	var result bytes.Buffer
	result.Grow(len(s))

	for i := 0; i < len(s); {
		if s[i] == '' && i+1 < len(s) && s[i+1] == '[' {
			i += 2
			for i < len(s) {
				c := s[i]
				i++
				if c >= 0x40 && c <= 0x7e {
					break
				}
			}
			continue
		}

		result.WriteByte(s[i])
		i++
	}

	return result.String()
}

func trimProtocolBlankLines(s string) string {
	lines := strings.Split(s, "\n")
	start := 0
	for start < len(lines) && strings.TrimSpace(lines[start]) == "" {
		start++
	}

	end := len(lines)
	for end > start && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}

	return strings.Join(lines[start:end], "\n")
}

// filterOutputMarkers removes transcript marker lines from output content.
func filterOutputMarkers(content string) string {
	lines := strings.Split(content, "\n")
	var filtered []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		// Skip marker lines
		if trimmed == "[thinking]" ||
			strings.HasPrefix(trimmed, "[thinking] ") ||
			strings.HasPrefix(trimmed, "[tool]") ||
			strings.HasPrefix(trimmed, "[done]") ||
			strings.HasPrefix(trimmed, "[acpx]") ||
			strings.HasPrefix(trimmed, "[client]") {
			continue
		}
		filtered = append(filtered, line)
	}
	return strings.TrimSpace(strings.Join(filtered, "\n"))
}

func startsWithProtocolWhitespace(s string) bool {
	if s == "" {
		return false
	}
	r, _ := utf8.DecodeRuneInString(s)
	return unicode.IsSpace(r)
}

// truncate truncates a string to maxLen characters
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// Close closes the agent
func (a *ACPXAgent) Close() error {
	return nil
}

func (a *ACPXAgent) runCommand(ctx context.Context, timeout int, args ...string) (string, error) {
	if timeout == 0 {
		timeout = 300
	}

	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	// Log the command being executed
	fullCmd := fmt.Sprintf("%s %s", a.command, strings.Join(args, " "))
	log.Printf("[acpx] Executing: %s", fullCmd)

	cmd := exec.CommandContext(ctx, a.command, args...)

	// Capture both stdout and stderr
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("[acpx] Command failed: %v, output: %s", err, string(output))
		// Check if output contains actual response despite error
		// acpx sometimes exits with non-zero but still outputs valid content
		if len(output) > 0 && !strings.Contains(string(output), "error") {
			// Might be valid output with informational stderr
			outputStr := string(output)
			// Try to extract the actual response (after any status messages)
			lines := strings.Split(outputStr, "\n")
			for i, line := range lines {
				// Skip status lines
				if strings.Contains(line, "[acpx]") || strings.Contains(line, "session") {
					continue
				}
				// Return the rest as the response
				if line != "" {
					return strings.Join(lines[i:], "\n"), nil
				}
			}
		}
		return "", fmt.Errorf("command failed: %v, output: %s", err, string(output))
	}

	log.Printf("[acpx] Command completed successfully, output length: %d bytes", len(output))
	return string(output), nil
}

func parseJSON(s string, v interface{}) error {
	return json.Unmarshal([]byte(s), v)
}
