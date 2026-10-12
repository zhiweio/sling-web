// handlers_runs.go serves run lifecycle APIs (ad-hoc runs, listing,
// detail, logs, cancel) and notifications.
package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/slingdata-io/sling-cli/core/sling"
)

// specFingerprint hashes the serialized spec so identical ad-hoc specs can
// be detected across runs.
func specFingerprint(spec JobSpec) string {
	b, err := json.Marshal(spec)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:16])
}

// handleRunsCreate starts an ad-hoc run from a raw spec (wizard "run now"
// without saving). An identical spec already in flight is rejected.
func (a *App) handleRunsCreate(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Spec JobSpec `json:"spec"`
	}
	if err := decodeBody(r, &in); err != nil {
		apiErr(w, err)
		return
	}
	if len(in.Spec.Streams) == 0 {
		writeErr(w, 400, "spec.streams is required")
		return
	}
	fp := specFingerprint(in.Spec)
	if a.Runner.HasActiveFingerprint(fp) {
		writeErr(w, 409, "an identical run is already in progress")
		return
	}

	if in.Spec.IsSingleTask() {
		cfg, err := in.Spec.CompileTask()
		if err != nil {
			apiErr(w, err)
			return
		}
		h := a.Runner.StartTask(nil, "api", 1, cfg, fp)
		if h == nil {
			writeErr(w, 409, "an identical run is already in progress")
			return
		}
		writeJSON(w, 202, h.Snapshot())
		return
	}

	repl, err := in.Spec.CompileReplication()
	if err != nil {
		apiErr(w, err)
		return
	}
	h := a.Runner.StartReplication(nil, "api", 1, repl, in.Spec, fp)
	if h == nil {
		writeErr(w, 409, "an identical run is already in progress")
		return
	}
	writeJSON(w, 202, h.Snapshot())
}

func (a *App) handleRunsList(w http.ResponseWriter, r *http.Request) {
	f := RunFilter{
		JobID:   r.URL.Query().Get("job_id"),
		Status:  r.URL.Query().Get("status"),
		Trigger: r.URL.Query().Get("trigger"),
	}
	if v := r.URL.Query().Get("from"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			f.From = &t
		}
	}
	if v := r.URL.Query().Get("to"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			f.To = &t
		}
	}
	f.Limit, _ = httpAtoi(r.URL.Query().Get("limit"))
	f.Offset, _ = httpAtoi(r.URL.Query().Get("offset"))

	runs, total, err := a.DB.ListRuns(f)
	if err != nil {
		apiErr(w, err)
		return
	}

	// overlay live snapshots for active runs
	for i := range runs {
		if h := a.Runner.Get(runs[i].ID); h != nil && !h.IsFinished() {
			snap := h.Snapshot()
			runs[i].Status = sling.ExecStatus(snap.Status)
			runs[i].Rows = snap.Rows
			runs[i].Bytes = snap.Bytes
			runs[i].DurationMs = snap.DurationMs
		}
	}

	writeJSON(w, 200, map[string]any{"runs": runs, "total": total, "active": len(a.Runner.Active())})
}

func (a *App) handleRunsGet(w http.ResponseWriter, r *http.Request) {
	id := pathVar(r, "id")

	if h := a.Runner.Get(id); h != nil {
		writeJSON(w, 200, h.Snapshot())
		return
	}

	run := &Run{}
	if err := a.DB.First(run, "id = ?", id).Error; err != nil {
		apiErr(w, err)
		return
	}
	body := map[string]any{
		"run_id":       run.ID,
		"job_id":       run.JobID,
		"job_name":     run.JobName,
		"type":         run.Type,
		"trigger":      run.Trigger,
		"attempt":      run.Attempt,
		"status":       string(run.Status),
		"rows":         run.Rows,
		"bytes":        run.Bytes,
		"duration_ms":  run.DurationMs,
		"start_time":   run.StartTime,
		"end_time":     run.EndTime,
		"error":        run.Error,
		"streams":      run.StreamSummary(),
		"log_tail":     run.LogTail,
	}
	writeJSON(w, 200, body)
}

func (a *App) handleRunLogs(w http.ResponseWriter, r *http.Request) {
	offset, _ := httpAtoi(r.URL.Query().Get("offset"))
	limit, _ := httpAtoi(r.URL.Query().Get("limit"))

	// live runs stream from the in-memory ring via SSE; the HTTP endpoint
	// serves persisted lines (works for finished and live runs alike).
	logs, err := a.DB.GetLogs(pathVar(r, "id"), int64(offset), limit)
	if err != nil {
		apiErr(w, err)
		return
	}
	type logLine struct {
		Seq   int64  `json:"seq"`
		Ts    string `json:"ts"`
		Level int8   `json:"level"`
		Text  string `json:"text"`
	}
	out := make([]logLine, 0, len(logs))
	for _, l := range logs {
		out = append(out, logLine{Seq: l.Seq, Ts: l.Ts.Format(time.RFC3339Nano), Level: l.Level, Text: l.Text})
	}
	writeJSON(w, 200, map[string]any{"logs": out, "count": len(out)})
}

func (a *App) handleRunCancel(w http.ResponseWriter, r *http.Request) {
	if !a.Runner.Cancel(pathVar(r, "id")) {
		writeErr(w, 404, "run not found or already finished")
		return
	}
	a.DB.Audit("cancel", "run", pathVar(r, "id"), "")
	writeJSON(w, 202, map[string]any{"cancelling": true})
}

/* ----------------------------- notifications ----------------------------- */

func (a *App) handleNotifications(w http.ResponseWriter, r *http.Request) {
	q := a.DB.Model(&Notification{})
	if r.URL.Query().Get("unread") == "1" {
		q = q.Where("read = ?", false)
	}
	if typ := r.URL.Query().Get("type"); typ != "" {
		q = q.Where("type = ?", typ)
	}
	limit, _ := httpAtoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 50
	}
	offset, _ := httpAtoi(r.URL.Query().Get("offset"))
	var items []Notification
	q.Order("created_at DESC").Limit(limit).Offset(offset).Find(&items)
	unread := int64(0)
	a.DB.Model(&Notification{}).Where("read = ?", false).Count(&unread)
	writeJSON(w, 200, map[string]any{"notifications": items, "unread": unread})
}

func (a *App) handleNotificationsRead(w http.ResponseWriter, r *http.Request) {
	var in struct {
		IDs []int64 `json:"ids,omitempty"` // empty = all
	}
	if err := decodeBody(r, &in); err != nil {
		apiErr(w, err)
		return
	}
	q := a.DB.Model(&Notification{}).Where("read = ?", false)
	if len(in.IDs) > 0 {
		q = q.Where("id IN ?", in.IDs)
	}
	q.Update("read", true)
	writeJSON(w, 200, map[string]any{"ok": true})
}

func httpAtoi(s string) (int, error) {
	if s == "" {
		return 0, nil
	}
	return strconv.Atoi(s)
}
