package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
	"github.com/smallnest/imclaw/internal/agent"
	"github.com/smallnest/imclaw/internal/event"
	"github.com/smallnest/imclaw/internal/permission"
	flag "github.com/spf13/pflag"
)

var (
	// Server connection (HTTP and WebSocket on same port)
	serverURL = flag.StringP("server", "s", "ws://localhost:8080/ws", "IMClaw server WebSocket URL")
	authToken = flag.StringP("token", "t", "", "Authentication token")

	// Session
	sessionID = flag.StringP("session", "S", "", "Session ID to use (empty for auto-create)")

	// One-shot prompt
	promptFlag = flag.StringP("prompt", "p", "", "Prompt message (one-shot mode)")

	// Agent selection
	agentType = flag.StringP("agent", "a", "", "Agent type (claude, codex, etc.)")

	// Working directory
	cwd = flag.StringP("cwd", "C", "", "Working directory")

	// Permission flags
	approveAll   = flag.Bool("approve-all", false, "Auto-approve all permission requests")
	approveReads = flag.Bool("approve-reads", false, "Auto-approve read/search requests and prompt for writes")
	denyAll      = flag.Bool("deny-all", false, "Deny all permission requests")

	// Permission policy
	permissionPreset = flag.String("permission-preset", "", "Permission preset: safe-readonly, dev-default, or full-auto")

	// Auth policy
	authPolicy = flag.String("auth-policy", "", "Authentication policy: skip or fail when auth is required")

	// Non-interactive permissions
	nonInteractivePerms = flag.String("non-interactive-permissions", "", "When prompting is unavailable: deny or fail")

	// Output format
	format = flag.String("format", "text", "Output format: text, json, quiet")

	// Suppress reads
	suppressReads = flag.Bool("suppress-reads", false, "Suppress raw read-file contents in output")

	// Model selection
	model = flag.String("model", "", "Agent model id")

	// Tools
	allowedTools = flag.String("allowed-tools", "", "Allowed tool names (comma-separated)")
	deniedTools  = flag.String("denied-tools", "", "Denied tool names (comma-separated)")

	// Session control
	maxTurns      = flag.Int("max-turns", 0, "Maximum turns for the session")
	promptRetries = flag.Int("prompt-retries", 0, "Retry failed prompt turns on transient errors")

	// JSON strict mode
	jsonStrict = flag.Bool("json-strict", false, "Strict JSON mode: requires --format json and suppresses non-JSON stderr output")

	// Transcript parsing
	parseTranscript = flag.Bool("parse-transcript", false, "Parse full IMClaw transcript output into structured message slices")

	// Timeouts
	timeout = flag.Int("timeout", 0, "Maximum time to wait for agent response (seconds)")
	ttl     = flag.Int("ttl", 0, "Queue owner idle TTL before shutdown (seconds)")

	// Version
	showVersion = flag.BoolP("version", "v", false, "Show version information")

	// 版本信息
	Version   = "dev"
	BuildTime = "unknown"
)

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "IMClaw CLI - Command line interface for IMClaw\n\n")
		fmt.Fprintf(os.Stderr, "Usage: %s [command] [options] [-p <message> | <message>]\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Commands:\n")
		fmt.Fprintf(os.Stderr, "  job       Manage background jobs\n")
		fmt.Fprintf(os.Stderr, "            (job submit, job status, job logs, job cancel, job list, job delete)\n")
		fmt.Fprintf(os.Stderr, "\n")
		fmt.Fprintf(os.Stderr, "If message is provided (-p or positional), sends it and exits.\n")
		fmt.Fprintf(os.Stderr, "If no message, starts interactive REPL mode.\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  # Interactive mode\n")
		fmt.Fprintf(os.Stderr, "  %s\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  # One-shot mode with -p\n")
		fmt.Fprintf(os.Stderr, "  %s -p \"What is Go?\"\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  # Submit a background job\n")
		fmt.Fprintf(os.Stderr, "  %s job submit -p \"What is Go?\"\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  # Check job status\n")
		fmt.Fprintf(os.Stderr, "  %s job status <job-id>\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  # One-shot mode with positional argument\n")
		fmt.Fprintf(os.Stderr, "  %s \"What is Go?\"\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  # Use specific agent\n")
		fmt.Fprintf(os.Stderr, "  %s --agent codex -p \"Hello\"\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  # JSON output with auto-approve\n")
		fmt.Fprintf(os.Stderr, "  %s --format json --approve-all -p \"Hello\"\n", os.Args[0])
	}

	// Parse flags but stop before processing commands
	flag.Parse()

	if *showVersion {
		fmt.Printf("IMClaw CLI %s\n", Version)
		fmt.Printf("Build Time: %s\n", BuildTime)
		os.Exit(0)
	}

	// Handle job subcommands
	if len(os.Args) > 1 && os.Args[1] == "job" {
		handleJobCommand()
		return
	}

	// Validate permission flags (only one can be set)
	permCount := 0
	if *approveAll {
		permCount++
	}
	if *approveReads {
		permCount++
	}
	if *denyAll {
		permCount++
	}
	if permCount > 1 {
		fmt.Fprintf(os.Stderr, "Error: Only one of --approve-all, --approve-reads, --deny-all can be set\n")
		os.Exit(1)
	}

	// Validate format
	switch *format {
	case "text", "json", "quiet":
		// valid
	default:
		fmt.Fprintf(os.Stderr, "Error: invalid format: %s\n", *format)
		fmt.Fprintf(os.Stderr, "Valid values: text, json, quiet\n")
		os.Exit(1)
	}

	// Validate json-strict requires json format
	if *jsonStrict && *format != "json" {
		fmt.Fprintf(os.Stderr, "Error: --json-strict requires --format json\n")
		os.Exit(1)
	}

	// Validate parse-transcript requires text format (it parses transcript output)
	if *parseTranscript && *format != "text" {
		fmt.Fprintf(os.Stderr, "Error: --parse-transcript requires --format text\n")
		fmt.Fprintf(os.Stderr, "Note: --parse-transcript parses transcript output into structured events\n")
		os.Exit(1)
	}

	// Get message from --prompt flag or remaining args
	var message string
	if *promptFlag != "" {
		message = *promptFlag
	} else {
		args := flag.Args()
		if len(args) > 0 {
			message = strings.Join(args, " ")
		}
	}

	client := NewClient(*serverURL, *authToken)

	// If message provided, send it and exit (one-shot mode)
	if message != "" {
		sendAndExit(client, message)
		return
	}

	// Otherwise, start REPL
	startREPL(client)
}

