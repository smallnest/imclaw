package gateway

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/smallnest/imclaw/internal/agent"
	"github.com/smallnest/imclaw/internal/event"
	"github.com/smallnest/imclaw/internal/job"
	"github.com/smallnest/imclaw/internal/session"
)

// TestStreamHubBasicPubSub verifies basic publish/subscribe behavior.
func TestStreamHubBasicPubSub(t *testing.T) {
	hub := NewStreamHub()

	ch := hub.Subscribe("sess-1", "sub-1")
	defer hub.Unsubscribe("sess-1", "sub-1")

	evt := HubEvent{Event: agent.Event{Type: agent.TypeOutputDelta, Content: "hello"}}
	hub.Publish("sess-1", evt)

	select {
	case got := <-ch:
		if got.Event.Content != "hello" {
			t.Fatalf("expected hello, got %q", got.Event.Content)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for event")
	}
}

// TestStreamHubMultipleSubscribers verifies fan-out to multiple subscribers.
func TestStreamHubMultipleSubscribers(t *testing.T) {
	hub := NewStreamHub()

	ch1 := hub.Subscribe("sess-1", "sub-1")
	ch2 := hub.Subscribe("sess-1", "sub-2")
	defer hub.Unsubscribe("sess-1", "sub-1")
	defer hub.Unsubscribe("sess-1", "sub-2")

	evt := HubEvent{Event: agent.Event{Type: agent.TypeToolStart, Name: "Read"}}
	hub.Publish("sess-1", evt)

	for i, ch := range []<-chan HubEvent{ch1, ch2} {
		select {
		case got := <-ch:
			if got.Event.Name != "Read" {
				t.Fatalf("subscriber %d: expected Read, got %q", i, got.Event.Name)
			}
		case <-time.After(time.Second):
			t.Fatalf("subscriber %d: timeout", i)
		}
	}
}

// TestStreamHubUnsubscribe verifies that unsubscribed clients stop receiving events.
func TestStreamHubUnsubscribe(t *testing.T) {
	hub := NewStreamHub()

	ch1 := hub.Subscribe("sess-1", "sub-1")
	ch2 := hub.Subscribe("sess-1", "sub-2")

	hub.Unsubscribe("sess-1", "sub-1")

	hub.Publish("sess-1", HubEvent{Event: agent.Event{Type: agent.TypeDone}})

	// ch1 should be closed
	_, ok := <-ch1
	if ok {
		t.Fatal("expected ch1 to be closed after unsubscribe")
	}

	// ch2 should still receive
	select {
	case <-ch2:
	case <-time.After(time.Second):
		t.Fatal("ch2 should have received the event")
	}

	if hub.SubscriberCount("sess-1") != 1 {
		t.Fatalf("expected 1 subscriber, got %d", hub.SubscriberCount("sess-1"))
	}
}

// TestStreamHubUnsubscribeAll verifies cleanup across sessions.
func TestStreamHubUnsubscribeAll(t *testing.T) {
	hub := NewStreamHub()

	ch1 := hub.Subscribe("sess-1", "sub-1")
	_ = hub.Subscribe("sess-2", "sub-1")

	hub.UnsubscribeAll("sub-1")

	// Both channels should be closed
	_, ok := <-ch1
	if ok {
		t.Fatal("expected ch1 to be closed")
	}

	if hub.SubscriberCount("sess-1") != 0 || hub.SubscriberCount("sess-2") != 0 {
		t.Fatal("expected zero subscribers after UnsubscribeAll")
	}
}

// TestStreamHubOrdering verifies message ordering is consistent across subscribers.
func TestStreamHubOrdering(t *testing.T) {
	hub := NewStreamHub()

	ch := hub.Subscribe("sess-1", "sub-1")
	defer hub.Unsubscribe("sess-1", "sub-1")

	const n = 100
	for i := 0; i < n; i++ {
		hub.Publish("sess-1", HubEvent{Event: agent.Event{Type: agent.TypeOutputDelta, Content: fmt.Sprintf("msg-%d", i)}})
	}

	for i := 0; i < n; i++ {
		select {
		case got := <-ch:
			expected := fmt.Sprintf("msg-%d", i)
			if got.Event.Content != expected {
				t.Fatalf("out of order: expected %q, got %q", expected, got.Event.Content)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("timeout at message %d", i)
		}
	}
}

// TestStreamHubBackpressureDrop verifies slow subscribers are dropped
// while fast subscribers that actively consume events are retained.
func TestStreamHubBackpressureDrop(t *testing.T) {
	hub := NewStreamHub()

	// Create a subscriber that never reads (will fill buffer).
	_ = hub.Subscribe("sess-1", "slow-sub")
	ch2 := hub.Subscribe("sess-1", "fast-sub")
	defer hub.Unsubscribe("sess-1", "fast-sub")

	// Drain the fast subscriber concurrently so its buffer never fills.
	go func() {
		for range ch2 {
		}
	}()

	// Overflow the slow subscriber's buffer.
	for i := 0; i < subscriberBufSize+10; i++ {
		hub.Publish("sess-1", HubEvent{Event: agent.Event{Type: agent.TypeOutputDelta, Content: fmt.Sprintf("msg-%d", i)}})
	}

	// Allow goroutines to settle.
	time.Sleep(50 * time.Millisecond)

	// The slow subscriber should have been dropped, fast subscriber should remain.
	if hub.SubscriberCount("sess-1") != 1 {
		t.Fatalf("expected 1 subscriber (fast-sub), got %d", hub.SubscriberCount("sess-1"))
	}
}

// TestStreamHubNoSubscribersPublishIsNoop verifies publishing to a session with no subscribers is safe.
func TestStreamHubNoSubscribersPublishIsNoop(t *testing.T) {
	hub := NewStreamHub()
	hub.Publish("nonexistent", HubEvent{Event: agent.Event{Type: agent.TypeDone}})
}

// TestStreamHubChunkDelivery verifies that raw stream chunks are delivered correctly.
func TestStreamHubChunkDelivery(t *testing.T) {
	hub := NewStreamHub()

	ch := hub.Subscribe("sess-1", "sub-1")
	defer hub.Unsubscribe("sess-1", "sub-1")

	hub.Publish("sess-1", HubEvent{Chunk: StreamChunkMsg{ID: "req-1", SessionID: "sess-1", Type: "content", Content: "hello world"}})

	select {
	case got := <-ch:
		if got.Chunk.Content != "hello world" {
			t.Fatalf("expected hello world, got %q", got.Chunk.Content)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}
}

// TestStreamHubResultDelivery verifies final results are delivered to subscribers.
func TestStreamHubResultDelivery(t *testing.T) {
	hub := NewStreamHub()

	ch := hub.Subscribe("sess-1", "sub-1")
	defer hub.Unsubscribe("sess-1", "sub-1")

	resp := &JSONRPCResponse{JSONRPC: "2.0", ID: "req-1", Result: map[string]interface{}{"content": "final"}}
	hub.Publish("sess-1", HubEvent{Result: resp})

	select {
	case got := <-ch:
		if got.Result == nil {
			t.Fatal("expected result to be set")
		}
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}
}

// TestMultipleClientsSameSession is an integration-level test using WebSocket connections.
// It verifies that multiple WebSocket clients can subscribe to the same session and
// all receive the same stream events.
func TestMultipleClientsSameSession(t *testing.T) {
	sessionMgr := session.NewManager()
	agentMgr := agent.NewManager()
	jobMgr := job.NewManager()

	srv := NewServer(&Config{Host: "127.0.0.1", Port: 0}, sessionMgr, agentMgr, jobMgr)

	// Create a session manually.
	sess := sessionMgr.Create(defaultSessionChannel, "", "multi-sess", "stub")

	// Start the server on a random port using httptest.
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ws":
			srv.handleWebSocket(w, r)
		}
	}))
	defer httpServer.Close()

	wsURL := "ws" + strings.TrimPrefix(httpServer.URL, "http") + "/ws"

	// Connect 3 clients.
	var conns []*websocket.Conn
	for i := 0; i < 3; i++ {
		conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			t.Fatalf("client %d dial failed: %v", i, err)
		}
		defer conn.Close()
		conns = append(conns, conn)

		// Read the "connected" and "session.snapshot" messages.
		for j := 0; j < 2; j++ {
			if _, _, err := conn.ReadMessage(); err != nil {
				t.Fatalf("client %d read welcome msg %d: %v", i, j, err)
			}
		}
	}

	// All clients subscribe to the same session.
	for i, conn := range conns {
		subReq := JSONRPCRequest{JSONRPC: "2.0", ID: fmt.Sprintf("sub-%d", i), Method: "session.subscribe", Params: map[string]interface{}{"session_id": sess.ID}}
		if err := conn.WriteJSON(subReq); err != nil {
			t.Fatalf("client %d subscribe write: %v", i, err)
		}
		// Read the subscribe response.
		if _, _, err := conn.ReadMessage(); err != nil {
			t.Fatalf("client %d subscribe read: %v", i, err)
		}
	}

	// Simulate publishing events via the hub (as handleAskStream would).
	srv.hub.Publish(sess.ID, HubEvent{Event: agent.Event{Version: agent.EventProtocolVersion, Type: agent.TypeThinkingStart}})
	srv.hub.Publish(sess.ID, HubEvent{Event: agent.Event{Version: agent.EventProtocolVersion, Type: agent.TypeThinkingDelta, Content: "reasoning..."}})
	srv.hub.Publish(sess.ID, HubEvent{Event: agent.Event{Version: agent.EventProtocolVersion, Type: agent.TypeDone}})
	srv.hub.Publish(sess.ID, HubEvent{Result: &JSONRPCResponse{JSONRPC: "2.0", ID: "req-1", Result: map[string]interface{}{"content": "done"}}})

	// All 3 clients should receive the events.
	var received [3]int
	for i, conn := range conns {
		// Set a read deadline to avoid hanging.
		_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				break // timeout or close
			}
			var msg map[string]interface{}
			_ = json.Unmarshal(data, &msg)
			method, _ := msg["method"].(string)
			if method == "event" || (msg["result"] != nil && msg["id"] == "req-1") {
				received[i]++
			}
		}
	}

	for i, count := range received {
		if count < 3 {
			t.Errorf("client %d received %d event messages, expected at least 3", i, count)
		}
	}
}

