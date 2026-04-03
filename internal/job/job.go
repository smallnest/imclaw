package job

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
)

const (
	// MaxLogEntries is the maximum number of log entries to keep per job.
	// This prevents unbounded memory growth for long-running jobs.
	MaxLogEntries = 1000
)

// JobStatus represents the current state of a job.
type JobStatus string

const (
	// StatusQueued indicates the job is waiting to be processed.
	StatusQueued JobStatus = "queued"
	// StatusRunning indicates the job is currently being processed.
	StatusRunning JobStatus = "running"
	// StatusCompleted indicates the job finished successfully.
	StatusCompleted JobStatus = "completed"
	// StatusFailed indicates the job failed with an error.
	StatusFailed JobStatus = "failed"
	// StatusCanceled indicates the job was canceled by the user.
	StatusCanceled JobStatus = "canceled"
)

// ValidTransitions defines valid state transitions.
var ValidTransitions = map[JobStatus][]JobStatus{
	StatusQueued:   {StatusRunning, StatusCanceled},
	StatusRunning:  {StatusCompleted, StatusFailed, StatusCanceled},
	StatusCompleted: {},
	StatusFailed:    {StatusQueued}, // Allow retry
	StatusCanceled:  {},
}

// Job represents a background job executing an agent prompt.
type Job struct {
	ID        string      `json:"id"`
	Status    JobStatus   `json:"status"`
	Prompt    string      `json:"prompt"`
	AgentName string      `json:"agent_name"`
	Timeout   time.Duration `json:"timeout,omitempty"` // Timeout for job execution, 0 means no timeout
	CreatedAt time.Time   `json:"created_at"`
	StartedAt *time.Time  `json:"started_at,omitempty"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	Result    string      `json:"result,omitempty"`
	Error     string      `json:"error,omitempty"`
	Logs      []LogEntry  `json:"logs,omitempty"`
	cancel    context.CancelFunc
}

// LogEntry represents a single log entry from job execution.
type LogEntry struct {
	Timestamp time.Time `json:"timestamp"`
	Level     string    `json:"level"`
	Message   string    `json:"message"`
}

// JobSummary is a lightweight projection used by list APIs.
type JobSummary struct {
	ID         string        `json:"id"`
	Status     JobStatus     `json:"status"`
	Prompt     string        `json:"prompt"`
	AgentName  string        `json:"agent_name"`
	Timeout    time.Duration `json:"timeout,omitempty"`
	CreatedAt  time.Time     `json:"created_at"`
	StartedAt  *time.Time    `json:"started_at,omitempty"`
	FinishedAt *time.Time    `json:"finished_at,omitempty"`
}

// Manager manages background jobs.
type Manager struct {
	mu   sync.RWMutex
	jobs map[string]*Job
}

// NewManager creates a new job manager.
func NewManager() *Manager {
	return &Manager{
		jobs: make(map[string]*Job),
	}
}

// newJob creates a new job with the given prompt, agent name, and timeout.
func newJob(prompt, agentName string, timeout time.Duration) *Job {
	return &Job{
		ID:        uuid.New().String(),
		Status:    StatusQueued,
		Prompt:    prompt,
		AgentName: agentName,
		Timeout:   timeout,
		CreatedAt: time.Now(),
		Logs:      make([]LogEntry, 0),
	}
}

// Summary returns a lightweight job view for list rendering.
func (j *Job) Summary() JobSummary {
	return JobSummary{
		ID:         j.ID,
		Status:     j.Status,
		Prompt:     j.Prompt,
		AgentName:  j.AgentName,
		Timeout:    j.Timeout,
		CreatedAt:  j.CreatedAt,
		StartedAt:  j.StartedAt,
		FinishedAt: j.FinishedAt,
	}
}

// addLog adds a log entry to the job.
func (j *Job) addLog(level, message string) {
	j.Logs = append(j.Logs, LogEntry{
		Timestamp: time.Now(),
		Level:     level,
		Message:   message,
	})

	// Limit log entries to prevent unbounded memory growth.
	// Keep only the most recent entries.
	if len(j.Logs) > MaxLogEntries {
		// Remove oldest entries (from the beginning)
		j.Logs = j.Logs[len(j.Logs)-MaxLogEntries:]
	}
}

// transitionStatus transitions the job to a new status if valid.
func (j *Job) transitionStatus(newStatus JobStatus) error {
	validTransitions, ok := ValidTransitions[j.Status]
	if !ok {
		return fmt.Errorf("invalid current status: %s", j.Status)
	}

	valid := false
	for _, status := range validTransitions {
		if status == newStatus {
			valid = true
			break
		}
	}

	if !valid {
		return fmt.Errorf("invalid status transition from %s to %s", j.Status, newStatus)
	}

	j.Status = newStatus

	// Update timestamps
	switch newStatus {
	case StatusRunning:
		now := time.Now()
		j.StartedAt = &now
	case StatusCompleted, StatusFailed, StatusCanceled:
		now := time.Now()
		j.FinishedAt = &now
	}

	return nil
}

// Submit submits a new job to the queue.
func (m *Manager) Submit(prompt, agentName string, timeout time.Duration) *Job {
	m.mu.Lock()
	defer m.mu.Unlock()

	job := newJob(prompt, agentName, timeout)
	m.jobs[job.ID] = job
	job.addLog("info", fmt.Sprintf("Job submitted: %s", job.ID))

	// Return a copy to avoid race conditions
	return m.cloneJob(job, true)
}

// Get retrieves a job by ID.
func (m *Manager) Get(id string) (*Job, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	job, ok := m.jobs[id]
	return m.cloneJob(job, true), ok // Include logs for Get()
}

// List lists all jobs ordered by creation time (newest first).
func (m *Manager) List() []*Job {
	m.mu.RLock()
	defer m.mu.RUnlock()

	jobs := make([]*Job, 0, len(m.jobs))
	for _, job := range m.jobs {
		jobs = append(jobs, m.cloneJob(job, false)) // Don't include logs for list operations
	}

	// Sort by CreatedAt descending using standard library (O(n log n))
	sort.Slice(jobs, func(i, j int) bool {
		return jobs[i].CreatedAt.After(jobs[j].CreatedAt)
	})

	return jobs
}

// Summaries lists all jobs using a lightweight projection.
func (m *Manager) Summaries() []JobSummary {
	m.mu.RLock()
	defer m.mu.RUnlock()

	summaries := make([]JobSummary, 0, len(m.jobs))
	for _, job := range m.jobs {
		summaries = append(summaries, job.Summary())
	}

	// Sort by CreatedAt descending using standard library (O(n log n))
	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].CreatedAt.After(summaries[j].CreatedAt)
	})

	return summaries
}

// Start starts a job execution.
func (m *Manager) Start(id string, cancel context.CancelFunc) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	job, ok := m.jobs[id]
	if !ok {
		return fmt.Errorf("job not found: %s", id)
	}

	if err := job.transitionStatus(StatusRunning); err != nil {
		return err
	}

	job.cancel = cancel
	job.addLog("info", "Job started")
	return nil
}

// Complete marks a job as completed with a result.
func (m *Manager) Complete(id, result string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	job, ok := m.jobs[id]
	if !ok {
		return fmt.Errorf("job not found: %s", id)
	}

	if err := job.transitionStatus(StatusCompleted); err != nil {
		return err
	}

	job.Result = result
	job.addLog("info", "Job completed successfully")
	return nil
}

// Fail marks a job as failed with an error message.
func (m *Manager) Fail(id, errorMsg string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	job, ok := m.jobs[id]
	if !ok {
		return fmt.Errorf("job not found: %s", id)
	}

	if err := job.transitionStatus(StatusFailed); err != nil {
		return err
	}

	job.Error = errorMsg
	job.addLog("error", fmt.Sprintf("Job failed: %s", errorMsg))
	return nil
}

// Cancel cancels a running or queued job.
func (m *Manager) Cancel(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	job, ok := m.jobs[id]
	if !ok {
		return fmt.Errorf("job not found: %s", id)
	}

	// Cancel the context if running
	if job.Status == StatusRunning && job.cancel != nil {
		job.cancel()
		job.addLog("info", "Job canceled by user")
	}

	return job.transitionStatus(StatusCanceled)
}

// AddLog adds a log entry to a job.
func (m *Manager) AddLog(id, level, message string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	job, ok := m.jobs[id]
	if !ok {
		return fmt.Errorf("job not found: %s", id)
	}

	job.addLog(level, message)
	return nil
}

// Delete removes a job from the manager.
func (m *Manager) Delete(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	job, ok := m.jobs[id]
	if !ok {
		return fmt.Errorf("job not found: %s", id)
	}

	// Cancel if running
	if job.Status == StatusRunning && job.cancel != nil {
		job.cancel()
	}

	delete(m.jobs, id)
	return nil
}

// Cleanup removes completed/failed/canceled jobs older than maxAge.
func (m *Manager) Cleanup(maxAge time.Duration) int {
	m.mu.Lock()
	defer m.mu.Unlock()

	cutoff := time.Now().Add(-maxAge)
	count := 0

	for id, job := range m.jobs {
		if job.Status != StatusRunning && job.Status != StatusQueued {
			if job.FinishedAt != nil && job.FinishedAt.Before(cutoff) {
				delete(m.jobs, id)
				count++
			}
		}
	}

	return count
}

// cloneJob creates a shallow copy of a job for safe external access.
// The includeLogs parameter controls whether to copy log entries.
// This allows methods like List() to avoid copying potentially large log data.
func (m *Manager) cloneJob(src *Job, includeLogs bool) *Job {
	if src == nil {
		return nil
	}

	dst := *src

	// Only copy logs if explicitly requested (e.g., for Get()).
	// For list operations, we skip log copying to avoid unnecessary memory usage.
	if includeLogs && len(src.Logs) > 0 {
		dst.Logs = make([]LogEntry, len(src.Logs))
		copy(dst.Logs, src.Logs)
	} else {
		dst.Logs = nil
	}

	return &dst
}

// ExecuteJob runs a job using the given agent executor.
// This is a blocking call that should be run in a goroutine.
func ExecuteJob(ctx context.Context, mgr *Manager, jobID string, executor func(ctx context.Context, prompt string, logFn func(level, msg string)) (string, error)) {
	job, ok := mgr.Get(jobID)
	if !ok {
		return
	}

	// Create a cancellable context for this job
	jobCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Apply timeout if specified
	if job.Timeout > 0 {
		var timeoutCancel context.CancelFunc
		jobCtx, timeoutCancel = context.WithTimeout(jobCtx, job.Timeout)
		defer timeoutCancel()
		mgr.AddLog(jobID, "info", fmt.Sprintf("Job timeout set to %v", job.Timeout))
	}

	// Start the job
	if err := mgr.Start(jobID, cancel); err != nil {
		mgr.Fail(jobID, err.Error())
		return
	}

	// Execute the prompt
	logFn := func(level, msg string) {
		mgr.AddLog(jobID, level, msg)
	}

	result, err := executor(jobCtx, job.Prompt, logFn)
	if err != nil {
		if jobCtx.Err() == context.Canceled {
			// Job was canceled
			mgr.Cancel(jobID)
		} else if jobCtx.Err() == context.DeadlineExceeded {
			// Job timed out
			mgr.Fail(jobID, fmt.Sprintf("Job execution timed out after %v", job.Timeout))
		} else {
			mgr.Fail(jobID, err.Error())
		}
		return
	}

	mgr.Complete(jobID, result)
}
