// runner.go executes jobs as sling tasks / replications and keeps live
// handles for progress, cancellation and SSE snapshots.
package server

import (
	"context"
	"encoding/json"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/flarco/g"
	"github.com/slingdata-io/sling-cli/core/sling"
)

// RunHandle is one in-flight (or just-finished) run.
type RunHandle struct {
	ID      string        `json:"id"`
	JobID   string        `json:"job_id,omitempty"`
	JobName string        `json:"job_name,omitempty"`
	Trigger string        `json:"trigger"`
	Type    string        `json:"type"` // task|replication
	Attempt int           `json:"attempt"`

	Spec JobSpec `json:"-"`

	cfg  *sling.Config              // single-task mode
	repl *sling.ReplicationConfig   // replication mode

	gctx    *g.Context
	cancel  context.CancelFunc
	logs    *runLogs
	started time.Time
	streamCap int

	// fingerprint dedups ad-hoc runs of an identical spec (empty for job runs).
	fingerprint string

	status  atomic.Value // string (sling.ExecStatus)

	mu       sync.Mutex
	tasks    []*sling.TaskExecution
	streams  map[string]*StreamStatus
	err      error
	endTime  *time.Time
	done     chan struct{}

	db     *DB
	broker *Broker
	router *LogRouter
	notify *Notifier
}

// Runner owns active runs.
type Runner struct {
	mu     sync.RWMutex
	runs   map[string]*RunHandle
	db     *DB
	broker *Broker
	router *LogRouter
	notify *Notifier

	// StreamConcurrency caps replication stream parallelism (server setting).
	StreamConcurrency int
}

func NewRunner(db *DB, broker *Broker, router *LogRouter, notify *Notifier) *Runner {
	return &Runner{runs: map[string]*RunHandle{}, db: db, broker: broker, router: router, notify: notify}
}

func newRunID() string {
	return g.F("run-%s", g.RandString(g.AlphaRunesLower+g.NumericRunes, 12))
}

// StartTask launches a prepared single-task run on a new goroutine and
// returns immediately. The returned handle is also registered. A non-empty
// fp (ad-hoc runs) rejects duplicates of an identical running spec by
// returning nil.
func (r *Runner) StartTask(job *Job, trigger string, attempt int, cfg *sling.Config, fp string) *RunHandle {
	h := &RunHandle{
		ID:          newRunID(),
		Trigger:     trigger,
		Attempt:     attempt,
		Type:        "task",
		cfg:         cfg,
		logs:        &runLogs{},
		started:     time.Now(),
		streamCap:   r.streamCap(),
		fingerprint: fp,
		done:        make(chan struct{}),
		db:          r.db, broker: r.broker, router: r.router,
	}
	if job != nil {
		h.JobID, h.JobName = job.ID, job.Name
		h.Spec = job.Spec()
	}
	h.notify = r.notify
	h.bindContext()
	if !r.register(h) {
		return nil
	}
	go func() {
		defer r.syncActive()
		h.run()
	}()
	return h
}

// StartReplication launches a compiled replication run. See StartTask for
// the fp semantics.
func (r *Runner) StartReplication(job *Job, trigger string, attempt int, repl *sling.ReplicationConfig, spec JobSpec, fp string) *RunHandle {
	h := &RunHandle{
		ID:          newRunID(),
		Trigger:     trigger,
		Attempt:     attempt,
		Type:        "replication",
		repl:        repl,
		Spec:        spec,
		logs:        &runLogs{},
		started:     time.Now(),
		streamCap:   r.streamCap(),
		fingerprint: fp,
		done:        make(chan struct{}),
		db:          r.db, broker: r.broker, router: r.router,
	}
	if job != nil {
		h.JobID, h.JobName = job.ID, job.Name
	}
	h.notify = r.notify
	h.bindContext()
	if !r.register(h) {
		return nil
	}
	go func() {
		defer r.syncActive()
		h.run()
	}()
	return h
}

func (r *Runner) streamCap() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.StreamConcurrency > 0 {
		return r.StreamConcurrency
	}
	return 1
}

// SetStreamConcurrency applies the runtime setting (clamped to >= 1).
func (r *Runner) SetStreamConcurrency(n int) {
	if n < 1 {
		n = 1
	}
	r.mu.Lock()
	r.StreamConcurrency = n
	r.mu.Unlock()
}

// register adds the handle, rejecting an identical-spec duplicate (ad-hoc
// fingerprint). Returns false when rejected. Also prunes finished handles
// so the map does not grow without bound.
func (r *Runner) register(h *RunHandle) bool {
	r.mu.Lock()
	if h.fingerprint != "" {
		for _, other := range r.runs {
			if other.fingerprint == h.fingerprint && !other.IsFinished() {
				r.mu.Unlock()
				return false
			}
		}
	}
	r.runs[h.ID] = h
	r.pruneFinishedLocked()
	r.mu.Unlock()
	r.syncActive()
	return true
}