// TestSubscriberDisconnectDoesNotTerminateSession verifies that disconnecting
// one subscriber does not affect other subscribers.
func TestSubscriberDisconnectDoesNotTerminateSession(t *testing.T) {
	hub := NewStreamHub()

	ch1 := hub.Subscribe("sess-1", "sub-1")
	ch2 := hub.Subscribe("sess-1", "sub-2")

	// Disconnect sub-1.
	hub.Unsubscribe("sess-1", "sub-1")

	// Publishing should still work for sub-2.
	hub.Publish("sess-1", HubEvent{Event: agent.Event{Type: agent.TypeDone}})

	select {
	case <-ch1:
		// ch1 was closed (unsubscribed), so this reads the zero value.
	case <-time.After(time.Second):
		t.Fatal("ch1 should be closed")
	}

	select {
	case got := <-ch2:
		if got.Event.Type != agent.TypeDone {
			t.Fatalf("expected done event, got %v", got.Event.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("ch2 should have received the event")
	}

	if hub.SubscriberCount("sess-1") != 1 {
		t.Fatalf("expected 1 subscriber, got %d", hub.SubscriberCount("sess-1"))
	}
}

// TestConcurrentPublish verifies the hub is safe under concurrent access.
func TestConcurrentPublish(t *testing.T) {
	hub := NewStreamHub()

	const numSubscribers = 10
	const numMessages = 50

	var channels []<-chan HubEvent
	for i := 0; i < numSubscribers; i++ {
		ch := hub.Subscribe("sess-1", fmt.Sprintf("sub-%d", i))
		channels = append(channels, ch)
	}

	var wg sync.WaitGroup
	for i := 0; i < numMessages; i++ {
		wg.Add(1)
		go func(seq int) {
			defer wg.Done()
			hub.Publish("sess-1", HubEvent{Event: agent.Event{Type: agent.TypeOutputDelta, Content: fmt.Sprintf("msg-%d", seq)}})
		}(i)
	}
	wg.Wait()

	// Each subscriber should have received all messages.
	for i, ch := range channels {
		count := 0
		timeout := time.After(5 * time.Second)
		for {
			select {
			case _, ok := <-ch:
				if !ok {
					t.Fatalf("sub %d: channel closed unexpectedly", i)
				}
				count++
				if count == numMessages {
					goto nextSub
				}
			case <-timeout:
				t.Fatalf("sub %d: only received %d/%d messages", i, count, numMessages)
			}
		}
	nextSub:
	}
}

// TestSessionSubscribeRPC tests the JSON-RPC session.subscribe method.
func TestSessionSubscribeRPC(t *testing.T) {
	sessionMgr := session.NewManager()
	agentMgr := agent.NewManager()
	jobMgr := job.NewManager()

	srv := NewServer(&Config{}, sessionMgr, agentMgr, jobMgr)
	sess := sessionMgr.Create(defaultSessionChannel, "", "rpc-sess", "stub")

	// Test missing session_id.
	resp := srv.handleRPCRequest("conn-1", &JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      "1",
		Method:  "session.subscribe",
		Params:  map[string]interface{}{},
	})
	if resp.Error == nil || resp.Error.Message != "Missing required param: session_id" {
		t.Fatalf("expected missing session_id error, got %v", resp)
	}

	// Test non-existent session.
	resp = srv.handleRPCRequest("conn-1", &JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      "2",
		Method:  "session.subscribe",
		Params:  map[string]interface{}{"session_id": "nonexistent"},
	})
	if resp.Error == nil || resp.Error.Message != "Session not found" {
		t.Fatalf("expected session not found error, got %v", resp)
	}

	// Test successful subscribe.
	resp = srv.handleRPCRequest("conn-1", &JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      "3",
		Method:  "session.subscribe",
		Params:  map[string]interface{}{"session_id": sess.ID},
	})
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}
	result, ok := resp.Result.(map[string]interface{})
	if !ok || !result["subscribed"].(bool) {
		t.Fatalf("expected subscribed=true, got %v", resp.Result)
	}
}

