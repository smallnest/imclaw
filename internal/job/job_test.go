package job

import (
	"context"
	"errors"
	"fmt"
	"sync"
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

// TestConcurrentAccess tests that the Manager is safe for concurrent access.
func TestConcurrentAccess(t *testing.T) {
	mgr := NewManager()
	const numGoroutines = 50
	const opsPerGoroutine = 100

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	// Run many goroutines that concurrently submit, get, and list jobs.
	for i := 0; i < numGoroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			for j := 0; j < opsPerGoroutine; j++ {
				switch j % 5 {
				case 0:
					// Submit a job
					mgr.Submit(fmt.Sprintf("prompt %d-%d", idx, j), "agent")
				case 1:
					mgr.List()
				case 2:
					mgr.Summaries()
				case 3:
					// Try to get a non-existent job (should not panic)
					_, _ = mgr.Get("non-existent")
				case 4:
					mgr.Cleanup(0)
				}
			}
		}(i)
	}

	wg.Wait()

	// Verify all jobs are accounted for
	jobs := mgr.List()
	if len(jobs) == 0 {
		t.Error("expected jobs to exist after concurrent access")
	}
}

// TestManagerCancel_NonExistent tests that canceling a non-existent job returns an error.
func TestManagerCancel_NonExistent(t *testing.T) {
	mgr := NewManager()

	err := mgr.Cancel("non-existent-id")
	if err == nil {
		t.Error("expected error when canceling non-existent job")
	}
}

// TestManagerDelete_NonExistent tests that deleting a non-existent job returns an error.
func TestManagerDelete_NonExistent(t *testing.T) {
	mgr := NewManager()

	err := mgr.Delete("non-existent-id")
	if err == nil {
		t.Error("expected error when deleting non-existent job")
	}
}

// TestManagerDelete_GetAfterDelete verifies that a job cannot be retrieved after deletion.
func TestManagerDelete_GetAfterDelete(t *testing.T) {
	mgr := NewManager()
	job := mgr.Submit("test", "agent")

	err := mgr.Delete(job.ID)
	if err != nil {
		t.Fatalf("unexpected error deleting job: %v", err)
	}

	_, ok := mgr.Get(job.ID)
	if ok {
		t.Error("expected Get to return false after deletion")
	}
}

// TestManagerCancel_ListAfterCancel verifies that a canceled job can still be listed
// and has the correct status.
func TestManagerCancel_ListAfterCancel(t *testing.T) {
	mgr := NewManager()
	job := mgr.Submit("test", "agent")

	err := mgr.Cancel(job.ID)
	if err != nil {
		t.Fatalf("unexpected error canceling job: %v", err)
	}

	// Verify Get returns canceled status
	retrieved, ok := mgr.Get(job.ID)
	if !ok {
		t.Fatal("expected job to be found")
	}
	if retrieved.Status != StatusCanceled {
		t.Errorf("expected status %s, got %s", StatusCanceled, retrieved.Status)
	}
	if retrieved.FinishedAt == nil {
		t.Error("expected FinishedAt to be set after cancel")
	}

	// Verify List includes the canceled job
	jobs := mgr.List()
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job in list, got %d", len(jobs))
	}
	if jobs[0].ID != job.ID {
		t.Errorf("expected job ID %s, got %s", job.ID, jobs[0].ID)
	}
}

// TestManagerStart_NonExistent tests starting a job that doesn't exist.
func TestManagerStart_NonExistent(t *testing.T) {
	mgr := NewManager()
	_, cancel := context.WithCancel(context.Background())

	err := mgr.Start("non-existent-id", cancel)
	if err == nil {
		t.Error("expected error when starting non-existent job")
	}
}

// TestManagerComplete_NonExistent tests completing a job that doesn't exist.
func TestManagerComplete_NonExistent(t *testing.T) {
	mgr := NewManager()

	err := mgr.Complete("non-existent-id", "result")
	if err == nil {
		t.Error("expected error when completing non-existent job")
	}
}

// TestManagerFail_NonExistent tests failing a job that doesn't exist.
func TestManagerFail_NonExistent(t *testing.T) {
	mgr := NewManager()

	err := mgr.Fail("non-existent-id", "error msg")
	if err == nil {
		t.Error("expected error when failing non-existent job")
	}
}

// TestManagerAddLog_NonExistent tests adding a log to a job that doesn't exist.
func TestManagerAddLog_NonExistent(t *testing.T) {
	mgr := NewManager()

	err := mgr.AddLog("non-existent-id", "info", "message")
	if err == nil {
		t.Error("expected error when adding log to non-existent job")
	}
	if err.Error() != "job not found: non-existent-id" {
		t.Errorf("expected specific error message, got: %v", err)
	}
}

