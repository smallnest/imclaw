package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/smallnest/imclaw/internal/agent"
	"github.com/smallnest/imclaw/internal/job"
	"github.com/smallnest/imclaw/internal/session"
)

func TestSessionsAPIAndDetailIncludePersistedActivity(t *testing.T) {
	sessionMgr := session.NewManager()
	agentMgr := agent.NewManager()
	srv := NewServer(&Config{}, sessionMgr, agentMgr, job.NewManager())

	sess := sessionMgr.Create("cli", "", "sess-1", "claude")
	if _, ok := sessionMgr.RecordPrompt("cli", sess.ID, "req-1", "hello"); !ok {
		t.Fatal("expected prompt to be recorded")
	}
	if _, ok := sessionMgr.RecordResult("cli", sess.ID, "req-1", "world"); !ok {
		t.Fatal("expected result to be recorded")
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	listRec := httptest.NewRecorder()
	srv.handleSessionsAPI(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("unexpected list status: %d", listRec.Code)
	}

	var listResp struct {
		Sessions []session.SessionSummary `json:"sessions"`
	}
	if err := json.Unmarshal(listRec.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("failed to decode list response: %v", err)
	}
	if len(listResp.Sessions) != 1 || listResp.Sessions[0].EventCount != 2 {
		t.Fatalf("unexpected list payload: %#v", listResp)
	}

	detailReq := httptest.NewRequest(http.MethodGet, "/api/sessions/sess-1", nil)
	detailRec := httptest.NewRecorder()
	srv.handleSessionDetailAPI(detailRec, detailReq)
	if detailRec.Code != http.StatusOK {
		t.Fatalf("unexpected detail status: %d", detailRec.Code)
	}

	var detail session.Session
	if err := json.Unmarshal(detailRec.Body.Bytes(), &detail); err != nil {
		t.Fatalf("failed to decode detail response: %v", err)
	}
	if len(detail.Activity) != 2 {
		t.Fatalf("expected activity in detail response, got %#v", detail)
	}
}

func TestHandleSessionUpdateChangesAgent(t *testing.T) {
	sessionMgr := session.NewManager()
	agentMgr := agent.NewManager()
	srv := NewServer(&Config{}, sessionMgr, agentMgr, job.NewManager())

	sess := sessionMgr.Create("cli", "", "sess-2", "claude")
	resp := srv.handleSessionUpdate("", &JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      "req-1",
		Method:  "session.update",
		Params: map[string]interface{}{
			"session_id": sess.ID,
			"agent":      "codex",
		},
	})
	if resp.Error != nil {
		t.Fatalf("unexpected error: %#v", resp.Error)
	}

	updated, ok := resp.Result.(*session.Session)
	if ok {
		if updated.AgentName != "codex" {
			t.Fatalf("expected updated agent, got %#v", updated)
		}
		return
	}

	reloaded, exists := sessionMgr.Get("cli", sess.ID)
	if !exists {
		t.Fatal("expected session to exist after update")
	}
	if reloaded.AgentName != "codex" {
		t.Fatalf("expected updated agent, got %#v", reloaded)
	}
}

func TestHandleSessionUpdateMissingSessionID(t *testing.T) {
	srv := NewServer(&Config{}, session.NewManager(), agent.NewManager(), job.NewManager())
	resp := srv.handleSessionUpdate("", &JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      "req-missing",
		Method:  "session.update",
		Params: map[string]interface{}{
			"agent": "codex",
		},
	})
	if resp.Error == nil || resp.Error.Message != "Missing required param: session_id" {
		t.Fatalf("unexpected response: %#v", resp)
	}
}

func TestHandleSessionUpdateMissingSession(t *testing.T) {
	srv := NewServer(&Config{}, session.NewManager(), agent.NewManager(), job.NewManager())
	resp := srv.handleSessionUpdate("", &JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      "req-missing-session",
		Method:  "session.update",
		Params: map[string]interface{}{
			"session_id": "does-not-exist",
			"agent":      "codex",
		},
	})
	if resp.Error == nil || resp.Error.Message != "Session not found" {
		t.Fatalf("unexpected response: %#v", resp)
	}
}