// getPermissions returns the resolved permission mode based on flags.
func getPermissions() string {
	policy, err := resolvePolicyFromFlags()
	if err != nil {
		if *denyAll {
			return "deny-all"
		}
		if *approveAll {
			return "approve-all"
		}
		return "approve-reads"
	}
	return policy.Permissions
}

func resolvePolicyFromFlags() (*permission.ResolvedPolicy, error) {
	permissions := ""
	if *approveAll {
		permissions = "approve-all"
	}
	if *approveReads {
		permissions = "approve-reads"
	}
	if *denyAll {
		permissions = "deny-all"
	}
	return permission.Resolve(permission.Policy{
		PresetName:          *permissionPreset,
		Permissions:         permissions,
		AllowedTools:        *allowedTools,
		DeniedTools:         *deniedTools,
		AuthPolicy:          *authPolicy,
		NonInteractivePerms: *nonInteractivePerms,
	})
}

func buildPromptParams(content string, includeFormat bool) (map[string]interface{}, error) {
	policy, err := resolvePolicyFromFlags()
	if err != nil {
		return nil, err
	}

	params := map[string]interface{}{
		"content":     content,
		"permissions": policy.Permissions,
	}
	if policy.PresetName != "" {
		params["permission_preset"] = policy.PresetName
	}
	if includeFormat {
		params["format"] = *format
	}
	if *sessionID != "" {
		params["session_id"] = *sessionID
	}
	if *agentType != "" {
		params["agent"] = *agentType
	}
	if *cwd != "" {
		params["cwd"] = *cwd
	}
	if policy.AuthPolicy != "" {
		params["auth_policy"] = policy.AuthPolicy
	}
	if policy.NonInteractivePerms != "" {
		params["non_interactive_permissions"] = policy.NonInteractivePerms
	}
	if *suppressReads {
		params["suppress_reads"] = true
	}
	if *model != "" {
		params["model"] = *model
	}
	if allowed := policy.AllowedToolsCSV(); allowed != "" {
		params["allowed_tools"] = allowed
	}
	if *deniedTools != "" {
		params["denied_tools"] = *deniedTools
	}
	if *maxTurns > 0 {
		params["max_turns"] = *maxTurns
	}
	if *promptRetries > 0 {
		params["prompt_retries"] = *promptRetries
	}
	if *timeout > 0 {
		params["timeout"] = *timeout
	}
	if *ttl > 0 {
		params["ttl"] = *ttl
	}
	return params, nil
}

