package session

import (
	"sort"
	"sync"
	"time"

	"github.com/smallnest/imclaw/internal/agent"
)

// ActivityType identifies the type of a persisted session activity.
type ActivityType string

const (
	// ActivityPrompt records a user prompt submitted to a session.
	ActivityPrompt ActivityType = "prompt"
	// ActivityEvent records a structured streaming event emitted by the agent.
	ActivityEvent ActivityType = "event"
	// ActivityResult records the final assistant output for a request.
	ActivityResult ActivityType = "result"
	// ActivityError records an error associated with a request.
	ActivityError ActivityType = "error"
)

// Activity captures a prompt, event, result, or error in a session timeline.
type Activity struct {
	ID        int64        `json:"id"`
	Type      ActivityType `json:"type"`
	RequestID string       `json:"request_id,omitempty"`
	Timestamp time.Time    `json:"timestamp"`
	Prompt    string       `json:"prompt,omitempty"`
	Content   string       `json:"content,omitempty"`
	Error     string       `json:"error,omitempty"`
	Event     *agent.Event `json:"event,omitempty"`
}

// Session represents a conversation session.
type Session struct {
	ID                 string                 `json:"id"`
	Channel            string                 `json:"channel"`
	AccountID          string                 `json:"account_id"`
	ChatID             string                 `json:"chat_id"`
	AgentName          string                 `json:"agent_name"`
	AgentSession       string                 `json:"agent_session"`        // ACPX internal session ID
	AgentSessionHandle string                 `json:"agent_session_handle"` // session handle used for subsequent prompts
	Name               string                 `json:"name,omitempty"`       // Human-readable session name
	Tags               []string               `json:"tags,omitempty"`       // User-assigned tags for organization
	Archived           bool                   `json:"archived"`             // Archived sessions are hidden from default listings
	CreatedAt          time.Time              `json:"created_at"`
	LastActive         time.Time              `json:"last_active"`
	Status             string                 `json:"status"`
	FirstPrompt        string                 `json:"first_prompt,omitempty"`
	LastPrompt         string                 `json:"last_prompt,omitempty"`
	LastOutput         string                 `json:"last_output,omitempty"`
	LastError          string                 `json:"last_error,omitempty"`
	Active             bool                   `json:"active"`
	Activity           []Activity             `json:"activity,omitempty"`
	Metadata           map[string]interface{} `json:"metadata"`
}

// SessionSummary is the lightweight projection used by list APIs and broadcasts.
type SessionSummary struct {
	ID          string    `json:"id"`
	Channel     string    `json:"channel"`
	AccountID   string    `json:"account_id"`
	ChatID      string    `json:"chat_id"`
	AgentName   string    `json:"agent_name"`
	Name        string    `json:"name,omitempty"`
	Tags        []string  `json:"tags,omitempty"`
	Archived    bool      `json:"archived"`
	CreatedAt   time.Time `json:"created_at"`
	LastActive  time.Time `json:"last_active"`
	Status      string    `json:"status"`
	FirstPrompt string    `json:"first_prompt,omitempty"`
	LastPrompt  string    `json:"last_prompt,omitempty"`
	LastOutput  string    `json:"last_output,omitempty"`
	LastError   string    `json:"last_error,omitempty"`
	Active      bool      `json:"active"`
	EventCount  int       `json:"event_count"`
}

// Manager manages sessions.
type Manager struct {
	mu       sync.RWMutex
	sessions map[string]*Session // session key -> session
}

// NewManager creates a new session manager.
func NewManager() *Manager {
	return &Manager{
		sessions: make(map[string]*Session),
	}
}

// SessionKey generates a session key.
func SessionKey(channel, chatID string) string {
	return chatID
}

func newSession(channel, accountID, chatID, agentName string) *Session {
	now := time.Now()
	return &Session{
		ID:         SessionKey(channel, chatID),
		Channel:    channel,
		AccountID:  accountID,
		ChatID:     chatID,
		AgentName:  agentName,
		CreatedAt:  now,
		LastActive: now,
		Status:     "idle",
		Metadata:   make(map[string]interface{}),
	}
}

func cloneEvent(evt agent.Event) *agent.Event {
	cloned := evt
	return &cloned
}

