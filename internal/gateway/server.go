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
	"github.com/smallnest/imclaw/internal/event"
	"github.com/smallnest/imclaw/internal/job"
	"github.com/smallnest/imclaw/internal/session"
)

// Config represents the server configuration.
type Config struct {
	Host      string
	Port      int
	Timeout   int
	AuthToken string
	DevMode   bool // Enable development mode for hot-reload UI
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// Server represents the gateway server.
type Server struct {
	config     *Config
	sessionMgr *session.Manager
	agentMgr   *agent.Manager
	jobMgr     *job.Manager
	uiHandler  *uiHandler

	httpServer *http.Server

	connections   map[string]*WSConnection
	connectionsMu sync.RWMutex

	hub *StreamHub

	running bool
	mu      sync.RWMutex
}

// WSConnection represents a WebSocket connection.
type WSConnection struct {
	*websocket.Conn
	ID       string
	ctx      context.Context
	cancel   context.CancelFunc
	streamWG sync.WaitGroup
	mu       sync.Mutex
}

const (
	defaultSessionChannel = "cli"
	maxWSConnections      = 1000
	wsWriteWait           = 10 * time.Second
	wsPongWait            = 60 * time.Second
	wsPingPeriod          = (wsPongWait * 9) / 10
)

// StreamEvent represents a structured event in the stream.
type StreamEvent = agent.Event

// NewServer creates a new gateway server.
func NewServer(cfg *Config, sessionMgr *session.Manager, agentMgr *agent.Manager, jobMgr *job.Manager) *Server {
	return &Server{
		config:      cfg,
		sessionMgr:  sessionMgr,
		agentMgr:    agentMgr,
		jobMgr:      jobMgr,
		uiHandler:   newUIHandler(cfg != nil && cfg.DevMode),
		connections: make(map[string]*WSConnection),
		hub:         NewStreamHub(),
	}
}

// Start starts the gateway server.
func (s *Server) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return fmt.Errorf("server already running")
	}
	s.running = true
	s.mu.Unlock()

	go s.startServer(ctx)
	go func() {
		<-ctx.Done()
		_ = s.Stop()
	}()

	return nil
}

func (s *Server) startServer(ctx context.Context) {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/api/auth/check", s.handleAuthCheck)
	mux.HandleFunc("/api/auth/verify", s.handleAuthVerify)
	mux.HandleFunc("/api/sessions", s.handleSessionsAPI)
	mux.HandleFunc("/api/sessions/export/", s.handleSessionExportAPI)
	mux.HandleFunc("/api/sessions/import", s.handleSessionImportAPI)
	mux.HandleFunc("/api/sessions/archive/", s.handleSessionArchiveAPI)
	mux.HandleFunc("/api/sessions/", s.handleSessionDetailAPI)
	mux.HandleFunc("/api/agents", s.handleAgentsAPI)
	mux.HandleFunc("/api/jobs", s.handleJobsAPI)
	mux.HandleFunc("/api/jobs/", s.handleJobDetailAPI)
	mux.HandleFunc("/rpc", s.handleJSONRPC)
	mux.HandleFunc("/ws", s.handleWebSocket)
	mux.HandleFunc("/assets/", s.handleUIAssets)
	mux.HandleFunc("/", s.handleUI)
	// Build info endpoint
	mux.HandleFunc("/api/build", s.handleBuildInfo)

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

	_ = ctx
}

// Stop stops the gateway server.
func (s *Server) Stop() error {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return nil
	}
	s.running = false
	s.mu.Unlock()

	s.closeAllConnections()
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
		_ = conn.Close()
	}
	s.connections = make(map[string]*WSConnection)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "ok",
		"time":   time.Now().Unix(),
	})
}

// handleAuthCheck returns whether authentication is required
func (s *Server) handleAuthCheck(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"required": s.config.AuthToken != "",
	})
}

// handleAuthVerify verifies the provided token
func (s *Server) handleAuthVerify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"valid": false,
			"error": "invalid request",
		})
		return
	}

	// If no token is configured, always return valid
	if s.config.AuthToken == "" {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"valid": true,
		})
		return
	}

	// Verify the token
	valid := subtle.ConstantTimeCompare([]byte(req.Token), []byte(s.config.AuthToken)) == 1
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"valid": valid,
	})
}

func (s *Server) handleBuildInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(GetBuildInfo())
}

func (s *Server) handleSessionsAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	tag := r.URL.Query().Get("tag")
	includeArchived := r.URL.Query().Get("archived") == "true"

	var summaries []session.SessionSummary
	if tag != "" || !includeArchived {
		summaries = s.sessionMgr.SummariesFiltered(tag, includeArchived)
	} else {
		summaries = s.sessionMgr.Summaries()
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"sessions": summaries,
		"count":    len(summaries),
	})
}

func sessionChannelFromRequest(r *http.Request) string {
	if channel := r.URL.Query().Get("channel"); channel != "" {
		return channel
	}
	return defaultSessionChannel
}

func sessionChannelFromParams(params map[string]interface{}) string {
	if params != nil {
		if channel := getStringParam(params, "channel"); channel != "" {
			return channel
		}
	}
	return defaultSessionChannel
}

