package gateway

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/smallnest/imclaw/internal/agent"
	"github.com/smallnest/imclaw/internal/job"
	"github.com/smallnest/imclaw/internal/session"
)

func newTestServer() *Server {
	return NewServer(&Config{}, session.NewManager(), agent.NewManager(), job.NewManager())
}

func TestHandleSessionRenameRPC(t *testing.T) {
	srv := newTestServer()
	sess := srv.sessionMgr.Create(defaultSessionChannel, "", "s-rename", "claude")

	resp := srv.handleRPCRequest("", &JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      "1",
		Method:  "session.rename",
		Params: map[string]interface{}{
			"session_id": sess.ID,
			"name":       "Renamed",
		},
	})

	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error.Message)
	}
	result, ok := resp.Result.(*session.Session)
	if !ok {
		t.Fatalf("expected *session.Session result, got %T", resp.Result)
	}
	if result.Name != "Renamed" {
		t.Fatalf("expected name 'Renamed', got %q", result.Name)
	}
}

func TestHandleSessionRenameMissingParams(t *testing.T) {
	srv := newTestServer()

	resp := srv.handleRPCRequest("", &JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      "1",
		Method:  "session.rename",
		Params:  map[string]interface{}{"session_id": "nonexistent"},
	})
	if resp.Error == nil {
		t.Fatal("expected error for missing name param")
	}
}

func TestHandleSessionRenameNotFound(t *testing.T) {
	srv := newTestServer()

	resp := srv.handleRPCRequest("", &JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      "1",
		Method:  "session.rename",
		Params: map[string]interface{}{
			"session_id": "nonexistent",
			"name":       "X",
		},
	})
	if resp.Error == nil {
		t.Fatal("expected error for nonexistent session")
	}
}

func TestHandleSessionTagRPC(t *testing.T) {
	srv := newTestServer()
	sess := srv.sessionMgr.Create(defaultSessionChannel, "", "s-tag", "claude")

	resp := srv.handleRPCRequest("", &JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      "1",
		Method:  "session.tag",
		Params: map[string]interface{}{
			"session_id": sess.ID,
			"tag":        "important",
		},
	})

	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error.Message)
	}
	result := resp.Result.(*session.Session)
	if len(result.Tags) != 1 || result.Tags[0] != "important" {
		t.Fatalf("expected tags ['important'], got %v", result.Tags)
	}
}

func TestHandleSessionUntagRPC(t *testing.T) {
	srv := newTestServer()
	sess := srv.sessionMgr.Create(defaultSessionChannel, "", "s-untag", "claude")
	srv.sessionMgr.AddTag(defaultSessionChannel, sess.ID, "a")
	srv.sessionMgr.AddTag(defaultSessionChannel, sess.ID, "b")

	resp := srv.handleRPCRequest("", &JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      "1",
		Method:  "session.untag",
		Params: map[string]interface{}{
			"session_id": sess.ID,
			"tag":        "a",
		},
	})

	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error.Message)
	}
	result := resp.Result.(*session.Session)
	if len(result.Tags) != 1 || result.Tags[0] != "b" {
		t.Fatalf("expected tags ['b'], got %v", result.Tags)
	}
}

func TestHandleSessionUntagNotFound(t *testing.T) {
	srv := newTestServer()

	resp := srv.handleRPCRequest("", &JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      "1",
		Method:  "session.untag",
		Params: map[string]interface{}{
			"session_id": "nonexistent",
			"tag":        "a",
		},
	})
	if resp.Error == nil {
		t.Fatal("expected error for nonexistent session")
	}
}

func TestHandleSessionArchiveRPC(t *testing.T) {
	srv := newTestServer()
	sess := srv.sessionMgr.Create(defaultSessionChannel, "", "s-archive", "claude")

	resp := srv.handleRPCRequest("", &JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      "1",
		Method:  "session.archive",
		Params: map[string]interface{}{
			"session_id": sess.ID,
		},
	})

	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error.Message)
	}
	result := resp.Result.(*session.Session)
	if !result.Archived {
		t.Fatal("expected session to be archived")
	}
}

