package server

// governance_test.go covers the governance layer: notify channel payloads and
// CRUD masking, run-finish notification wiring, audit filtering, settings
// defaults and the backup lifecycle.

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

/* ------------------------------ channel payloads --------------------------- */

func TestChannelPayloadsAndMasking(t *testing.T) {
	ev := &webhookEvent{
		Event: "run_failed", Title: "my-job failed", Body: "boom",
		Level: "error", Meta: map[string]any{"run_id": "run-1"}, At: "2026-01-01T00:00:00Z",
	}

	// wecom: markdown message
	b, err := buildChannelPayload("wecom", ev, "")
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	json.Unmarshal(b, &m)
	if m["msgtype"] != "markdown" {
		t.Fatalf("wecom msgtype: %v", m["msgtype"])
	}
	md := m["markdown"].(map[string]any)
	if !strings.Contains(md["content"].(string), "my-job failed") || !strings.Contains(md["content"].(string), "run_id: run-1") {
		t.Fatalf("wecom content: %v", md["content"])
	}

	// feishu: text message, plus timestamp+sign when a secret is set
	b, _ = buildChannelPayload("feishu", ev, "")
	m = map[string]any{}
	json.Unmarshal(b, &m)
	if m["msg_type"] != "text" {
		t.Fatalf("feishu msg_type: %v", m["msg_type"])
	}
	if _, ok := m["sign"]; ok {
		t.Fatal("feishu must not sign without a secret")
	}
	b, _ = buildChannelPayload("feishu", ev, "sekrit")
	m = map[string]any{}
	json.Unmarshal(b, &m)
	if m["sign"] == "" || m["timestamp"] == "" {
		t.Fatalf("feishu sign: %v", m)
	}

	// dingtalk: markdown with title
	b, _ = buildChannelPayload("dingtalk", ev, "")
	m = map[string]any{}
	json.Unmarshal(b, &m)
	md = m["markdown"].(map[string]any)
	if md["title"] != "my-job failed" {
		t.Fatalf("dingtalk title: %v", md["title"])
	}

	// slack: text
	b, _ = buildChannelPayload("slack", ev, "")
	m = map[string]any{}
	json.Unmarshal(b, &m)
	if !strings.Contains(m["text"].(string), "my-job failed") {
		t.Fatalf("slack text: %v", m["text"])
	}

	// generic: flat json with event + meta fields
	b, _ = buildChannelPayload("generic", ev, "")
	m = map[string]any{}
	json.Unmarshal(b, &m)
	if m["event"] != "run_failed" || m["run_id"] != "run-1" {
		t.Fatalf("generic flat: %v", m)
	}

	// dingtalk signed URL
	signed := signDingTalkURL("https://oapi.dingtalk.com/robot/send?access_token=tk", "sekrit")
	if !strings.Contains(signed, "timestamp=") || !strings.Contains(signed, "sign=") || !strings.Contains(signed, "access_token=tk") {
		t.Fatalf("dingtalk signed url: %s", signed)
	}

	// masking: wecom key rides in the query, feishu/slack in the path
	masked := maskChannelURL("wecom", "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=SECRET")
	if strings.Contains(masked, "SECRET") || !strings.Contains(masked, "key=%2A%2A%2A") && !strings.Contains(masked, "key=***") {
		t.Fatalf("wecom mask: %s", masked)
	}
	masked = maskChannelURL("feishu", "https://open.feishu.cn/open-apis/bot/v2/hook/SECRET")
	if strings.Contains(masked, "SECRET") {
		t.Fatalf("feishu mask: %s", masked)
	}

	// subscription filter
	ch := NotifyChannel{EventsJSON: `["run_failed","paused"]`}
	if !ch.SubscribesTo("run_failed") || ch.SubscribesTo("run_success") {
		t.Fatal("events filter broken")
	}
	if !(NotifyChannel{}).SubscribesTo("anything") {
		t.Fatal("empty events must subscribe to all")
	}
}

/* ------------------------------ channel CRUD ------------------------------- */

