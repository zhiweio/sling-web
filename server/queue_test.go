// queue_test.go covers the per-job single-instance reservation, the HTTP
// 409 on duplicate triggers, skipped-run traceability for refused
// scheduled fires, and ad-hoc spec fingerprint dedup.
package server

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/slingdata-io/sling-cli/core/sling"
)

func queueTestJob(t *testing.T, app *App, id, name string) *Job {
	t.Helper()
	job := &Job{ID: id, Name: name}
	job.SetSpec(JobSpec{
		SourceConn: "A", TargetConn: "B",
		Streams: []StreamSpec{{SourceObject: "s.t", TargetObject: "d.t"}},
	})
	if err := app.DB.Create(job).Error; err != nil {
		t.Fatal(err)
	}
	return job
}

func TestExecutorSingleInstanceReservation(t *testing.T) {
	// no Boot: workers never start, so a queued request holds its
	// reservation deterministically
	app := newTestApp(t)
	job := queueTestJob(t, app, "j1", "single")

	if res := app.Executor.Enqueue(job, "manual"); res != EnqueueOK {
		t.Fatalf("first enqueue: %v", res)
	}
	if res := app.Executor.Enqueue(job, "manual"); res != EnqueueRejectedRunning {
		t.Fatalf("second enqueue: %v, want EnqueueRejectedRunning", res)
	}

	// a different job is unaffected
	other := queueTestJob(t, app, "j2", "other")
	if res := app.Executor.Enqueue(other, "manual"); res != EnqueueOK {
		t.Fatalf("other job enqueue: %v", res)
	}

	// stop drains the queue and releases every slot; drained items leave
	// a skipped trace
	app.Executor.Stop()
	if ids := app.Executor.InflightJobIDs(); len(ids) != 0 {
		t.Fatalf("inflight after stop+drain: %v", ids)
	}
	var skipped int64
	app.DB.Model(&Run{}).Where("status = ?", "skipped").Count(&skipped)
	if skipped != 2 {
		t.Fatalf("drained runs recorded: %d, want 2", skipped)
	}
}

func TestExecutorReservationReleasedAfterFinish(t *testing.T) {
	app := newTestApp(t)
	// no conns -> compile fails -> execute() returns quickly
	job := &Job{ID: "jbad2", Name: "compile-fail"}
	job.SetSpec(JobSpec{Streams: []StreamSpec{{SourceObject: "s.t", TargetObject: "d.t"}}})
	if err := app.DB.Create(job).Error; err != nil {
		t.Fatal(err)
	}

	if res := app.Executor.Enqueue(job, "manual"); res != EnqueueOK {
		t.Fatalf("enqueue: %v", res)
	}
	if res := app.Executor.Enqueue(job, "manual"); res != EnqueueRejectedRunning {
		t.Fatalf("while queued: %v", res)
	}

	// simulate a worker picking the request up: the slot must be released
	// once the (failing) execution completes
	app.Executor.execute(runRequest{Job: job, Trigger: "manual"})
	if res := app.Executor.Enqueue(job, "manual"); res != EnqueueOK {
		t.Fatalf("after finish: %v, want EnqueueOK", res)
	}
}

func TestScheduledFireWhileRunningRecordsSkipped(t *testing.T) {
	app := newTestApp(t)
	job := queueTestJob(t, app, "jsk", "skipper")

	if res := app.Executor.Enqueue(job, "manual"); res != EnqueueOK {
		t.Fatalf("manual: %v", res)
	}
	if res := app.Executor.Enqueue(job, "schedule"); res != EnqueueRejectedRunning {
		t.Fatalf("schedule fire: %v", res)
	}

	var count int64
	app.DB.Model(&Run{}).Where("job_id = ? AND status = ?", "jsk", "skipped").Count(&count)
	if count != 1 {
		t.Fatalf("skipped rows: %d, want 1", count)
	}
	var run Run
	if err := app.DB.Where("job_id = ? AND status = ?", "jsk", "skipped").First(&run).Error; err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(run.Error, "already running") {
		t.Fatalf("skip reason: %s", run.Error)
	}
}

