package job

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestNewJob(t *testing.T) {
	prompt := "test prompt"
	agentName := "test-agent"

	job := newJob(prompt, agentName)

	if job.ID == "" {
		t.Error("expected job ID to be set")
	}
	if job.Status != StatusQueued {
		t.Errorf("expected status %s, got %s", StatusQueued, job.Status)
	}
	if job.Prompt != prompt {
		t.Errorf("expected prompt %s, got %s", prompt, job.Prompt)
	}
	if job.AgentName != agentName {
		t.Errorf("expected agent name %s, got %s", agentName, job.AgentName)
	}
	if job.CreatedAt.IsZero() {
		t.Error("expected CreatedAt to be set")
	}
	if job.StartedAt != nil {
		t.Error("expected StartedAt to be nil")
	}
	if job.FinishedAt != nil {
		t.Error("expected FinishedAt to be nil")
	}
}

func TestJobStatusTransition_Valid(t *testing.T) {
	tests := []struct {
		name     string
		from     JobStatus
		to       JobStatus
		wantErr  bool
	}{
		{"queued to running", StatusQueued, StatusRunning, false},
		{"queued to canceled", StatusQueued, StatusCanceled, false},
		{"running to completed", StatusRunning, StatusCompleted, false},
		{"running to failed", StatusRunning, StatusFailed, false},
		{"running to canceled", StatusRunning, StatusCanceled, false},
		{"failed to queued (retry)", StatusFailed, StatusQueued, false},
		{"completed to completed (invalid)", StatusCompleted, StatusCompleted, true},
		{"running to queued (invalid)", StatusRunning, StatusQueued, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			job := &Job{Status: tt.from}
			err := job.transitionStatus(tt.to)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error but got nil")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if job.Status != tt.to {
					t.Errorf("expected status %s, got %s", tt.to, job.Status)
				}
			}
		})
	}
}

func TestJobStatusTransition_UpdatesTimestamps(t *testing.T) {
	t.Run("running sets StartedAt", func(t *testing.T) {
		job := &Job{Status: StatusQueued}
		if err := job.transitionStatus(StatusRunning); err != nil {
			t.Fatal(err)
		}
		if job.StartedAt == nil {
			t.Error("expected StartedAt to be set")
		}
	})

	t.Run("completed sets FinishedAt", func(t *testing.T) {
		job := &Job{Status: StatusRunning, StartedAt: &time.Time{}}
		if err := job.transitionStatus(StatusCompleted); err != nil {
			t.Fatal(err)
		}
		if job.FinishedAt == nil {
			t.Error("expected FinishedAt to be set")
		}
	})

	t.Run("failed sets FinishedAt", func(t *testing.T) {
		job := &Job{Status: StatusRunning, StartedAt: &time.Time{}}
		if err := job.transitionStatus(StatusFailed); err != nil {
			t.Fatal(err)
		}
		if job.FinishedAt == nil {
			t.Error("expected FinishedAt to be set")
		}
	})

	t.Run("canceled sets FinishedAt", func(t *testing.T) {
		job := &Job{Status: StatusRunning, StartedAt: &time.Time{}}
		if err := job.transitionStatus(StatusCanceled); err != nil {
			t.Fatal(err)
		}
		if job.FinishedAt == nil {
			t.Error("expected FinishedAt to be set")
		}
	})
}

func TestManagerSubmit(t *testing.T) {
	mgr := NewManager()
	prompt := "test prompt"
	agentName := "test-agent"

	job := mgr.Submit(prompt, agentName)

	if job == nil {
		t.Fatal("expected job to be returned")
	}

	if job.ID == "" {
		t.Error("expected job ID to be set")
	}

	// Verify job is stored
	retrieved, ok := mgr.Get(job.ID)
	if !ok {
		t.Error("expected job to be stored in manager")
	}
	if retrieved.ID != job.ID {
		t.Errorf("expected ID %s, got %s", job.ID, retrieved.ID)
	}
}