func TestNotifyChannelCRUDAndMasking(t *testing.T) {
	app := newTestApp(t)
	ts := httptest.NewServer(app.Handler())
	defer ts.Close()

	const realURL = "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=SECRETKEY123"

	// invalid provider / url -> 400
	if resp, _ := apiPost(t, ts.URL, "/api/notify-channels", map[string]any{"name": "x", "provider": "pigeon", "url": realURL}); resp != 400 {
		t.Fatal("invalid provider must 400")
	}
	if resp, _ := apiPost(t, ts.URL, "/api/notify-channels", map[string]any{"name": "x", "provider": "wecom", "url": "not-a-url"}); resp != 400 {
		t.Fatal("invalid url must 400")
	}

	// create
	resp, body := apiPost(t, ts.URL, "/api/notify-channels", map[string]any{
		"name": "ops-alert", "provider": "wecom", "url": realURL,
		"secret": "sign-secret", "events": []string{"run_failed", "paused"},
	})
	if resp != 201 {
		t.Fatalf("create: %d %s", resp, body)
	}
	ch := channelView{}
	json.Unmarshal([]byte(body), &ch)
	if ch.URL == realURL || strings.Contains(ch.URL, "SECRETKEY123") || !strings.Contains(ch.URL, "key=") {
		t.Fatalf("create must return masked url, got %s", ch.URL)
	}
	if !ch.HasSecret {
		t.Fatal("create must report has_secret")
	}

	// list masked
	resp, body = apiGet(t, ts.URL, "/api/notify-channels")
	if resp != 200 || strings.Contains(body, "SECRETKEY123") || strings.Contains(body, "sign-secret") {
		t.Fatalf("list leaks secrets: %d %s", resp, body)
	}

	// update with the masked URL/secret -> stored values are kept
	in := map[string]any{
		"name": "ops-alert", "provider": "wecom",
		"url":  maskChannelURL("wecom", realURL),
		"secret": "***", "events": []string{"run_failed"},
	}
	if resp, body = apiPut(t, ts.URL, "/api/notify-channels/"+itoa(ch.ID), in); resp != 200 {
		t.Fatalf("update: %d %s", resp, body)
	}
	stored := NotifyChannel{}
	if err := app.DB.First(&stored, "id = ?", ch.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.URL != realURL {
		t.Fatalf("masked update must keep url, got %s", stored.URL)
	}
	if s, _ := app.Vault.LoadSecret(channelSecretPath(ch.ID)); s != "sign-secret" {
		t.Fatalf("masked update must keep secret, got %q", s)
	}

	// delete removes row + secret
	if resp, _ = apiDelete(t, ts.URL, "/api/notify-channels/"+itoa(ch.ID)); resp != 200 {
		t.Fatal("delete failed")
	}
	var count int64
	app.DB.Model(&NotifyChannel{}).Count(&count)
	if count != 0 {
		t.Fatal("channel row must be deleted")
	}
	if s, _ := app.Vault.LoadSecret(channelSecretPath(ch.ID)); s != "" {
		t.Fatal("channel secret must be deleted")
	}
}

/* ------------------------------ channel test send -------------------------- */

func TestNotifyChannelTestSend(t *testing.T) {
	var mu sync.Mutex
	var gotPath string
	var gotBody map[string]any
	rcv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		gotPath, gotBody = r.URL.RawQuery, map[string]any{}
		json.Unmarshal(b, &gotBody)
		mu.Unlock()
		w.Write([]byte(`{"errcode":0,"errmsg":"ok"}`))
	}))
	defer rcv.Close()

	app := newTestApp(t)
	ts := httptest.NewServer(app.Handler())
	defer ts.Close()

	resp, body := apiPost(t, ts.URL, "/api/notify-channels", map[string]any{
		"name": "w", "provider": "wecom", "url": rcv.URL + "/hook?key=kkk",
	})
	if resp != 201 {
		t.Fatalf("create: %d %s", resp, body)
	}
	ch := channelView{}
	json.Unmarshal([]byte(body), &ch)

	resp, body = apiPost(t, ts.URL, "/api/notify-channels/"+itoa(ch.ID)+"/test", nil)
	if resp != 200 || !strings.Contains(body, `"ok":true`) {
		t.Fatalf("test send: %d %s", resp, body)
	}
	mu.Lock()
	defer mu.Unlock()
	if gotPath != "key=kkk" {
		t.Fatalf("test send must hit the full url, got query %q", gotPath)
	}
	if gotBody["msgtype"] != "markdown" {
		t.Fatalf("test send payload: %v", gotBody)
	}

	// provider error codes are surfaced (wecom returns 200 + errcode)
	errRcv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		w.Write([]byte(`{"errcode":93000,"errmsg":"invalid webhook url"}`))
	}))
	defer errRcv.Close()
	resp, body = apiPost(t, ts.URL, "/api/notify-channels", map[string]any{
		"name": "bad", "provider": "wecom", "url": errRcv.URL + "/hook",
	})
	ch2 := channelView{}
	json.Unmarshal([]byte(body), &ch2)
	resp, body = apiPost(t, ts.URL, "/api/notify-channels/"+itoa(ch2.ID)+"/test", nil)
	if resp != 200 || !strings.Contains(body, `"ok":false`) || !strings.Contains(body, "93000") {
		t.Fatalf("provider error must surface: %d %s", resp, body)
	}
}

