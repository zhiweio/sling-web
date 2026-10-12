package server

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/flarco/g"
)

/* ------------------------------ log router -------------------------------- */

func TestLogRouterGIDAttribution(t *testing.T) {
	broker := NewBroker()
	db := openTestDB(t)
	lr := NewLogRouter(broker, db)

	// subscriber captures routed events
	got := make(chan Event, 16)
	s := broker.subscribe()
	defer broker.unsubscribe(s)
	go func() {
		for ev := range s.ch {
			got <- ev
		}
	}()

	logs := &runLogs{}
	var routed []g.LogLine
	lr.mu.Lock()
	lr.prev = func(ll *g.LogLine) { routed = append(routed, *ll) }
	lr.mu.Unlock()

	done := make(chan struct{})
	go func() {
		defer close(done)
		unbind := lr.bind("run-A", logs)
		defer unbind()
		lr.sink(&g.LogLine{Level: 1, Text: "from run A goroutine\n"})
	}()
	<-done

	// unbound goroutine: no attribution
	lr.sink(&g.LogLine{Level: 1, Text: "from main goroutine\n"})

	if len(routed) != 2 {
		t.Fatalf("prev sink should see both lines, got %d", len(routed))
	}
	if routed[0].Group != "run-A" {
		t.Fatalf("bound line group = %q, want run-A", routed[0].Group)
	}
	if routed[1].Group == "run-A" {
		t.Fatal("unbound line must not be attributed")
	}

	// persisted with sequence
	persisted, err := db.GetLogs("run-A", 0, 10)
	if err != nil || len(persisted) != 1 {
		t.Fatalf("persisted logs: %d err=%v", len(persisted), err)
	}
	if persisted[0].Text != "from run A goroutine\n" || persisted[0].Seq != 1 {
		t.Fatalf("unexpected persisted line: %+v", persisted[0])
	}

	// SSE event emitted
	select {
	case ev := <-got:
		if ev.Type != "log" || ev.RunID != "run-A" {
			t.Fatalf("unexpected event: %+v", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("no SSE log event")
	}
}

/* --------------------------------- broker ---------------------------------- */

func TestBrokerFanOutAndSlowConsumer(t *testing.T) {
	b := NewBroker()
	s1 := b.subscribe()
	s2 := b.subscribe()
	defer b.unsubscribe(s1)
	defer b.unsubscribe(s2)

	b.Publish(Event{Type: "execution", RunID: "r1"})

	ev1, ev2 := <-s1.ch, <-s2.ch
	if ev1.RunID != "r1" || ev2.RunID != "r1" {
		t.Fatal("both subscribers should receive")
	}

	// slow consumer: fill the buffer, publisher must not block
	slow := b.subscribe()
	defer b.unsubscribe(slow)
	for i := 0; i < 6000; i++ {
		b.Publish(Event{Type: "log"})
	}
	select {
	case <-slow.ch:
	default:
		// either some events arrived or all dropped; must not deadlock
	}
}

/* -------------------------------- scheduler -------------------------------- */

func TestSchedulerNextRun(t *testing.T) {
	db := openTestDB(t)
	s := NewScheduler(db, NewExecutor(db, NewRunner(db, NewBroker(), NewLogRouter(NewBroker(), db), nil), NewBroker(), nil, 1))

	job := &Job{ScheduleCron: "0 9 * * *", ScheduleTZ: "UTC"}
	next := s.NextRun(job, 1)
	if next == nil {
		t.Fatal("next run should exist")
	}
	if next.Hour() != 9 || next.Minute() != 0 {
		t.Fatalf("next = %v, want 09:00 UTC", next)
	}

	second := s.NextRun(job, 2)
	if !second.After(*next) {
		t.Fatal("second occurrence must be after the first")
	}

	if s.NextRun(&Job{ScheduleCron: ""}, 1) != nil {
		t.Fatal("no cron -> nil")
	}
}

func TestSchedulerUpcoming(t *testing.T) {
	db := openTestDB(t)
	e := NewExecutor(db, NewRunner(db, NewBroker(), NewLogRouter(NewBroker(), db), nil), NewBroker(), nil, 1)
	s := NewScheduler(db, e)
	db.Create(&Job{ID: "j1", Name: "every-minute", ScheduleCron: "* * * * *", ScheduleEnabled: true})

	items := s.Upcoming(5)
	if len(items) != 1 || items[0].JobName != "every-minute" {
		t.Fatalf("upcoming: %+v", items)
	}
	if time.Until(items[0].At) > 90*time.Second {
		t.Fatal("every-minute should fire within ~60s")
	}
}

/* ------------------------------ retention prune ---------------------------- */

func TestPrune(t *testing.T) {
	db := openTestDB(t)
	old := time.Now().AddDate(0, 0, -30)
	db.Create(&Run{ID: "old", Status: "success", StartTime: old, EndTime: &old})
	db.Create(&RunLog{RunID: "old", Seq: 1, Ts: old, Text: "x"})
	db.Create(&RunLog{RunID: "ghost", Seq: 1, Ts: old, Text: "orphan"}) // run row never existed
	fresh := time.Now()
	db.Create(&Run{ID: "fresh", Status: "success", StartTime: fresh, EndTime: &fresh})

	// notifications: old+read pruned, old+unread kept, fresh+read kept
	db.Create(&Notification{Type: "system", Level: "info", Title: "old read", Read: true, CreatedAt: old})
	db.Create(&Notification{Type: "system", Level: "info", Title: "old unread", Read: false, CreatedAt: old})
	db.Create(&Notification{Type: "system", Level: "info", Title: "fresh read", Read: true, CreatedAt: fresh})

	// audit: old pruned, fresh kept
	db.Create(&AuditEntry{Action: "create", EntityType: "job", EntityID: "j1", CreatedAt: old})
	db.Create(&AuditEntry{Action: "run", EntityType: "job", EntityID: "j1", CreatedAt: fresh})

	// versions: 12 old versions -> keep 10 newest; fresh version kept on top
	for i := 1; i <= 12; i++ {
		db.Create(&JobVersion{JobID: "j1", Version: i, SpecJSON: "{}", CreatedAt: old.Add(time.Duration(i) * time.Minute)})
	}
	db.Create(&JobVersion{JobID: "j1", Version: 13, SpecJSON: "{}", CreatedAt: fresh})

	res, err := db.Prune(PruneConfig{Runs: 7, Notifications: 7, Audit: 7})
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if res.Runs != 1 || res.RunLogs != 2 {
		t.Fatalf("prune runs=%d logs=%d (want 1/2: one run log + one orphan)", res.Runs, res.RunLogs)
	}
	if res.Notifications != 1 {
		t.Fatalf("prune notifications=%d, want 1 (old read only)", res.Notifications)
	}
	if res.AuditEntries != 1 {
		t.Fatalf("prune audit=%d, want 1", res.AuditEntries)
	}
	if res.JobVersions != 3 {
		t.Fatalf("prune versions=%d, want 3 (keep the 10 newest overall, incl. fresh)", res.JobVersions)
	}

	var remain []Run
	db.Find(&remain)
	if len(remain) != 1 || remain[0].ID != "fresh" {
		t.Fatalf("remaining runs: %+v", remain)
	}
	var notifs []Notification
	db.Find(&notifs)
	if len(notifs) != 2 {
		t.Fatalf("remaining notifications: %+v", notifs)
	}
	var versions []JobVersion
	db.Find(&versions)
	if len(versions) != 10 {
		t.Fatalf("remaining versions: %d, want the 10 newest", len(versions))
	}
	var audits []AuditEntry
	db.Find(&audits)
	if len(audits) != 1 || audits[0].Action != "run" {
		t.Fatalf("remaining audits: %+v", audits)
	}
}

/* ------------------------------- HTTP layer -------------------------------- */

func newTestApp(t *testing.T) *App {
	t.Helper()
	dir := testEnv(t)
	app, err := New(Options{DBPath: dir + "\\test.db", Concurrency: 2})
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	t.Cleanup(app.Shutdown)
	return app
}

func TestAPIJobsCRUDAndValidation(t *testing.T) {
	app := newTestApp(t)
	ts := httptest.NewServer(app.Handler())
	defer ts.Close()

	// create without streams -> 400
	resp, body := apiPost(t, ts.URL, "/api/jobs", map[string]any{"name": "x", "spec": JobSpec{}})
	if resp != 400 || !strings.Contains(body, "streams") {
		t.Fatalf("validation: %d %s", resp, body)
	}

	spec := JobSpec{
		SourceConn: "A", TargetConn: "B",
		Defaults: StreamSpec{Mode: "replace"},
		Streams:  []StreamSpec{{SourceObject: "s.t", TargetObject: "d.t"}},
	}
	resp, body = apiPost(t, ts.URL, "/api/jobs", map[string]any{"name": "my-job", "spec": spec})
	if resp != 201 {
		t.Fatalf("create: %d %s", resp, body)
	}
	job := map[string]any{}
	json.Unmarshal([]byte(body), &job)
	id := job["id"].(string)

	// duplicate name -> 400
	if resp, _ = apiPost(t, ts.URL, "/api/jobs", map[string]any{"name": "my-job", "spec": spec}); resp == 201 {
		t.Fatal("duplicate name should fail")
	}

	// list
	resp, body = apiGet(t, ts.URL, "/api/jobs")
	if resp != 200 || !strings.Contains(body, "my-job") {
		t.Fatalf("list: %d %s", resp, body)
	}

	// update -> version bump + version history
	spec.Defaults.Mode = "truncate"
	if resp, body = apiPut(t, ts.URL, "/api/jobs/"+id, map[string]any{"name": "my-job", "spec": spec}); resp != 200 {
		t.Fatalf("update: %d %s", resp, body)
	}
	if resp, body = apiGet(t, ts.URL, "/api/jobs/"+id+"/versions"); resp != 200 || !strings.Contains(body, "\"version\":2") {
		t.Fatalf("versions: %d %s", resp, body)
	}

	// schedule validation
	if resp, _ = apiPatch(t, ts.URL, "/api/jobs/"+id+"/schedule", map[string]any{"enabled": true}); resp != 400 {
		t.Fatal("enable schedule without cron must 400")
	}
	if resp, _ = apiPatch(t, ts.URL, "/api/jobs/"+id+"/schedule", map[string]any{"cron": "bad-spec", "enabled": true}); resp == 200 {
		t.Fatal("invalid cron must fail")
	}
	if resp, _ = apiPatch(t, ts.URL, "/api/jobs/"+id+"/schedule", map[string]any{"cron": "*/5 * * * *", "enabled": true}); resp != 200 {
		t.Fatal("valid cron must pass")
	}

	// delete
	if resp, _ = apiDelete(t, ts.URL, "/api/jobs/"+id); resp != 200 {
		t.Fatal("delete failed")
	}
}

func TestAPITokenAuth(t *testing.T) {
	dir := testEnv(t)
	app, err := New(Options{DBPath: dir + "\\test.db", Token: "sekrit"})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Shutdown()
	ts := httptest.NewServer(app.Handler())
	defer ts.Close()

	resp, _ := apiGet(t, ts.URL, "/api/health")
	if resp != 401 {
		t.Fatalf("no token = %d, want 401", resp)
	}
	resp, _ = apiGetAuth(t, ts.URL, "/api/health", "sekrit")
	if resp != 200 {
		t.Fatalf("with token = %d, want 200", resp)
	}
}

func TestAPISettingsWhitelist(t *testing.T) {
	app := newTestApp(t)
	ts := httptest.NewServer(app.Handler())
	defer ts.Close()

	if resp, _ := apiPut(t, ts.URL, "/api/settings", map[string]string{"evil": "1"}); resp != 400 {
		t.Fatal("unknown setting must 400")
	}
	if resp, _ := apiPut(t, ts.URL, "/api/settings", map[string]string{"ui.language": "zh-CN"}); resp != 200 {
		t.Fatal("known setting must pass")
	}
	if resp, body := apiGet(t, ts.URL, "/api/settings"); resp != 200 || !strings.Contains(body, "zh-CN") {
		t.Fatalf("settings get: %d %s", resp, body)
	}
}

func TestConnsTestPropsDoesNotClobberStoredRecord(t *testing.T) {
	app := newTestApp(t)

	_, err := app.Conns.Upsert(UpsertInput{
		Name: "ORA1",
		Type: "oracle",
		Props: map[string]any{"host": "127.0.0.1", "port": "1", "username": "u", "password": "p"},
	})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}

	// wizard edit flow: testing unsaved props under an existing name must not
	// touch the stored record (dial to 127.0.0.1:1 is refused instantly)
	if _, err = app.Conns.TestProps(UpsertInput{
		Name:  "ORA1",
		Type:  "oracle",
		Props: map[string]any{"host": "127.0.0.1", "port": "1", "username": "u"},
	}); err != nil {
		t.Fatalf("test props: %v", err)
	}

	rec, err := app.Conns.Get("ORA1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	props := rec.Props()
	if props["host"] != "127.0.0.1" || props["username"] != "u" {
		t.Fatalf("stored props clobbered by TestProps: %+v", props)
	}
	if len(rec.SecretFieldList()) == 0 {
		t.Fatal("secret field list clobbered by TestProps")
	}

	// wizard create flow: testing a brand-new name must not create a record
	if _, err = app.Conns.TestProps(UpsertInput{
		Name:  "ORA2",
		Type:  "oracle",
		Props: map[string]any{"host": "127.0.0.1", "port": "1", "username": "u"},
	}); err != nil {
		t.Fatalf("test props (new name): %v", err)
	}
	if _, err = app.Conns.Get("ORA2"); err == nil {
		t.Fatal("TestProps must not create a phantom connection record")
	}
}

func TestConnsDuplicateNameAndRenameRejected(t *testing.T) {
	app := newTestApp(t)

	if _, err := app.Conns.Create(UpsertInput{
		Name: "ORA1", Type: "oracle",
		Props: map[string]any{"host": "h1", "port": "1521", "username": "u", "password": "p"},
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	// duplicate create (any case) must be rejected, not overwrite
	if _, err := app.Conns.Create(UpsertInput{
		Name: "ora1", Type: "oracle",
		Props: map[string]any{"host": "h2", "port": "1521", "username": "x"},
	}); err == nil {
		t.Fatal("duplicate create must be rejected")
	}
	rec, err := app.Conns.Get("ORA1")
	if err != nil {
		t.Fatal(err)
	}
	if rec.Props()["host"] != "h1" || rec.Props()["username"] != "u" {
		t.Fatalf("existing connection was overwritten: %+v", rec.Props())
	}

	// updating with another connection's name must not clobber the target
	if _, err := app.Conns.Create(UpsertInput{
		Name: "ORA2", Type: "oracle",
		Props: map[string]any{"host": "h9", "port": "1521", "username": "z"},
	}); err != nil {
		t.Fatalf("create 2: %v", err)
	}
	if _, err := app.Conns.Update("ORA1", UpsertInput{
		Name: "ORA2", Type: "oracle",
		Props: map[string]any{"host": "h1", "port": "1521", "username": "u"},
	}); err == nil {
		t.Fatal("renaming onto an existing name must be rejected")
	}
	rec2, err := app.Conns.Get("ORA2")
	if err != nil {
		t.Fatal(err)
	}
	if rec2.Props()["host"] != "h9" || rec2.Props()["username"] != "z" {
		t.Fatalf("ORA2 was clobbered via rename: %+v", rec2.Props())
	}

	// unknown target fails
	if _, err := app.Conns.Update("NOPE", UpsertInput{Type: "oracle"}); err == nil {
		t.Fatal("updating an unknown connection must be rejected")
	}
}
