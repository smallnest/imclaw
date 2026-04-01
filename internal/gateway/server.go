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
	uiHandler  *uiHandler

	httpServer *http.Server

	connections   map[string]*WSConnection
	connectionsMu sync.RWMutex

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
func NewServer(cfg *Config, sessionMgr *session.Manager, agentMgr *agent.Manager) *Server {
	return &Server{
		config:      cfg,
		sessionMgr:  sessionMgr,
		agentMgr:    agentMgr,
		uiHandler:   newUIHandler(cfg != nil && cfg.DevMode),
		connections: make(map[string]*WSConnection),
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
	mux.HandleFunc("/api/sessions", s.handleSessionsAPI)
	mux.HandleFunc("/api/sessions/", s.handleSessionDetailAPI)
	mux.HandleFunc("/api/agents", s.handleAgentsAPI)
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

	w.Header().Set("Content-Type", "application/json")
	summaries := s.sessionMgr.Summaries()
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
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	sessionID := strings.TrimPrefix(r.URL.Path, "/api/sessions/")
	if sessionID == "" {
		http.NotFound(w, r)
		return
	}

	sess, ok := s.sessionMgr.Get(sessionChannelFromRequest(r), sessionID)
	if !ok {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(sess)
}

func (s *Server) handleAgentsAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	agents := s.agentMgr.List()
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"agents": agents,
		"count":  len(agents),
	})
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
	case "agents.list":
		return s.handleAgentsList(connID, req)
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

	ctx, cancel := context.WithCancel(conn.ctx)
	defer cancel()

	stream, err := ag.PromptStream(ctx, agentSessionID, content, opts)
	if err != nil {
		rpcErr := s.rpcAgentError(sess.ID, req.ID, "Failed to start stream", err)
		_ = conn.SendJSON(rpcErr)
		return
	}

	var fullContent strings.Builder
	var streamErr string
	parser := event.NewParser()
	sawNativeEvents := false
	sawErrorEvent := false
	for chunk := range stream {
		applyStreamChunk(&fullContent, &streamErr, chunk)
		if len(chunk.Events) > 0 {
			sawNativeEvents = true
		}

		for _, evt := range buildStructuredEvents(parser, chunk) {
			if evt.Type == agent.TypeError {
				sawErrorEvent = true
			}
			s.recordEvent(sess.ID, req.ID, evt)
			if err := conn.SendJSON(newEventNotification(sess.ID, req.ID, evt)); err != nil {
				log.Printf("[gateway] WebSocket send failed: %v, cancelling stream", err)
				cancel()
				return
			}
		}

		// Strip ANSI escape sequences from content before sending to WebSocket
		cleanContent := event.StripANSI(chunk.Content)
		if err := conn.SendJSON(JSONRPCRequest{JSONRPC: "2.0", Method: "stream", Params: map[string]interface{}{"id": req.ID, "session_id": sess.ID, "type": chunk.Type, "content": cleanContent}}); err != nil {
			log.Printf("[gateway] WebSocket send failed: %v, cancelling stream", err)
			cancel()
			return
		}
	}

	if !sawNativeEvents {
		for _, evt := range flushStructuredEvents(parser, streamErr == "") {
			if evt.Type == agent.TypeError {
				sawErrorEvent = true
			}
			s.recordEvent(sess.ID, req.ID, evt)
			if err := conn.SendJSON(newEventNotification(sess.ID, req.ID, evt)); err != nil {
				log.Printf("[gateway] WebSocket send failed: %v, cancelling stream", err)
				cancel()
				return
			}
		}
	}

	if streamErr != "" {
		if !sawErrorEvent {
			s.recordError(sess.ID, req.ID, streamErr)
		}
		_ = conn.SendJSON(JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Error: &JSONRPCError{Code: -32603, Message: fmt.Sprintf("Agent error: %s", streamErr)}})
		return
	}

	// Strip ANSI escape sequences and filter transcript markers from final content
	finalContent := filterTranscriptMarkers(event.StripANSI(fullContent.String()))
	s.recordResult(sess.ID, req.ID, finalContent)
	_ = conn.SendJSON(JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]interface{}{"content": finalContent, "session_id": sess.ID, "agent": ag.Type()}})
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
			events = append(events, agent.Event{Version: agent.EventProtocolVersion, Type: agent.TypeOutputFinal, Content: evt.Content})
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
		AllowedTools:        getStringParam(params, "allowed_tools"),
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

func (s *Server) handleAgentsList(connID string, req *JSONRPCRequest) *JSONRPCResponse {
	_ = connID
	return &JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]interface{}{"agents": s.agentMgr.List()}}
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