func cloneSession(src *Session) *Session {
	if src == nil {
		return nil
	}

	dst := *src
	if src.Metadata != nil {
		dst.Metadata = make(map[string]interface{}, len(src.Metadata))
		for k, v := range src.Metadata {
			dst.Metadata[k] = v
		}
	}
	if len(src.Tags) > 0 {
		dst.Tags = make([]string, len(src.Tags))
		copy(dst.Tags, src.Tags)
	}
	if len(src.Activity) > 0 {
		dst.Activity = make([]Activity, len(src.Activity))
		for i, activity := range src.Activity {
			dst.Activity[i] = activity
			if activity.Event != nil {
				dst.Activity[i].Event = cloneEvent(*activity.Event)
			}
		}
	}

	return &dst
}

func (s *Session) appendActivity(activity Activity) {
	s.Activity = append(s.Activity, activity)
	s.LastActive = activity.Timestamp
}

func (s *Session) recordPrompt(requestID, prompt string, at time.Time) {
	s.Active = true
	s.Status = "running"
	// Set FirstPrompt only on the first prompt
	if s.FirstPrompt == "" {
		s.FirstPrompt = prompt
	}
	s.LastPrompt = prompt
	s.LastError = ""
	s.appendActivity(Activity{
		ID:        int64(len(s.Activity) + 1),
		Type:      ActivityPrompt,
		RequestID: requestID,
		Timestamp: at,
		Prompt:    prompt,
	})
}

func (s *Session) recordEvent(requestID string, evt agent.Event, at time.Time) {
	switch evt.Type {
	case agent.TypeError:
		s.Status = "error"
		s.Active = false
		s.LastError = evt.Content
	case agent.TypeOutputFinal:
		s.Status = "idle"
		s.LastOutput = evt.Content
	case agent.TypeDone:
		s.Status = "idle"
		s.Active = false
	default:
		s.Status = "running"
	}

	s.appendActivity(Activity{
		ID:        int64(len(s.Activity) + 1),
		Type:      ActivityEvent,
		RequestID: requestID,
		Timestamp: at,
		Event:     cloneEvent(evt),
		Content:   evt.Content,
	})
}

func (s *Session) recordResult(requestID, content string, at time.Time) {
	s.Active = false
	s.Status = "idle"
	s.LastOutput = content
	s.appendActivity(Activity{
		ID:        int64(len(s.Activity) + 1),
		Type:      ActivityResult,
		RequestID: requestID,
		Timestamp: at,
		Content:   content,
	})
}

func (s *Session) recordError(requestID, message string, at time.Time) {
	s.Active = false
	s.Status = "error"
	s.LastError = message
	s.appendActivity(Activity{
		ID:        int64(len(s.Activity) + 1),
		Type:      ActivityError,
		RequestID: requestID,
		Timestamp: at,
		Error:     message,
	})
}

// Summary returns a lightweight session view for list rendering.
func (s *Session) Summary() SessionSummary {
	return SessionSummary{
		ID:          s.ID,
		Channel:     s.Channel,
		AccountID:   s.AccountID,
		ChatID:      s.ChatID,
		AgentName:   s.AgentName,
		Name:        s.Name,
		Tags:        s.Tags,
		Archived:    s.Archived,
		CreatedAt:   s.CreatedAt,
		LastActive:  s.LastActive,
		Status:      s.Status,
		FirstPrompt: s.FirstPrompt,
		LastPrompt:  s.LastPrompt,
		LastOutput:  s.LastOutput,
		LastError:   s.LastError,
		Active:      s.Active,
		EventCount:  len(s.Activity),
	}
}

// Create creates a new session.
func (m *Manager) Create(channel, accountID, chatID, agentName string) *Session {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := SessionKey(channel, chatID)
	session := newSession(channel, accountID, chatID, agentName)
	m.sessions[key] = session
	return cloneSession(session)
}

// Get gets a session by key.
func (m *Manager) Get(channel, chatID string) (*Session, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	session, ok := m.sessions[SessionKey(channel, chatID)]
	return cloneSession(session), ok
}

// GetOrCreate gets or creates a session.
func (m *Manager) GetOrCreate(channel, accountID, chatID, defaultAgent string) *Session {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := SessionKey(channel, chatID)
	if sess, ok := m.sessions[key]; ok {
		sess.LastActive = time.Now()
		return cloneSession(sess)
	}

	session := newSession(channel, accountID, chatID, defaultAgent)
	m.sessions[key] = session
	return cloneSession(session)
}

