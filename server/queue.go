// queue.go is the execution orchestrator: a FIFO queue with a worker pool,
// per-job single-instance reservation at enqueue time, retry with backoff,
// consecutive-failure auto-pause, and completion side effects (notifications,
// job bookkeeping).
package server

import (
	"sync"
	"time"

	"github.com/flarco/g"
	"github.com/slingdata-io/sling-cli/core/sling"
)

// maxConsecutiveFailures auto-pauses a scheduled job after this many
// consecutive failed runs (Airbyte-style circuit breaker).
const maxConsecutiveFailures = 5

// runRequest is one queued job execution.
type runRequest struct {
	Job     *Job
	Trigger string
}

// Executor coordinates job runs.
type Executor struct {
	db      *DB
	runner  *Runner
	broker  *Broker
	notify  *Notifier

	queue  chan runRequest
	stopCh chan struct{}
	stopped bool
	workers int

	// mu guards inflight: the per-job single-instance reservation, held
	// from enqueue until the run (including retries) finishes.
	mu       sync.Mutex
	inflight map[string]bool
}

func NewExecutor(db *DB, runner *Runner, broker *Broker, notify *Notifier, concurrency int) *Executor {
	if concurrency < 1 {
		concurrency = 4
	}
	e := &Executor{
		db:         db,
		runner:     runner,
		broker:     broker,
		notify:     notify,
		queue:      make(chan runRequest, 1000),
		stopCh:     make(chan struct{}),
		workers:    concurrency,
		inflight:   map[string]bool{},
	}
	return e
}

// Start launches the worker goroutines.
func (e *Executor) Start() {
	for i := 0; i < e.workers; i++ {
		go e.worker()
	}
}

// Stop drains: no new picks; running tasks are cancelled by the caller.
// Anything still queued gets a skipped run row so it leaves a trace.
func (e *Executor) Stop() {
	e.mu.Lock()
	if e.stopped {
		e.mu.Unlock()
		return
	}
	e.stopped = true
	e.mu.Unlock()
	close(e.stopCh)
	e.drain()
}

// drain empties the queue after shutdown, releasing reservations and
// recording what never started.
func (e *Executor) drain() {
	for {
		select {
		case req := <-e.queue:
			e.mu.Lock()
			delete(e.inflight, req.Job.ID)
			e.mu.Unlock()
			e.recordSkipped(req.Job, req.Trigger, "server shutting down before run started")
		default:
			return
		}
	}
}

// EnqueueResult explains why an Enqueue attempt was refused.
type EnqueueResult int

const (
	EnqueueOK EnqueueResult = iota
	EnqueueRejectedRunning  // an instance is already queued or running
	EnqueueRejectedFull     // queue at capacity
	EnqueueRejectedStopped  // executor shutting down
)

// Enqueue queues a job run, reserving the job's single-instance slot for
// its whole lifecycle (queued, running, retries). Non-manual triggers that
// are refused get a skipped run row for traceability; manual refusals are
// surfaced to the caller as HTTP errors instead.
func (e *Executor) Enqueue(job *Job, trigger string) EnqueueResult {
	e.mu.Lock()
	if e.stopped {
		e.mu.Unlock()
		return e.reject(job, trigger, EnqueueRejectedStopped, "server shutting down")
	}
	if e.inflight[job.ID] {
		e.mu.Unlock()
		return e.reject(job, trigger, EnqueueRejectedRunning, "job already running")
	}
	select {
	case e.queue <- runRequest{Job: job, Trigger: trigger}:
		e.inflight[job.ID] = true
		e.mu.Unlock()
		return EnqueueOK
	default:
		e.mu.Unlock()
		return e.reject(job, trigger, EnqueueRejectedFull, "execution queue is full")
	}
}

// reject records a skipped run for refused scheduled triggers so a cron
// fire never disappears silently.
func (e *Executor) reject(job *Job, trigger string, res EnqueueResult, reason string) EnqueueResult {
	if trigger != "manual" {
		e.recordSkipped(job, trigger, reason)
	}
	return res
}

// InflightJobIDs snapshots which jobs currently hold a slot (queued or
// running).
func (e *Executor) InflightJobIDs() map[string]bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make(map[string]bool, len(e.inflight))
	for id := range e.inflight {
		out[id] = true
	}
	return out
}

func (e *Executor) isStopped() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.stopped
}

// releaseInflight frees the job's slot when its execution fully ends.
func (e *Executor) releaseInflight(jobID string) {
	e.mu.Lock()
	delete(e.inflight, jobID)
	e.mu.Unlock()
}

func (e *Executor) recordSkipped(job *Job, trigger, reason string) {
	run := &Run{
		ID:       newRunID(),
		JobID:    job.ID,
		JobName:  job.Name,
		Type:     jobRunType(job),
		Trigger:  trigger,
		Status:   sling.ExecStatusSkipped,
		Error:    "skipped: " + reason,
		StartTime: time.Now(),
		EndTime:   timePtr(time.Now()),
	}
	if err := e.db.Create(run).Error; err != nil {
		g.Debug("could not record skipped run: %s", err)
	}
}