// JSONRPCRequest represents a JSON-RPC request
type JSONRPCRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      string      `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params"`
}

// JSONRPCResponse represents a JSON-RPC response
type JSONRPCResponse struct {
	JSONRPC string        `json:"jsonrpc"`
	ID      string        `json:"id"`
	Result  interface{}   `json:"result,omitempty"`
	Error   *JSONRPCError `json:"error,omitempty"`
}

// JSONRPCError represents a JSON-RPC error
type JSONRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// StreamEvent is an alias for agent.Event
type StreamEvent = agent.Event

// Client is the IMClaw client
type Client struct {
	wsURL   string
	httpURL string
	token   string
	conn    *websocket.Conn
	connID  string
}

// NewClient creates a new client
func NewClient(wsURL, token string) *Client {
	// Derive HTTP URL from WebSocket URL
	// ws://host:port/ws -> http://host:port
	httpURL := strings.Replace(wsURL, "ws://", "http://", 1)
	httpURL = strings.Replace(httpURL, "wss://", "https://", 1)
	httpURL = strings.TrimSuffix(httpURL, "/ws")

	return &Client{
		wsURL:   wsURL,
		httpURL: httpURL,
		token:   token,
	}
}

// Connect connects to the WebSocket server
func (c *Client) Connect() error {
	u, err := url.Parse(c.wsURL)
	if err != nil {
		return fmt.Errorf("invalid WebSocket URL: %w", err)
	}

	// Add token to URL if provided
	if c.token != "" {
		q := u.Query()
		q.Set("token", c.token)
		u.RawQuery = q.Encode()
	}

	dialer := websocket.DefaultDialer
	conn, _, err := dialer.Dial(u.String(), nil)
	if err != nil {
		return fmt.Errorf("failed to connect to WebSocket: %w", err)
	}

	c.conn = conn

	// Read welcome message to get session ID
	var welcome JSONRPCRequest
	if err := conn.ReadJSON(&welcome); err != nil {
		return fmt.Errorf("failed to read welcome: %w", err)
	}

	if params, ok := welcome.Params.(map[string]interface{}); ok {
		if sid, ok := params["session_id"].(string); ok {
			c.connID = sid
		}
	}

	return nil
}

// Close closes the connection
func (c *Client) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// Ask sends a message via the ask method
func (c *Client) Ask(content string) (*JSONRPCResponse, error) {
	if c.conn == nil {
		if err := c.Connect(); err != nil {
			return nil, err
		}
	}

	params, err := buildPromptParams(content, true)
	if err != nil {
		return nil, err
	}

	req := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      fmt.Sprintf("%d", time.Now().UnixNano()),
		Method:  "ask",
		Params:  params,
	}

	if err := c.conn.WriteJSON(req); err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}

	var resp JSONRPCResponse
	if err := c.conn.ReadJSON(&resp); err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	return &resp, nil
}