// Delete deletes a session.
func (m *Manager) Delete(channel, chatID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.sessions, SessionKey(channel, chatID))
}

// Update updates a session.
func (m *Manager) Update(session *Session) {
	m.mu.Lock()
	defer m.mu.Unlock()

	cloned := cloneSession(session)
	cloned.LastActive = time.Now()
	m.sessions[cloned.ID] = cloned
}

// List lists all sessions.
func (m *Manager) List() []*Session {
	m.mu.RLock()
	defer m.mu.RUnlock()

	sessions := make([]*Session, 0, len(m.sessions))
	for _, sess := range m.sessions {
		sessions = append(sessions, cloneSession(sess))
	}
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].LastActive.After(sessions[j].LastActive)
	})
	return sessions
}

// Summaries lists all sessions using a lightweight projection.
func (m *Manager) Summaries() []SessionSummary {
	m.mu.RLock()
	defer m.mu.RUnlock()

	summaries := make([]SessionSummary, 0, len(m.sessions))
	for _, sess := range m.sessions {
		summaries = append(summaries, sess.Summary())
	}
	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].LastActive.After(summaries[j].LastActive)
	})
	return summaries
}

// SummariesFiltered returns session summaries with optional filtering.
// When tag is non-empty, only sessions with that tag are returned.
// When includeArchived is false, archived sessions are excluded.
func (m *Manager) SummariesFiltered(tag string, includeArchived bool) []SessionSummary {
	m.mu.RLock()
	defer m.mu.RUnlock()

	summaries := make([]SessionSummary, 0, len(m.sessions))
	for _, sess := range m.sessions {
		s := sess.Summary()
		if !includeArchived && s.Archived {
			continue
		}
		if tag != "" {
			found := false
			for _, t := range s.Tags {
				if t == tag {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}
		summaries = append(summaries, s)
	}
	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].LastActive.After(summaries[j].LastActive)
	})
	return summaries
}

// RecordPrompt appends a prompt activity to the session timeline.
func (m *Manager) RecordPrompt(channel, chatID, requestID, prompt string) (*Session, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	sess, ok := m.sessions[SessionKey(channel, chatID)]
	if !ok {
		return nil, false
	}
	sess.recordPrompt(requestID, prompt, time.Now())
	return cloneSession(sess), true
}

// RecordEvent appends an event activity to the session timeline.
func (m *Manager) RecordEvent(channel, chatID, requestID string, evt agent.Event) (*Session, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	sess, ok := m.sessions[SessionKey(channel, chatID)]
	if !ok {
		return nil, false
	}
	sess.recordEvent(requestID, evt, time.Now())
	return cloneSession(sess), true
}

// RecordResult appends a final result activity to the session timeline.
func (m *Manager) RecordResult(channel, chatID, requestID, content string) (*Session, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	sess, ok := m.sessions[SessionKey(channel, chatID)]
	if !ok {
		return nil, false
	}
	sess.recordResult(requestID, content, time.Now())
	return cloneSession(sess), true
}

// RecordError appends an error activity to the session timeline.
func (m *Manager) RecordError(channel, chatID, requestID, message string) (*Session, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	sess, ok := m.sessions[SessionKey(channel, chatID)]
	if !ok {
		return nil, false
	}
	sess.recordError(requestID, message, time.Now())
	return cloneSession(sess), true
}

// Cleanup cleans up expired sessions.
func (m *Manager) Cleanup(maxAge time.Duration) int {
	m.mu.Lock()
	defer m.mu.Unlock()

	cutoff := time.Now().Add(-maxAge)
	count := 0
	for key, sess := range m.sessions {
		if sess.LastActive.Before(cutoff) {
			delete(m.sessions, key)
			count++
		}
	}
	return count
}

// Rename sets a human-readable name for the session.
func (m *Manager) Rename(channel, chatID, name string) (*Session, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	sess, ok := m.sessions[SessionKey(channel, chatID)]
	if !ok {
		return nil, false
	}
	sess.Name = name
	sess.LastActive = time.Now()
	return cloneSession(sess), true
}

// AddTag adds a tag to the session. Returns the updated session or false if not found.
func (m *Manager) AddTag(channel, chatID, tag string) (*Session, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	sess, ok := m.sessions[SessionKey(channel, chatID)]
	if !ok {
		return nil, false
	}
	for _, t := range sess.Tags {
		if t == tag {
			return cloneSession(sess), true
		}
	}
	sess.Tags = append(sess.Tags, tag)
	sess.LastActive = time.Now()
	return cloneSession(sess), true
}