// TestStatusTransition_CompletedToCanceled tests that a completed job can't be canceled.
func TestStatusTransition_CompletedToCanceled(t *testing.T) {
	job := &Job{Status: StatusCompleted}
	err := job.transitionStatus(StatusCanceled)
	if err == nil {
		t.Error("expected error when canceling a completed job")
	}
}

// TestStatusTransition_FailedToCompleted tests invalid transition.
func TestStatusTransition_FailedToCompleted(t *testing.T) {
	job := &Job{Status: StatusFailed}
	err := job.transitionStatus(StatusCompleted)
	if err == nil {
		t.Error("expected error when transitioning from failed to completed")
	}
}

// TestRetryAfterFailure tests that a failed job can be retried by transitioning to queued.
func TestRetryAfterFailure(t *testing.T) {
	mgr := NewManager()
	job := mgr.Submit("test prompt", "agent")

	// Start the job
	_, cancel := context.WithCancel(context.Background())
	if err := mgr.Start(job.ID, cancel); err != nil {
		t.Fatalf("unexpected error starting job: %v", err)
	}

	// Fail the job
	if err := mgr.Fail(job.ID, "execution error"); err != nil {
		t.Fatalf("unexpected error failing job: %v", err)
	}

	// Verify failed status
	retrieved, _ := mgr.Get(job.ID)
	if retrieved.Status != StatusFailed {
		t.Fatalf("expected status %s, got %s", StatusFailed, retrieved.Status)
	}

	// Retry by transitioning to queued (valid state transition per ValidTransitions)
	// Note: Failed jobs can transition to Queued for retry
	err := retrieved.transitionStatus(StatusQueued)
	if err != nil {
		t.Errorf("failed to transition to queued for retry: %v", err)
	}

	// Verify the transition succeeded
	if retrieved.Status != StatusQueued {
		t.Errorf("expected status %s after retry transition, got %s", StatusQueued, retrieved.Status)
	}
}

// TestExecuteJob_NonExistentID tests that ExecuteJob handles non-existent job ID gracefully.
func TestExecuteJob_NonExistentID(t *testing.T) {
	mgr := NewManager()

	executorCalled := false
	executor := func(ctx context.Context, prompt string, logFn func(level, msg string)) (string, error) {
		executorCalled = true
		t.Error("executor should not be called for non-existent job")
		return "result", nil
	}

	// ExecuteJob should return early for non-existent jobs
	ExecuteJob(context.Background(), mgr, "non-existent-id", executor)

	// Give some time for any goroutines to start
	time.Sleep(50 * time.Millisecond)

	// Verify executor was not called
	if executorCalled {
		t.Error("executor should not be called for non-existent job ID")
	}

	// Verify no job was created
	jobs := mgr.List()
	if len(jobs) != 0 {
		t.Errorf("expected no jobs, got %d", len(jobs))
	}
}

// TestManagerSubmit_EmptyPrompt tests submitting a job with an empty prompt.
func TestManagerSubmit_EmptyPrompt(t *testing.T) {
	mgr := NewManager()
	job := mgr.Submit("", "agent")

	// Empty prompt should be allowed (validation is done at API level)
	if job.Prompt != "" {
		t.Errorf("expected empty prompt, got %s", job.Prompt)
	}
	if job.Status != StatusQueued {
		t.Errorf("expected status %s, got %s", StatusQueued, job.Status)
	}
}

// TestManagerSubmit_EmptyAgentName tests submitting a job without an agent name.
func TestManagerSubmit_EmptyAgentName(t *testing.T) {
	mgr := NewManager()
	job := mgr.Submit("test prompt", "")

	// Empty agent name should be allowed
	if job.AgentName != "" {
		t.Errorf("expected empty agent name, got %s", job.AgentName)
	}
	if job.Status != StatusQueued {
		t.Errorf("expected status %s, got %s", StatusQueued, job.Status)
	}
}

// TestManagerAddLog_LogLevels tests different log levels.
func TestManagerAddLog_LogLevels(t *testing.T) {
	mgr := NewManager()
	job := mgr.Submit("test", "agent")

	levels := []string{"info", "error", "debug", "warn"}
	for _, level := range levels {
		err := mgr.AddLog(job.ID, level, fmt.Sprintf("%s message", level))
		if err != nil {
			t.Errorf("unexpected error for level %s: %v", level, err)
		}
	}

	retrieved, _ := mgr.Get(job.ID)
	// Submit adds 1 log + 4 more logs
	if len(retrieved.Logs) != 5 {
		t.Errorf("expected 5 logs, got %d", len(retrieved.Logs))
	}

	// Verify log levels are preserved
	for i, level := range levels {
		if retrieved.Logs[i+1].Level != level {
			t.Errorf("expected log level %s at index %d, got %s", level, i+1, retrieved.Logs[i+1].Level)
		}
	}
}

