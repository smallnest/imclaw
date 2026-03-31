package session

import (
	"sync"
	"time"
)

// Session represents a conversation session
type Session struct {
	ID           string            `json:"id"`
	Channel      string            `json:"channel"`
	AccountID    string            `json:"account_id"`
	ChatID       string            `json:"chat_id"`
	AgentName    string            `json:"agent_name"`
	AgentSession string            `json:"agent_session"` // acpx session ID
	CreatedAt    time.Time         `json:"created_at"`
	LastActive   time.Time         `json:"last_active"`
	Metadata     map[string]interface{} `json:"metadata"`
}

// Manager manages sessions
type Manager struct {
	mu       sync.RWMutex
	sessions map[string]*Session // session key -> session
}

// NewManager creates a new session manager
func NewManager() *Manager {
	return &Manager{
		sessions: make(map[string]*Session),
	}
}

// SessionKey generates a session key
func SessionKey(channel, chatID string) string {
	// Simply return the chatID, no prefix needed
	return chatID
}

// Create creates a new session
func (m *Manager) Create(channel, accountID, chatID, agentName string) *Session {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := SessionKey(channel, chatID)
	session := &Session{
		ID:         key,
		Channel:    channel,
		AccountID:  accountID,
		ChatID:     chatID,
		AgentName:  agentName,
		CreatedAt:  time.Now(),
		LastActive: time.Now(),
		Metadata:   make(map[string]interface{}),
	}

	m.sessions[key] = session
	return session
}

// Get gets a session by key
func (m *Manager) Get(channel, chatID string) (*Session, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	key := SessionKey(channel, chatID)
	session, ok := m.sessions[key]
	return session, ok
}

// GetOrCreate gets or creates a session
func (m *Manager) GetOrCreate(channel, accountID, chatID, defaultAgent string) *Session {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := SessionKey(channel, chatID)
	if session, ok := m.sessions[key]; ok {
		session.LastActive = time.Now()
		return session
	}

	session := &Session{
		ID:         key,
		Channel:    channel,
		AccountID:  accountID,
		ChatID:     chatID,
		AgentName:  defaultAgent,
		CreatedAt:  time.Now(),
		LastActive: time.Now(),
		Metadata:   make(map[string]interface{}),
	}

	m.sessions[key] = session
	return session
}

// Delete deletes a session
func (m *Manager) Delete(channel, chatID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := SessionKey(channel, chatID)
	delete(m.sessions, key)
}

// Update updates a session
func (m *Manager) Update(session *Session) {
	m.mu.Lock()
	defer m.mu.Unlock()

	session.LastActive = time.Now()
	m.sessions[session.ID] = session
}

// SetAgentSession sets the agent session ID
func (s *Session) SetAgentSession(agentSession string) {
	s.AgentSession = agentSession
}

// List lists all sessions
func (m *Manager) List() []*Session {
	m.mu.RLock()
	defer m.mu.RUnlock()

	sessions := make([]*Session, 0, len(m.sessions))
	for _, session := range m.sessions {
		sessions = append(sessions, session)
	}
	return sessions
}

// Cleanup cleans up expired sessions
func (m *Manager) Cleanup(maxAge time.Duration) int {
	m.mu.Lock()
	defer m.mu.Unlock()

	cutoff := time.Now().Add(-maxAge)
	count := 0

	for key, session := range m.sessions {
		if session.LastActive.Before(cutoff) {
			delete(m.sessions, key)
			count++
		}
	}

	return count
}