func (s *Server) handleSessionDetailAPI(w http.ResponseWriter, r *http.Request) {
	sessionID := strings.TrimPrefix(r.URL.Path, "/api/sessions/")
	if sessionID == "" {
		http.NotFound(w, r)
		return
	}
	channel := sessionChannelFromRequest(r)

	switch r.Method {
	case http.MethodGet:
		sess, ok := s.sessionMgr.Get(channel, sessionID)
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(sess)

	case http.MethodPatch:
		var req struct {
			Name       *string  `json:"name,omitempty"`
			Tags       []string `json:"tags,omitempty"`
			AddTags    []string `json:"add_tags,omitempty"`
			RemoveTags []string `json:"remove_tags,omitempty"`
			Archived   *bool    `json:"archived,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": "invalid request"})
			return
		}

		updates := session.SessionUpdates{
			Name:       req.Name,
			AddTags:    req.AddTags,
			RemoveTags: req.RemoveTags,
			Archived:   req.Archived,
		}
		if req.Tags != nil {
			updates.SetTags = req.Tags
		}
		sess, ok := s.sessionMgr.ApplyUpdates(channel, sessionID, updates)
		if !ok {
			http.NotFound(w, r)
			return
		}

		s.broadcastSession(sess)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(sess)

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleAgentsAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	agents := agent.SupportedAgents()
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"agents": agents,
		"count":  len(agents),
	})
}

func (s *Server) handleSessionExportAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	sessionID := strings.TrimPrefix(r.URL.Path, "/api/sessions/export/")
	if sessionID == "" {
		http.NotFound(w, r)
		return
	}

	channel := sessionChannelFromRequest(r)
	sess, ok := s.sessionMgr.Get(channel, sessionID)
	if !ok {
		http.NotFound(w, r)
		return
	}

	format := r.URL.Query().Get("format")
	if format == "" {
		format = "json"
	}

	data, err := session.ExportSession(sess, session.ExportFormat(format))
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": err.Error()})
		return
	}

	switch format {
	case "markdown":
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=session-%s.md", sessionID))
	default:
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=session-%s.json", sessionID))
	}
	_, _ = w.Write(data)
}

func (s *Server) handleSessionImportAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Data string `json:"data"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": "invalid request"})
		return
	}
	if req.Data == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": "data is required"})
		return
	}

	sess, err := session.ImportSession([]byte(req.Data))
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": fmt.Sprintf("Import failed: %v", err)})
		return
	}

	s.sessionMgr.Update(sess)
	s.broadcastSession(sess)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(sess)
}

func (s *Server) handleSessionArchiveAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	// /api/sessions/archive/ lists archived sessions
	archivedSessions := s.sessionMgr.ListArchived()
	summaries := make([]session.SessionSummary, 0, len(archivedSessions))
	for _, sess := range archivedSessions {
		summaries = append(summaries, sess.Summary())
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"sessions": summaries,
		"count":    len(summaries),
	})
}