// TestStreamHubPublishTypes verifies all HubEvent variants work.
func TestStreamHubPublishTypes(t *testing.T) {
	hub := NewStreamHub()
	ch := hub.Subscribe("sess-1", "sub-1")
	defer hub.Unsubscribe("sess-1", "sub-1")

	// Publish an agent Event.
	hub.Publish("sess-1", HubEvent{Event: agent.Event{Type: agent.TypeToolStart, Name: "Bash"}})
	// Publish a Chunk.
	hub.Publish("sess-1", HubEvent{Chunk: StreamChunkMsg{ID: "r1", Type: "content", Content: "output"}})
	// Publish a Result.
	hub.Publish("sess-1", HubEvent{Result: &JSONRPCResponse{JSONRPC: "2.0", ID: "r1", Result: "ok"}})
	// Publish an Error.
	hub.Publish("sess-1", HubEvent{Error: &JSONRPCResponse{JSONRPC: "2.0", ID: "r1", Error: &JSONRPCError{Code: -32603, Message: "fail"}}})

	expected := []string{"event", "chunk", "result", "error"}
	for i, typ := range expected {
		select {
		case got := <-ch:
			switch typ {
			case "event":
				if got.Event.Type != agent.TypeToolStart {
					t.Errorf("%d: expected event, got %v", i, got)
				}
			case "chunk":
				if got.Chunk.Content != "output" {
					t.Errorf("%d: expected chunk, got %v", i, got)
				}
			case "result":
				if got.Result == nil {
					t.Errorf("%d: expected result, got %v", i, got)
				}
			case "error":
				if got.Error == nil {
					t.Errorf("%d: expected error, got %v", i, got)
				}
			}
		case <-time.After(time.Second):
			t.Fatalf("timeout waiting for %s at index %d", typ, i)
		}
	}
}

