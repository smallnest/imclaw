package gateway

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/smallnest/imclaw/internal/agent"
	"github.com/smallnest/imclaw/internal/session"
)

// Config represents the server configuration
type Config struct {
	Host      string
	Port      int
	Timeout   int
	AuthToken string
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// Server represents the gateway server
type Server struct {
	config     *Config
	sessionMgr *session.Manager
	agentMgr   *agent.Manager

	httpServer *http.Server

	connections   map[string]*WSConnection
	connectionsMu sync.RWMutex

	running bool
	mu      sync.RWMutex
}

// WSConnection represents a WebSocket connection
type WSConnection struct {
	*websocket.Conn
	ID string
	mu sync.Mutex
}

// NewServer creates a new gateway server
func NewServer(cfg *Config, sessionMgr *session.Manager, agentMgr *agent.Manager) *Server {
	return &Server{
		config:      cfg,
		sessionMgr:  sessionMgr,
		agentMgr:    agentMgr,
		connections: make(map[string]*WSConnection),
	}
}

// Start starts the gateway server
func (s *Server) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return fmt.Errorf("server already running")
	}
	s.running = true
	s.mu.Unlock()

	// Start server
	go s.startServer(ctx)

	// Watch for context cancellation
	go func() {
		<-ctx.Done()
		_ = s.Stop()
	}()

	return nil
}

func (s *Server) startServer(ctx context.Context) {
	mux := http.NewServeMux()

	// HTTP endpoints
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/api/sessions", s.handleSessionsAPI)
	mux.HandleFunc("/api/agents", s.handleAgentsAPI)
	mux.HandleFunc("/rpc", s.handleJSONRPC)

	// WebSocket endpoint
	mux.HandleFunc("/ws", s.handleWebSocket)

	s.httpServer = &http.Server{
		Addr:    fmt.Sprintf("%s:%d", s.config.Host, s.config.Port),
		Handler: mux,
	}

	go func() {
		log.Printf("[gateway] Server listening on %s", s.httpServer.Addr)
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("Server error: %v", err)
		}
	}()
}

// Stop stops the gateway server
func (s *Server) Stop() error {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return nil
	}
	s.running = false
	s.mu.Unlock()

	// Close all WebSocket connections
	s.closeAllConnections()

	// Shutdown server
	if s.httpServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.httpServer.Shutdown(ctx)
	}

	return nil
}

func (s *Server) closeAllConnections() {
	s.connectionsMu.Lock()
	defer s.connectionsMu.Unlock()

	for _, conn := range s.connections {
		conn.Close()
	}
	s.connections = make(map[string]*WSConnection)
}

// handleHealth handles health check requests
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "ok",
		"time":   time.Now().Unix(),
	})
}

// handleSessionsAPI handles sessions API requests
func (s *Server) handleSessionsAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	sessions := s.sessionMgr.List()
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"sessions": sessions,
		"count":    len(sessions),
	})
}

// handleAgentsAPI handles agents API requests
func (s *Server) handleAgentsAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	agents := s.agentMgr.List()
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"agents": agents,
		"count":  len(agents),
	})
}

// handleJSONRPC handles JSON-RPC requests
func (s *Server) handleJSONRPC(w http.ResponseWriter, r *http.Request) {
	// Check auth
	if s.config.AuthToken != "" && !s.authenticateHTTP(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req JSONRPCRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(JSONRPCResponse{
			JSONRPC: "2.0",
			Error: &JSONRPCError{
				Code:    -32700,
				Message: "Parse error",
			},
		})
		return
	}

	resp := s.handleRPCRequest("", &req)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// handleWebSocket handles WebSocket connections
func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	// Check auth
	if s.config.AuthToken != "" && !s.authenticateWS(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}

	wsConn := &WSConnection{
		Conn: conn,
		ID:   uuid.New().String(),
	}

	s.connectionsMu.Lock()
	s.connections[wsConn.ID] = wsConn
	s.connectionsMu.Unlock()

	// Send welcome message
	welcome := JSONRPCRequest{
		JSONRPC: "2.0",
		Method:  "connected",
		Params: map[string]interface{}{
			"session_id": wsConn.ID,
		},
	}
	_ = wsConn.SendJSON(welcome)

	// Handle messages
	go s.handleWSMessages(wsConn)
}

func (s *Server) authenticateHTTP(r *http.Request) bool {
	auth := r.Header.Get("Authorization")
	if len(auth) > 7 && auth[:7] == "Bearer " {
		token := auth[7:]
		return subtle.ConstantTimeCompare([]byte(token), []byte(s.config.AuthToken)) == 1
	}
	return false
}