// AskStream sends a message and streams the response.
func (c *Client) AskStream(content string, onChunk func(chunkType, chunk string), onEvent func(StreamEvent)) (*JSONRPCResponse, error) {
	if c.conn == nil {
		if err := c.Connect(); err != nil {
			return nil, err
		}
	}

	params, err := buildPromptParams(content, true)
	if err != nil {
		return nil, err
	}

	reqID := fmt.Sprintf("%d", time.Now().UnixNano())
	req := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      reqID,
		Method:  "ask_stream",
		Params:  params,
	}

	if err := c.conn.WriteJSON(req); err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}

	// Set read deadline based on timeout
	readTimeout := time.Duration(300) * time.Second
	if *timeout > 0 {
		readTimeout = time.Duration(*timeout+30) * time.Second // Add buffer
	}

	// Read streaming notifications and final response
	var finalResp JSONRPCResponse
	for {
		// Set deadline for each read
		if err := c.conn.SetReadDeadline(time.Now().Add(readTimeout)); err != nil {
			return nil, fmt.Errorf("failed to set read deadline: %w", err)
		}

		var msg json.RawMessage
		if err := c.conn.ReadJSON(&msg); err != nil {
			return nil, fmt.Errorf("failed to read message: %w", err)
		}

		// Try to parse as notification first
		var notification JSONRPCRequest
		if err := json.Unmarshal(msg, &notification); err == nil {
			if params, ok := notification.Params.(map[string]interface{}); ok {
				switch notification.Method {
				case "stream":
					if !notificationMatchesRequest(params, reqID) {
						continue
					}
					chunkType, _ := params["type"].(string)
					chunkContent, _ := params["content"].(string)
					if onChunk != nil {
						onChunk(chunkType, chunkContent)
					}
					continue
				case "event":
					if !notificationMatchesRequest(params, reqID) {
						continue
					}
					evt := parseEventParams(params)
					if onEvent != nil {
						onEvent(evt)
					}
					continue
				}
			}
		}

		// Try to parse as response
		var resp JSONRPCResponse
		if err := json.Unmarshal(msg, &resp); err == nil && resp.ID == reqID {
			finalResp = resp
			break
		}
	}

	// Clear read deadline
	_ = c.conn.SetReadDeadline(time.Time{})

	return &finalResp, nil
}

// GetSession gets the current session info
func (c *Client) GetSession(sessionID string) (*JSONRPCResponse, error) {
	if c.conn == nil {
		if err := c.Connect(); err != nil {
			return nil, err
		}
	}

	params := map[string]interface{}{}
	if sessionID != "" {
		params["session_id"] = sessionID
	}

	req := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      fmt.Sprintf("%d", time.Now().UnixNano()),
		Method:  "session.get",
		Params:  params,
	}

	if err := c.conn.WriteJSON(req); err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}

	var resp JSONRPCResponse
	if err := c.conn.ReadJSON(&resp); err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	return &resp, nil
}

// InitSession initializes or gets the session
func (c *Client) InitSession(sessionID, agentType string) (*JSONRPCResponse, error) {
	if c.conn == nil {
		if err := c.Connect(); err != nil {
			return nil, err
		}
	}

	params := map[string]interface{}{}
	if sessionID != "" {
		params["session_id"] = sessionID
	}
	if agentType != "" {
		params["agent"] = agentType
	}

	req := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      fmt.Sprintf("%d", time.Now().UnixNano()),
		Method:  "session.init",
		Params:  params,
	}

	if err := c.conn.WriteJSON(req); err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}

	var resp JSONRPCResponse
	if err := c.conn.ReadJSON(&resp); err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	return &resp, nil
}

