package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// StreamChunk represents a chunk of streaming output
type StreamChunk struct {
	Type    string `json:"type"`    // "content", "error", "done"
	Content string `json:"content"` // The content of the chunk
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

	// Allowed tools
	AllowedTools string

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

// List lists all agent types
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
	// Build args in correct order: global options before agent type
	// acpx [options] <agent> -s <session> <prompt>
	args := []string{}

	// Working directory
	if opts.Cwd != "" {
		args = append(args, "--cwd", opts.Cwd)
	}

	// Auth policy
	if opts.AuthPolicy != "" {
		args = append(args, "--auth-policy", opts.AuthPolicy)
	}

	// Permission flags
	if opts.Permissions != "" {
		switch opts.Permissions {
		case "approve-all":
			args = append(args, "--approve-all")
		case "approve-reads":
			args = append(args, "--approve-reads")
		case "deny-all":
			args = append(args, "--deny-all")
		}
	}

	// Non-interactive permissions
	if opts.NonInteractivePerms != "" {
		args = append(args, "--non-interactive-permissions", opts.NonInteractivePerms)
	}

	// Format
	format := opts.Format
	if format == "" {
		format = "text"
	}
	args = append(args, "--format", format)

	// Suppress reads
	if opts.SuppressReads {
		args = append(args, "--suppress-reads")
	}

	// Model
	if opts.Model != "" {
		args = append(args, "--model", opts.Model)
	}

	// Allowed tools
	if opts.AllowedTools != "" {
		args = append(args, "--allowed-tools", opts.AllowedTools)
	}

	// Max turns
	if opts.MaxTurns > 0 {
		args = append(args, "--max-turns", fmt.Sprintf("%d", opts.MaxTurns))
	}

	// Prompt retries
	if opts.PromptRetries > 0 {
		args = append(args, "--prompt-retries", fmt.Sprintf("%d", opts.PromptRetries))
	}

	// Timeout
	timeout := 300
	if opts.Timeout > 0 {
		timeout = opts.Timeout
		args = append(args, "--timeout", fmt.Sprintf("%d", opts.Timeout))
	}

	// TTL
	if opts.TTL > 0 {
		args = append(args, "--ttl", fmt.Sprintf("%d", opts.TTL))
	}

	// Agent type and session
	args = append(args, a.agentType, "-s", sessionID, prompt)

	log.Printf("[acpx] Sending prompt to session %s (permissions=%s, format=%s)", sessionID, opts.Permissions, format)
	log.Printf("[acpx] Prompt: %s", truncate(prompt, 200))

	return a.runCommand(ctx, timeout, args...)
}

// doPromptStream executes the prompt and streams the output
func (a *ACPXAgent) doPromptStream(ctx context.Context, sessionID, prompt string, opts *PromptOptions) (<-chan StreamChunk, error) {
	// Build args in correct order: global options before agent type
	args := []string{}

	// Working directory
	if opts.Cwd != "" {
		args = append(args, "--cwd", opts.Cwd)
	}

	// Auth policy
	if opts.AuthPolicy != "" {
		args = append(args, "--auth-policy", opts.AuthPolicy)
	}

	// Permission flags
	if opts.Permissions != "" {
		switch opts.Permissions {
		case "approve-all":
			args = append(args, "--approve-all")
		case "approve-reads":
			args = append(args, "--approve-reads")
		case "deny-all":
			args = append(args, "--deny-all")
		}
	}

	// Non-interactive permissions
	if opts.NonInteractivePerms != "" {
		args = append(args, "--non-interactive-permissions", opts.NonInteractivePerms)
	}

	// Format - always use text for streaming
	args = append(args, "--format", "text")

	// Suppress reads
	if opts.SuppressReads {
		args = append(args, "--suppress-reads")
	}

	// Model
	if opts.Model != "" {
		args = append(args, "--model", opts.Model)
	}

	// Allowed tools
	if opts.AllowedTools != "" {
		args = append(args, "--allowed-tools", opts.AllowedTools)
	}

	// Max turns
	if opts.MaxTurns > 0 {
		args = append(args, "--max-turns", fmt.Sprintf("%d", opts.MaxTurns))
	}

	// Prompt retries
	if opts.PromptRetries > 0 {
		args = append(args, "--prompt-retries", fmt.Sprintf("%d", opts.PromptRetries))
	}

	// Timeout
	timeout := 300
	if opts.Timeout > 0 {
		timeout = opts.Timeout
		args = append(args, "--timeout", fmt.Sprintf("%d", opts.Timeout))
	}

	// TTL
	if opts.TTL > 0 {
		args = append(args, "--ttl", fmt.Sprintf("%d", opts.TTL))
	}

	// Agent type and session
	args = append(args, a.agentType, "-s", sessionID, prompt)

	log.Printf("[acpx] Streaming prompt to session %s (permissions=%s)", sessionID, opts.Permissions)
	log.Printf("[acpx] Prompt: %s", truncate(prompt, 200))

	return a.runCommandStream(ctx, timeout, args...)
}

