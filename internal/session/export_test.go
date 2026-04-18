package session

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/smallnest/imclaw/internal/agent"
)

func TestExportSessionJSON(t *testing.T) {
	sess := &Session{
		ID:        "test-1",
		Channel:   "cli",
		AgentName: "claude",
		Name:      "Test Session",
		Tags:      []string{"important", "review"},
		CreatedAt: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		LastActive: time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC),
		Status:    "idle",
		Metadata:  map[string]interface{}{},
		Activity: []Activity{
			{ID: 1, Type: ActivityPrompt, RequestID: "r1", Timestamp: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), Prompt: "hello"},
			{ID: 2, Type: ActivityResult, RequestID: "r1", Timestamp: time.Date(2025, 1, 1, 0, 1, 0, 0, time.UTC), Content: "world"},
		},
	}

	data, err := ExportSession(sess, ExportJSON)
	if err != nil {
		t.Fatalf("export failed: %v", err)
	}

	var exportData ExportData
	if err := json.Unmarshal(data, &exportData); err != nil {
		t.Fatalf("failed to parse exported JSON: %v", err)
	}
	if exportData.Format != "json" {
		t.Fatalf("expected format 'json', got %q", exportData.Format)
	}
	if exportData.Session == nil {
		t.Fatal("expected session in export")
	}
	if exportData.Session.ID != "test-1" {
		t.Fatalf("expected session ID 'test-1', got %q", exportData.Session.ID)
	}
	if exportData.Session.Name != "Test Session" {
		t.Fatalf("expected name 'Test Session', got %q", exportData.Session.Name)
	}
	if len(exportData.Session.Tags) != 2 {
		t.Fatalf("expected 2 tags, got %d", len(exportData.Session.Tags))
	}
	if len(exportData.Session.Activity) != 2 {
		t.Fatalf("expected 2 activities, got %d", len(exportData.Session.Activity))
	}
}

func TestExportSessionMarkdown(t *testing.T) {
	sess := &Session{
		ID:        "test-md",
		Channel:   "cli",
		AgentName: "claude",
		Name:      "Markdown Session",
		Tags:      []string{"tag1"},
		Archived:  true,
		CreatedAt: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		LastActive: time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC),
		Status:    "idle",
		Metadata:  map[string]interface{}{},
		Activity: []Activity{
			{ID: 1, Type: ActivityPrompt, RequestID: "r1", Timestamp: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), Prompt: "What is Go?"},
			{ID: 2, Type: ActivityResult, RequestID: "r1", Timestamp: time.Date(2025, 1, 1, 0, 1, 0, 0, time.UTC), Content: "Go is a programming language."},
			{ID: 3, Type: ActivityError, RequestID: "r2", Timestamp: time.Date(2025, 1, 1, 0, 2, 0, 0, time.UTC), Error: "something went wrong"},
		},
	}

	data, err := ExportSession(sess, ExportMarkdown)
	if err != nil {
		t.Fatalf("export failed: %v", err)
	}

	md := string(data)
	if !strings.Contains(md, "# Session: Markdown Session") {
		t.Fatalf("expected session name in markdown header, got:\n%s", md)
	}
	if !strings.Contains(md, "**Tags**: tag1") {
		t.Fatalf("expected tags in markdown, got:\n%s", md)
	}
	if !strings.Contains(md, "**Archived**: yes") {
		t.Fatalf("expected archived flag in markdown, got:\n%s", md)
	}
	if !strings.Contains(md, "What is Go?") {
		t.Fatalf("expected prompt in activity, got:\n%s", md)
	}
	if !strings.Contains(md, "Go is a programming language.") {
		t.Fatalf("expected result in activity, got:\n%s", md)
	}
	if !strings.Contains(md, "something went wrong") {
		t.Fatalf("expected error in activity, got:\n%s", md)
	}
}

func TestExportSessionMarkdownUsesIDWhenNoName(t *testing.T) {
	sess := &Session{
		ID:        "test-no-name",
		AgentName: "claude",
		Metadata:  map[string]interface{}{},
	}
	data, err := ExportSession(sess, ExportMarkdown)
	if err != nil {
		t.Fatalf("export failed: %v", err)
	}
	if !strings.Contains(string(data), "test-no-name") {
		t.Fatalf("expected ID in header when no name set, got:\n%s", string(data))
	}
}

func TestExportSessionUnsupported(t *testing.T) {
	sess := &Session{ID: "x", Metadata: map[string]interface{}{}}
	_, err := ExportSession(sess, ExportFormat("xml"))
	if err == nil {
		t.Fatal("expected error for unsupported format")
	}
}

func TestExportSessionNil(t *testing.T) {
	_, err := ExportSession(nil, ExportJSON)
	if err == nil {
		t.Fatal("expected error for nil session")
	}
}

func TestImportSessionFromJSON(t *testing.T) {
	// First export a session
	sess := &Session{
		ID:        "import-test",
		Channel:   "cli",
		AgentName: "claude",
		Name:      "Imported Session",
		Tags:      []string{"imported"},
		CreatedAt: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		LastActive: time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC),
		Status:    "idle",
		Metadata:  map[string]interface{}{},
		Activity: []Activity{
			{ID: 1, Type: ActivityPrompt, RequestID: "r1", Prompt: "hello"},
		},
	}

	data, err := ExportSession(sess, ExportJSON)
	if err != nil {
		t.Fatalf("export failed: %v", err)
	}

	// Import it back
	imported, err := ImportSession(data)
	if err != nil {
		t.Fatalf("import failed: %v", err)
	}
	if imported.ID != "import-test" {
		t.Fatalf("expected ID 'import-test', got %q", imported.ID)
	}
	if imported.Name != "Imported Session" {
		t.Fatalf("expected name 'Imported Session', got %q", imported.Name)
	}
	if len(imported.Tags) != 1 || imported.Tags[0] != "imported" {
		t.Fatalf("expected tags ['imported'], got %v", imported.Tags)
	}
	if len(imported.Activity) != 1 {
		t.Fatalf("expected 1 activity, got %d", len(imported.Activity))
	}
}