// RemoveTag removes a tag from the session.
func (m *Manager) RemoveTag(channel, chatID, tag string) (*Session, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	sess, ok := m.sessions[SessionKey(channel, chatID)]
	if !ok {
		return nil, false
	}
	for i, t := range sess.Tags {
		if t == tag {
			sess.Tags = append(sess.Tags[:i], sess.Tags[i+1:]...)
			break
		}
	}
	sess.LastActive = time.Now()
	return cloneSession(sess), true
}

// SetTags replaces all tags on the session. Duplicate tags are removed.
func (m *Manager) SetTags(channel, chatID string, tags []string) (*Session, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	sess, ok := m.sessions[SessionKey(channel, chatID)]
	if !ok {
		return nil, false
	}
	sess.Tags = deduplicateTags(tags)
	sess.LastActive = time.Now()
	return cloneSession(sess), true
}

// Archive marks a session as archived.
func (m *Manager) Archive(channel, chatID string) (*Session, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	sess, ok := m.sessions[SessionKey(channel, chatID)]
	if !ok {
		return nil, false
	}
	sess.Archived = true
	sess.LastActive = time.Now()
	return cloneSession(sess), true
}

// Unarchive removes the archived flag from a session.
func (m *Manager) Unarchive(channel, chatID string) (*Session, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	sess, ok := m.sessions[SessionKey(channel, chatID)]
	if !ok {
		return nil, false
	}
	sess.Archived = false
	sess.LastActive = time.Now()
	return cloneSession(sess), true
}

// SessionUpdates contains the fields to update on a session in a single atomic operation.
type SessionUpdates struct {
	Name       *string
	AddTags    []string
	RemoveTags []string
	SetTags    []string // If non-nil, replaces all tags (takes precedence over AddTags/RemoveTags)
	Archived   *bool
}

// ApplyUpdates atomically applies multiple updates to a session within a single lock.
// This avoids partial failures from multiple separate lock acquisitions.
func (m *Manager) ApplyUpdates(channel, chatID string, updates SessionUpdates) (*Session, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	sess, ok := m.sessions[SessionKey(channel, chatID)]
	if !ok {
		return nil, false
	}

	if updates.Name != nil {
		sess.Name = *updates.Name
	}

	if updates.SetTags != nil {
		sess.Tags = deduplicateTags(updates.SetTags)
	} else {
		// Remove tags first, then add
		if len(updates.RemoveTags) > 0 {
			for _, tag := range updates.RemoveTags {
				for i, t := range sess.Tags {
					if t == tag {
						sess.Tags = append(sess.Tags[:i], sess.Tags[i+1:]...)
						break
					}
				}
			}
		}
		if len(updates.AddTags) > 0 {
			existing := make(map[string]bool, len(sess.Tags))
			for _, t := range sess.Tags {
				existing[t] = true
			}
			for _, tag := range updates.AddTags {
				if !existing[tag] {
					sess.Tags = append(sess.Tags, tag)
					existing[tag] = true
				}
			}
		}
	}

	if updates.Archived != nil {
		sess.Archived = *updates.Archived
	}

	sess.LastActive = time.Now()
	return cloneSession(sess), true
}

// deduplicateTags removes duplicate tags while preserving order.
func deduplicateTags(tags []string) []string {
	seen := make(map[string]bool, len(tags))
	result := make([]string, 0, len(tags))
	for _, t := range tags {
		if !seen[t] {
			seen[t] = true
			result = append(result, t)
		}
	}
	return result
}

// ListByTag returns sessions that have the specified tag.
func (m *Manager) ListByTag(tag string) []*Session {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []*Session
	for _, sess := range m.sessions {
		for _, t := range sess.Tags {
			if t == tag {
				result = append(result, cloneSession(sess))
				break
			}
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].LastActive.After(result[j].LastActive)
	})
	return result
}

// ListArchived returns all archived sessions.
func (m *Manager) ListArchived() []*Session {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []*Session
	for _, sess := range m.sessions {
		if sess.Archived {
			result = append(result, cloneSession(sess))
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].LastActive.After(result[j].LastActive)
	})
	return result
}