// keepFinishedRuns caps how many finished handles are retained for API
// reads (run detail falls back to the DB row when pruned).
const keepFinishedRuns = 64

// pruneFinishedLocked drops the oldest finished handles beyond the cap.
// Active handles are never pruned. Callers must hold r.mu.
func (r *Runner) pruneFinishedLocked() {
	type aged struct {
		id      string
		started time.Time
	}
	finished := make([]aged, 0, 8)
	for id, h := range r.runs {
		if h.IsFinished() {
			finished = append(finished, aged{id, h.started})
		}
	}
	if len(finished) <= keepFinishedRuns {
		return
	}
	sort.Slice(finished, func(i, j int) bool { return finished[i].started.Before(finished[j].started) })
	for _, a := range finished[:len(finished)-keepFinishedRuns] {
		delete(r.runs, a.id)
	}
}

// syncActive pushes the active run set to the log router (single-run
// fallback attribution for engine logs emitted on internal goroutines).
func (r *Runner) syncActive() {
	if r.router == nil {
		return
	}
	ids := make([]string, 0, 4)
	for _, h := range r.Active() {
		ids = append(ids, h.ID)
	}
	r.router.SetActiveRuns(ids)
}

// Get returns an active run handle.
func (r *Runner) Get(runID string) *RunHandle {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.runs[runID]
}

// Active returns all unfinished runs.
func (r *Runner) Active() (hs []*RunHandle) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, h := range r.runs {
		if !h.IsFinished() {
			hs = append(hs, h)
		}
	}
	return hs
}

// bindContext wires the execution context before the handle is registered,
// so Cancel can never race a nil cancel func.
func (h *RunHandle) bindContext() {
	h.gctx = g.NewContext(context.Background())
	h.cancel = func() { h.gctx.Cancel() }
}

// requestCancel is nil-safe: handles are always bound before registration,
// but stay defensive for any handle constructed outside the start paths.
func (h *RunHandle) requestCancel() {
	if h.cancel != nil {
		h.cancel()
	}
}

// HasActiveFingerprint reports whether an unfinished run holds the spec
// fingerprint (ad-hoc duplicate pre-check; register enforces it atomically).
func (r *Runner) HasActiveFingerprint(fp string) bool {
	if fp == "" {
		return false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, h := range r.runs {
		if h.fingerprint == fp && !h.IsFinished() {
			return true
		}
	}
	return false
}

// Cancel terminates a run. Returns false when unknown/finished.
func (r *Runner) Cancel(runID string) bool {
	h := r.Get(runID)
	if h == nil || h.IsFinished() {
		return false
	}
	h.requestCancel()
	return true
}

// CancelAll cancels every active run (graceful shutdown path).
func (r *Runner) CancelAll() {
	for _, h := range r.Active() {
		h.requestCancel()
	}
}

/* ------------------------------ execution ------------------------------- */

func (h *RunHandle) Status() sling.ExecStatus { return h.currentStatus() }

func (h *RunHandle) IsFinished() bool {
	return h.currentStatus().IsFinished()
}

func (h *RunHandle) Err() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.err
}

// Done returns a channel closed when the run finishes.
func (h *RunHandle) Done() <-chan struct{} { return h.done }

// Rows/Bytes aggregate across the run's tasks.
func (h *RunHandle) Rows() (n uint64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, t := range h.tasks {
		n += t.GetCount()
	}
	return n
}

func (h *RunHandle) Bytes() (n uint64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, t := range h.tasks {
		b, _ := t.GetBytes()
		n += b
	}
	return n
}

// Snapshot is the JSON payload published over SSE and served by the API.
type Snapshot struct {
	RunID       string          `json:"run_id"`
	JobID       string          `json:"job_id,omitempty"`
	JobName     string          `json:"job_name,omitempty"`
	Type        string          `json:"type"`
	Trigger     string          `json:"trigger"`
	Attempt     int             `json:"attempt,omitempty"`
	Status      string          `json:"status"`
	Rows        uint64          `json:"rows"`
	Bytes       uint64          `json:"bytes"`
	Rate        float64         `json:"rate"` // rows/sec
	Progress    string          `json:"progress,omitempty"`
	Streams     []StreamStatus  `json:"streams,omitempty"`
	StartTime   time.Time       `json:"start_time"`
	EndTime     *time.Time      `json:"end_time,omitempty"`
	DurationMs  int64           `json:"duration_ms"`
	Error       string          `json:"error,omitempty"`
}