// TestManagerSubmit_UniqueIDs verifies that submitted jobs have unique IDs.
func TestManagerSubmit_UniqueIDs(t *testing.T) {
	mgr := NewManager()
	const numJobs = 100

	ids := make(map[string]bool)
	for i := 0; i < numJobs; i++ {
		job := mgr.Submit(fmt.Sprintf("prompt-%d", i), "agent")
		if ids[job.ID] {
			t.Fatalf("duplicate job ID found: %s", job.ID)
		}
		ids[job.ID] = true
	}

	if len(ids) != numJobs {
		t.Errorf("expected %d unique IDs, got %d", numJobs, len(ids))
	}
}

// TestCloneJobPreservesFields verifies that cloneJob properly copies all fields except logs (when requested).
func TestCloneJobPreservesFields(t *testing.T) {
	mgr := NewManager()
	job := mgr.Submit("test prompt", "test-agent")

	// Start and complete the job
	_, cancel := context.WithCancel(context.Background())
	mgr.Start(job.ID, cancel)
	mgr.Complete(job.ID, "result")

	// Add some logs
	mgr.AddLog(job.ID, "info", "log message")

	// Get with logs
	retrieved, _ := mgr.Get(job.ID)
	if retrieved.ID != job.ID {
		t.Errorf("expected ID %s, got %s", job.ID, retrieved.ID)
	}
	if retrieved.Status != StatusCompleted {
		t.Errorf("expected status %s, got %s", StatusCompleted, retrieved.Status)
	}
	if retrieved.Prompt != "test prompt" {
		t.Errorf("expected prompt 'test prompt', got %s", retrieved.Prompt)
	}
	if retrieved.Result != "result" {
		t.Errorf("expected result 'result', got %s", retrieved.Result)
	}
	if len(retrieved.Logs) == 0 {
		t.Error("expected logs to be included in Get()")
	}
}

// TestCanceledJobCannotBeCanceledAgain verifies canceling an already canceled job returns an error.
func TestCanceledJobCannotBeCanceledAgain(t *testing.T) {
	mgr := NewManager()
	job := mgr.Submit("test", "agent")

	// Cancel once
	if err := mgr.Cancel(job.ID); err != nil {
		t.Fatalf("unexpected error canceling job: %v", err)
	}

	// Try to cancel again
	err := mgr.Cancel(job.ID)
	if err == nil {
		t.Error("expected error when canceling an already canceled job")
	}
}

// TestCompletedJobCannotBeCompletedAgain verifies completing an already completed job returns an error.
func TestCompletedJobCannotBeCompletedAgain(t *testing.T) {
	mgr := NewManager()
	job := mgr.Submit("test", "agent")

	// Start the job
	_, cancel := context.WithCancel(context.Background())
	if err := mgr.Start(job.ID, cancel); err != nil {
		t.Fatalf("unexpected error starting job: %v", err)
	}

	// Complete once
	if err := mgr.Complete(job.ID, "result"); err != nil {
		t.Fatalf("unexpected error completing job: %v", err)
	}

	// Try to complete again
	err := mgr.Complete(job.ID, "result")
	if err == nil {
		t.Error("expected error when completing an already completed job")
	}
}

// TestDeleteCancelsRunningJob verifies that deleting a running job cancels its context.
func TestDeleteCancelsRunningJob(t *testing.T) {
	mgr := NewManager()
	job := mgr.Submit("test", "agent")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create a goroutine that blocks on the context
	done := make(chan struct{})
	go func() {
		<-ctx.Done()
		close(done)
	}()

	// Start the job with the context
	if err := mgr.Start(job.ID, cancel); err != nil {
		t.Fatalf("unexpected error starting job: %v", err)
	}

	// Delete the running job
	if err := mgr.Delete(job.ID); err != nil {
		t.Fatalf("unexpected error deleting job: %v", err)
	}

	// Verify context was cancelled
	select {
	case <-done:
		// Context was cancelled, as expected
	case <-time.After(100 * time.Millisecond):
		t.Error("expected context to be cancelled after deleting running job")
	}
}