func TestHandleSessionDetailAPINotFound(t *testing.T) {
	srv := NewServer(&Config{}, session.NewManager(), agent.NewManager(), job.NewManager())
	req := httptest.NewRequest(http.MethodGet, "/api/sessions/missing", nil)
	rec := httptest.NewRecorder()

	srv.handleSessionDetailAPI(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unexpected status: %d", rec.Code)
	}
}

func TestHandleUIServesEmbeddedFrontend(t *testing.T) {
	srv := NewServer(&Config{}, session.NewManager(), agent.NewManager(), job.NewManager())
	req := httptest.NewRequest(http.MethodGet, "/sessions/demo", nil)
	rec := httptest.NewRecorder()

	srv.handleUI(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected UI status: %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got == "" {
		t.Fatal("expected content type to be set")
	}
	if body := rec.Body.String(); body == "" || body[:9] != "<!doctype" {
		t.Fatalf("unexpected UI body prefix: %q", body)
	}
}

func TestHandleUIServesAssetWithCorrectMimeType(t *testing.T) {
	srv := NewServer(&Config{}, session.NewManager(), agent.NewManager(), job.NewManager())

	tests := []struct {
		path        string
		contentType string
	}{
		{"/assets/app.js", "application/javascript"},
		{"/assets/styles.css", "text/css; charset=utf-8"},
		{"/assets/index.html", "text/html; charset=utf-8"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			rec := httptest.NewRecorder()
			srv.handleUIAssets(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("unexpected status: %d", rec.Code)
			}

			if got := rec.Header().Get("Content-Type"); got == "" {
				t.Fatal("expected content type to be set")
			}
		})
	}
}

func TestHandleUINoCacheInDevMode(t *testing.T) {
	srv := NewServer(&Config{DevMode: true}, session.NewManager(), agent.NewManager(), job.NewManager())
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	srv.handleUI(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", rec.Code)
	}

	cacheControl := rec.Header().Get("Cache-Control")
	if cacheControl != "no-cache, no-store, must-revalidate" {
		t.Fatalf("expected no-cache in dev mode, got: %s", cacheControl)
	}
}

func TestHandleUICacheHeadersInProduction(t *testing.T) {
	srv := NewServer(&Config{DevMode: false}, session.NewManager(), agent.NewManager(), job.NewManager())
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	srv.handleUI(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", rec.Code)
	}

	cacheControl := rec.Header().Get("Cache-Control")
	if cacheControl == "" {
		t.Fatal("expected cache control header in production mode")
	}
}

func TestHandleBuildInfo(t *testing.T) {
	srv := NewServer(&Config{}, session.NewManager(), agent.NewManager(), job.NewManager())
	req := httptest.NewRequest(http.MethodGet, "/api/build", nil)
	rec := httptest.NewRecorder()

	srv.handleBuildInfo(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", rec.Code)
	}

	var info map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &info); err != nil {
		t.Fatalf("failed to decode build info: %v", err)
	}

	// Check that build info contains expected keys
	if _, ok := info["version"]; !ok {
		t.Error("expected version key in build info")
	}
	if _, ok := info["time"]; !ok {
		t.Error("expected time key in build info")
	}
	if _, ok := info["commit"]; !ok {
		t.Error("expected commit key in build info")
	}
}

func TestHandleUIAssetNotFound(t *testing.T) {
	srv := NewServer(&Config{}, session.NewManager(), agent.NewManager(), job.NewManager())
	req := httptest.NewRequest(http.MethodGet, "/assets/nonexistent.js", nil)
	rec := httptest.NewRecorder()

	srv.handleUIAssets(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected not found status, got: %d", rec.Code)
	}
}

func TestHandleUIAssetRejectsTraversal(t *testing.T) {
	srv := NewServer(&Config{}, session.NewManager(), agent.NewManager(), job.NewManager())
	req := httptest.NewRequest(http.MethodGet, "/assets/../server.go", nil)
	rec := httptest.NewRecorder()

	srv.handleUIAssets(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected bad request status for path traversal, got: %d", rec.Code)
	}
}