func (e *Executor) worker() {
	for {
		select {
		case <-e.stopCh:
			return
		case req := <-e.queue:
			e.execute(req)
		}
	}
}

// execute runs a job request through retry attempts. The job's inflight
// slot (reserved at enqueue) is held across all retries.
func (e *Executor) execute(req runRequest) {
	job := req.Job
	if e.isStopped() {
		e.releaseInflight(job.ID)
		e.recordSkipped(job, req.Trigger, "server shutting down before run started")
		return
	}
	defer e.releaseInflight(job.ID)

	attempts := job.RetryMax + 1
	if attempts < 1 {
		attempts = 1
	}

	var lastHandle *RunHandle
	for attempt := 1; attempt <= attempts; attempt++ {
		trigger := req.Trigger
		if attempt > 1 {
			trigger = "retry"
		}

		handle, err := e.launch(job, trigger, attempt)
		if err != nil {
			// compile failure: no handle, record and stop retrying
			e.finishCompileError(job, trigger, err)
			e.afterFinish(job, false)
			if e.notify != nil {
				e.notify.RunFinished(job, Snapshot{
					Status: string(sling.ExecStatusError),
					Error:  err.Error(),
				})
			}
			return
		}
		lastHandle = handle
		if attempt == 1 {
			// queued -> running: let clients refresh the job's live state
			e.PublishJobUpdate(job.ID)
		}
		<-handle.Done()

		if handle.Status().IsSuccess() || handle.Status() == sling.ExecStatusCancelled || handle.Status() == sling.ExecStatusSkipped {
			break
		}

		// failed
		if attempt < attempts {
			backoff := time.Duration(job.RetryBackoffSec) * time.Second
			if backoff <= 0 {
				backoff = 30 * time.Second
			}
			g.Debug("job %s attempt %d failed, retrying in %s", job.Name, attempt, backoff)
			select {
			case <-e.stopCh:
				e.afterFinish(job, false)
				return
			case <-time.After(backoff):
			}
		}
	}

	success := lastHandle != nil && lastHandle.Status().IsSuccess()
	e.afterFinish(job, success)

	// one notification per job execution (after all retries), not per attempt
	if e.notify != nil && lastHandle != nil {
		e.notify.RunFinished(job, lastHandle.Snapshot())
	}
}

// launch compiles the job and starts the run handle.
func (e *Executor) launch(job *Job, trigger string, attempt int) (*RunHandle, error) {
	spec := job.Spec()
	if spec.SourceConn == "" || spec.TargetConn == "" {
		return nil, g.Error("job %s has no source/target connection", job.Name)
	}

	if spec.IsSingleTask() {
		cfg, err := spec.CompileTask()
		if err != nil {
			return nil, g.Error(err, "could not compile job %s", job.Name)
		}
		return e.runner.StartTask(job, trigger, attempt, cfg, ""), nil
	}

	repl, err := spec.CompileReplication()
	if err != nil {
		return nil, g.Error(err, "could not compile job %s", job.Name)
	}
	return e.runner.StartReplication(job, trigger, attempt, repl, spec, ""), nil
}

func (e *Executor) finishCompileError(job *Job, trigger string, err error) {
	run := &Run{
		ID:       newRunID(),
		JobID:    job.ID,
		JobName:  job.Name,
		Type:     jobRunType(job),
		Trigger:  trigger,
		Status:   sling.ExecStatusError,
		Error:    err.Error(),
		StartTime: time.Now(),
		EndTime:   timePtr(time.Now()),
	}
	if e2 := e.db.Create(run).Error; e2 != nil {
		g.Debug("could not record compile error run: %s", e2)
	}
}

// afterFinish updates job bookkeeping, fires notifications, pauses on
// consecutive failures.
func (e *Executor) afterFinish(job *Job, success bool) {
	now := time.Now()
	updates := map[string]any{"last_run_at": now}

	if success {
		updates["consecutive_failures"] = 0
	} else {
		failures := job.ConsecutiveFailures + 1
		updates["consecutive_failures"] = failures
		if failures >= maxConsecutiveFailures && job.ScheduleEnabled && !job.AutoPaused {
			updates["auto_paused"] = true
			updates["schedule_enabled"] = false
			if e.notify != nil {
				e.notify.JobAutoPaused(job, failures)
			}
		}
	}

	if err := e.db.Model(&Job{}).Where("id = ?", job.ID).Updates(updates).Error; err != nil {
		g.Debug("could not update job bookkeeping: %s", err)
	}
	e.PublishJobUpdate(job.ID)
}

// PublishJobUpdate nudges clients to refetch job state (live run started,
// bookkeeping changed).
func (e *Executor) PublishJobUpdate(jobID string) {
	if e.broker != nil {
		e.broker.Publish(Event{Type: "job", Payload: map[string]any{"id": jobID, "updated_at": time.Now()}})
	}
}

// jobRunType maps a job to its run type label.
func jobRunType(job *Job) string {
	if job.Spec().IsSingleTask() {
		return "task"
	}
	return "replication"
}