func TestHandleJobsRunDuplicateRejected(t *testing.T) {
	app := newTestApp(t) // no Boot: the run stays queued, assertions are deterministic
	ts := httptest.NewServer(app.Handler())
	defer ts.Close()

	spec := JobSpec{SourceConn: "A", TargetConn: "B", Streams: []StreamSpec{{SourceObject: "s.t", TargetObject: "d.t"}}}
	resp, body := apiPost(t, ts.URL, "/api/jobs", map[string]any{"name": "dup-run", "spec": spec})
	if resp != 201 {
		t.Fatalf("create: %d %s", resp, body)
	}
	job := map[string]any{}
	if err := json.Unmarshal([]byte(body), &job); err != nil {
		t.Fatal(err)
	}

	if resp, _ := apiPost(t, ts.URL, "/api/jobs/"+job["id"].(string)+"/run", nil); resp != 202 {
		t.Fatal("first run must be accepted")
	}
	resp, body = apiPost(t, ts.URL, "/api/jobs/"+job["id"].(string)+"/run", nil)
	if resp != 409 || !strings.Contains(body, "already") {
		t.Fatalf("duplicate run: %d %s, want 409", resp, body)
	}

	// the jobs list exposes the queued state for the UI
	resp, body = apiGet(t, ts.URL, "/api/jobs")
	if resp != 200 || !strings.Contains(body, `"active_run_state":"queued"`) {
		t.Fatalf("list annotation: %d %s", resp, body)
	}
}

func fakeActiveRun(id, fp string) *RunHandle {
	h := &RunHandle{ID: id, fingerprint: fp, logs: &runLogs{}, done: make(chan struct{})}
	h.bindContext()
	h.setStatus(sling.ExecStatusRunning)
	return h
}

func TestAdhocDuplicateSpecRejected(t *testing.T) {
	app := newTestApp(t)
	ts := httptest.NewServer(app.Handler())
	defer ts.Close()

	// HTTP guard: an identical active spec is rejected before compiling
	// (the spec below does not compile against real connections, which is
	// exactly why the fingerprint check must come first)
	spec := JobSpec{SourceConn: "A", TargetConn: "B", Streams: []StreamSpec{{SourceObject: "s.t", TargetObject: "d.t"}}}
	active := fakeActiveRun("run-x", specFingerprint(spec))
	app.Runner.register(active)
	t.Cleanup(func() { active.setStatus(sling.ExecStatusCancelled) }) // skip shutdown grace wait

	resp, body := apiPost(t, ts.URL, "/api/runs", map[string]any{"spec": spec})
	if resp != 409 || !strings.Contains(body, "identical") {
		t.Fatalf("duplicate adhoc: %d %s, want 409", resp, body)
	}
	other := spec
	other.Streams = []StreamSpec{{SourceObject: "s.t", TargetObject: "d.other"}}
	if resp, _ := apiPost(t, ts.URL, "/api/runs", map[string]any{"spec": other}); resp == 409 {
		t.Fatal("distinct spec must not be rejected as duplicate")
	}
}

func TestRegisterFingerprintDedup(t *testing.T) {
	app := newTestApp(t)
	fp := specFingerprint(JobSpec{SourceConn: "A", TargetConn: "B"})

	first := fakeActiveRun("run-a", fp)
	if !app.Runner.register(first) {
		t.Fatal("first register must succeed")
	}
	if app.Runner.register(fakeActiveRun("run-b", fp)) {
		t.Fatal("duplicate fingerprint register must be refused")
	}
	// once finished, the same spec may run again
	first.setStatus(sling.ExecStatusError)
	last := fakeActiveRun("run-c", fp)
	if !app.Runner.register(last) {
		t.Fatal("register after finish must succeed")
	}
	t.Cleanup(func() { last.setStatus(sling.ExecStatusCancelled) })
}

func TestCancelBeforeRunStartsDoesNotPanic(t *testing.T) {
	app := newTestApp(t)
	h := fakeActiveRun("run-cancel", "")
	if !app.Runner.register(h) {
		t.Fatal("register must succeed")
	}
	// cancel immediately: must be a safe no-op even though run() never started
	if !app.Runner.Cancel("run-cancel") {
		t.Fatal("cancel of a fresh handle must succeed")
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if h.gctx.Ctx.Err() != nil {
			h.setStatus(sling.ExecStatusCancelled)
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("cancel did not propagate to the run context")
}

func TestCancelledRunDoesNotStayActive(t *testing.T) {
	app := newTestApp(t)
	h := fakeActiveRun("run-cancelled-active", "")
	if !app.Runner.register(h) {
		t.Fatal("register must succeed")
	}
	t.Cleanup(func() { h.setStatus(sling.ExecStatusCancelled) })

	if n := len(app.Runner.Active()); n != 1 {
		t.Fatalf("active runs while running: %d, want 1", n)
	}

	// once the run ends up cancelled it is terminal: it must not keep
	// counting as active (run-center badge) nor accept another cancel
	h.setStatus(sling.ExecStatusCancelled)
	if !h.IsFinished() {
		t.Fatal("cancelled run must report finished")
	}
	if n := len(app.Runner.Active()); n != 0 {
		t.Fatalf("active runs after cancel: %d, want 0", n)
	}
	if app.Runner.Cancel(h.ID) {
		t.Fatal("cancel of a cancelled run must report false")
	}
}
