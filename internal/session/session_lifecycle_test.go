package session

import (
	"sync"
	"testing"
)

func TestRename(t *testing.T) {
	mgr := NewManager()
	sess := mgr.Create("cli", "", "s-rename", "claude")

	updated, ok := mgr.Rename("cli", sess.ID, "My Session")
	if !ok {
		t.Fatal("expected rename to succeed")
	}
	if updated.Name != "My Session" {
		t.Fatalf("expected name 'My Session', got %q", updated.Name)
	}

	// Verify persistence
	fetched, ok := mgr.Get("cli", sess.ID)
	if !ok {
		t.Fatal("expected session to exist")
	}
	if fetched.Name != "My Session" {
		t.Fatalf("expected name persisted, got %q", fetched.Name)
	}
}

func TestRenameNonexistent(t *testing.T) {
	mgr := NewManager()
	_, ok := mgr.Rename("cli", "nonexistent", "X")
	if ok {
		t.Fatal("expected rename to fail for nonexistent session")
	}
}

func TestAddTag(t *testing.T) {
	mgr := NewManager()
	sess := mgr.Create("cli", "", "s-tag", "claude")

	updated, ok := mgr.AddTag("cli", sess.ID, "important")
	if !ok {
		t.Fatal("expected add tag to succeed")
	}
	if len(updated.Tags) != 1 || updated.Tags[0] != "important" {
		t.Fatalf("expected tags ['important'], got %v", updated.Tags)
	}

	// Add second tag
	updated, ok = mgr.AddTag("cli", sess.ID, "review")
	if !ok {
		t.Fatal("expected add tag to succeed")
	}
	if len(updated.Tags) != 2 {
		t.Fatalf("expected 2 tags, got %d", len(updated.Tags))
	}

	// Duplicate tag should not be added
	updated, ok = mgr.AddTag("cli", sess.ID, "important")
	if !ok {
		t.Fatal("expected duplicate add to succeed (no-op)")
	}
	if len(updated.Tags) != 2 {
		t.Fatalf("expected still 2 tags after duplicate, got %d", len(updated.Tags))
	}
}

func TestRemoveTag(t *testing.T) {
	mgr := NewManager()
	sess := mgr.Create("cli", "", "s-untag", "claude")

	mgr.AddTag("cli", sess.ID, "a")
	mgr.AddTag("cli", sess.ID, "b")
	mgr.AddTag("cli", sess.ID, "c")

	updated, ok := mgr.RemoveTag("cli", sess.ID, "b")
	if !ok {
		t.Fatal("expected remove tag to succeed")
	}
	if len(updated.Tags) != 2 {
		t.Fatalf("expected 2 tags, got %d", len(updated.Tags))
	}
	for _, tag := range updated.Tags {
		if tag == "b" {
			t.Fatal("tag 'b' should have been removed")
		}
	}

	// Remove non-existent tag is a no-op
	updated, ok = mgr.RemoveTag("cli", sess.ID, "nonexistent")
	if !ok {
		t.Fatal("expected remove non-existent tag to succeed")
	}
	if len(updated.Tags) != 2 {
		t.Fatalf("expected still 2 tags, got %d", len(updated.Tags))
	}
}

func TestArchiveAndUnarchive(t *testing.T) {
	mgr := NewManager()
	sess := mgr.Create("cli", "", "s-archive", "claude")

	if sess.Archived {
		t.Fatal("new session should not be archived")
	}

	// Archive
	updated, ok := mgr.Archive("cli", sess.ID)
	if !ok {
		t.Fatal("expected archive to succeed")
	}
	if !updated.Archived {
		t.Fatal("expected session to be archived")
	}

	// Verify persistence
	fetched, _ := mgr.Get("cli", sess.ID)
	if !fetched.Archived {
		t.Fatal("expected archived flag to persist")
	}

	// Unarchive
	updated, ok = mgr.Unarchive("cli", sess.ID)
	if !ok {
		t.Fatal("expected unarchive to succeed")
	}
	if updated.Archived {
		t.Fatal("expected session to be unarchived")
	}
}

func TestArchiveNonexistent(t *testing.T) {
	mgr := NewManager()
	_, ok := mgr.Archive("cli", "nonexistent")
	if ok {
		t.Fatal("expected archive to fail for nonexistent session")
	}
}

func TestUnarchiveNonexistent(t *testing.T) {
	mgr := NewManager()
	_, ok := mgr.Unarchive("cli", "nonexistent")
	if ok {
		t.Fatal("expected unarchive to fail for nonexistent session")
	}
}

func TestListByTag(t *testing.T) {
	mgr := NewManager()
	mgr.Create("cli", "", "s-1", "claude")
	mgr.Create("cli", "", "s-2", "claude")
	mgr.Create("cli", "", "s-3", "codex")

	mgr.AddTag("cli", "s-1", "important")
	mgr.AddTag("cli", "s-2", "important")
	mgr.AddTag("cli", "s-3", "review")

	tagged := mgr.ListByTag("important")
	if len(tagged) != 2 {
		t.Fatalf("expected 2 sessions with 'important' tag, got %d", len(tagged))
	}

	tagged = mgr.ListByTag("review")
	if len(tagged) != 1 {
		t.Fatalf("expected 1 session with 'review' tag, got %d", len(tagged))
	}

	tagged = mgr.ListByTag("nonexistent")
	if len(tagged) != 0 {
		t.Fatalf("expected 0 sessions, got %d", len(tagged))
	}
}