// HTTPGet makes a GET request
func (c *Client) HTTPGet(path string) ([]byte, error) {
	req, err := http.NewRequest("GET", c.httpURL+path, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Add auth token if provided
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	return io.ReadAll(resp.Body)
}

func writeStreamChunk(stdout, stderr io.Writer, chunkType, chunk string) {
	// acpx already formats output based on --format flag
	// Just pass through the content
	if chunkType == "error" {
		fmt.Fprintf(stderr, "[error] %s\n", chunk)
	} else if chunkType == "content" {
		fmt.Fprint(stdout, chunk)
	}
}

func looksLikeTranscript(content string) bool {
	return strings.Contains(content, "[thinking]") ||
		strings.Contains(content, "[tool]") ||
		strings.Contains(content, "[client]") ||
		strings.Contains(content, "[acpx]") ||
		strings.Contains(content, "[done]")
}

func printResponseContent(content string) {
	if *parseTranscript && looksLikeTranscript(content) {
		events := event.Parse(content)
		printJSON(events)
		return
	}
	fmt.Println(content)
}

// handleParsedResult handles the response result with optional transcript parsing.
// Returns true if output was produced, false otherwise.
func handleParsedResult(result interface{}, sawStructuredEvent bool) bool {
	if !*parseTranscript {
		return false
	}

	if m, ok := result.(map[string]interface{}); ok {
		if content, ok := m["content"].(string); ok {
			if looksLikeTranscript(content) {
				if sawStructuredEvent {
					return true
				}
				printResponseContent(content)
				return true
			}
			printResponseContent(content)
			return true
		}
		printJSON(m)
		return true
	}
	printJSON(result)
	return true
}

// streamHandler returns a callback function for AskStream that handles
// direct output based on the parseTranscript flag.
func streamHandler() func(chunkType, chunk string) {
	return func(chunkType, chunk string) {
		if !*parseTranscript {
			writeStreamChunk(os.Stdout, os.Stderr, chunkType, chunk)
		}
	}
}

func notificationMatchesRequest(params map[string]interface{}, reqID string) bool {
	id, ok := params["id"].(string)
	if !ok || id == "" {
		return true
	}
	return id == reqID
}

// parseEventParams parses event parameters from JSON-RPC notification params.
func parseEventParams(params map[string]interface{}) agent.Event {
	evt := agent.Event{}
	if v, ok := params["version"].(string); ok {
		evt.Version = v
	}
	if t, ok := params["type"].(string); ok {
		evt.Type = agent.EventType(t)
	}
	if c, ok := params["content"].(string); ok {
		evt.Content = c
	}
	if n, ok := params["name"].(string); ok {
		evt.Name = n
	}
	if i, ok := params["input"].(string); ok {
		evt.Input = i
	}
	if o, ok := params["output"].(string); ok {
		evt.Output = o
	}
	return evt
}

func writeStructuredEvent(stdout, stderr io.Writer, evt agent.Event) {
	if evt.Type == agent.TypeError {
		fmt.Fprintf(stderr, "[error] %s\n", evt.Content)
		return
	}

	data, err := json.Marshal(evt)
	if err != nil {
		fmt.Fprintf(stdout, "{\"type\":\"output\",\"content\":%q}\n", evt.Content)
		return
	}
	fmt.Fprintln(stdout, string(data))
}

func printCLIError(stderr io.Writer, message string) {
	fmt.Fprintf(stderr, "Error: %s\n", message)

	if shouldSuggestApproveAll(message) {
		fmt.Fprintln(stderr, "Hint: this request likely needs broader tool permission. Retry with --permission-preset full-auto or --approve-all.")
	}
}

func shouldSuggestApproveAll(message string) bool {
	if getPermissions() == "approve-all" {
		return false
	}

	message = strings.ToLower(message)
	return strings.Contains(message, "exit status 5") ||
		strings.Contains(message, "user refused permission") ||
		strings.Contains(message, "permission")
}

// sendAndExit sends a single message and exits
func sendAndExit(client *Client, message string) {
	if err := client.Connect(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	defer client.Close()

	var resp *JSONRPCResponse
	var err error
	var sawStructuredEvent bool

	resp, err = client.AskStream(message, streamHandler(), func(event StreamEvent) {
		sawStructuredEvent = true
		if *parseTranscript {
			writeStructuredEvent(os.Stdout, os.Stderr, event)
		}
	})

	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if resp.Error != nil {
		printCLIError(os.Stderr, resp.Error.Message)
		os.Exit(1)
	}

	// In normal mode, content is already printed via callback.
	// In transcript mode, fall back to the final content when structured events were unavailable.
	handleParsedResult(resp.Result, sawStructuredEvent)
}

// startREPL starts the interactive REPL
func startREPL(client *Client) {
	// Connect to WebSocket
	if err := client.Connect(); err != nil {
		fmt.Fprintf(os.Stderr, "Error connecting to server: %v\n", err)
		os.Exit(1)
	}
	defer client.Close()

	fmt.Printf("IMClaw CLI %s\n", Version)
	fmt.Printf("Connected to %s\n", *serverURL)

	// Initialize session on startup
	initResp, err := client.InitSession(*sessionID, *agentType)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error initializing session: %v\n", err)
		os.Exit(1)
	}

	// Show session info
	if initResp.Error != nil {
		printCLIError(os.Stderr, initResp.Error.Message)
	} else if result, ok := initResp.Result.(map[string]interface{}); ok {
		if sid, ok := result["session_id"].(string); ok {
			fmt.Printf("Session: %s", sid)
			if agent, ok := result["agent"].(string); ok && agent != "" {
				fmt.Printf(" | Agent: %s", agent)
			}
			fmt.Println()
		}
	}

	if policy, err := resolvePolicyFromFlags(); err == nil {
		fmt.Printf("Permissions: %s | Format: %s\n", policy.Summary(), *format)
	} else {
		fmt.Printf("Permissions: %s | Format: %s\n", getPermissions(), *format)
	}
	if *cwd != "" {
		fmt.Printf("Working directory: %s\n", *cwd)
	}

	fmt.Println()
	fmt.Println("Type your message and press Enter. Use /help for commands, /quit to exit.")
	fmt.Println()

	// Setup signal handling
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Read input in a goroutine
	inputCh := make(chan string)
	go func() {
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			inputCh <- scanner.Text()
		}
	}()

	for {
		fmt.Print("> ")
		select {
		case <-sigCh:
			fmt.Println("\nGoodbye!")
			return
		case line := <-inputCh:
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}

			// Handle special commands
			switch {
			case line == "/quit" || line == "/exit":
				fmt.Println("Goodbye!")
				return
			case line == "/help":
				printHelp()
				continue
			case line == "/session":
				showSession(client, *sessionID)
				continue
			case line == "/agents":
				listAgents(client)
				continue
			case strings.HasPrefix(line, "/agent "):
				newAgent := strings.TrimSpace(strings.TrimPrefix(line, "/agent "))
				*agentType = newAgent
				fmt.Printf("Switched to agent: %s\n", newAgent)
				continue
			case line == "/new":
				resp, err := client.Ask("/new")
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error: %v\n", err)
					continue
				}
				if resp.Error != nil {
					printCLIError(os.Stderr, resp.Error.Message)
					continue
				}
				fmt.Println("New session created. Context cleared.")
				continue
			}

			// Send message
			fmt.Println()
			var sawStructuredEvent bool

			resp, err := client.AskStream(line, streamHandler(), func(event StreamEvent) {
				sawStructuredEvent = true
				if *parseTranscript {
					writeStructuredEvent(os.Stdout, os.Stderr, event)
				}
			})

			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				continue
			}

			if resp.Error != nil {
				printCLIError(os.Stderr, resp.Error.Message)
				continue
			}

			fmt.Println()
			handleParsedResult(resp.Result, sawStructuredEvent)
			fmt.Println()
		}
	}
}