func TestImportSessionInvalidData(t *testing.T) {
	_, err := ImportSession([]byte("not json"))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestImportSessionMissingSession(t *testing.T) {
	data := []byte(`{"exported_at":"2025-01-01T00:00:00Z","format":"json","version":"1.0"}`)
	_, err := ImportSession(data)
	if err == nil {
		t.Fatal("expected error for missing session field")
	}
}

func TestRoundTripExportImport(t *testing.T) {
	sess := &Session{
		ID:        "round-trip",
		Channel:   "cli",
		AgentName: "claude",
		Name:      "Round Trip",
		Tags:      []string{"a", "b", "c"},
		Archived:  true,
		CreatedAt: time.Date(2025, 6, 15, 10, 30, 0, 0, time.UTC),
		LastActive: time.Date(2025, 6, 15, 11, 0, 0, 0, time.UTC),
		Status:    "idle",
		Metadata:  map[string]interface{}{"key": "value"},
		Activity: []Activity{
			{ID: 1, Type: ActivityPrompt, RequestID: "r1", Prompt: "first"},
			{ID: 2, Type: ActivityResult, RequestID: "r1", Content: "result"},
			{ID: 3, Type: ActivityError, RequestID: "r2", Error: "oops"},
		},
	}

	data, err := ExportSession(sess, ExportJSON)
	if err != nil {
		t.Fatalf("export: %v", err)
	}

	restored, err := ImportSession(data)
	if err != nil {
		t.Fatalf("import: %v", err)
	}

	if restored.ID != sess.ID {
		t.Fatalf("ID mismatch: %q vs %q", restored.ID, sess.ID)
	}
	if restored.Name != sess.Name {
		t.Fatalf("Name mismatch: %q vs %q", restored.Name, sess.Name)
	}
	if restored.Archived != sess.Archived {
		t.Fatalf("Archived mismatch: %v vs %v", restored.Archived, sess.Archived)
	}
	if len(restored.Tags) != len(sess.Tags) {
		t.Fatalf("Tags count mismatch: %d vs %d", len(restored.Tags), len(sess.Tags))
	}
	if len(restored.Activity) != len(sess.Activity) {
		t.Fatalf("Activity count mismatch: %d vs %d", len(restored.Activity), len(sess.Activity))
	}
}

func TestExportMarkdownWithEvent(t *testing.T) {
	sess := &Session{
		ID:        "test-event-md",
		AgentName: "claude",
		Name:      "Event Session",
		Metadata:  map[string]interface{}{},
		Activity: []Activity{
			{
				ID:        1,
				Type:      ActivityEvent,
				RequestID: "r1",
				Timestamp: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
				Content:   "event content here",
				Event: &agent.Event{
					Name:   "tool_use",
					Input:  "search(query=\"hello\")",
					Output: "found 3 results",
				},
			},
			{
				ID:        2,
				Type:      ActivityEvent,
				RequestID: "r2",
				Timestamp: time.Date(2025, 1, 1, 0, 1, 0, 0, time.UTC),
				Content:   "content-only event",
			},
		},
	}

	data, err := ExportSession(sess, ExportMarkdown)
	if err != nil {
		t.Fatalf("export failed: %v", err)
	}

	md := string(data)
	if !strings.Contains(md, "**Name**: tool_use") {
		t.Fatalf("expected event name in markdown, got:\n%s", md)
	}
	if !strings.Contains(md, "**Input**: search(query=\"hello\")") {
		t.Fatalf("expected event input in markdown, got:\n%s", md)
	}
	if !strings.Contains(md, "**Output**: found 3 results") {
		t.Fatalf("expected event output in markdown, got:\n%s", md)
	}
	if !strings.Contains(md, "event content here") {
		t.Fatalf("expected event content in markdown, got:\n%s", md)
	}
	if !strings.Contains(md, "content-only event") {
		t.Fatalf("expected content-only event in markdown, got:\n%s", md)
	}
}

func TestImportSessionVersionValidation(t *testing.T) {
	// Valid version should succeed
	validData := []byte(`{"exported_at":"2025-01-01T00:00:00Z","format":"json","version":"1.0","session":{"id":"v1","metadata":{}}}`)
	_, err := ImportSession(validData)
	if err != nil {
		t.Fatalf("expected valid version to import, got: %v", err)
	}

	// Unsupported version should fail
	invalidData := []byte(`{"exported_at":"2025-01-01T00:00:00Z","format":"json","version":"2.0","session":{"id":"v2","metadata":{}}}`)
	_, err = ImportSession(invalidData)
	if err == nil {
		t.Fatal("expected error for unsupported version")
	}
	if !strings.Contains(err.Error(), "unsupported export version") {
		t.Fatalf("expected version error, got: %v", err)
	}

	// Empty version (legacy/missing) should still succeed
	emptyVerData := []byte(`{"exported_at":"2025-01-01T00:00:00Z","format":"json","version":"","session":{"id":"v3","metadata":{}}}`)
	_, err = ImportSession(emptyVerData)
	if err != nil {
		t.Fatalf("expected empty version to import, got: %v", err)
	}
}
