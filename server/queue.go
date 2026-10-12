// queue.go is the execution orchestrator: a FIFO queue with a worker pool,
// job-level concurrency policy, retry with backoff, consecutive-failure
// auto-pause, and completion side effects (notifications, job bookkeeping).
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

	// activeMu guards activeJobs for the skip/queue concurrency policy.
	activeMu   sync.Mutex
	activeJobs map[string]bool
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
		activeJobs: map[string]bool{},
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
func (e *Executor) Stop() {
	e.activeMu.Lock()
	if e.stopped {
		e.activeMu.Unlock()
		return
	}
	e.stopped = true
	e.activeMu.Unlock()
	close(e.stopCh)
}

// Enqueue queues a job run. Returns false when the queue is full.
func (e *Executor) Enqueue(job *Job, trigger string) bool {
	select {
	case e.queue <- runRequest{Job: job, Trigger: trigger}:
		return true
	default:
		return false
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

// tryAcquire applies the job's concurrency policy. Returns:
//   acquired=true  -> proceed
//   acquired=false -> skipped (a skipped run row was recorded when wanted)
func (e *Executor) tryAcquire(job *Job, trigger string) bool {
	e.activeMu.Lock()
	defer e.activeMu.Unlock()
	if e.stopped {
		return false
	}
	if active := e.activeJobs[job.ID]; active {
		if job.ConcurrencyPolicy == "queue" {
			// requeue with a delay; the queue keeps FIFO order per worker pick
			go func() {
				select {
				case <-e.stopCh:
				case <-time.After(2 * time.Second):
					select {
					case e.queue <- runRequest{Job: job, Trigger: trigger}:
					default:
					}
				}
			}()
			return false
		}
		// skip policy: record a skipped run for traceability
		e.recordSkipped(job, trigger)
		return false
	}
	e.activeJobs[job.ID] = true
	return true
}

func (e *Executor) release(jobID string) {
	e.activeMu.Lock()
	delete(e.activeJobs, jobID)
	e.activeMu.Unlock()
}

func (e *Executor) recordSkipped(job *Job, trigger string) {
	run := &Run{
		ID:       newRunID(),
		JobID:    job.ID,
		JobName:  job.Name,
		Type:     jobRunType(job),
		Trigger:  trigger,
		Status:   sling.ExecStatusSkipped,
		Error:    "skipped: job already running",
		StartTime: time.Now(),
		EndTime:   timePtr(time.Now()),
	}
	if err := e.db.Create(run).Error; err != nil {
		g.Debug("could not record skipped run: %s", err)
	}
}

// execute runs a job request through retry attempts.
func (e *Executor) execute(req runRequest) {
	job := req.Job
	if !e.tryAcquire(job, req.Trigger) {
		return
	}
	defer e.release(job.ID)

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
		return e.runner.StartTask(job, trigger, attempt, cfg), nil
	}

	repl, err := spec.CompileReplication()
	if err != nil {
		return nil, g.Error(err, "could not compile job %s", job.Name)
	}
	return e.runner.StartReplication(job, trigger, attempt, repl, spec), nil
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
	if e.broker != nil {
		e.broker.Publish(Event{Type: "job", Payload: map[string]any{"id": job.ID, "updated_at": now}})
	}
}

// jobRunType maps a job to its run type label.
func jobRunType(job *Job) string {
	if job.Spec().IsSingleTask() {
		return "task"
	}
	return "replication"
}