func (s *Server) authenticateWS(r *http.Request) bool {
	token := r.URL.Query().Get("token")
	if token == "" {
		auth := r.Header.Get("Authorization")
		if len(auth) > 7 && auth[:7] == "Bearer " {
			token = auth[7:]
		}
	}

	if token == "" {
		return false
	}

	return subtle.ConstantTimeCompare([]byte(token), []byte(s.config.AuthToken)) == 1
}

func (s *Server) handleWSMessages(conn *WSConnection) {
	defer func() {
		conn.Close()
		s.connectionsMu.Lock()
		delete(s.connections, conn.ID)
		s.connectionsMu.Unlock()
	}()

	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			break
		}

		var req JSONRPCRequest
		if err := json.Unmarshal(data, &req); err != nil {
			errorResp := JSONRPCResponse{
				JSONRPC: "2.0",
				Error: &JSONRPCError{
					Code:    -32700,
					Message: "Parse error",
				},
			}
			_ = conn.SendJSON(errorResp)
			continue
		}

		// Handle ask_stream specially for streaming responses
		if req.Method == "ask_stream" {
			s.handleAskStream(conn, &req)
			continue
		}

		resp := s.handleRPCRequest(conn.ID, &req)
		_ = conn.SendJSON(resp)
	}
}

func (s *Server) handleRPCRequest(connID string, req *JSONRPCRequest) *JSONRPCResponse {
	switch req.Method {
	case "ask":
		return s.handleAsk(connID, req)
	case "ask_stream":
		// This is handled separately in handleWSMessages for streaming
		return &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &JSONRPCError{
				Code:    -32601,
				Message: "ask_stream requires WebSocket connection",
			},
		}
	case "session.init":
		return s.handleSessionInit(connID, req)
	case "session.new":
		return s.handleSessionNew(connID, req)
	case "session.get":
		return s.handleSessionGet(connID, req)
	case "session.list":
		return s.handleSessionList(connID, req)
	case "session.delete":
		return s.handleSessionDelete(connID, req)
	case "agents.list":
		return s.handleAgentsList(connID, req)
	default:
		return &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &JSONRPCError{
				Code:    -32601,
				Message: "Method not found",
			},
		}
	}
}

// handleAsk handles direct ask requests
func (s *Server) handleAsk(connID string, req *JSONRPCRequest) *JSONRPCResponse {
	params, ok := req.Params.(map[string]interface{})
	if !ok {
		return &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &JSONRPCError{
				Code:    -32602,
				Message: "Invalid params",
			},
		}
	}

	content, _ := params["content"].(string)
	agentType, _ := params["agent"].(string)
	specifiedSessionID := getStringParam(params, "session_id")

	if content == "" {
		return &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &JSONRPCError{
				Code:    -32602,
				Message: "Missing required param: content",
			},
		}
	}

	// Parse prompt options from params
	opts := &agent.PromptOptions{
		Permissions:         getStringParam(params, "permissions"),
		Format:              getStringParam(params, "format"),
		Cwd:                 getStringParam(params, "cwd"),
		AuthPolicy:          getStringParam(params, "auth_policy"),
		NonInteractivePerms: getStringParam(params, "non_interactive_permissions"),
		SuppressReads:       getBoolParam(params, "suppress_reads"),
		Model:               getStringParam(params, "model"),
		AllowedTools:        getStringParam(params, "allowed_tools"),
		MaxTurns:            getIntParam(params, "max_turns"),
		PromptRetries:       getIntParam(params, "prompt_retries"),
		Timeout:             getIntParam(params, "timeout"),
		TTL:                 getIntParam(params, "ttl"),
	}

	// Determine session ID
	var sessionID string
	if specifiedSessionID != "" {
		sessionID = specifiedSessionID
	} else {
		sessionID = connID
		if connID == "" {
			sessionID = "default"
		}
	}

	// Check for special commands
	if strings.HasPrefix(content, "/agent ") {
		newAgent := strings.TrimSpace(strings.TrimPrefix(content, "/agent "))
		sess, _ := s.sessionMgr.Get("cli", sessionID)
		if sess != nil {
			sess.AgentName = newAgent
			s.sessionMgr.Update(sess)
		}
		return &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]interface{}{
				"agent":   newAgent,
				"message": fmt.Sprintf("Switched to agent: %s", newAgent),
			},
		}
	}

	if content == "/new" {
		sess := s.sessionMgr.Create("cli", "", sessionID, agentType)
		return &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]interface{}{
				"session_id": sess.ID,
				"message":    "New session created",
			},
		}
	}

	// Get or create session
	sess := s.sessionMgr.GetOrCreate("cli", "", sessionID, agentType)

	// Determine agent type from session or params
	effectiveAgentType := agentType
	if sess.AgentName != "" {
		effectiveAgentType = sess.AgentName
	}

	// Get or create agent
	ag := s.agentMgr.GetOrCreate(effectiveAgentType)
	log.Printf("[gateway] Using agent: %s (requested: %s, session: %s)", ag.Type(), agentType, sess.AgentName)

	// Ensure agent session exists
	agentSessionID := sess.AgentSession
	if agentSessionID == "" {
		// Use session ID as acpx session name, and store it for later use
		// acpx returns a different internal ID, but we need to use the name for prompts
		id, err := ag.EnsureSession(context.Background(), sess.ID)
		if err != nil {
			return &JSONRPCResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error: &JSONRPCError{
					Code:    -32603,
					Message: fmt.Sprintf("Failed to create agent session: %v", err),
				},
			}
		}
		// Store the session name (our ID), not acpx's internal ID
		// We use the name for subsequent prompts
		agentSessionID = sess.ID
		sess.SetAgentSession(agentSessionID)
		log.Printf("[gateway] Created agent session, name=%s, acpx_id=%s", sess.ID, id)
		s.sessionMgr.Update(sess)
	}

	// Send prompt to agent with options
	response, err := ag.PromptWithOptions(context.Background(), agentSessionID, content, opts)
	if err != nil {
		return &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &JSONRPCError{
				Code:    -32603,
				Message: fmt.Sprintf("Agent error: %v", err),
			},
		}
	}

	return &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]interface{}{
			"content":    response,
			"session_id": sess.ID,
			"agent":      ag.Type(),
		},
	}
}