func printHelp() {
	fmt.Print(`
Commands:
  <message>       Send a message to the agent
  /new            Create a new session (clear context)
  /session        Show current session info
  /agent <name>   Switch to a different agent
  /agents         List available agents
  /help           Show this help
  /quit           Exit the CLI

Options (can be set via flags at startup):
  --agent <type>        Agent type (claude, codex, etc.)
  --cwd <dir>           Working directory
  --approve-all         Auto-approve all permission requests
  --approve-reads       Auto-approve read requests, prompt for writes
  --deny-all            Deny all permission requests
  --format <fmt>        Output format: text, json, quiet
  --parse-transcript    Parse final IMClaw transcript into structured messages
  --model <id>          Agent model id
  --timeout <seconds>   Maximum time to wait for agent response
  --ttl <seconds>       Queue owner idle TTL before shutdown
  --verbose             Enable verbose debug logs
`)
}

func showSession(client *Client, sessionID string) {
	resp, err := client.GetSession(sessionID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return
	}

	if resp.Error != nil {
		printCLIError(os.Stderr, resp.Error.Message)
		return
	}

	if resp.Result == nil {
		fmt.Println("No active session")
		return
	}

	// Parse session info
	session, ok := resp.Result.(map[string]interface{})
	if !ok {
		printJSON(resp.Result)
		return
	}

	fmt.Println("Current Session:")
	fmt.Printf("  ID:            %v\n", session["id"])
	fmt.Printf("  Agent:         %v\n", session["agent_name"])
	fmt.Printf("  Agent Session: %v\n", session["agent_session"])
	if createdAt, ok := session["created_at"].(string); ok {
		fmt.Printf("  Created:       %s\n", createdAt)
	}
	if lastActive, ok := session["last_active"].(string); ok {
		fmt.Printf("  Last Active:   %s\n", lastActive)
	}
}