// TestHubSubscriberCountAccuracy verifies count accuracy under concurrent subscribe/unsubscribe.
func TestHubSubscriberCountAccuracy(t *testing.T) {
	hub := NewStreamHub()

	var wg sync.WaitGroup
	const n = 50
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			subID := fmt.Sprintf("sub-%d", id)
			hub.Subscribe("sess-1", subID)
			hub.Unsubscribe("sess-1", subID)
		}(i)
	}
	wg.Wait()

	if hub.SubscriberCount("sess-1") != 0 {
		t.Fatalf("expected 0 subscribers, got %d", hub.SubscriberCount("sess-1"))
	}
}

// TestHasSubscribers returns correctly.
func TestHasSubscribers(t *testing.T) {
	hub := NewStreamHub()
	if hub.HasSubscribers("s1") {
		t.Fatal("expected no subscribers")
	}
	hub.Subscribe("s1", "sub-1")
	if !hub.HasSubscribers("s1") {
		t.Fatal("expected subscribers")
	}
	hub.Unsubscribe("s1", "sub-1")
	if hub.HasSubscribers("s1") {
		t.Fatal("expected no subscribers after unsubscribe")
	}
}

// TestSessionSubscribeRequiresWebSocket verifies that session.subscribe fails for non-WS connections.
func TestSessionSubscribeRequiresWebSocket(t *testing.T) {
	sessionMgr := session.NewManager()
	agentMgr := agent.NewManager()
	jobMgr := job.NewManager()
	srv := NewServer(&Config{}, sessionMgr, agentMgr, jobMgr)
	sessionMgr.Create(defaultSessionChannel, "", "test-sess", "stub")

	// connID is empty → should error.
	resp := srv.handleRPCRequest("", &JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      "1",
		Method:  "session.subscribe",
		Params:  map[string]interface{}{"session_id": "test-sess"},
	})
	if resp.Error == nil {
		t.Fatal("expected error for non-WebSocket connection")
	}
	if !strings.Contains(resp.Error.Message, "requires WebSocket") {
		t.Fatalf("expected WebSocket required error, got %q", resp.Error.Message)
	}
}

