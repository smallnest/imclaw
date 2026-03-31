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
	flag "github.com/spf13/pflag"
)

var (
	// Server connection (HTTP and WebSocket on same port)
	serverURL = flag.String("server", "ws://localhost:8080/ws", "IMClaw server WebSocket URL")
	authToken = flag.String("token", "", "Authentication token")

	// Session
	sessionID = flag.String("session", "", "Session ID to use (empty for auto-create)")

	// One-shot prompt
	promptFlag = flag.StringP("prompt", "p", "", "Prompt message (one-shot mode)")

	// Agent selection
	agentType = flag.String("agent", "", "Agent type (claude, codex, etc.)")

	// Working directory
	cwd = flag.String("cwd", "", "Working directory")

	// Permission flags
	approveAll   = flag.Bool("approve-all", false, "Auto-approve all permission requests")
	approveReads = flag.Bool("approve-reads", false, "Auto-approve read/search requests and prompt for writes")
	denyAll      = flag.Bool("deny-all", false, "Deny all permission requests")

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
	allowedTools = flag.String("allowed-tools", "Bash,Read,Write", "Allowed tool names (comma-separated). Empty=allow all, \"\"=no tools")

	// Session control
	maxTurns      = flag.Int("max-turns", 0, "Maximum turns for the session")
	promptRetries = flag.Int("prompt-retries", 0, "Retry failed prompt turns on transient errors")

	// JSON strict mode
	jsonStrict = flag.Bool("json-strict", false, "Strict JSON mode: requires --format json and suppresses non-JSON stderr output")

	// Timeouts
	timeout = flag.Int("timeout", 0, "Maximum time to wait for agent response (seconds)")
	ttl     = flag.Int("ttl", 0, "Queue owner idle TTL before shutdown (seconds)")

	// Verbose
	verbose = flag.Bool("verbose", false, "Enable verbose debug logs")

	// Version
	showVersion = flag.Bool("version", false, "Show version information")

	// 版本信息
	Version   = "dev"
	BuildTime = "unknown"
)

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "IMClaw CLI - Command line interface for IMClaw\n\n")
		fmt.Fprintf(os.Stderr, "Usage: %s [options] [-p <message> | <message>]\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "If message is provided (-p or positional), sends it and exits.\n")
		fmt.Fprintf(os.Stderr, "If no message, starts interactive REPL mode.\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  # Interactive mode\n")
		fmt.Fprintf(os.Stderr, "  %s\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  # One-shot mode with -p\n")
		fmt.Fprintf(os.Stderr, "  %s -p \"What is Go?\"\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  # One-shot mode with positional argument\n")
		fmt.Fprintf(os.Stderr, "  %s \"What is Go?\"\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  # Use specific agent\n")
		fmt.Fprintf(os.Stderr, "  %s --agent codex -p \"Hello\"\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  # JSON output with auto-approve\n")
		fmt.Fprintf(os.Stderr, "  %s --format json --approve-all -p \"Hello\"\n", os.Args[0])
	}

	flag.Parse()

	if *showVersion {
		fmt.Printf("IMClaw CLI %s\n", Version)
		fmt.Printf("Build Time: %s\n", BuildTime)
		os.Exit(0)
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

// getPermissions returns the permission mode based on flags
func getPermissions() string {
	if *approveAll {
		return "approve-all"
	}
	if *denyAll {
		return "deny-all"
	}
	// approve-reads is default
	return "approve-reads"
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

	params := map[string]interface{}{
		"content": content,
	}

	// Add session ID if specified
	if *sessionID != "" {
		params["session_id"] = *sessionID
	}

	// Add agent
	if *agentType != "" {
		params["agent"] = *agentType
	}

	// Add permissions
	params["permissions"] = getPermissions()

	// Add format
	params["format"] = *format

	// Add cwd
	if *cwd != "" {
		params["cwd"] = *cwd
	}

	// Add auth policy
	if *authPolicy != "" {
		params["auth_policy"] = *authPolicy
	}

	// Add non-interactive permissions
	if *nonInteractivePerms != "" {
		params["non_interactive_permissions"] = *nonInteractivePerms
	}

	// Add suppress reads
	if *suppressReads {
		params["suppress_reads"] = true
	}

	// Add model
	if *model != "" {
		params["model"] = *model
	}

	// Add allowed tools
	if *allowedTools != "" {
		params["allowed_tools"] = *allowedTools
	}

	// Add max turns
	if *maxTurns > 0 {
		params["max_turns"] = *maxTurns
	}

	// Add prompt retries
	if *promptRetries > 0 {
		params["prompt_retries"] = *promptRetries
	}

	// Add timeout
	if *timeout > 0 {
		params["timeout"] = *timeout
	}

	// Add ttl
	if *ttl > 0 {
		params["ttl"] = *ttl
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

// sendAndExit sends a single message and exits
func sendAndExit(client *Client, message string) {
	if err := client.Connect(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	defer client.Close()

	resp, err := client.Ask(message)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if resp.Error != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", resp.Error.Message)
		os.Exit(1)
	}

	if result, ok := resp.Result.(map[string]interface{}); ok {
		if content, ok := result["content"].(string); ok {
			fmt.Println(content)
		} else {
			printJSON(result)
		}
	} else {
		printJSON(resp.Result)
	}
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
		fmt.Fprintf(os.Stderr, "Error: %s\n", initResp.Error.Message)
	} else if result, ok := initResp.Result.(map[string]interface{}); ok {
		if sid, ok := result["session_id"].(string); ok {
			fmt.Printf("Session: %s", sid)
			if agent, ok := result["agent"].(string); ok && agent != "" {
				fmt.Printf(" | Agent: %s", agent)
			}
			fmt.Println()
		}
	}

	fmt.Printf("Permissions: %s | Format: %s\n", getPermissions(), *format)
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
					fmt.Fprintf(os.Stderr, "Error: %s\n", resp.Error.Message)
					continue
				}
				fmt.Println("New session created. Context cleared.")
				continue
			}

			// Send message
			resp, err := client.Ask(line)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				continue
			}

			if resp.Error != nil {
				fmt.Fprintf(os.Stderr, "Error: %s\n", resp.Error.Message)
				continue
			}

			// Print response
			fmt.Println()
			if result, ok := resp.Result.(map[string]interface{}); ok {
				if content, ok := result["content"].(string); ok {
					fmt.Println(content)
				} else {
					printJSON(result)
				}
			} else {
				printJSON(resp.Result)
			}
			fmt.Println()
		}
	}
}

func printHelp() {
	fmt.Println(`
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
		fmt.Fprintf(os.Stderr, "Error: %s\n", resp.Error.Message)
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
