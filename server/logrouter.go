// logrouter.go attributes process-global log lines to the run that emitted
// them. It reuses the goroutine-ID routing pattern proven in
// core/sling/build/executor.go: before a task runs on a goroutine we bind
// that goroutine's ID to its run; the global env.LogSink looks the emitter
// goroutine up and tags/buffers the line accordingly.
//
// Known limitation (same as sling build): lines emitted from helper
// goroutines spawned *inside* a task carry no binding and fall to the
// global stream.
package server

import (
	"fmt"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/flarco/g"
	"github.com/slingdata-io/sling-cli/core/env"
)

// runLogs is the per-run in-memory log ring; persistence goes through db.
type runLogs struct {
	seq   atomic.Int64
	lines []g.LogLine
	mu    sync.Mutex
}

const maxRunLogLines = 10000

// append buffers a line in memory and persists it (best effort).
func (rl *runLogs) append(db *DB, runID string, ll g.LogLine) {
	seq := rl.seq.Add(1)
	rl.mu.Lock()
	if len(rl.lines) < maxRunLogLines {
		rl.lines = append(rl.lines, ll)
	}
	rl.mu.Unlock()
	if db != nil {
		db.AppendLog(runID, seq, time.Now(), ll.Level, ll.Text)
	}
}

func (rl *runLogs) tail(n int) string {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	if n > len(rl.lines) {
		n = len(rl.lines)
	}
	var sb strings.Builder
	for _, ll := range rl.lines[len(rl.lines)-n:] {
		sb.WriteString(ll.Text)
		if !strings.HasSuffix(ll.Text, "\n") {
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

// LogRouter routes env.LogSink lines to in-flight runs by goroutine ID.
// Sling task.Execute runs its work on internal goroutines, so lines emitted
// there carry no binding; when exactly one run is active, unattributed lines
// fall back to it (concurrent runs: unattributed lines are dropped).
type LogRouter struct {
	mu      sync.RWMutex
	gidRuns map[uint64]string // goroutine id -> run id
	noRuns  map[uint64]struct{} // goroutines whose lines never attribute to a run
	runs    map[string]*runLogs
	active  map[string]struct{}
	prev    func(*g.LogLine)

	broker *Broker
	db     *DB
}

func NewLogRouter(broker *Broker, db *DB) *LogRouter {
	return &LogRouter{
		gidRuns: map[uint64]string{},
		noRuns:  map[uint64]struct{}{},
		runs:    map[string]*runLogs{},
		active:  map[string]struct{}{},
		broker:  broker,
		db:      db,
	}
}

// attach installs the global sink (chaining over any existing one).
func (lr *LogRouter) attach() {
	lr.mu.Lock()
	lr.prev = env.LogSink
	lr.mu.Unlock()
	env.LogSink = lr.sink
}

func (lr *LogRouter) sink(ll *g.LogLine) {
	gid := goroutineID()
	runID := lr.runForGID(gid)
	if runID == "" && !lr.gidSuppressed(gid) {
		runID = lr.soleActiveRun()
	}
	if runID != "" {
		ll.Group = runID
		renderLogLine(ll)
		if logs := lr.logsFor(runID); logs != nil {
			logs.append(lr.db, runID, *ll)
			if lr.broker != nil {
				lr.broker.Publish(Event{Type: "log", RunID: runID, Payload: ll})
			}
		}
	}
	if prev := lr.prevSink(); prev != nil {
		prev(ll)
	}
}

// SuppressGID marks the current goroutine as a non-run logger: its lines are
// never attributed to a run, not even through the sole-active-run fallback.
// The HTTP middleware uses this so request/access logs never pollute the
// console of whatever run happens to be active.
func (lr *LogRouter) SuppressGID() (restore func()) {
	gid := goroutineID()
	lr.mu.Lock()
	lr.noRuns[gid] = struct{}{}
	lr.mu.Unlock()
	return func() {
		lr.mu.Lock()
		delete(lr.noRuns, gid)
		lr.mu.Unlock()
	}
}

func (lr *LogRouter) gidSuppressed(gid uint64) bool {
	lr.mu.RLock()
	defer lr.mu.RUnlock()
	_, ok := lr.noRuns[gid]
	return ok
}

// SetActiveRuns is called by the runner on run start/finish.
func (lr *LogRouter) SetActiveRuns(ids []string) {
	lr.mu.Lock()
	lr.active = make(map[string]struct{}, len(ids))
	for _, id := range ids {
		lr.active[id] = struct{}{}
	}
	lr.mu.Unlock()
}

// soleActiveRun returns the only active run id, or "".
func (lr *LogRouter) soleActiveRun() string {
	lr.mu.RLock()
	defer lr.mu.RUnlock()
	if len(lr.active) == 1 {
		for id := range lr.active {
			return id
		}
	}
	return ""
}

func (lr *LogRouter) prevSink() func(*g.LogLine) {
	lr.mu.RLock()
	defer lr.mu.RUnlock()
	return lr.prev
}

// bind registers a run on the current goroutine and returns its undo func.
// Call it on the goroutine that will run task.Execute().
func (lr *LogRouter) bind(runID string, logs *runLogs) func() {
	gid := goroutineID()
	lr.mu.Lock()
	lr.gidRuns[gid] = runID
	if logs != nil {
		lr.runs[runID] = logs
	}
	lr.mu.Unlock()
	return func() {
		lr.mu.Lock()
		delete(lr.gidRuns, gid)
		delete(lr.runs, runID)
		lr.mu.Unlock()
	}
}

func (lr *LogRouter) runForGID(gid uint64) string {
	lr.mu.RLock()
	defer lr.mu.RUnlock()
	return lr.gidRuns[gid]
}

func (lr *LogRouter) logsFor(runID string) *runLogs {
	lr.mu.RLock()
	defer lr.mu.RUnlock()
	return lr.runs[runID]
}

// renderLogLine expands printf-style Text templates with their Args so the
// web log terminal shows final text (the terminal sink does this rendering
// for the CLI, the raw hook does not).
func renderLogLine(ll *g.LogLine) {
	if len(ll.Args) > 0 && strings.Contains(ll.Text, "%") {
		defer func() { recover() }() // mismatched verbs must not kill a run
		ll.Text = fmt.Sprintf(ll.Text, ll.Args...)
		ll.Args = nil
	}
}

// goroutineID returns the current goroutine's id from the stack prefix
// (same technique as core/sling/build/executor.go).
func goroutineID() uint64 {
	var buf [64]byte
	n := runtime.Stack(buf[:], false)
	// "goroutine 123 [running]..."
	id := uint64(0)
	for i := 10; i < n; i++ {
		c := buf[i]
		if c < '0' || c > '9' {
			break
		}
		id = id*10 + uint64(c-'0')
	}
	return id
}