/* --------------------------- run-finish notifications ---------------------- */

func TestRunFinishedGates(t *testing.T) {
	db := openTestDB(t)
	n := NewNotifier(db, nil, NewBroker())
	job := &Job{ID: "j1", Name: "myjob"}

	var count int64
	n.RunFinished(job, Snapshot{Status: "success", Rows: 10})
	db.Model(&Notification{}).Count(&count)
	if count != 0 {
		t.Fatal("success must not notify by default (notify.on_success=off)")
	}

	n.RunFinished(job, Snapshot{Status: "error", Error: "boom"})
	db.Model(&Notification{}).Where("type = ?", "run_failed").Count(&count)
	if count != 1 {
		t.Fatal("failure must notify")
	}

	db.SetSetting("notify.on_success", "true")
	n.RunFinished(job, Snapshot{Status: "success"})
	db.Model(&Notification{}).Where("type = ?", "run_success").Count(&count)
	if count != 1 {
		t.Fatal("opt-in success must notify")
	}

	db.SetSetting("notify.on_failure", "false")
	n.RunFinished(job, Snapshot{Status: "error"})
	db.Model(&Notification{}).Where("type = ?", "run_failed").Count(&count)
	if count != 1 {
		t.Fatal("notify.on_failure=false must silence failures")
	}
}