// TestStreamHubDifferentSessionsAreIndependent verifies that subscriptions
// for different sessions do not interfere with each other.
func TestStreamHubDifferentSessionsAreIndependent(t *testing.T) {
	hub := NewStreamHub()

	ch1 := hub.Subscribe("sess-1", "sub-1")
	ch2 := hub.Subscribe("sess-2", "sub-2")

	hub.Publish("sess-1", HubEvent{Event: agent.Event{Type: agent.TypeDone}})

	// Only ch1 should receive.
	select {
	case <-ch1:
	case <-time.After(time.Second):
		t.Fatal("ch1 should have received event")
	}

	// ch2 should not receive.
	select {
	case <-ch2:
		t.Fatal("ch2 should NOT have received event for sess-1")
	case <-time.After(100 * time.Millisecond):
		// Expected.
	}

	// Now publish to sess-2.
	hub.Publish("sess-2", HubEvent{Event: agent.Event{Type: agent.TypeDone}})

	select {
	case <-ch2:
	case <-time.After(time.Second):
		t.Fatal("ch2 should have received event")
	}
}

// TestBuildStructuredEventsWithHubPublish verifies that structured events
// built from stream chunks can be published to the hub.
func TestBuildStructuredEventsWithHubPublish(t *testing.T) {
	hub := NewStreamHub()
	ch := hub.Subscribe("sess-1", "sub-1")
	defer hub.Unsubscribe("sess-1", "sub-1")

	parser := event.NewParser()
	chunk := agent.StreamChunk{Type: "content", Content: "[thinking] hello\nworld\n"}

	events := buildStructuredEvents(parser, chunk)
	for _, evt := range events {
		hub.Publish("sess-1", HubEvent{Event: evt})
	}

	// Should get: thinking_start, thinking_delta, thinking_end
	count := 0
	timeout := time.After(time.Second)
	for {
		select {
		case <-ch:
			count++
			if count == 3 {
				return
			}
		case <-timeout:
			t.Fatalf("expected 3 events, got %d", count)
		}
	}
}

// TestHubEventFieldsAreDistinct verifies that the HubEvent type correctly
// differentiates between Event, Chunk, Result, and Error fields.
func TestHubEventFieldsAreDistinct(t *testing.T) {
	evt := HubEvent{Event: agent.Event{Type: agent.TypeDone}}
	if evt.Chunk.Type != "" {
		t.Fatal("Chunk should be empty")
	}
	if evt.Result != nil {
		t.Fatal("Result should be nil")
	}
	if evt.Error != nil {
		t.Fatal("Error should be nil")
	}

	chunk := HubEvent{Chunk: StreamChunkMsg{Content: "x"}}
	if chunk.Event.Type != "" {
		t.Fatal("Event should be empty")
	}
}