func TestListArchived(t *testing.T) {
	mgr := NewManager()
	mgr.Create("cli", "", "s-a1", "claude")
	mgr.Create("cli", "", "s-a2", "claude")
	mgr.Create("cli", "", "s-a3", "codex")

	mgr.Archive("cli", "s-a1")
	mgr.Archive("cli", "s-a3")

	archived := mgr.ListArchived()
	if len(archived) != 2 {
		t.Fatalf("expected 2 archived sessions, got %d", len(archived))
	}
}

func TestSummaryIncludesNewFields(t *testing.T) {
	mgr := NewManager()
	sess := mgr.Create("cli", "", "s-summary", "claude")
	mgr.Rename("cli", sess.ID, "Test Session")
	mgr.AddTag("cli", sess.ID, "tag1")
	mgr.AddTag("cli", sess.ID, "tag2")
	mgr.Archive("cli", sess.ID)

	updated, _ := mgr.Get("cli", sess.ID)
	summary := updated.Summary()

	if summary.Name != "Test Session" {
		t.Fatalf("expected name in summary, got %q", summary.Name)
	}
	if len(summary.Tags) != 2 {
		t.Fatalf("expected 2 tags in summary, got %d", len(summary.Tags))
	}
	if !summary.Archived {
		t.Fatal("expected archived=true in summary")
	}
}


func TestSetTags(t *testing.T) {
	mgr := NewManager()
	sess := mgr.Create("cli", "", "s-settags", "claude")
	mgr.AddTag("cli", sess.ID, "old-1")
	mgr.AddTag("cli", sess.ID, "old-2")

	updated, ok := mgr.SetTags("cli", sess.ID, []string{"new-a", "new-b", "new-c"})
	if !ok {
		t.Fatal("expected SetTags to succeed")
	}
	if len(updated.Tags) != 3 {
		t.Fatalf("expected 3 tags, got %d", len(updated.Tags))
	}
	if updated.Tags[0] != "new-a" || updated.Tags[2] != "new-c" {
		t.Fatalf("expected tags ['new-a','new-b','new-c'], got %v", updated.Tags)
	}

	// Verify persistence
	fetched, _ := mgr.Get("cli", sess.ID)
	if len(fetched.Tags) != 3 {
		t.Fatalf("expected 3 tags persisted, got %d", len(fetched.Tags))
	}

	// SetTags with empty list clears tags
	updated, ok = mgr.SetTags("cli", sess.ID, []string{})
	if !ok {
		t.Fatal("expected SetTags with empty list to succeed")
	}
	if len(updated.Tags) != 0 {
		t.Fatalf("expected 0 tags after clearing, got %d", len(updated.Tags))
	}
}

func TestSetTagsNonexistent(t *testing.T) {
	mgr := NewManager()
	_, ok := mgr.SetTags("cli", "nonexistent", []string{"tag"})
	if ok {
		t.Fatal("expected SetTags to fail for nonexistent session")
	}
}

func TestCloneSessionPreservesTags(t *testing.T) {
	mgr := NewManager()
	sess := mgr.Create("cli", "", "s-clone", "claude")
	mgr.AddTag("cli", sess.ID, "tag-a")
	mgr.AddTag("cli", sess.ID, "tag-b")
	mgr.Rename("cli", sess.ID, "Cloned")

	original, _ := mgr.Get("cli", sess.ID)
	cloned := cloneSession(original)

	if cloned.Name != original.Name {
		t.Fatalf("expected name %q, got %q", original.Name, cloned.Name)
	}
	if len(cloned.Tags) != len(original.Tags) {
		t.Fatalf("expected %d tags, got %d", len(original.Tags), len(cloned.Tags))
	}

	// Mutating clone should not affect original
	cloned.Tags[0] = "modified"
	originalAgain, _ := mgr.Get("cli", sess.ID)
	if originalAgain.Tags[0] == "modified" {
		t.Fatal("clone mutation should not affect original")
	}
}

func TestConcurrentTagOperations(t *testing.T) {
	mgr := NewManager()
	sess := mgr.Create("cli", "", "s-concurrent", "claude")

	const goroutines = 20
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(i int) {
			defer wg.Done()
			tag := "tag-" + string(rune('a'+i%10))
			mgr.AddTag("cli", sess.ID, tag)
			mgr.Rename("cli", sess.ID, "name-"+string(rune('a'+i%5)))
		}(i)
	}

	// Also do concurrent SetTags and removes
	wg.Add(10)
	for i := 0; i < 10; i++ {
		go func(i int) {
			defer wg.Done()
			mgr.SetTags("cli", sess.ID, []string{"set-" + string(rune('a'+i%3))})
			mgr.RemoveTag("cli", sess.ID, "tag-"+string(rune('a'+i%10)))
		}(i)
	}

	wg.Wait()

	// Verify the session is still valid (no panic, no corruption)
	final, ok := mgr.Get("cli", sess.ID)
	if !ok {
		t.Fatal("expected session to exist after concurrent operations")
	}
	if final.ID != sess.ID {
		t.Fatalf("session ID corrupted: %q", final.ID)
	}
}
