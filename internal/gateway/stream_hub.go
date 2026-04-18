package gateway

import (
	"log"
	"sync"

	"github.com/smallnest/imclaw/internal/agent"
	"github.com/smallnest/imclaw/internal/metrics"
)

const (
	// subscriberBufSize is the per-subscriber channel buffer size.
	// When full, slow subscribers are dropped to avoid blocking the publisher.
	subscriberBufSize = 256
)

// HubEvent wraps an event or raw stream chunk for fan-out delivery.
type HubEvent struct {
	// Exactly one field is set per message.

	// Event holds a structured agent event (tool_start, output_final, etc.).
	Event agent.Event
	// Chunk holds a raw stream content chunk.
	Chunk StreamChunkMsg
	// Result holds the final JSON-RPC response for a completed ask_stream.
	Result *JSONRPCResponse
	// Error holds a terminal error to send before closing.
	Error *JSONRPCResponse
}

// StreamChunkMsg is a raw content chunk to be sent as a "stream" notification.
type StreamChunkMsg struct {
	ID        string
	SessionID string
	Type      string
	Content   string
}

// StreamHub manages per-session fan-out of live stream events.
// Each session can have multiple subscribers; events published to the hub
// are delivered to all of them in order.
type StreamHub struct {
	mu          sync.RWMutex
	subscribers map[string]map[string]chan HubEvent // sessionID -> subscriberID -> channel
}

// NewStreamHub creates a new StreamHub.
func NewStreamHub() *StreamHub {
	return &StreamHub{
		subscribers: make(map[string]map[string]chan HubEvent),
	}
}

// Subscribe registers a subscriber for a session's live stream.
// Returns a channel that will receive all events for that session.
// The caller must call Unsubscribe when done.
func (h *StreamHub) Subscribe(sessionID, subscriberID string) <-chan HubEvent {
	ch := make(chan HubEvent, subscriberBufSize)

	h.mu.Lock()
	defer h.mu.Unlock()

	if h.subscribers[sessionID] == nil {
		h.subscribers[sessionID] = make(map[string]chan HubEvent)
	}
	h.subscribers[sessionID][subscriberID] = ch
	metrics.Default().Gauge(metrics.WSSubscribers).Inc()
	return ch
}

// Unsubscribe removes a subscriber. The subscriber's channel is closed.
func (h *StreamHub) Unsubscribe(sessionID, subscriberID string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	subs, ok := h.subscribers[sessionID]
	if !ok {
		return
	}
	if ch, exists := subs[subscriberID]; exists {
		delete(subs, subscriberID)
		close(ch)
		metrics.Default().Gauge(metrics.WSSubscribers).Dec()
	}
	if len(subs) == 0 {
		delete(h.subscribers, sessionID)
	}
}

// UnsubscribeAll removes all subscriptions for a subscriber across all sessions.
func (h *StreamHub) UnsubscribeAll(subscriberID string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	for sessionID, subs := range h.subscribers {
		if ch, exists := subs[subscriberID]; exists {
			delete(subs, subscriberID)
			close(ch)
			metrics.Default().Gauge(metrics.WSSubscribers).Dec()
		}
		if len(subs) == 0 {
			delete(h.subscribers, sessionID)
		}
	}
}

// Publish sends an event to all subscribers of a session.
// Slow subscribers are dropped (their channel is closed and removed).
func (h *StreamHub) Publish(sessionID string, evt HubEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()

	subs := h.subscribers[sessionID]
	if len(subs) == 0 {
		return
	}

	// Identify slow subscribers whose buffers are full.
	var dropped []string
	for subID, ch := range subs {
		select {
		case ch <- evt:
		default:
			dropped = append(dropped, subID)
		}
	}

	// Remove and close channels for slow subscribers.
	for _, subID := range dropped {
		if ch, ok := subs[subID]; ok {
			log.Printf("[stream-hub] Dropping slow subscriber %s for session %s", subID, sessionID)
			delete(subs, subID)
			close(ch)
			metrics.Default().Counter(metrics.WSDroppedSubs).Inc()
			metrics.Default().Gauge(metrics.WSSubscribers).Dec()
		}
	}
	if len(subs) == 0 {
		delete(h.subscribers, sessionID)
	}
}

// SubscriberCount returns the number of active subscribers for a session.
func (h *StreamHub) SubscriberCount(sessionID string) int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.subscribers[sessionID])
}

// HasSubscribers returns true if the session has at least one subscriber.
func (h *StreamHub) HasSubscribers(sessionID string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.subscribers[sessionID]) > 0
}
