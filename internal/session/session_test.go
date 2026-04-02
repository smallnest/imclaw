package session

import (
	"testing"

	"github.com/smallnest/imclaw/internal/agent"
)

func TestManagerRecordsSessionActivity(t *testing.T) {
	mgr := NewManager()
	sess := mgr.Create("cli", "", "s-1", "claude")

	updated, ok := mgr.RecordPrompt("cli", sess.ID, "req-1", "hello")
	if !ok {
		t.Fatal("expected prompt to be recorded")
	}
	if !updated.Active || updated.Status != "running" {
		t.Fatalf("expected running session after prompt, got active=%v status=%q", updated.Active, updated.Status)
	}

	updated, ok = mgr.RecordEvent("cli", sess.ID, "req-1", agent.Event{Type: agent.TypeToolStart, Name: "Read"})
	if !ok {
		t.Fatal("expected event to be recorded")
	}
	if got := len(updated.Activity); got != 2 {
		t.Fatalf("expected 2 activities, got %d", got)
	}

	updated, ok = mgr.RecordResult("cli", sess.ID, "req-1", "done")
	if !ok {
		t.Fatal("expected result to be recorded")
	}
	if updated.Active || updated.Status != "idle" || updated.LastOutput != "done" {
		t.Fatalf("unexpected session summary after result: %#v", updated)
	}

	updated, ok = mgr.RecordError("cli", sess.ID, "req-2", "permission denied")
	if !ok {
		t.Fatal("expected error to be recorded")
	}
	if updated.Status != "error" || updated.LastError != "permission denied" {
		t.Fatalf("unexpected session error state: %#v", updated)
	}
}

func TestSummariesSortedByLastActive(t *testing.T) {
	mgr := NewManager()
	older := mgr.Create("cli", "", "older", "claude")
	mgr.Create("cli", "", "newer", "codex")
	if _, ok := mgr.RecordPrompt("cli", older.ID, "req-1", "hello"); !ok {
		t.Fatal("expected prompt to update session ordering")
	}

	summaries := mgr.Summaries()
	if len(summaries) != 2 {
		t.Fatalf("expected 2 summaries, got %d", len(summaries))
	}
	if summaries[0].ID != older.ID {
		t.Fatalf("expected most recently active session first, got %#v", summaries)
	}
}