// runCommandStream executes command and streams the output
func (a *ACPXAgent) runCommandStream(ctx context.Context, timeout int, args ...string) (<-chan StreamChunk, error) {
	if timeout == 0 {
		timeout = 300
	}

	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)

	// Log the command being executed
	fullCmd := fmt.Sprintf("%s %s", a.command, strings.Join(args, " "))
	log.Printf("[acpx] Executing (stream): %s", fullCmd)

	cmd := exec.CommandContext(ctx, a.command, args...)

	// Get stdout and stderr pipes
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	// Start the command
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("failed to start command: %w", err)
	}

	// Create output channel with larger buffer
	outputCh := make(chan StreamChunk, 200)

	// Goroutine to read and stream output
	go func() {
		defer close(outputCh)
		defer cancel()

		// Ensure process is killed on exit
		defer func() {
			if cmd.Process != nil && cmd.ProcessState == nil {
				cmd.Process.Kill()
			}
		}()

		// Read stderr in background
		stderrDone := make(chan struct{})
		go func() {
			defer close(stderrDone)
			stderrReader := bufio.NewReader(stderr)
			for {
				line, err := stderrReader.ReadString('\n')
				if err != nil {
					if err != io.EOF {
						log.Printf("[acpx] stderr error: %v", err)
					}
					return
				}
				line = strings.TrimSuffix(line, "\n")
				if line != "" {
					log.Printf("[acpx] stderr: %s", line)
				}
			}
		}()

		// Read stdout and send chunks
		reader := bufio.NewReader(stdout)
		var accumulatedContent strings.Builder

		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				if err != io.EOF {
					log.Printf("[acpx] stdout error: %v", err)
				}
				break
			}

			line = strings.TrimSuffix(line, "\n")

			// Skip empty lines and status lines
			if line == "" || strings.HasPrefix(line, "[acpx]") {
				log.Printf("[acpx] status: %s", line)
				continue
			}

			// Send content chunk
			accumulatedContent.WriteString(line)
			accumulatedContent.WriteString("\n")

			select {
			case outputCh <- StreamChunk{Type: "content", Content: line}:
				// Successfully sent
			case <-ctx.Done():
				// Context cancelled, stop sending
				return
			}
		}

		// Wait for command to complete
		waitErr := cmd.Wait()

		// Wait for stderr to finish (with timeout protection via context)
		select {
		case <-stderrDone:
		case <-ctx.Done():
		}

		// Send done or error (with context check to avoid deadlock)
		if waitErr != nil {
			// Check if we have accumulated content despite error
			if accumulatedContent.Len() > 0 {
				select {
				case outputCh <- StreamChunk{Type: "done", Content: accumulatedContent.String()}:
				case <-ctx.Done():
				}
			} else {
				select {
				case outputCh <- StreamChunk{Type: "error", Content: waitErr.Error()}:
				case <-ctx.Done():
				}
			}
		} else {
			select {
			case outputCh <- StreamChunk{Type: "done", Content: accumulatedContent.String()}:
			case <-ctx.Done():
			}
		}

		log.Printf("[acpx] Stream completed, total bytes: %d", accumulatedContent.Len())
	}()

	return outputCh, nil
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