func TestManagerGet(t *testing.T) {
	mgr := NewManager()

	t.Run("existing job", func(t *testing.T) {
		job := mgr.Submit("test", "agent")
		retrieved, ok := mgr.Get(job.ID)
		if !ok {
			t.Error("expected job to be found")
		}
		if retrieved.ID != job.ID {
			t.Errorf("expected ID %s, got %s", job.ID, retrieved.ID)
		}
	})

	t.Run("non-existing job", func(t *testing.T) {
		_, ok := mgr.Get("non-existent")
		if ok {
			t.Error("expected ok to be false for non-existent job")
		}
	})
}

func TestManagerList(t *testing.T) {
	mgr := NewManager()

	// Submit multiple jobs
	job1 := mgr.Submit("prompt1", "agent1")
	time.Sleep(10 * time.Millisecond)
	job2 := mgr.Submit("prompt2", "agent2")
	time.Sleep(10 * time.Millisecond)
	job3 := mgr.Submit("prompt3", "agent3")

	jobs := mgr.List()

	if len(jobs) != 3 {
		t.Errorf("expected 3 jobs, got %d", len(jobs))
	}

	// Check that jobs are sorted by CreatedAt descending (newest first)
	if jobs[0].ID != job3.ID {
		t.Errorf("expected first job to be %s, got %s", job3.ID, jobs[0].ID)
	}
	if jobs[1].ID != job2.ID {
		t.Errorf("expected second job to be %s, got %s", job2.ID, jobs[1].ID)
	}
	if jobs[2].ID != job1.ID {
		t.Errorf("expected third job to be %s, got %s", job1.ID, jobs[2].ID)
	}
}

func TestManagerSummaries(t *testing.T) {
	mgr := NewManager()

	job := mgr.Submit("test prompt", "test-agent")
	summaries := mgr.Summaries()

	if len(summaries) != 1 {
		t.Errorf("expected 1 summary, got %d", len(summaries))
	}

	summary := summaries[0]
	if summary.ID != job.ID {
		t.Errorf("expected ID %s, got %s", job.ID, summary.ID)
	}
	if summary.Status != job.Status {
		t.Errorf("expected status %s, got %s", job.Status, summary.Status)
	}
	if summary.Prompt != job.Prompt {
		t.Errorf("expected prompt %s, got %s", job.Prompt, summary.Prompt)
	}
}

func TestManagerStart(t *testing.T) {
	mgr := NewManager()
	job := mgr.Submit("test", "agent")
	_, cancel := context.WithCancel(context.Background())

	err := mgr.Start(job.ID, cancel)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify status changed
	retrieved, _ := mgr.Get(job.ID)
	if retrieved.Status != StatusRunning {
		t.Errorf("expected status %s, got %s", StatusRunning, retrieved.Status)
	}
	if retrieved.StartedAt == nil {
		t.Error("expected StartedAt to be set")
	}
}