func (h *RunHandle) Snapshot() Snapshot {
	h.mu.Lock()
	defer h.mu.Unlock()

	snap := Snapshot{
		RunID:      h.ID,
		JobID:      h.JobID,
		JobName:    h.JobName,
		Type:       h.Type,
		Trigger:    h.Trigger,
		Attempt:    h.Attempt,
		Status:     string(h.Status()),
		Rows:       0,
		StartTime:  h.started,
		EndTime:    h.endTime,
		DurationMs: time.Since(h.started).Milliseconds(),
	}
	var rows, bytes uint64
	var progress string
	for _, t := range h.tasks {
		rows += t.GetCount()
		// same counter the engine's progress bar displays (bytes read from source)
		inBytes, _ := t.GetBytes()
		bytes += inBytes
		if p := t.Progress; p != "" && !t.Status.IsFinished() {
			progress = p
		}
	}
	snap.Rows = rows
	snap.Bytes = bytes
	snap.DurationMs = time.Since(h.started).Milliseconds()
	if snap.DurationMs > 0 {
		snap.Rate = float64(rows) / (float64(snap.DurationMs) / 1000)
	}
	snap.Progress = progress
	for _, ss := range h.streams {
		snap.Streams = append(snap.Streams, *ss)
	}
	if h.err != nil {
		snap.Error = h.err.Error()
	}
	if h.endTime != nil {
		snap.DurationMs = h.endTime.Sub(h.started).Milliseconds()
	}
	return snap
}

func (h *RunHandle) publish() {
	if h.broker != nil {
		h.broker.Publish(Event{Type: "execution", RunID: h.ID, Payload: h.Snapshot()})
	}
}

// run is the main execution coroutine of a run.
func (h *RunHandle) run() {
	// route this goroutine's logs to the run
	unbind := h.router.bind(h.ID, h.logs)
	defer unbind()

	defer close(h.done)

	h.streams = map[string]*StreamStatus{}

	h.setStatus(sling.ExecStatusRunning)
	h.insertRow()
	h.publish()

	// progress ticker: SSE every 1s, DB every 5s
	sseTick := time.NewTicker(1 * time.Second)
	dbTick := time.NewTicker(5 * time.Second)
	stopTicks := make(chan struct{})
	go func() {
		for {
			select {
			case <-stopTicks:
				return
			case <-h.gctx.Ctx.Done():
				return
			case <-sseTick.C:
				h.publish()
			case <-dbTick.C:
				h.updateRow()
			}
		}
	}()

	var err error
	if h.cfg != nil {
		err = h.executeStreamTask(h.ID, h.cfg, nil)
	} else if h.repl != nil {
		err = h.executeReplication()
	} else {
		err = g.Error("run has neither task nor replication config")
	}

	sseTick.Stop()
	dbTick.Stop()
	close(stopTicks)

	end := time.Now()
	h.mu.Lock()
	h.err = err
	h.endTime = &end
	h.mu.Unlock()

	final := h.Status()
	if err != nil {
		if h.wasCancelled() {
			final = sling.ExecStatusCancelled
		} else {
			final = sling.ExecStatusError
		}
	} else if final.IsFailure() {
		// keep whatever failure status tasks reported
	} else {
		final = h.aggregateStatus()
	}
	h.setStatus(final)

	h.finalizeRow()
	h.publish()

	// ad-hoc runs bypass the executor (no retry loop), so notify here; job
	// runs are notified once by the executor after all retries
	if h.notify != nil && h.JobID == "" {
		name := h.JobName
		if name == "" {
			name = "ad-hoc"
		}
		h.notify.RunFinished(&Job{ID: h.JobID, Name: name}, h.Snapshot())
	}
}

// wasCancelled reports whether the run context was cancelled.
func (h *RunHandle) wasCancelled() bool {
	return h.gctx != nil && h.gctx.Ctx.Err() != nil
}

// aggregateStatus rolls up stream statuses.
func (h *RunHandle) aggregateStatus() sling.ExecStatus {
	h.mu.Lock()
	defer h.mu.Unlock()
	agg := sling.ExecStatusSuccess
	seen := false
	for _, ss := range h.streams {
		seen = true
		st := ss.Status
		if st.IsFailure() || st == sling.ExecStatusCancelled || st == sling.ExecStatusInterrupted {
			return st
		}
		if st == sling.ExecStatusWarning {
			agg = sling.ExecStatusWarning
		}
	}
	if !seen {
		return sling.ExecStatusSuccess
	}
	return agg
}

// executeReplication runs the compiled tasks with bounded parallelism.
func (h *RunHandle) executeReplication() error {
	tasks := h.repl.Tasks
	if len(tasks) == 0 {
		return g.Error("replication compiled to zero streams")
	}

	concurrency := h.streamCap
	if concurrency < 1 {
		concurrency = 1
	}
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	var errMu sync.Mutex
	var firstErr error

	for i, cfg := range tasks {
		if h.wasCancelled() {
			break
		}
		i, cfg := i, cfg
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			err := h.executeStreamTask(g.F("%s.%03d", h.ID, i), cfg, h.repl)
			if err != nil {
				errMu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				errMu.Unlock()
			}
		}()
	}
	wg.Wait()

	if h.wasCancelled() && firstErr == nil {
		return nil // cancelled, not failed
	}
	return firstErr
}