func listAgents(client *Client) {
	data, err := client.HTTPGet("/api/agents")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return
	}

	var result struct {
		Agents []string `json:"agents"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing response: %v\n", err)
		return
	}

	if len(result.Agents) == 0 {
		fmt.Println("No agents available")
		return
	}

	fmt.Println("Available agents:")
	for _, a := range result.Agents {
		fmt.Printf("  - %s\n", a)
	}
}

// printJSON prints JSON in a formatted way
func printJSON(v interface{}) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error formatting JSON: %v\n", err)
		return
	}
	fmt.Println(string(data))
}

// handleJobCommand handles job subcommands
func handleJobCommand() {
	if len(os.Args) < 3 {
		fmt.Fprintf(os.Stderr, "Error: job command requires an action\n")
		fmt.Fprintf(os.Stderr, "Usage: %s job <action> [options]\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Actions: submit, status, logs, cancel, list, delete\n")
		os.Exit(1)
	}

	action := os.Args[2]

	// Rebuild args without the "job" prefix for flag parsing
	jobArgs := []string{os.Args[0]}
	if len(os.Args) > 3 {
		jobArgs = append(jobArgs, os.Args[3:]...)
	}

	// Parse flags for job commands
	flag.CommandLine.Parse(jobArgs)

	switch action {
	case "submit":
		handleJobSubmit()
	case "status":
		handleJobStatus()
	case "logs":
		handleJobLogs()
	case "cancel":
		handleJobCancel()
	case "list":
		handleJobList()
	case "delete":
		handleJobDelete()
	default:
		fmt.Fprintf(os.Stderr, "Error: unknown job action: %s\n", action)
		fmt.Fprintf(os.Stderr, "Valid actions: submit, status, logs, cancel, list, delete\n")
		os.Exit(1)
	}
}

// handleJobSubmit submits a new background job
func handleJobSubmit() {
	if *promptFlag == "" {
		fmt.Fprintf(os.Stderr, "Error: -p <prompt> is required for job submit\n")
		os.Exit(1)
	}

	serverHTTP := getServerHTTPURL()

	reqBody := map[string]interface{}{
		"prompt":     *promptFlag,
		"agent_name": *agentType,
	}
	if reqBody["agent_name"] == "" {
		reqBody["agent_name"] = "acpx"
	}

	reqJSON, _ := json.Marshal(reqBody)
	resp, err := http.Post(serverHTTP+"/api/jobs", "application/json", strings.NewReader(string(reqJSON)))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error submitting job: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		fmt.Fprintf(os.Stderr, "Error: server returned status %d: %s\n", resp.StatusCode, string(body))
		os.Exit(1)
	}

	var job map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&job); err != nil {
		fmt.Fprintf(os.Stderr, "Error decoding response: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Job submitted successfully\n")
	fmt.Printf("ID: %s\n", job["id"])
	fmt.Printf("Status: %s\n", job["status"])
	fmt.Printf("Created: %s\n", job["created_at"])
	fmt.Printf("\nUse '%s job status %s' to check status\n", os.Args[0], job["id"])
}

// handleJobStatus shows the status of a job
func handleJobStatus() {
	if len(os.Args) < 4 {
		fmt.Fprintf(os.Stderr, "Error: job status requires a job ID\n")
		fmt.Fprintf(os.Stderr, "Usage: %s job status <job-id>\n", os.Args[0])
		os.Exit(1)
	}

	jobID := os.Args[3]
	serverHTTP := getServerHTTPURL()

	resp, err := http.Get(serverHTTP + "/api/jobs/" + jobID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error fetching job: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		fmt.Fprintf(os.Stderr, "Error: job not found\n")
		os.Exit(1)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		fmt.Fprintf(os.Stderr, "Error: server returned status %d: %s\n", resp.StatusCode, string(body))
		os.Exit(1)
	}

	var job map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&job); err != nil {
		fmt.Fprintf(os.Stderr, "Error decoding response: %v\n", err)
		os.Exit(1)
	}

	// Print job status
	if *format == "json" {
		printJSON(job)
		return
	}

	fmt.Printf("Job ID: %s\n", job["id"])
	fmt.Printf("Status: %s\n", job["status"])
	fmt.Printf("Prompt: %s\n", job["prompt"])
	fmt.Printf("Agent: %s\n", job["agent_name"])
	fmt.Printf("Created: %s\n", job["created_at"])

	if startedAt, ok := job["started_at"].(string); ok && startedAt != "" {
		fmt.Printf("Started: %s\n", startedAt)
	}
	if finishedAt, ok := job["finished_at"].(string); ok && finishedAt != "" {
		fmt.Printf("Finished: %s\n", finishedAt)
	}
	if result, ok := job["result"].(string); ok && result != "" {
		fmt.Printf("\nResult:\n%s\n", result)
	}
	if errMsg, ok := job["error"].(string); ok && errMsg != "" {
		fmt.Printf("\nError: %s\n", errMsg)
	}

	// Show logs if available
	if logs, ok := job["logs"].([]interface{}); ok && len(logs) > 0 {
		fmt.Printf("\nLogs (%d entries):\n", len(logs))
		for _, logEntry := range logs {
			if entry, ok := logEntry.(map[string]interface{}); ok {
				timestamp, _ := entry["timestamp"].(string)
				level, _ := entry["level"].(string)
				message, _ := entry["message"].(string)
				fmt.Printf("  [%s] %s: %s\n", timestamp, level, message)
			}
		}
	}
}

// handleJobLogs shows the logs of a job
func handleJobLogs() {
	if len(os.Args) < 4 {
		fmt.Fprintf(os.Stderr, "Error: job logs requires a job ID\n")
		fmt.Fprintf(os.Stderr, "Usage: %s job logs <job-id>\n", os.Args[0])
		os.Exit(1)
	}

	handleJobStatus() // Logs are included in status output
}

// handleJobCancel cancels a running or queued job
func handleJobCancel() {
	if len(os.Args) < 4 {
		fmt.Fprintf(os.Stderr, "Error: job cancel requires a job ID\n")
		fmt.Fprintf(os.Stderr, "Usage: %s job cancel <job-id>\n", os.Args[0])
		os.Exit(1)
	}

	jobID := os.Args[3]
	serverHTTP := getServerHTTPURL()

	reqBody := map[string]string{"action": "cancel"}
	reqJSON, _ := json.Marshal(reqBody)
	resp, err := http.Post(serverHTTP+"/api/jobs/"+jobID, "application/json", strings.NewReader(string(reqJSON)))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error canceling job: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		fmt.Fprintf(os.Stderr, "Error: server returned status %d: %s\n", resp.StatusCode, string(body))
		os.Exit(1)
	}

	fmt.Printf("Job %s canceled successfully\n", jobID)
}

// handleJobList lists all jobs
func handleJobList() {
	serverHTTP := getServerHTTPURL()

	resp, err := http.Get(serverHTTP + "/api/jobs")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error fetching jobs: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "Error: server returned status %d\n", resp.StatusCode)
		os.Exit(1)
	}

	var result struct {
		Jobs  []map[string]interface{} `json:"jobs"`
		Count int                     `json:"count"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		fmt.Fprintf(os.Stderr, "Error decoding response: %v\n", err)
		os.Exit(1)
	}

	if *format == "json" {
		printJSON(result)
		return
	}

	if result.Count == 0 {
		fmt.Println("No jobs found")
		return
	}

	fmt.Printf("Jobs (%d total):\n\n", result.Count)
	for _, job := range result.Jobs {
		fmt.Printf("ID: %s\n", job["id"])
		fmt.Printf("  Status: %s\n", job["status"])
		fmt.Printf("  Prompt: %s\n", job["prompt"])
		fmt.Printf("  Created: %s\n", job["created_at"])
		if startedAt, ok := job["started_at"].(string); ok && startedAt != "" {
			fmt.Printf("  Started: %s\n", startedAt)
		}
		if finishedAt, ok := job["finished_at"].(string); ok && finishedAt != "" {
			fmt.Printf("  Finished: %s\n", finishedAt)
		}
		fmt.Println()
	}
}

// handleJobDelete deletes a job
func handleJobDelete() {
	if len(os.Args) < 4 {
		fmt.Fprintf(os.Stderr, "Error: job delete requires a job ID\n")
		fmt.Fprintf(os.Stderr, "Usage: %s job delete <job-id>\n", os.Args[0])
		os.Exit(1)
	}

	jobID := os.Args[3]
	serverHTTP := getServerHTTPURL()

	req, err := http.NewRequest(http.MethodDelete, serverHTTP+"/api/jobs/"+jobID, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating request: %v\n", err)
		os.Exit(1)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error deleting job: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		fmt.Fprintf(os.Stderr, "Error: server returned status %d: %s\n", resp.StatusCode, string(body))
		os.Exit(1)
	}

	fmt.Printf("Job %s deleted successfully\n", jobID)
}

// getServerHTTPURL converts WebSocket URL to HTTP URL
func getServerHTTPURL() string {
	wsURL := *serverURL
	u, err := url.Parse(wsURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing server URL: %v\n", err)
		os.Exit(1)
	}

	scheme := "http"
	if u.Scheme == "wss" {
		scheme = "https"
	}

	return fmt.Sprintf("%s://%s", scheme, u.Host)
}