func TestManagerComplete(t *testing.T) {
	mgr := NewManager()
	job := mgr.Submit("test", "agent")
	_, cancel := context.WithCancel(context.Background())
	mgr.Start(job.ID, cancel)

	result := "task completed successfully"
	err := mgr.Complete(job.ID, result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify status and result
	retrieved, _ := mgr.Get(job.ID)
	if retrieved.Status != StatusCompleted {
		t.Errorf("expected status %s, got %s", StatusCompleted, retrieved.Status)
	}
	if retrieved.Result != result {
		t.Errorf("expected result %s, got %s", result, retrieved.Result)
	}
	if retrieved.FinishedAt == nil {
		t.Error("expected FinishedAt to be set")
	}
}

func TestManagerFail(t *testing.T) {
	mgr := NewManager()
	job := mgr.Submit("test", "agent")
	_, cancel := context.WithCancel(context.Background())
	mgr.Start(job.ID, cancel)

	errorMsg := "something went wrong"
	err := mgr.Fail(job.ID, errorMsg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify status and error
	retrieved, _ := mgr.Get(job.ID)
	if retrieved.Status != StatusFailed {
		t.Errorf("expected status %s, got %s", StatusFailed, retrieved.Status)
	}
	if retrieved.Error != errorMsg {
		t.Errorf("expected error %s, got %s", errorMsg, retrieved.Error)
	}
	if retrieved.FinishedAt == nil {
		t.Error("expected FinishedAt to be set")
	}
}

func TestManagerCancel(t *testing.T) {
	t.Run("cancel queued job", func(t *testing.T) {
		mgr := NewManager()
		job := mgr.Submit("test", "agent")

		err := mgr.Cancel(job.ID)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		retrieved, _ := mgr.Get(job.ID)
		if retrieved.Status != StatusCanceled {
			t.Errorf("expected status %s, got %s", StatusCanceled, retrieved.Status)
		}
	})

	t.Run("cancel running job", func(t *testing.T) {
		mgr := NewManager()
		job := mgr.Submit("test", "agent")
		_, cancel := context.WithCancel(context.Background())
		mgr.Start(job.ID, cancel)

		err := mgr.Cancel(job.ID)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		retrieved, _ := mgr.Get(job.ID)
		if retrieved.Status != StatusCanceled {
			t.Errorf("expected status %s, got %s", StatusCanceled, retrieved.Status)
		}
	})
}

func TestManagerAddLog(t *testing.T) {
	mgr := NewManager()
	job := mgr.Submit("test", "agent")

	err := mgr.AddLog(job.ID, "info", "test message")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	retrieved, _ := mgr.Get(job.ID)
	if len(retrieved.Logs) != 2 { // Submit adds one log
		t.Errorf("expected 2 logs, got %d", len(retrieved.Logs))
	}

	// Check log content
	log := retrieved.Logs[1]
	if log.Level != "info" {
		t.Errorf("expected level 'info', got '%s'", log.Level)
	}
	if log.Message != "test message" {
		t.Errorf("expected message 'test message', got '%s'", log.Message)
	}
}

func TestManagerDelete(t *testing.T) {
	mgr := NewManager()
	job := mgr.Submit("test", "agent")

	err := mgr.Delete(job.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify job is deleted
	_, ok := mgr.Get(job.ID)
	if ok {
		t.Error("expected job to be deleted")
	}
}

func TestManagerDelete_RunningJob(t *testing.T) {
	mgr := NewManager()
	job := mgr.Submit("test", "agent")
	_, cancel := context.WithCancel(context.Background())
	mgr.Start(job.ID, cancel)

	err := mgr.Delete(job.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify job is deleted
	_, ok := mgr.Get(job.ID)
	if ok {
		t.Error("expected job to be deleted")
	}
}

func TestManagerCleanup(t *testing.T) {
	mgr := NewManager()

	// Create jobs with different states
	job1 := mgr.Submit("queued", "agent")
	_, cancel := context.WithCancel(context.Background())
	mgr.Start(job1.ID, cancel)
	mgr.Complete(job1.ID, "done")

	job2 := mgr.Submit("running", "agent")
	_, cancel2 := context.WithCancel(context.Background())
	mgr.Start(job2.ID, cancel2)

	// Manually set FinishedAt to old time by modifying internal job directly
	oldTime := time.Now().Add(-2 * time.Hour)
	mgr.mu.Lock()
	if j, ok := mgr.jobs[job1.ID]; ok {
		j.FinishedAt = &oldTime
	}
	mgr.mu.Unlock()

	// Cleanup jobs older than 1 hour
	count := mgr.Cleanup(1 * time.Hour)
	if count != 1 {
		t.Errorf("expected to cleanup 1 job, got %d", count)
	}

	// Verify job1 is deleted, job2 still exists
	_, ok := mgr.Get(job1.ID)
	if ok {
		t.Error("expected completed job to be cleaned up")
	}

	_, ok = mgr.Get(job2.ID)
	if !ok {
		t.Error("expected running job to still exist")
	}
}

func TestExecuteJob_Success(t *testing.T) {
	mgr := NewManager()
	job := mgr.Submit("test prompt", "agent")

	executor := func(ctx context.Context, prompt string, logFn func(level, msg string)) (string, error) {
		logFn("info", "starting execution")
		return "result", nil
	}

	go ExecuteJob(context.Background(), mgr, job.ID, executor)

	// Wait for execution to complete
	time.Sleep(100 * time.Millisecond)

	retrieved, _ := mgr.Get(job.ID)
	if retrieved.Status != StatusCompleted {
		t.Errorf("expected status %s, got %s", StatusCompleted, retrieved.Status)
	}
	if retrieved.Result != "result" {
		t.Errorf("expected result 'result', got '%s'", retrieved.Result)
	}
}

func TestExecuteJob_Failure(t *testing.T) {
	mgr := NewManager()
	job := mgr.Submit("test prompt", "agent")

	executor := func(ctx context.Context, prompt string, logFn func(level, msg string)) (string, error) {
		return "", errors.New("execution failed")
	}

	go ExecuteJob(context.Background(), mgr, job.ID, executor)

	// Wait for execution to complete
	time.Sleep(100 * time.Millisecond)

	retrieved, _ := mgr.Get(job.ID)
	if retrieved.Status != StatusFailed {
		t.Errorf("expected status %s, got %s", StatusFailed, retrieved.Status)
	}
	if retrieved.Error != "execution failed" {
		t.Errorf("expected error 'execution failed', got '%s'", retrieved.Error)
	}
}

func TestExecuteJob_Cancellation(t *testing.T) {
	mgr := NewManager()
	job := mgr.Submit("test prompt", "agent")

	executor := func(ctx context.Context, prompt string, logFn func(level, msg string)) (string, error) {
		// Check if context is cancelled
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(200 * time.Millisecond):
			return "result", nil
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	go ExecuteJob(ctx, mgr, job.ID, executor)

	// Cancel immediately
	time.Sleep(10 * time.Millisecond)
	cancel()

	// Wait for cancellation to process
	time.Sleep(300 * time.Millisecond)

	retrieved, _ := mgr.Get(job.ID)
	if retrieved.Status != StatusCanceled {
		t.Errorf("expected status %s, got %s", StatusCanceled, retrieved.Status)
	}
}

func TestJobSummary(t *testing.T) {
	job := &Job{
		ID:        "test-id",
		Status:    StatusCompleted,
		Prompt:    "test prompt",
		AgentName: "test-agent",
		CreatedAt: time.Now(),
	}

	now := time.Now()
	job.StartedAt = &now
	job.FinishedAt = &now

	summary := job.Summary()

	if summary.ID != job.ID {
		t.Errorf("expected ID %s, got %s", job.ID, summary.ID)
	}
	if summary.Status != job.Status {
		t.Errorf("expected status %s, got %s", job.Status, summary.Status)
	}
	if summary.Prompt != job.Prompt {
		t.Errorf("expected prompt %s, got %s", job.Prompt, summary.Prompt)
	}
}

// TestListDoesNotIncludeLogs verifies that List() does not copy log entries.
// This prevents memory leaks when listing jobs with large logs.
func TestListDoesNotIncludeLogs(t *testing.T) {
	mgr := NewManager()
	job := mgr.Submit("test prompt", "test-agent")

	// Add many log entries
	for i := 0; i < 100; i++ {
		mgr.AddLog(job.ID, "info", fmt.Sprintf("Log entry %d", i))
	}

	// Verify logs are in the original job
	originalJob, ok := mgr.Get(job.ID)
	if !ok {
		t.Fatal("job not found")
	}
	if len(originalJob.Logs) != 101 { // Submit adds 1 log + 100 logs
		t.Errorf("expected 101 logs in original job, got %d", len(originalJob.Logs))
	}

	// Verify List() does not include logs
	jobs := mgr.List()
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}
	if jobs[0].Logs != nil {
		t.Errorf("List() should not include logs, but got %d logs", len(jobs[0].Logs))
	}
}

// TestLogSizeLimit verifies that log entries are limited to MaxLogEntries.
func TestLogSizeLimit(t *testing.T) {
	mgr := NewManager()
	job := mgr.Submit("test prompt", "test-agent")

	// Add more log entries than MaxLogEntries
	for i := 0; i < MaxLogEntries+100; i++ {
		mgr.AddLog(job.ID, "info", fmt.Sprintf("Log entry %d", i))
	}

	// Verify logs are limited
	retrieved, ok := mgr.Get(job.ID)
	if !ok {
		t.Fatal("job not found")
	}
	if len(retrieved.Logs) > MaxLogEntries {
		t.Errorf("logs should be limited to %d, got %d", MaxLogEntries, len(retrieved.Logs))
	}

	// Verify we kept the most recent entries
	// The first log should be around index 100 (not 0)
	if retrieved.Logs[0].Message != "Log entry 100" {
		t.Errorf("expected oldest log to be 'Log entry 100', got '%s'", retrieved.Logs[0].Message)
	}
}

// TestListSorting verifies that List() returns jobs in descending order by creation time.
func TestListSorting(t *testing.T) {
	mgr := NewManager()

	// Create multiple jobs with slight delays to ensure different timestamps
	ids := make([]string, 5)
	for i := 0; i < 5; i++ {
		job := mgr.Submit(fmt.Sprintf("prompt-%d", i), "agent")
		ids[i] = job.ID
		time.Sleep(10 * time.Millisecond) // Ensure different timestamps
	}

	jobs := mgr.List()

	// Verify we have all jobs
	if len(jobs) != 5 {
		t.Fatalf("expected 5 jobs, got %d", len(jobs))
	}

	// Verify descending order (newest first)
	for i := 0; i < len(jobs)-1; i++ {
		if jobs[i].CreatedAt.Before(jobs[i+1].CreatedAt) {
			t.Errorf("jobs not sorted in descending order: job[%d].CreatedAt=%v after job[%d].CreatedAt=%v",
				i, jobs[i].CreatedAt, i+1, jobs[i+1].CreatedAt)
		}
	}

	// Verify the newest job is last (highest index in creation order)
	if jobs[0].ID != ids[4] {
		t.Errorf("expected newest job ID %s at position 0, got %s", ids[4], jobs[0].ID)
	}
}

// TestSummariesSorting verifies that Summaries() returns jobs in descending order.
func TestSummariesSorting(t *testing.T) {
	mgr := NewManager()

	// Create multiple jobs
	for i := 0; i < 5; i++ {
		mgr.Submit(fmt.Sprintf("prompt-%d", i), "agent")
		time.Sleep(10 * time.Millisecond)
	}

	summaries := mgr.Summaries()

	// Verify descending order
	for i := 0; i < len(summaries)-1; i++ {
		if summaries[i].CreatedAt.Before(summaries[i+1].CreatedAt) {
			t.Errorf("summaries not sorted in descending order")
		}
	}
}

// BenchmarkListJobs benchmarks the List() method with many jobs.
func BenchmarkListJobs(b *testing.B) {
	mgr := NewManager()
	// Create 1000 jobs
	for i := 0; i < 1000; i++ {
		mgr.Submit(fmt.Sprintf("prompt-%d", i), "agent")
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mgr.List()
	}
}

// BenchmarkSummaries benchmarks the Summaries() method with many jobs.
func BenchmarkSummaries(b *testing.B) {
	mgr := NewManager()
	// Create 1000 jobs
	for i := 0; i < 1000; i++ {
		mgr.Submit(fmt.Sprintf("prompt-%d", i), "agent")
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mgr.Summaries()
	}
}