// executeStreamTask mirrors cmd/sling runTask(): prepare, execute, track.
func (h *RunHandle) executeStreamTask(taskExecID string, cfg *sling.Config, repl *sling.ReplicationConfig) error {
	defer h.recoverPanic(taskExecID)

	cfg.SetDefault()

	streamName := cfg.StreamID()
	if repl != nil && cfg.ReplicationStream != nil {
		streamName = cfg.StreamID()
	}

	h.mu.Lock()
	ss := &StreamStatus{Stream: streamName, Object: cfg.Target.Object, Status: sling.ExecStatusRunning}
	h.streams[streamName] = ss
	h.mu.Unlock()
	h.publish()

	task := sling.NewTask(taskExecID, cfg)
	task.Replication = repl
	task.Context = h.gctx

	h.mu.Lock()
	h.tasks = append(h.tasks, task)
	h.mu.Unlock()

	err := task.Execute()

	h.mu.Lock()
	ss = h.streams[streamName]
	if ss == nil {
		ss = &StreamStatus{Stream: streamName}
	}
	ss.Status = task.Status
	ss.Rows = task.GetCount()
	if inBytes, _ := task.GetBytes(); inBytes > 0 {
		ss.Bytes = inBytes
	}
	if task.Err != nil {
		ss.Error = task.Err.Error()
	}
	h.mu.Unlock()
	h.publish()

	return err
}

func (h *RunHandle) recoverPanic(where string) {
	if r := recover(); r != nil {
		g.Debug("panic in run %s (%s): %v", h.ID, where, r)
		h.mu.Lock()
		h.err = g.Error("internal error in %s: %v", where, r)
		h.mu.Unlock()
		h.setStatus(sling.ExecStatusError)
	}
}

/* ----------------------------- persistence ------------------------------ */

func (h *RunHandle) insertRow() {
	if h.db == nil {
		return
	}
	run := h.runRow()
	if err := h.db.Create(run).Error; err != nil {
		g.Debug("could not insert run row: %s", err)
	}
	h.db.Audit("run", "job", h.JobID, g.F("started run %s (trigger=%s)", h.ID, h.Trigger))
}

func (h *RunHandle) updateRow() {
	if h.db == nil {
		return
	}
	h.updateRowWith(func(run *Run) {})
}

func (h *RunHandle) finalizeRow() {
	if h.db == nil {
		return
	}
	h.updateRowWith(func(run *Run) {
		run.LogTail = h.logs.tail(50)
	})
}

func (h *RunHandle) updateRowWith(extra func(*Run)) {
	snap := h.Snapshot()
	run := &Run{
		ID:       h.ID,
		Status:   sling.ExecStatus(snap.Status),
		Rows:     snap.Rows,
		Bytes:    snap.Bytes,
		DurationMs: snap.DurationMs,
	}
	if snap.EndTime != nil {
		run.EndTime = snap.EndTime
	}
	if snap.Error != "" {
		run.Error = snap.Error
	}
	if len(snap.Streams) > 0 {
		if b, err := json.Marshal(snap.Streams); err == nil {
			run.StreamSummaryJSON = string(b)
		}
	}
	extra(run)

	err := h.db.Model(&Run{}).Where("id = ?", h.ID).Updates(map[string]any{
		"status":          run.Status,
		"rows":            run.Rows,
		"bytes":           run.Bytes,
		"duration_ms":     run.DurationMs,
		"end_time":        run.EndTime,
		"error":           run.Error,
		"stream_summary":  run.StreamSummaryJSON,
		"log_tail":        run.LogTail,
	}).Error
	if err != nil {
		g.Debug("could not update run row %s: %s", h.ID, err)
	}
}

func (h *RunHandle) runRow() *Run {
	snap := h.Snapshot()
	return &Run{
		ID:       h.ID,
		JobID:    h.JobID,
		JobName:  h.JobName,
		Type:     h.Type,
		Trigger:  h.Trigger,
		Attempt:  h.Attempt,
		Status:   sling.ExecStatus(snap.Status),
		StartTime: h.started,
	}
}

/* --------------------------- status helpers ------------------------------ */

// setStatus/status wrap an atomic string for lock-free reads.
func (h *RunHandle) setStatus(s sling.ExecStatus) { h.status.Store(string(s)) }

func (h *RunHandle) currentStatus() sling.ExecStatus {
	v, _ := h.status.Load().(string)
	if v == "" {
		return sling.ExecStatusCreated
	}
	return sling.ExecStatus(v)
}