// TestRunFinishedWiredViaExecutor pushes a compile-failing job through the
// executor and expects a run_failed notification (the previously dead wire).
func TestRunFinishedWiredViaExecutor(t *testing.T) {
	app := newTestApp(t)
	job := &Job{ID: "jbad", Name: "compile-fail-job"}
	job.SetSpec(JobSpec{Streams: []StreamSpec{{SourceObject: "s.t", TargetObject: "d.t"}}}) // no conns
	if err := app.DB.Create(job).Error; err != nil {
		t.Fatal(err)
	}

	app.Executor.Start()
	if !app.Executor.Enqueue(job, "manual") {
		t.Fatal("queue full?!")
	}

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var count int64
		app.DB.Model(&Notification{}).Where("type = ?", "run_failed").Count(&count)
		var runs int64
		app.DB.Model(&Run{}).Where("job_id = ? AND status = ?", "jbad", "error").Count(&runs)
		if count >= 1 && runs >= 1 {
			var notif Notification
			app.DB.Where("type = ?", "run_failed").First(&notif)
			if !strings.Contains(notif.Title, "compile-fail-job") {
				t.Fatalf("notification title: %s", notif.Title)
			}
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("compile-failing job produced no run_failed notification")
}

/* ------------------------------ audit filtering ---------------------------- */

func TestAuditFilteringAndSettingsDefaults(t *testing.T) {
	app := newTestApp(t)
	ts := httptest.NewServer(app.Handler())
	defer ts.Close()

	// channel create writes an audit entry with entity_type "channel"
	if resp, body := apiPost(t, ts.URL, "/api/notify-channels", map[string]any{"name": "c1", "provider": "generic", "url": "http://example.com/hook"}); resp != 201 {
		t.Fatalf("channel create: %d %s", resp, body)
	}

	resp, body := apiGet(t, ts.URL, "/api/audit?entity_type=channel&action=create")
	if resp != 200 {
		t.Fatalf("audit filter: %d %s", resp, body)
	}
	var out struct {
		Entries []AuditEntry `json:"entries"`
		Total   int64        `json:"total"`
	}
	json.Unmarshal([]byte(body), &out)
	if out.Total < 1 || len(out.Entries) < 1 {
		t.Fatalf("audit filter found nothing: %s", body)
	}
	for _, e := range out.Entries {
		if e.EntityType != "channel" || e.Action != "create" {
			t.Fatalf("audit filter leaked rows: %+v", e)
		}
	}

	// settings: governance defaults are returned even when unset
	resp, body = apiGet(t, ts.URL, "/api/settings")
	if resp != 200 {
		t.Fatalf("settings: %d", resp)
	}
	settings := map[string]string{}
	json.Unmarshal([]byte(body), &settings)
	for key, want := range map[string]string{
		"notify.on_success":         "false",
		"notify.on_failure":         "true",
		"retention.notifications_days": "30",
		"retention.audit_days":      "365",
		"backup.auto_enabled":       "true",
		"backup.cron":               "0 3 * * *",
		"backup.keep_count":         "7",
	} {
		if settings[key] != want {
			t.Fatalf("setting %s = %q, want %q", key, settings[key], want)
		}
	}
	if resp, _ := apiPut(t, ts.URL, "/api/settings", map[string]string{"backup.keep_count": "3"}); resp != 200 {
		t.Fatal("backup.keep_count must be writable")
	}
}

/* ------------------------------ backup lifecycle --------------------------- */

func TestBackupLifecycle(t *testing.T) {
	app := newTestApp(t)
	ts := httptest.NewServer(app.Handler())
	defer ts.Close()

	// create via API
	resp, body := apiPost(t, ts.URL, "/api/backups", nil)
	if resp != 201 {
		t.Fatalf("backup create: %d %s", resp, body)
	}
	created := BackupFile{}
	json.Unmarshal([]byte(body), &created)
	if !validBackupName(created.Name) {
		t.Fatalf("bad backup name: %s", created.Name)
	}
	if _, err := statBackup(created.Name); err != nil {
		t.Fatalf("snapshot file missing: %v", err)
	}

	// list
	resp, body = apiGet(t, ts.URL, "/api/backups")
	if resp != 200 || !strings.Contains(body, created.Name) {
		t.Fatalf("backup list: %d %s", resp, body)
	}

	// download
	resp, _ = apiGet(t, ts.URL, "/api/backups/"+created.Name+"/download")
	if resp != 200 {
		t.Fatalf("backup download: %d", resp)
	}

	// traversal is rejected
	if resp, _ := apiGet(t, ts.URL, "/api/backups/..%2Fserver.db/download"); resp != 400 && resp != 404 {
		t.Fatalf("traversal download must be rejected, got %d", resp)
	}
	if resp, _ := apiDelete(t, ts.URL, "/api/backups/evil.db"); resp != 400 {
		t.Fatalf("bad name delete must 400, got %d", resp)
	}

	// rotation keeps only the newest N auto snapshots
	for i := 0; i < 3; i++ {
		if _, err := app.Maintenance.createSnapshot("auto"); err != nil {
			t.Fatal(err)
		}
	}
	app.Maintenance.rotateBackups(1)
	autos, err := listBackupFiles("auto")
	if err != nil || len(autos) != 1 {
		t.Fatalf("rotation must keep 1 auto snapshot, got %d (%v)", len(autos), err)
	}

	// delete
	resp, _ = apiDelete(t, ts.URL, "/api/backups/"+created.Name)
	if resp != 200 {
		t.Fatalf("backup delete: %d", resp)
	}
	if _, err := statBackup(created.Name); err == nil {
		t.Fatal("snapshot file must be deleted")
	}
}

/* ------------------------------ maintenance stop --------------------------- */

func TestMaintenanceStopIsIdempotent(t *testing.T) {
	db := openTestDB(t)
	m := NewMaintenance(db, nil, 90)

	// stop before start returns immediately
	done := make(chan struct{})
	go func() { m.Stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("stop before start must not block")
	}

	m.Start()
	m.Stop()
	m.Stop() // idempotent
}

func statBackup(name string) (int64, error) {
	st, err := os.Stat(filepath.Join(backupsDir(), name))
	if err != nil {
		return 0, err
	}
	return st.Size(), nil
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