// TestCleanupDoesNotRemoveRunningOrQueued verifies that Cleanup leaves running/queued jobs.
func TestCleanupDoesNotRemoveRunningOrQueued(t *testing.T) {
	mgr := NewManager()

	// Create a queued job
	queued := mgr.Submit("queued prompt", "agent")

	// Create a completed job that's old
	completed := mgr.Submit("completed prompt", "agent")
	_, cancel := context.WithCancel(context.Background())
	mgr.Start(completed.ID, cancel)
	mgr.Complete(completed.ID, "result")

	// Manually age the completed job
	oldTime := time.Now().Add(-2 * time.Hour)
	mgr.mu.Lock()
	if job, ok := mgr.jobs[completed.ID]; ok {
		job.FinishedAt = &oldTime
	}
	mgr.mu.Unlock()

	// Cleanup with 1 hour threshold
	removed := mgr.Cleanup(1 * time.Hour)

	if removed != 1 {
		t.Errorf("expected 1 job removed, got %d", removed)
	}

	// Verify queued job still exists
	_, ok := mgr.Get(queued.ID)
	if !ok {
		t.Error("expected queued job to still exist")
	}

	// Verify completed job was removed
	_, ok = mgr.Get(completed.ID)
	if ok {
		t.Error("expected old completed job to be removed")
	}
}

// TestCleanupWithNoOldJobs verifies Cleanup returns 0 when no jobs are old enough.
func TestCleanupWithNoOldJobs(t *testing.T) {
	mgr := NewManager()

	// Create a recently completed job
	job := mgr.Submit("test", "agent")
	_, cancel := context.WithCancel(context.Background())
	mgr.Start(job.ID, cancel)
	mgr.Complete(job.ID, "result")

	// Cleanup with 1 hour threshold (job is brand new)
	removed := mgr.Cleanup(1 * time.Hour)

	if removed != 0 {
		t.Errorf("expected 0 jobs removed, got %d", removed)
	}

	// Verify job still exists
	_, ok := mgr.Get(job.ID)
	if !ok {
		t.Error("expected job to still exist")
	}
}

// TestJobSummary_ExcludesLogs verifies that Job.Summary() doesn't include logs.
func TestJobSummary_ExcludesLogs(t *testing.T) {
	mgr := NewManager()
	job := mgr.Submit("test", "agent")

	// Add many logs
	for i := 0; i < 50; i++ {
		mgr.AddLog(job.ID, "info", fmt.Sprintf("log %d", i))
	}

	// Get full job
	fullJob, _ := mgr.Get(job.ID)
	if len(fullJob.Logs) != 51 {
		t.Errorf("expected 51 logs in full job, got %d", len(fullJob.Logs))
	}

	// Get summary
	summaries := mgr.Summaries()
	if len(summaries) != 1 {
		t.Fatalf("expected 1 summary, got %d", len(summaries))
	}

	// Summary should not have logs
	// Note: Summary() is a method on Job, it returns JobSummary which doesn't have Logs field
	if summaries[0].ID != job.ID {
		t.Errorf("expected summary ID %s, got %s", job.ID, summaries[0].ID)
	}
}

// TestConcurrentReadWrite verifies no race conditions with concurrent reads and writes.
func TestConcurrentReadWrite(t *testing.T) {
	mgr := NewManager()

	// Create some initial jobs
	for i := 0; i < 10; i++ {
		mgr.Submit(fmt.Sprintf("initial-%d", i), "agent")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup

	// Writer goroutines
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				job := mgr.Submit(fmt.Sprintf("job-%d-%d", idx, j), "agent")
				// Try to start and complete
				_, c := context.WithCancel(ctx)
				_ = mgr.Start(job.ID, c)
				_ = mgr.Complete(job.ID, "done")
			}
		}(i)
	}

	// Reader goroutines
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				mgr.List()
				mgr.Summaries()
				summaries := mgr.Summaries()
				for _, summary := range summaries {
					mgr.Get(summary.ID)
				}
			}
		}()
	}

	wg.Wait()
}

// TestExecuteJob_ContextCancellationDuringExecution tests context cancellation propagates correctly.
func TestExecuteJob_ContextCancellationDuringExecution(t *testing.T) {
	mgr := NewManager()
	job := mgr.Submit("test prompt", "agent")

	execCtx, execCancel := context.WithCancel(context.Background())

	executor := func(ctx context.Context, prompt string, logFn func(level, msg string)) (string, error) {
		// Wait for context cancellation
		<-ctx.Done()
		return "", ctx.Err()
	}

	go ExecuteJob(execCtx, mgr, job.ID, executor)

	// Give the executor time to start
	time.Sleep(50 * time.Millisecond)

	// Cancel the execution context
	execCancel()

	// Wait for cancellation to process
	time.Sleep(200 * time.Millisecond)

	retrieved, ok := mgr.Get(job.ID)
	if !ok {
		t.Fatal("job not found")
	}

	// The job should be canceled
	if retrieved.Status != StatusCanceled {
		t.Errorf("expected status %s, got %s", StatusCanceled, retrieved.Status)
	}
}