func (s *Server) handleJobsAPI(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		w.Header().Set("Content-Type", "application/json")
		summaries := s.jobMgr.Summaries()
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"jobs":  summaries,
			"count": len(summaries),
		})
	case http.MethodPost:
		var req struct {
			Prompt    string `json:"prompt"`
			AgentName string `json:"agent_name"`
			Timeout   int    `json:"timeout"` // Timeout in seconds, 0 means no timeout
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": "invalid request"})
			return
		}
		if req.Prompt == "" {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": "prompt is required"})
			return
		}
		if req.AgentName == "" {
			req.AgentName = "acpx"
		}

		// Convert timeout from seconds to duration
		timeout := time.Duration(req.Timeout) * time.Second

		submittedJob := s.jobMgr.Submit(req.Prompt, req.AgentName, timeout)

		// Start executing the job in background
		go job.ExecuteJob(context.Background(), s.jobMgr, submittedJob.ID, s.executeJobPrompt)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(submittedJob)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleJobDetailAPI(w http.ResponseWriter, r *http.Request) {
	jobID := strings.TrimPrefix(r.URL.Path, "/api/jobs/")
	if jobID == "" {
		http.NotFound(w, r)
		return
	}

	switch r.Method {
	case http.MethodGet:
		job, ok := s.jobMgr.Get(jobID)
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(job)
	case http.MethodDelete:
		if err := s.jobMgr.Delete(jobID); err != nil {
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": err.Error()})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
	case http.MethodPost:
		// Handle job actions (cancel, retry)
		var req struct {
			Action string `json:"action"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		switch req.Action {
		case "cancel":
			if err := s.jobMgr.Cancel(jobID); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": err.Error()})
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
		default:
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": "unknown action"})
		}
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// executeJobPrompt executes a job prompt using the agent manager
func (s *Server) executeJobPrompt(ctx context.Context, prompt string, logFn func(level, msg string)) (string, error) {
	// Create a temporary session for this job
	agentType := "acpx"
	ag := s.agentMgr.GetOrCreate(agentType)

	// Create a unique session ID for this job
	sessionID := uuid.NewString()
	agentSessionID, err := ag.EnsureSession(ctx, sessionID)
	if err != nil {
		return "", fmt.Errorf("failed to create agent session: %w", err)
	}

	logFn("info", fmt.Sprintf("Started execution with agent: %s", agentType))

	// Execute the prompt
	response, err := ag.PromptWithOptions(ctx, agentSessionID, prompt, &agent.PromptOptions{
		NonInteractivePerms: "allow",
	})
	if err != nil {
		logFn("error", fmt.Sprintf("Execution failed: %v", err))
		return "", err
	}

	logFn("info", "Execution completed successfully")
	return response, nil
}

func (s *Server) handleJSONRPC(w http.ResponseWriter, r *http.Request) {
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
			Error:   &JSONRPCError{Code: -32700, Message: "Parse error"},
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(s.handleRPCRequest("", &req))
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	if s.config.AuthToken != "" && !s.authenticateWS(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	s.connectionsMu.RLock()
	if len(s.connections) >= maxWSConnections {
		s.connectionsMu.RUnlock()
		http.Error(w, "Too many connections", http.StatusServiceUnavailable)
		return
	}
	s.connectionsMu.RUnlock()

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	wsConn := &WSConnection{Conn: conn, ID: uuid.NewString(), ctx: ctx, cancel: cancel}
	s.connectionsMu.Lock()
	s.connections[wsConn.ID] = wsConn
	s.connectionsMu.Unlock()

	_ = wsConn.SendJSON(JSONRPCRequest{
		JSONRPC: "2.0",
		Method:  "connected",
		Params: map[string]interface{}{
			"session_id":  wsConn.ID,
			"server_time": time.Now().UTC(),
		},
	})
	_ = wsConn.SendJSON(JSONRPCRequest{
		JSONRPC: "2.0",
		Method:  "session.snapshot",
		Params: map[string]interface{}{
			"sessions": s.sessionMgr.Summaries(),
		},
	})

	go s.handleWSMessages(wsConn)
}

func (s *Server) authenticateHTTP(r *http.Request) bool {
	auth := r.Header.Get("Authorization")
	if len(auth) > 7 && auth[:7] == "Bearer " {
		return subtle.ConstantTimeCompare([]byte(auth[7:]), []byte(s.config.AuthToken)) == 1
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
		s.hub.UnsubscribeAll(conn.ID)
		conn.cancel()
		_ = conn.Close()
		conn.streamWG.Wait()
		s.connectionsMu.Lock()
		delete(s.connections, conn.ID)
		s.connectionsMu.Unlock()
	}()

	_ = conn.SetReadDeadline(time.Now().Add(wsPongWait))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(wsPongWait))
	})

	go s.writePingLoop(conn)

	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			break
		}

		var req JSONRPCRequest
		if err := json.Unmarshal(data, &req); err != nil {
			_ = conn.SendJSON(JSONRPCResponse{
				JSONRPC: "2.0",
				Error:   &JSONRPCError{Code: -32700, Message: "Parse error"},
			})
			continue
		}

		if req.Method == "ask_stream" {
			conn.streamWG.Add(1)
			go func(streamReq JSONRPCRequest) {
				defer conn.streamWG.Done()
				s.handleAskStream(conn, &streamReq)
			}(req)
			continue
		}

		if req.Method == "session.subscribe" {
			s.handleWSSubscribe(conn, &req)
			continue
		}

		_ = conn.SendJSON(s.handleRPCRequest(conn.ID, &req))
	}
}

func (s *Server) handleRPCRequest(connID string, req *JSONRPCRequest) *JSONRPCResponse {
	switch req.Method {
	case "ask":
		return s.handleAsk(connID, req)
	case "ask_stream":
		return &JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Error: &JSONRPCError{Code: -32601, Message: "ask_stream requires WebSocket connection"}}
	case "session.init":
		return s.handleSessionInit(connID, req)
	case "session.new":
		return s.handleSessionNew(connID, req)
	case "session.get":
		return s.handleSessionGet(connID, req)
	case "session.list":
		return s.handleSessionList(connID, req)
	case "session.update":
		return s.handleSessionUpdate(connID, req)
	case "session.delete":
		return s.handleSessionDelete(connID, req)
	case "session.rename":
		return s.handleSessionRename(connID, req)
	case "session.tag":
		return s.handleSessionTag(connID, req)
	case "session.untag":
		return s.handleSessionUntag(connID, req)
	case "session.archive":
		return s.handleSessionArchive(connID, req)
	case "session.unarchive":
		return s.handleSessionUnarchive(connID, req)
	case "session.subscribe":
		return s.handleSessionSubscribe(connID, req)
	case "session.export":
		return s.handleSessionExport(connID, req)
	case "session.import":
		return s.handleSessionImport(connID, req)
	case "agents.list":
		return s.handleAgentsList(connID, req)
	case "job.submit":
		return s.handleJobSubmit(connID, req)
	case "job.get":
		return s.handleJobGet(connID, req)
	case "job.list":
		return s.handleJobList(connID, req)
	case "job.cancel":
		return s.handleJobCancel(connID, req)
	case "job.delete":
		return s.handleJobDelete(connID, req)
	default:
		return &JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Error: &JSONRPCError{Code: -32601, Message: "Method not found"}}
	}
}

func (s *Server) handleAsk(connID string, req *JSONRPCRequest) *JSONRPCResponse {
	params, ok := req.Params.(map[string]interface{})
	if !ok {
		return invalidParams(req.ID)
	}

	content, _ := params["content"].(string)
	agentType, _ := params["agent"].(string)
	if content == "" {
		return missingParam(req.ID, "content")
	}

	sessionID := resolveSessionID(connID, getStringParam(params, "session_id"))
	if strings.HasPrefix(content, "/agent ") {
		newAgent := strings.TrimSpace(strings.TrimPrefix(content, "/agent "))
		sess := s.sessionMgr.GetOrCreate(defaultSessionChannel, "", sessionID, newAgent)
		sess.AgentName = newAgent
		s.sessionMgr.Update(sess)
		s.broadcastSession(sess)
		return &JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]interface{}{"agent": newAgent, "message": fmt.Sprintf("Switched to agent: %s", newAgent), "session_id": sess.ID}}
	}
	if content == "/new" {
		created := s.sessionMgr.Create(defaultSessionChannel, "", uuid.NewString(), agentType)
		s.broadcastSession(created)
		return &JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]interface{}{"session_id": created.ID, "message": "New session created"}}
	}

	opts := parsePromptOptions(params)
	sess := s.prepareSession(sessionID, agentType)
	s.recordPrompt(sess.ID, req.ID, content)

	ag := s.agentMgr.GetOrCreate(sess.AgentName)
	agentSessionID, err := s.ensureAgentSession(sess, ag, req.ID)
	if err != nil {
		return s.rpcAgentError(sess.ID, req.ID, "Failed to create agent session", err)
	}

	response, err := ag.PromptWithOptions(context.Background(), agentSessionID, content, opts)
	if err != nil {
		return s.rpcAgentError(sess.ID, req.ID, "Agent error", err)
	}

	s.recordResult(sess.ID, req.ID, response)
	return &JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]interface{}{"content": response, "session_id": sess.ID, "agent": ag.Type()}}
}

func (s *Server) handleSessionSubscribe(connID string, req *JSONRPCRequest) *JSONRPCResponse {
	params, ok := req.Params.(map[string]interface{})
	if !ok {
		return invalidParams(req.ID)
	}
	sessionID := getStringParam(params, "session_id")
	if sessionID == "" {
		return missingParam(req.ID, "session_id")
	}
	if connID == "" {
		return &JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Error: &JSONRPCError{Code: -32601, Message: "session.subscribe requires WebSocket connection"}}
	}
	// Verify the session exists.
	if _, ok := s.sessionMgr.Get(sessionChannelFromParams(params), sessionID); !ok {
		return &JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Error: &JSONRPCError{Code: -32602, Message: "Session not found"}}
	}

	// The actual subscription relay goroutine is spawned by the WS handler.
	// Store the pending subscription so the WS read loop picks it up.
	// For the JSON-RPC-over-HTTP path we return a confirmation.
	return &JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]interface{}{"subscribed": true, "session_id": sessionID}}
}

// handleWSSubscribe handles a session.subscribe request over WebSocket.
// It registers the connection as a subscriber to the session's live stream
// and starts a goroutine to relay events from the hub to the connection.
func (s *Server) handleWSSubscribe(conn *WSConnection, req *JSONRPCRequest) {
	params, ok := req.Params.(map[string]interface{})
	if !ok {
		_ = conn.SendJSON(invalidParams(req.ID))
		return
	}
	sessionID := getStringParam(params, "session_id")
	if sessionID == "" {
		_ = conn.SendJSON(missingParam(req.ID, "session_id"))
		return
	}
	if _, ok := s.sessionMgr.Get(sessionChannelFromParams(params), sessionID); !ok {
		_ = conn.SendJSON(&JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Error: &JSONRPCError{Code: -32602, Message: "Session not found"}})
		return
	}

	ch := s.hub.Subscribe(sessionID, conn.ID)

	_ = conn.SendJSON(&JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]interface{}{"subscribed": true, "session_id": sessionID}})

	conn.streamWG.Add(1)
	go func() {
		defer conn.streamWG.Done()
		defer s.hub.Unsubscribe(sessionID, conn.ID)
		s.relaySubscription(conn, sessionID, ch)
	}()
}

// relaySubscription forwards events from a subscription channel to a WebSocket connection.
func (s *Server) relaySubscription(conn *WSConnection, sessionID string, ch <-chan HubEvent) {
	for evt := range ch {
		switch {
		case evt.Result != nil:
			_ = conn.SendJSON(evt.Result)
		case evt.Error != nil:
			_ = conn.SendJSON(evt.Error)
		case evt.Event.Type != "":
			_ = conn.SendJSON(newEventNotification(sessionID, "", evt.Event))
		case evt.Chunk.Type != "":
			_ = conn.SendJSON(JSONRPCRequest{
				JSONRPC: "2.0",
				Method:  "stream",
				Params: map[string]interface{}{
					"id":         evt.Chunk.ID,
					"session_id": evt.Chunk.SessionID,
					"type":       evt.Chunk.Type,
					"content":    evt.Chunk.Content,
				},
			})
		}
	}
}

func (s *Server) handleAskStream(conn *WSConnection, req *JSONRPCRequest) {
	params, ok := req.Params.(map[string]interface{})
	if !ok {
		_ = conn.SendJSON(invalidParams(req.ID))
		return
	}

	content, _ := params["content"].(string)
	agentType, _ := params["agent"].(string)
	if content == "" {
		_ = conn.SendJSON(missingParam(req.ID, "content"))
		return
	}

	sessionID := resolveSessionID(conn.ID, getStringParam(params, "session_id"))
	opts := parsePromptOptions(params)
	sess := s.prepareSession(sessionID, agentType)
	s.recordPrompt(sess.ID, req.ID, content)

	ag := s.agentMgr.GetOrCreate(sess.AgentName)
	agentSessionID, err := s.ensureAgentSession(sess, ag, req.ID)
	if err != nil {
		_ = conn.SendJSON(s.rpcAgentError(sess.ID, req.ID, "Failed to create agent session", err))
		return
	}

	// Use a standalone context not tied to a single connection so that
	// the stream survives if the originating client disconnects.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stream, err := ag.PromptStream(ctx, agentSessionID, content, opts)
	if err != nil {
		rpcErr := s.rpcAgentError(sess.ID, req.ID, "Failed to start stream", err)
		_ = conn.SendJSON(rpcErr)
		return
	}

	var fullContent strings.Builder
	var streamErr string
	var finalOutput string
	var sawFinalOutput bool
	parser := event.NewParser()
	sawNativeEvents := false
	sawErrorEvent := false

	publishAndSend := func(evt agent.Event) {
		s.recordEvent(sess.ID, req.ID, evt)
		// Fan-out to all hub subscribers.
		s.hub.Publish(sess.ID, HubEvent{Event: evt})
		// Also send directly to the originating connection.
		if err := conn.SendJSON(newEventNotification(sess.ID, req.ID, evt)); err != nil {
			log.Printf("[gateway] WebSocket send failed: %v", err)
		}
	}

	for chunk := range stream {
		applyStreamChunk(&fullContent, &streamErr, chunk)
		if len(chunk.Events) > 0 {
			sawNativeEvents = true
		}

		for _, evt := range buildStructuredEvents(parser, chunk) {
			if evt.Type == agent.TypeError {
				sawErrorEvent = true
			}
			if evt.Type == agent.TypeOutputFinal {
				finalOutput = evt.Content
				sawFinalOutput = true
			}
			publishAndSend(evt)
		}

		// Strip ANSI escape sequences from content before sending to WebSocket
		cleanContent := event.StripANSI(chunk.Content)
		chunkMsg := StreamChunkMsg{ID: req.ID, SessionID: sess.ID, Type: chunk.Type, Content: cleanContent}
		s.hub.Publish(sess.ID, HubEvent{Chunk: chunkMsg})
		if err := conn.SendJSON(JSONRPCRequest{JSONRPC: "2.0", Method: "stream", Params: map[string]interface{}{"id": req.ID, "session_id": sess.ID, "type": chunk.Type, "content": cleanContent}}); err != nil {
			log.Printf("[gateway] WebSocket send failed: %v", err)
		}
	}

	if !sawNativeEvents {
		for _, evt := range flushStructuredEvents(parser, streamErr == "") {
			if evt.Type == agent.TypeError {
				sawErrorEvent = true
			}
			if evt.Type == agent.TypeOutputFinal {
				finalOutput = evt.Content
				sawFinalOutput = true
			}
			publishAndSend(evt)
		}
	}

	if streamErr != "" {
		if !sawErrorEvent {
			s.recordError(sess.ID, req.ID, streamErr)
		}
		errResp := JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Error: &JSONRPCError{Code: -32603, Message: fmt.Sprintf("Agent error: %s", streamErr)}}
		s.hub.Publish(sess.ID, HubEvent{Error: &errResp})
		_ = conn.SendJSON(errResp)
		return
	}

	// Prefer the protocol-level final output when available. Aggregating raw stream
	// content can include thinking transcript text for native-event agents.
	finalContent := filterTranscriptMarkers(event.StripANSI(fullContent.String()))
	if sawFinalOutput {
		finalContent = filterTranscriptMarkers(event.StripANSI(finalOutput))
	}
	s.recordResult(sess.ID, req.ID, finalContent)
	resultResp := JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]interface{}{"content": finalContent, "session_id": sess.ID, "agent": ag.Type()}}
	s.hub.Publish(sess.ID, HubEvent{Result: &resultResp})
	_ = conn.SendJSON(resultResp)
}

func (s *Server) prepareSession(sessionID, agentType string) *session.Session {
	sess := s.sessionMgr.GetOrCreate(defaultSessionChannel, "", sessionID, agentType)
	if agentType != "" && sess.AgentName != agentType {
		sess.AgentName = agentType
		s.sessionMgr.Update(sess)
		sess, _ = s.sessionMgr.Get(defaultSessionChannel, sessionID)
	}
	s.broadcastSession(sess)
	return sess
}

func (s *Server) ensureAgentSession(sess *session.Session, ag agent.Agent, requestID string) (string, error) {
	agentSessionHandle := sess.AgentSessionHandle
	if agentSessionHandle != "" {
		return agentSessionHandle, nil
	}

	id, err := ag.EnsureSession(context.Background(), sess.ID)
	if err != nil {
		return "", err
	}
	// ACPX prompts are addressed by the stable session name we chose (`sess.ID`).
	// Preserve the returned internal session ID separately for observability.
	sess.AgentSession = id
	sess.AgentSessionHandle = sess.ID
	s.sessionMgr.Update(sess)
	updated, _ := s.sessionMgr.Get(defaultSessionChannel, sess.ID)
	s.broadcastSession(updated)
	log.Printf("[gateway] Created agent session, name=%s, acpx_id=%s, request=%s", sess.ID, id, requestID)
	return sess.AgentSessionHandle, nil
}

func (s *Server) rpcAgentError(sessionID, requestID, prefix string, err error) *JSONRPCResponse {
	message := fmt.Sprintf("%s: %v", prefix, err)
	s.recordError(sessionID, requestID, message)
	return &JSONRPCResponse{JSONRPC: "2.0", ID: requestID, Error: &JSONRPCError{Code: -32603, Message: message}}
}

func (s *Server) recordPrompt(sessionID, requestID, prompt string) {
	if sess, ok := s.sessionMgr.RecordPrompt(defaultSessionChannel, sessionID, requestID, prompt); ok {
		s.broadcastSession(sess)
		s.broadcastActivity(sess.ID, sess.Activity[len(sess.Activity)-1])
	}
}

func (s *Server) recordEvent(sessionID, requestID string, evt agent.Event) {
	if sess, ok := s.sessionMgr.RecordEvent(defaultSessionChannel, sessionID, requestID, evt); ok {
		s.broadcastSession(sess)
		s.broadcastActivity(sess.ID, sess.Activity[len(sess.Activity)-1])
	}
}

func (s *Server) recordResult(sessionID, requestID, content string) {
	if sess, ok := s.sessionMgr.RecordResult(defaultSessionChannel, sessionID, requestID, content); ok {
		s.broadcastSession(sess)
		s.broadcastActivity(sess.ID, sess.Activity[len(sess.Activity)-1])
	}
}

func (s *Server) recordError(sessionID, requestID, message string) {
	if sess, ok := s.sessionMgr.RecordError(defaultSessionChannel, sessionID, requestID, message); ok {
		s.broadcastSession(sess)
		s.broadcastActivity(sess.ID, sess.Activity[len(sess.Activity)-1])
	}
}

func (s *Server) broadcastSession(sess *session.Session) {
	if sess == nil {
		return
	}
	s.broadcastJSON(JSONRPCRequest{JSONRPC: "2.0", Method: "session.updated", Params: map[string]interface{}{"session": sess.Summary()}})
}

func (s *Server) broadcastActivity(sessionID string, activity session.Activity) {
	s.broadcastJSON(JSONRPCRequest{JSONRPC: "2.0", Method: "session.activity", Params: map[string]interface{}{"session_id": sessionID, "activity": activity}})
}

func (s *Server) broadcastSessionDeleted(sessionID string) {
	s.broadcastJSON(JSONRPCRequest{JSONRPC: "2.0", Method: "session.deleted", Params: map[string]interface{}{"session_id": sessionID}})
}

func (s *Server) broadcastJSON(v interface{}) {
	s.connectionsMu.RLock()
	conns := make([]*WSConnection, 0, len(s.connections))
	for _, conn := range s.connections {
		conns = append(conns, conn)
	}
	s.connectionsMu.RUnlock()

	var failed []string
	for _, conn := range conns {
		if err := conn.SendJSON(v); err != nil {
			failed = append(failed, conn.ID)
		}
	}
	if len(failed) == 0 {
		return
	}

	s.connectionsMu.Lock()
	defer s.connectionsMu.Unlock()
	for _, id := range failed {
		if conn, ok := s.connections[id]; ok {
			_ = conn.Close()
			delete(s.connections, id)
		}
	}
}

func applyStreamChunk(fullContent *strings.Builder, streamErr *string, chunk agent.StreamChunk) {
	switch chunk.Type {
	case "content":
		fullContent.WriteString(chunk.Content)
	case "error":
		*streamErr = chunk.Content
	}
}

// filterTranscriptMarkers removes transcript marker lines like [thinking], [tool], [done], [acpx], [client]
func filterTranscriptMarkers(content string) string {
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

func buildStructuredEvents(parser *event.Parser, chunk agent.StreamChunk) []agent.Event {
	if len(chunk.Events) > 0 {
		return append([]agent.Event(nil), chunk.Events...)
	}

	var events []agent.Event
	if chunk.Type == "content" {
		events = append(events, convertLegacyEvents(parser.Feed(chunk.Content))...)
	}
	if chunk.Type == "error" {
		events = append(events, agent.Event{Version: agent.EventProtocolVersion, Type: agent.TypeError, Content: chunk.Content})
	}
	return events
}

func flushStructuredEvents(parser *event.Parser, includeDone bool) []agent.Event {
	events := convertLegacyEvents(parser.Flush())
	if includeDone {
		events = append(events, agent.Event{Version: agent.EventProtocolVersion, Type: agent.TypeDone})
	}
	return events
}

func convertLegacyEvents(legacy []event.Event) []agent.Event {
	var events []agent.Event
	for _, evt := range legacy {
		switch evt.Type {
		case event.TypeThinking:
			events = append(events,
				agent.Event{Version: agent.EventProtocolVersion, Type: agent.TypeThinkingStart},
				agent.Event{Version: agent.EventProtocolVersion, Type: agent.TypeThinkingDelta, Content: evt.Content},
				agent.Event{Version: agent.EventProtocolVersion, Type: agent.TypeThinkingEnd, Content: evt.Content},
			)
		case event.TypeToolStart:
			events = append(events, agent.Event{Version: agent.EventProtocolVersion, Type: agent.TypeToolStart, Name: evt.Name})
		case event.TypeToolInput:
			events = append(events, agent.Event{Version: agent.EventProtocolVersion, Type: agent.TypeToolInput, Name: evt.Name, Input: evt.Input})
		case event.TypeToolEnd:
			if evt.Input != "" {
				events = append(events, agent.Event{Version: agent.EventProtocolVersion, Type: agent.TypeToolInput, Name: evt.Name, Input: evt.Input})
			}
			if evt.Output != "" {
				events = append(events, agent.Event{Version: agent.EventProtocolVersion, Type: agent.TypeToolOutput, Name: evt.Name, Output: evt.Output})
			}
			events = append(events, agent.Event{Version: agent.EventProtocolVersion, Type: agent.TypeToolEnd, Name: evt.Name, Input: evt.Input, Output: evt.Output})
		case event.TypeToolError, event.TypeError:
			events = append(events, agent.Event{Version: agent.EventProtocolVersion, Type: agent.TypeError, Content: evt.Content, Name: evt.Name, Input: evt.Input, Output: evt.Output})
		case event.TypeOutput:
			// Filter transcript markers from output_final content
			content := filterTranscriptMarkers(evt.Content)
			events = append(events, agent.Event{Version: agent.EventProtocolVersion, Type: agent.TypeOutputFinal, Content: content})
		}
	}
	return events
}

func newEventNotification(sessionID, id string, evt agent.Event) JSONRPCRequest {
	params := map[string]interface{}{
		"id":         id,
		"session_id": sessionID,
		"version":    evt.Version,
		"type":       string(evt.Type),
		"content":    evt.Content,
	}
	if evt.Name != "" {
		params["name"] = evt.Name
	}
	if evt.Input != "" {
		params["input"] = evt.Input
	}
	if evt.Output != "" {
		params["output"] = evt.Output
	}
	return JSONRPCRequest{JSONRPC: "2.0", Method: "event", Params: params}
}

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

func parsePromptOptions(params map[string]interface{}) *agent.PromptOptions {
	return &agent.PromptOptions{
		Permissions:         getStringParam(params, "permissions"),
		Format:              getStringParam(params, "format"),
		Cwd:                 getStringParam(params, "cwd"),
		AuthPolicy:          getStringParam(params, "auth_policy"),
		NonInteractivePerms: getStringParam(params, "non_interactive_permissions"),
		SuppressReads:       getBoolParam(params, "suppress_reads"),
		Model:               getStringParam(params, "model"),
		PermissionPreset:    getStringParam(params, "permission_preset"),
		AllowedTools:        getStringParam(params, "allowed_tools"),
		DeniedTools:         getStringParam(params, "denied_tools"),
		MaxTurns:            getIntParam(params, "max_turns"),
		PromptRetries:       getIntParam(params, "prompt_retries"),
		Timeout:             getIntParam(params, "timeout"),
		TTL:                 getIntParam(params, "ttl"),
	}
}

func resolveSessionID(connID, specifiedSessionID string) string {
	if specifiedSessionID != "" {
		return specifiedSessionID
	}
	if connID != "" {
		return connID
	}
	return "default"
}

func invalidParams(id string) *JSONRPCResponse {
	return &JSONRPCResponse{JSONRPC: "2.0", ID: id, Error: &JSONRPCError{Code: -32602, Message: "Invalid params"}}
}

func missingParam(id, name string) *JSONRPCResponse {
	return &JSONRPCResponse{JSONRPC: "2.0", ID: id, Error: &JSONRPCError{Code: -32602, Message: fmt.Sprintf("Missing required param: %s", name)}}
}

func (s *Server) handleSessionInit(connID string, req *JSONRPCRequest) *JSONRPCResponse {
	params, _ := req.Params.(map[string]interface{})
	sessionID := resolveSessionID(connID, getStringParam(params, "session_id"))
	agentType := getStringParam(params, "agent")
	sess := s.prepareSession(sessionID, agentType)
	return &JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]interface{}{"session_id": sess.ID, "agent": sess.AgentName, "created_at": sess.CreatedAt, "last_active": sess.LastActive}}
}

func (s *Server) handleSessionNew(connID string, req *JSONRPCRequest) *JSONRPCResponse {
	params, _ := req.Params.(map[string]interface{})
	agentType := getStringParam(params, "agent")
	sessionID := getStringParam(params, "session_id")
	if sessionID == "" {
		sessionID = uuid.NewString()
	}
	created := s.sessionMgr.Create(sessionChannelFromParams(params), "", sessionID, agentType)
	s.broadcastSession(created)
	return &JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: created}
}

func (s *Server) handleSessionGet(connID string, req *JSONRPCRequest) *JSONRPCResponse {
	params, _ := req.Params.(map[string]interface{})
	sessionID := resolveSessionID(connID, getStringParam(params, "session_id"))
	sess, ok := s.sessionMgr.Get(sessionChannelFromParams(params), sessionID)
	if !ok {
		return &JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: nil}
	}
	return &JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: sess}
}

func (s *Server) handleSessionList(connID string, req *JSONRPCRequest) *JSONRPCResponse {
	_ = connID
	return &JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]interface{}{"sessions": s.sessionMgr.Summaries()}}
}

func (s *Server) handleSessionUpdate(connID string, req *JSONRPCRequest) *JSONRPCResponse {
	_ = connID
	params, ok := req.Params.(map[string]interface{})
	if !ok {
		return invalidParams(req.ID)
	}

	sessionID := getStringParam(params, "session_id")
	if sessionID == "" {
		return missingParam(req.ID, "session_id")
	}

	sess, ok := s.sessionMgr.Get(sessionChannelFromParams(params), sessionID)
	if !ok {
		return &JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Error: &JSONRPCError{Code: -32602, Message: "Session not found"}}
	}
	if agentType := getStringParam(params, "agent"); agentType != "" {
		sess.AgentName = agentType
	}
	s.sessionMgr.Update(sess)
	updated, _ := s.sessionMgr.Get(sessionChannelFromParams(params), sessionID)
	s.broadcastSession(updated)
	return &JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: updated}
}

func (s *Server) handleSessionDelete(connID string, req *JSONRPCRequest) *JSONRPCResponse {
	_ = connID
	params, ok := req.Params.(map[string]interface{})
	if !ok {
		return invalidParams(req.ID)
	}

	sessionID := getStringParam(params, "session_id")
	s.sessionMgr.Delete(sessionChannelFromParams(params), sessionID)
	s.broadcastSessionDeleted(sessionID)
	return &JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]interface{}{"success": true}}
}

func (s *Server) handleSessionRename(connID string, req *JSONRPCRequest) *JSONRPCResponse {
	_ = connID
	params, ok := req.Params.(map[string]interface{})
	if !ok {
		return invalidParams(req.ID)
	}
	sessionID := getStringParam(params, "session_id")
	name := getStringParam(params, "name")
	if sessionID == "" {
		return missingParam(req.ID, "session_id")
	}
	if name == "" {
		return missingParam(req.ID, "name")
	}
	sess, found := s.sessionMgr.Rename(sessionChannelFromParams(params), sessionID, name)
	if !found {
		return &JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Error: &JSONRPCError{Code: -32602, Message: "Session not found"}}
	}
	s.broadcastSession(sess)
	return &JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: sess}
}

func (s *Server) handleSessionTag(connID string, req *JSONRPCRequest) *JSONRPCResponse {
	_ = connID
	params, ok := req.Params.(map[string]interface{})
	if !ok {
		return invalidParams(req.ID)
	}
	sessionID := getStringParam(params, "session_id")
	tag := getStringParam(params, "tag")
	if sessionID == "" {
		return missingParam(req.ID, "session_id")
	}
	if tag == "" {
		return missingParam(req.ID, "tag")
	}
	sess, found := s.sessionMgr.AddTag(sessionChannelFromParams(params), sessionID, tag)
	if !found {
		return &JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Error: &JSONRPCError{Code: -32602, Message: "Session not found"}}
	}
	s.broadcastSession(sess)
	return &JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: sess}
}

func (s *Server) handleSessionUntag(connID string, req *JSONRPCRequest) *JSONRPCResponse {
	_ = connID
	params, ok := req.Params.(map[string]interface{})
	if !ok {
		return invalidParams(req.ID)
	}
	sessionID := getStringParam(params, "session_id")
	tag := getStringParam(params, "tag")
	if sessionID == "" {
		return missingParam(req.ID, "session_id")
	}
	if tag == "" {
		return missingParam(req.ID, "tag")
	}
	sess, found := s.sessionMgr.RemoveTag(sessionChannelFromParams(params), sessionID, tag)
	if !found {
		return &JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Error: &JSONRPCError{Code: -32602, Message: "Session not found"}}
	}
	s.broadcastSession(sess)
	return &JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: sess}
}

func (s *Server) handleSessionArchive(connID string, req *JSONRPCRequest) *JSONRPCResponse {
	_ = connID
	params, ok := req.Params.(map[string]interface{})
	if !ok {
		return invalidParams(req.ID)
	}
	sessionID := getStringParam(params, "session_id")
	if sessionID == "" {
		return missingParam(req.ID, "session_id")
	}
	sess, found := s.sessionMgr.Archive(sessionChannelFromParams(params), sessionID)
	if !found {
		return &JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Error: &JSONRPCError{Code: -32602, Message: "Session not found"}}
	}
	s.broadcastSession(sess)
	return &JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: sess}
}

func (s *Server) handleSessionUnarchive(connID string, req *JSONRPCRequest) *JSONRPCResponse {
	_ = connID
	params, ok := req.Params.(map[string]interface{})
	if !ok {
		return invalidParams(req.ID)
	}
	sessionID := getStringParam(params, "session_id")
	if sessionID == "" {
		return missingParam(req.ID, "session_id")
	}
	sess, found := s.sessionMgr.Unarchive(sessionChannelFromParams(params), sessionID)
	if !found {
		return &JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Error: &JSONRPCError{Code: -32602, Message: "Session not found"}}
	}
	s.broadcastSession(sess)
	return &JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: sess}
}

func (s *Server) handleSessionExport(connID string, req *JSONRPCRequest) *JSONRPCResponse {
	_ = connID
	params, ok := req.Params.(map[string]interface{})
	if !ok {
		return invalidParams(req.ID)
	}
	sessionID := getStringParam(params, "session_id")
	if sessionID == "" {
		return missingParam(req.ID, "session_id")
	}
	sess, found := s.sessionMgr.Get(sessionChannelFromParams(params), sessionID)
	if !found {
		return &JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Error: &JSONRPCError{Code: -32602, Message: "Session not found"}}
	}
	format := getStringParam(params, "format")
	if format == "" {
		format = "json"
	}
	data, err := session.ExportSession(sess, session.ExportFormat(format))
	if err != nil {
		return &JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Error: &JSONRPCError{Code: -32603, Message: err.Error()}}
	}
	return &JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]interface{}{
		"session_id": sessionID,
		"format":     format,
		"data":       string(data),
	}}
}

func (s *Server) handleSessionImport(connID string, req *JSONRPCRequest) *JSONRPCResponse {
	_ = connID
	params, ok := req.Params.(map[string]interface{})
	if !ok {
		return invalidParams(req.ID)
	}
	dataStr := getStringParam(params, "data")
	if dataStr == "" {
		return missingParam(req.ID, "data")
	}
	sess, err := session.ImportSession([]byte(dataStr))
	if err != nil {
		return &JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Error: &JSONRPCError{Code: -32603, Message: fmt.Sprintf("Import failed: %v", err)}}
	}
	s.sessionMgr.Update(sess)
	s.broadcastSession(sess)
	return &JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: sess}
}

func (s *Server) handleAgentsList(connID string, req *JSONRPCRequest) *JSONRPCResponse {
	_ = connID
	return &JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]interface{}{"agents": s.agentMgr.List()}}
}

func (s *Server) handleJobSubmit(connID string, req *JSONRPCRequest) *JSONRPCResponse {
	_ = connID
	params, ok := req.Params.(map[string]interface{})
	if !ok {
		return invalidParams(req.ID)
	}

	prompt := getStringParam(params, "prompt")
	agentName := getStringParam(params, "agent")
	timeoutSeconds := getIntParam(params, "timeout")
	if prompt == "" {
		return missingParam(req.ID, "prompt")
	}
	if agentName == "" {
		agentName = "acpx"
	}

	// Convert timeout from seconds to duration (0 means no timeout)
	timeout := time.Duration(timeoutSeconds) * time.Second

	submittedJob := s.jobMgr.Submit(prompt, agentName, timeout)

	// Start executing the job in background
	go job.ExecuteJob(context.Background(), s.jobMgr, submittedJob.ID, s.executeJobPrompt)

	return &JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: submittedJob}
}

func (s *Server) handleJobGet(connID string, req *JSONRPCRequest) *JSONRPCResponse {
	_ = connID
	params, ok := req.Params.(map[string]interface{})
	if !ok {
		return invalidParams(req.ID)
	}

	jobID := getStringParam(params, "job_id")
	if jobID == "" {
		return missingParam(req.ID, "job_id")
	}

	job, ok := s.jobMgr.Get(jobID)
	if !ok {
		return &JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: nil}
	}

	return &JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: job}
}

func (s *Server) handleJobList(connID string, req *JSONRPCRequest) *JSONRPCResponse {
	_ = connID
	summaries := s.jobMgr.Summaries()
	return &JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]interface{}{"jobs": summaries}}
}

func (s *Server) handleJobCancel(connID string, req *JSONRPCRequest) *JSONRPCResponse {
	_ = connID
	params, ok := req.Params.(map[string]interface{})
	if !ok {
		return invalidParams(req.ID)
	}

	jobID := getStringParam(params, "job_id")
	if jobID == "" {
		return missingParam(req.ID, "job_id")
	}

	if err := s.jobMgr.Cancel(jobID); err != nil {
		return &JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Error: &JSONRPCError{Code: -32603, Message: err.Error()}}
	}

	job, _ := s.jobMgr.Get(jobID)
	return &JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: job}
}

func (s *Server) handleJobDelete(connID string, req *JSONRPCRequest) *JSONRPCResponse {
	_ = connID
	params, ok := req.Params.(map[string]interface{})
	if !ok {
		return invalidParams(req.ID)
	}

	jobID := getStringParam(params, "job_id")
	if jobID == "" {
		return missingParam(req.ID, "job_id")
	}

	if err := s.jobMgr.Delete(jobID); err != nil {
		return &JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Error: &JSONRPCError{Code: -32603, Message: err.Error()}}
	}

	return &JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]interface{}{"success": true}}
}

// SendJSON sends a JSON message.
func (c *WSConnection) SendJSON(v interface{}) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	_ = c.SetWriteDeadline(time.Now().Add(wsWriteWait))
	return c.WriteJSON(v)
}

func (c *WSConnection) SendPing() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.WriteControl(websocket.PingMessage, nil, time.Now().Add(wsWriteWait))
}

// JSONRPCRequest represents a JSON-RPC request.
type JSONRPCRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      string      `json:"id,omitempty"`
	Method  string      `json:"method,omitempty"`
	Params  interface{} `json:"params,omitempty"`
}

// JSONRPCResponse represents a JSON-RPC response.
type JSONRPCResponse struct {
	JSONRPC string        `json:"jsonrpc"`
	ID      string        `json:"id,omitempty"`
	Result  interface{}   `json:"result,omitempty"`
	Error   *JSONRPCError `json:"error,omitempty"`
}

// JSONRPCError represents a JSON-RPC error.
type JSONRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (s *Server) writePingLoop(conn *WSConnection) {
	ticker := time.NewTicker(wsPingPeriod)
	defer ticker.Stop()

	for {
		select {
		case <-conn.ctx.Done():
			return
		case <-ticker.C:
			if err := conn.SendPing(); err != nil {
				conn.cancel()
				return
			}
		}
	}
}