func TestHandleSessionUnarchiveRPC(t *testing.T) {
	srv := newTestServer()
	sess := srv.sessionMgr.Create(defaultSessionChannel, "", "s-unarchive", "claude")
	srv.sessionMgr.Archive(defaultSessionChannel, sess.ID)

	resp := srv.handleRPCRequest("", &JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      "1",
		Method:  "session.unarchive",
		Params: map[string]interface{}{
			"session_id": sess.ID,
		},
	})

	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error.Message)
	}
	result := resp.Result.(*session.Session)
	if result.Archived {
		t.Fatal("expected session to be unarchived")
	}
}

func TestHandleSessionExportRPC(t *testing.T) {
	srv := newTestServer()
	sess := srv.sessionMgr.Create(defaultSessionChannel, "", "s-export", "claude")
	srv.sessionMgr.Rename(defaultSessionChannel, sess.ID, "Exportable")
	srv.sessionMgr.AddTag(defaultSessionChannel, sess.ID, "test")

	resp := srv.handleRPCRequest("", &JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      "1",
		Method:  "session.export",
		Params: map[string]interface{}{
			"session_id": sess.ID,
			"format":     "json",
		},
	})

	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error.Message)
	}
	result := resp.Result.(map[string]interface{})
	if result["session_id"] != sess.ID {
		t.Fatalf("expected session_id %q, got %v", sess.ID, result["session_id"])
	}
	dataStr, ok := result["data"].(string)
	if !ok || dataStr == "" {
		t.Fatal("expected non-empty data in export result")
	}
}

func TestHandleSessionExportMarkdownRPC(t *testing.T) {
	srv := newTestServer()
	sess := srv.sessionMgr.Create(defaultSessionChannel, "", "s-export-md", "claude")
	srv.sessionMgr.Rename(defaultSessionChannel, sess.ID, "MD Export")

	resp := srv.handleRPCRequest("", &JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      "1",
		Method:  "session.export",
		Params: map[string]interface{}{
			"session_id": sess.ID,
			"format":     "markdown",
		},
	})

	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error.Message)
	}
	result := resp.Result.(map[string]interface{})
	dataStr := result["data"].(string)
	if len(dataStr) == 0 {
		t.Fatal("expected non-empty markdown export")
	}
}

func TestHandleSessionExportNotFoundRPC(t *testing.T) {
	srv := newTestServer()

	resp := srv.handleRPCRequest("", &JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      "1",
		Method:  "session.export",
		Params: map[string]interface{}{
			"session_id": "nonexistent",
		},
	})
	if resp.Error == nil {
		t.Fatal("expected error for nonexistent session")
	}
}

func TestHandleSessionImportRPC(t *testing.T) {
	srv := newTestServer()

	// Create and export a session
	sess := srv.sessionMgr.Create(defaultSessionChannel, "", "s-import", "claude")
	srv.sessionMgr.Rename(defaultSessionChannel, sess.ID, "Importable")
	srv.sessionMgr.AddTag(defaultSessionChannel, sess.ID, "import-tag")
	srv.sessionMgr.Archive(defaultSessionChannel, sess.ID)

	exported, _ := srv.sessionMgr.Get(defaultSessionChannel, sess.ID)
	data, err := session.ExportSession(exported, session.ExportJSON)
	if err != nil {
		t.Fatalf("export failed: %v", err)
	}

	// Delete the original session
	srv.sessionMgr.Delete(defaultSessionChannel, sess.ID)

	// Import it back
	resp := srv.handleRPCRequest("", &JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      "1",
		Method:  "session.import",
		Params: map[string]interface{}{
			"data": string(data),
		},
	})

	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error.Message)
	}
	result := resp.Result.(*session.Session)
	if result.Name != "Importable" {
		t.Fatalf("expected name 'Importable', got %q", result.Name)
	}
	if !result.Archived {
		t.Fatal("expected imported session to be archived")
	}
}