// handleAskStream handles streaming ask requests over WebSocket
func (s *Server) handleAskStream(conn *WSConnection, req *JSONRPCRequest) {
	params, ok := req.Params.(map[string]interface{})
	if !ok {
		_ = conn.SendJSON(JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &JSONRPCError{
				Code:    -32602,
				Message: "Invalid params",
			},
		})
		return
	}

	content, _ := params["content"].(string)
	agentType, _ := params["agent"].(string)
	specifiedSessionID := getStringParam(params, "session_id")

	if content == "" {
		_ = conn.SendJSON(JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &JSONRPCError{
				Code:    -32602,
				Message: "Missing required param: content",
			},
		})
		return
	}

	// Parse prompt options from params
	opts := &agent.PromptOptions{
		Permissions:         getStringParam(params, "permissions"),
		Format:              getStringParam(params, "format"),
		Cwd:                 getStringParam(params, "cwd"),
		AuthPolicy:          getStringParam(params, "auth_policy"),
		NonInteractivePerms: getStringParam(params, "non_interactive_permissions"),
		SuppressReads:       getBoolParam(params, "suppress_reads"),
		Model:               getStringParam(params, "model"),
		AllowedTools:        getStringParam(params, "allowed_tools"),
		MaxTurns:            getIntParam(params, "max_turns"),
		PromptRetries:       getIntParam(params, "prompt_retries"),
		Timeout:             getIntParam(params, "timeout"),
		TTL:                 getIntParam(params, "ttl"),
	}

	// Determine session ID
	var sessionID string
	if specifiedSessionID != "" {
		sessionID = specifiedSessionID
	} else {
		sessionID = conn.ID
		if conn.ID == "" {
			sessionID = "default"
		}
	}

	// Get or create session
	sess := s.sessionMgr.GetOrCreate("cli", "", sessionID, agentType)

	// Determine agent type from session or params
	effectiveAgentType := agentType
	if sess.AgentName != "" {
		effectiveAgentType = sess.AgentName
	}

	// Get or create agent
	ag := s.agentMgr.GetOrCreate(effectiveAgentType)
	log.Printf("[gateway] Streaming with agent: %s (requested: %s, session: %s)", ag.Type(), agentType, sess.AgentName)

	// Ensure agent session exists
	agentSessionID := sess.AgentSession
	if agentSessionID == "" {
		id, err := ag.EnsureSession(context.Background(), sess.ID)
		if err != nil {
			_ = conn.SendJSON(JSONRPCResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error: &JSONRPCError{
					Code:    -32603,
					Message: fmt.Sprintf("Failed to create agent session: %v", err),
				},
			})
			return
		}
		agentSessionID = sess.ID
		sess.SetAgentSession(agentSessionID)
		log.Printf("[gateway] Created agent session, name=%s, acpx_id=%s", sess.ID, id)
		s.sessionMgr.Update(sess)
	}

	// Create cancellable context for this request
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start streaming
	stream, err := ag.PromptStream(ctx, agentSessionID, content, opts)
	if err != nil {
		_ = conn.SendJSON(JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &JSONRPCError{
				Code:    -32603,
				Message: fmt.Sprintf("Failed to start stream: %v", err),
			},
		})
		return
	}

	// Stream chunks to client
	var fullContent strings.Builder
	for chunk := range stream {
		fullContent.WriteString(chunk.Content)
		fullContent.WriteString("\n")

		// Send streaming notification
		notification := JSONRPCRequest{
			JSONRPC: "2.0",
			Method:  "stream",
			Params: map[string]interface{}{
				"id":      req.ID,
				"type":    chunk.Type,
				"content": chunk.Content,
			},
		}
		if err := conn.SendJSON(notification); err != nil {
			// WebSocket connection failed, cancel the context
			log.Printf("[gateway] WebSocket send failed: %v, cancelling stream", err)
			cancel()
			return
		}
	}

	// Send final response
	_ = conn.SendJSON(JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]interface{}{
			"content":    strings.TrimSuffix(fullContent.String(), "\n"),
			"session_id": sess.ID,
			"agent":      ag.Type(),
		},
	})
}