func TestPatchRemoveTagsAPI(t *testing.T) {
	srv := newTestServer()
	sess := srv.sessionMgr.Create(defaultSessionChannel, "", "s-rmtag", "claude")
	srv.sessionMgr.AddTag(defaultSessionChannel, sess.ID, "keep")
	srv.sessionMgr.AddTag(defaultSessionChannel, sess.ID, "remove")
	srv.sessionMgr.AddTag(defaultSessionChannel, sess.ID, "also-remove")

	body := strings.NewReader(`{"remove_tags": ["remove", "also-remove"]}`)
	req := httptest.NewRequest(http.MethodPatch, "/api/sessions/"+sess.ID, body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.handleSessionDetailAPI(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var result session.Session
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if len(result.Tags) != 1 || result.Tags[0] != "keep" {
		t.Fatalf("expected tags ['keep'], got %v", result.Tags)
	}
}

func TestPatchSetTagsAPI(t *testing.T) {
	srv := newTestServer()
	sess := srv.sessionMgr.Create(defaultSessionChannel, "", "s-settag", "claude")
	srv.sessionMgr.AddTag(defaultSessionChannel, sess.ID, "old")

	body := strings.NewReader(`{"tags": ["new-a", "new-b"]}`)
	req := httptest.NewRequest(http.MethodPatch, "/api/sessions/"+sess.ID, body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.handleSessionDetailAPI(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var result session.Session
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if len(result.Tags) != 2 || result.Tags[0] != "new-a" {
		t.Fatalf("expected tags ['new-a','new-b'], got %v", result.Tags)
	}
}

func TestPatchClearTagsAPI(t *testing.T) {
	srv := newTestServer()
	sess := srv.sessionMgr.Create(defaultSessionChannel, "", "s-clrtag", "claude")
	srv.sessionMgr.AddTag(defaultSessionChannel, sess.ID, "tag-a")
	srv.sessionMgr.AddTag(defaultSessionChannel, sess.ID, "tag-b")

	body := strings.NewReader(`{"tags": []}`)
	req := httptest.NewRequest(http.MethodPatch, "/api/sessions/"+sess.ID, body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.handleSessionDetailAPI(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var result session.Session
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if len(result.Tags) != 0 {
		t.Fatalf("expected 0 tags after clearing, got %v", result.Tags)
	}
}

func TestArchiveListAPIEfficiency(t *testing.T) {
	srv := newTestServer()
	srv.sessionMgr.Create(defaultSessionChannel, "", "s-arch1", "claude")
	srv.sessionMgr.Create(defaultSessionChannel, "", "s-arch2", "claude")
	srv.sessionMgr.Create(defaultSessionChannel, "", "s-arch3", "codex")
	srv.sessionMgr.Archive(defaultSessionChannel, "s-arch1")
	srv.sessionMgr.Archive(defaultSessionChannel, "s-arch3")

	req := httptest.NewRequest(http.MethodGet, "/api/sessions/archive/", nil)
	rec := httptest.NewRecorder()
	srv.handleSessionArchiveAPI(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var result map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	sessions := result["sessions"].([]interface{})
	if len(sessions) != 2 {
		t.Fatalf("expected 2 archived sessions, got %d", len(sessions))
	}
}

func TestExportAPIReturnsJSON(t *testing.T) {
	srv := newTestServer()
	sess := srv.sessionMgr.Create(defaultSessionChannel, "", "s-exp-api", "claude")
	srv.sessionMgr.Rename(defaultSessionChannel, sess.ID, "Export API Test")

	req := httptest.NewRequest(http.MethodGet, "/api/sessions/export/"+sess.ID+"?format=json", nil)
	rec := httptest.NewRecorder()
	srv.handleSessionExportAPI(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	ct := rec.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Fatalf("expected application/json content type, got %q", ct)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}
	if result["format"] != "json" {
		t.Fatalf("expected format 'json', got %v", result["format"])
	}
	sessionData := result["session"].(map[string]interface{})
	if sessionData["id"] != sess.ID {
		t.Fatalf("expected session ID %q, got %v", sess.ID, sessionData["id"])
	}
}

func TestExportAPIReturnsMarkdown(t *testing.T) {
	srv := newTestServer()
	sess := srv.sessionMgr.Create(defaultSessionChannel, "", "s-exp-md-api", "claude")
	srv.sessionMgr.Rename(defaultSessionChannel, sess.ID, "MD API")

	req := httptest.NewRequest(http.MethodGet, "/api/sessions/export/"+sess.ID+"?format=markdown", nil)
	rec := httptest.NewRecorder()
	srv.handleSessionExportAPI(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	ct := rec.Header().Get("Content-Type")
	if ct != "text/markdown; charset=utf-8" {
		t.Fatalf("expected markdown content type, got %q", ct)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "MD API") {
		t.Fatalf("expected session name in markdown output, got:\n%s", body)
	}
}

func TestImportAPIRoundTrip(t *testing.T) {
	srv := newTestServer()
	sess := srv.sessionMgr.Create(defaultSessionChannel, "", "s-imp-api", "claude")
	srv.sessionMgr.Rename(defaultSessionChannel, sess.ID, "Import API")
	srv.sessionMgr.AddTag(defaultSessionChannel, sess.ID, "api-test")

	// Export
	full, _ := srv.sessionMgr.Get(defaultSessionChannel, sess.ID)
	data, err := session.ExportSession(full, session.ExportJSON)
	if err != nil {
		t.Fatalf("export: %v", err)
	}

	// Delete original
	srv.sessionMgr.Delete(defaultSessionChannel, sess.ID)

	// Import via HTTP API
	body := strings.NewReader(fmt.Sprintf(`{"data": %q}`, string(data)))
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/import", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.handleSessionImportAPI(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var result session.Session
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if result.Name != "Import API" {
		t.Fatalf("expected name 'Import API', got %q", result.Name)
	}
	if len(result.Tags) != 1 || result.Tags[0] != "api-test" {
		t.Fatalf("expected tags ['api-test'], got %v", result.Tags)
	}
}

func TestImportAPIInvalidData(t *testing.T) {
	srv := newTestServer()

	body := strings.NewReader(`{"data": "not-json"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/import", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.handleSessionImportAPI(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestSessionsAPIExcludesArchivedByDefault(t *testing.T) {
	srv := newTestServer()
	srv.sessionMgr.Create(defaultSessionChannel, "", "s-active", "claude")
	srv.sessionMgr.Create(defaultSessionChannel, "", "s-to-archive", "claude")
	srv.sessionMgr.Archive(defaultSessionChannel, "s-to-archive")

	// Default list should exclude archived
	req := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	rec := httptest.NewRecorder()
	srv.handleSessionsAPI(rec, req)

	var result map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	sessions := result["sessions"].([]interface{})
	if len(sessions) != 1 {
		t.Fatalf("expected 1 active session (archived excluded), got %d", len(sessions))
	}

	// With archived=true, should include all
	req = httptest.NewRequest(http.MethodGet, "/api/sessions?archived=true", nil)
	rec = httptest.NewRecorder()
	srv.handleSessionsAPI(rec, req)

	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	sessions = result["sessions"].([]interface{})
	if len(sessions) != 2 {
		t.Fatalf("expected 2 sessions with archived=true, got %d", len(sessions))
	}
}

func TestSessionsAPIFilterByTag(t *testing.T) {
	srv := newTestServer()
	srv.sessionMgr.Create(defaultSessionChannel, "", "s-t1", "claude")
	srv.sessionMgr.Create(defaultSessionChannel, "", "s-t2", "claude")
	srv.sessionMgr.AddTag(defaultSessionChannel, "s-t1", "important")
	srv.sessionMgr.AddTag(defaultSessionChannel, "s-t2", "review")

	req := httptest.NewRequest(http.MethodGet, "/api/sessions?tag=important", nil)
	rec := httptest.NewRecorder()
	srv.handleSessionsAPI(rec, req)

	var result map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	sessions := result["sessions"].([]interface{})
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session with tag 'important', got %d", len(sessions))
	}
}

func TestSummariesFilteredExcludesArchived(t *testing.T) {
	mgr := session.NewManager()
	mgr.Create("cli", "", "s-f1", "claude")
	mgr.Create("cli", "", "s-f2", "claude")
	mgr.Archive("cli", "s-f2")

	// Default: exclude archived
	summaries := mgr.SummariesFiltered("", false)
	if len(summaries) != 1 {
		t.Fatalf("expected 1 active session, got %d", len(summaries))
	}

	// Include archived
	summaries = mgr.SummariesFiltered("", true)
	if len(summaries) != 2 {
		t.Fatalf("expected 2 sessions with archived, got %d", len(summaries))
	}
}

func TestSummariesFilteredByTag(t *testing.T) {
	mgr := session.NewManager()
	mgr.Create("cli", "", "s-ft1", "claude")
	mgr.Create("cli", "", "s-ft2", "claude")
	mgr.AddTag("cli", "s-ft1", "alpha")
	mgr.AddTag("cli", "s-ft2", "beta")

	summaries := mgr.SummariesFiltered("alpha", true)
	if len(summaries) != 1 {
		t.Fatalf("expected 1 session with tag 'alpha', got %d", len(summaries))
	}

	summaries = mgr.SummariesFiltered("nonexistent", true)
	if len(summaries) != 0 {
		t.Fatalf("expected 0 sessions, got %d", len(summaries))
	}
}