// Helper functions to parse params
func getStringParam(params map[string]interface{}, key string) string {
	if v, ok := params[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func getBoolParam(params map[string]interface{}, key string) bool {
	if v, ok := params[key]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return false
}

func getIntParam(params map[string]interface{}, key string) int {
	if v, ok := params[key]; ok {
		switch n := v.(type) {
		case int:
			return n
		case int64:
			return int(n)
		case float64:
			return int(n)
		}
	}
	return 0
}

func (s *Server) handleSessionInit(connID string, req *JSONRPCRequest) *JSONRPCResponse {
	params, _ := req.Params.(map[string]interface{})
	specifiedSessionID := getStringParam(params, "session_id")
	agentType := getStringParam(params, "agent")

	// Determine session ID
	var sessionID string
	if specifiedSessionID != "" {
		sessionID = specifiedSessionID
	} else {
		sessionID = connID
		if connID == "" {
			sessionID = "default"
		}
	}

	// Get or create session
	sess := s.sessionMgr.GetOrCreate("cli", "", sessionID, agentType)

	return &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]interface{}{
			"session_id":   sess.ID,
			"agent":        sess.AgentName,
			"created_at":   sess.CreatedAt,
			"last_active":  sess.LastActive,
		},
	}
}

func (s *Server) handleSessionNew(connID string, req *JSONRPCRequest) *JSONRPCResponse {
	params, ok := req.Params.(map[string]interface{})
	if !ok {
		return &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &JSONRPCError{
				Code:    -32602,
				Message: "Invalid params",
			},
		}
	}

	agentType, _ := params["agent"].(string)

	// chatID is connID (SessionKey will add "cli:" prefix)
	sessionID := connID
	if connID == "" {
		sessionID = "default"
	}
	sess := s.sessionMgr.Create("cli", "", sessionID, agentType)

	return &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  sess,
	}
}

func (s *Server) handleSessionGet(connID string, req *JSONRPCRequest) *JSONRPCResponse {
	// Get session_id from params if specified
	var sessionID string
	if params, ok := req.Params.(map[string]interface{}); ok {
		if specifiedID := getStringParam(params, "session_id"); specifiedID != "" {
			sessionID = specifiedID
		}
	}

	// Fall back to connID
	if sessionID == "" {
		sessionID = connID
		if connID == "" {
			sessionID = "default"
		}
	}

	sess, ok := s.sessionMgr.Get("cli", sessionID)
	if !ok {
		return &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  nil,
		}
	}

	return &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  sess,
	}
}

func (s *Server) handleSessionList(connID string, req *JSONRPCRequest) *JSONRPCResponse {
	sessions := s.sessionMgr.List()
	return &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]interface{}{
			"sessions": sessions,
		},
	}
}

func (s *Server) handleSessionDelete(connID string, req *JSONRPCRequest) *JSONRPCResponse {
	params, ok := req.Params.(map[string]interface{})
	if !ok {
		return &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &JSONRPCError{
				Code:    -32602,
				Message: "Invalid params",
			},
		}
	}

	sessionID, _ := params["session_id"].(string)
	s.sessionMgr.Delete("cli", sessionID)

	return &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]interface{}{
			"success": true,
		},
	}
}

func (s *Server) handleAgentsList(connID string, req *JSONRPCRequest) *JSONRPCResponse {
	agents := s.agentMgr.List()
	return &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]interface{}{
			"agents": agents,
		},
	}
}

// SendJSON sends a JSON message
func (c *WSConnection) SendJSON(v interface{}) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.WriteJSON(v)
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
