// handlers_jobs.go serves the job management APIs (CRUD, scheduling,
// versions, batch creation, YAML import/export).
package server

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/flarco/g"
)

func newID() string {
	return g.RandString(g.AlphaRunesLower+g.NumericRunes, 20)
}

/* ---------------------------------- CRUD --------------------------------- */

// jobWithState decorates a job with its live execution state so the UI can
// disable duplicate triggers. ActiveRunState is "queued" or "running";
// ActiveRunID is only known once the run has started.
type jobWithState struct {
	Job
	ActiveRunID    string `json:"active_run_id,omitempty"`
	ActiveRunState string `json:"active_run_state,omitempty"`
}

func (a *App) decorateJobs(jobs []Job) []jobWithState {
	inflight := a.Executor.InflightJobIDs()
	runIDByJob := map[string]string{}
	for _, h := range a.Runner.Active() {
		if h.JobID != "" {
			runIDByJob[h.JobID] = h.ID
		}
	}
	out := make([]jobWithState, len(jobs))
	for i := range jobs {
		out[i] = jobWithState{Job: jobs[i], ActiveRunID: runIDByJob[jobs[i].ID]}
		if inflight[jobs[i].ID] {
			if out[i].ActiveRunID != "" {
				out[i].ActiveRunState = "running"
			} else {
				out[i].ActiveRunState = "queued"
			}
		}
	}
	return out
}

func (a *App) handleJobsList(w http.ResponseWriter, r *http.Request) {
	f := JobFilter{
		Search: r.URL.Query().Get("q"),
		Folder: r.URL.Query().Get("folder"),
		Tag:    r.URL.Query().Get("tag"),
	}
	if v := r.URL.Query().Get("paused"); v != "" {
		b := v == "1" || v == "true"
		f.Paused = &b
	}
	f.Limit, _ = httpAtoi(r.URL.Query().Get("limit"))
	f.Offset, _ = httpAtoi(r.URL.Query().Get("offset"))

	jobs, total, err := a.DB.ListJobs(f)
	if err != nil {
		apiErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"jobs": a.decorateJobs(jobs), "total": total})
}

// jobInput is the create/update payload.
type jobInput struct {
	Name              string          `json:"name"`
	Description       string          `json:"description,omitempty"`
	Folder            string          `json:"folder,omitempty"`
	Tags              []string        `json:"tags,omitempty"`
	Spec              JobSpec         `json:"spec"`
	ScheduleCron      string          `json:"schedule_cron,omitempty"`
	ScheduleTZ        string          `json:"schedule_tz,omitempty"`
	ScheduleEnabled   bool            `json:"schedule_enabled,omitempty"`
	SchemaPolicy      string          `json:"schema_policy,omitempty"`
	ConcurrencyPolicy string          `json:"concurrency_policy,omitempty"`
	RetryMax          int             `json:"retry_max,omitempty"`
	RetryBackoffSec   int             `json:"retry_backoff_sec,omitempty"`
	VerifyRows        bool            `json:"verify_rows,omitempty"`
	NotifyWebhooks    []string        `json:"notify_webhooks,omitempty"`
	IsTemplate        bool            `json:"is_template,omitempty"`
}

func (in jobInput) validate() error {
	if in.Name == "" {
		return g.Error("name is required")
	}
	if len(in.Spec.Streams) == 0 {
		return g.Error("spec.streams is required (at least one)")
	}
	if in.Spec.SourceConn == "" || in.Spec.TargetConn == "" {
		return g.Error("spec.source_conn and spec.target_conn are required")
	}
	if in.ScheduleEnabled && in.ScheduleCron == "" {
		return g.Error("schedule_cron is required when scheduling is enabled")
	}
	return nil
}

func (a *App) handleJobsCreate(w http.ResponseWriter, r *http.Request) {
	var in jobInput
	if err := decodeBody(r, &in); err != nil {
		apiErr(w, err)
		return
	}
	if err := in.validate(); err != nil {
		apiErr(w, err)
		return
	}

	job := &Job{
		ID:                newID(),
		Name:              in.Name,
		Description:       in.Description,
		Folder:            in.Folder,
		ScheduleCron:      in.ScheduleCron,
		ScheduleTZ:        in.ScheduleTZ,
		ScheduleEnabled:   in.ScheduleEnabled,
		SchemaPolicy:      orDefault(in.SchemaPolicy, "auto"),
		ConcurrencyPolicy: orDefault(in.ConcurrencyPolicy, "skip"),
		RetryMax:          in.RetryMax,
		RetryBackoffSec:   orDefaultInt(in.RetryBackoffSec, 60),
		VerifyRows:        in.VerifyRows,
		IsTemplate:        in.IsTemplate,
		Version:           1,
	}
	job.SetTags(in.Tags)
	if err := job.SetSpec(in.Spec); err != nil {
		apiErr(w, err)
		return
	}
	if b, err := jsonMarshal(in.NotifyWebhooks); err == nil {
		job.NotifyWebhooks = b
	}

	if err := a.DB.Create(job).Error; err != nil {
		apiErr(w, g.Error(err, "job %s already exists", in.Name))
		return
	}
	a.saveVersion(job, "created")
	a.DB.Audit("create", "job", job.ID, job.Name)
	if err := a.Scheduler.AddJob(job); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 201, job)
}

func (a *App) handleJobsGet(w http.ResponseWriter, r *http.Request) {
	job, err := a.DB.GetJob(pathVar(r, "id"))
	if err != nil {
		apiErr(w, err)
		return
	}
	writeJSON(w, 200, a.decorateJobs([]Job{*job})[0])
}

func (a *App) handleJobsUpdate(w http.ResponseWriter, r *http.Request) {
	job, err := a.DB.GetJob(pathVar(r, "id"))
	if err != nil {
		apiErr(w, err)
		return
	}
	var in jobInput
	if err = decodeBody(r, &in); err != nil {
		apiErr(w, err)
		return
	}
	if err = in.validate(); err != nil {
		apiErr(w, err)
		return
	}

	old := *job
	job.Name = in.Name
	job.Description = in.Description
	job.Folder = in.Folder
	job.ScheduleCron = in.ScheduleCron
	job.ScheduleTZ = in.ScheduleTZ
	job.ScheduleEnabled = in.ScheduleEnabled
	job.SchemaPolicy = orDefault(in.SchemaPolicy, "auto")
	job.ConcurrencyPolicy = orDefault(in.ConcurrencyPolicy, "skip")
	job.RetryMax = in.RetryMax
	job.RetryBackoffSec = orDefaultInt(in.RetryBackoffSec, 60)
	job.VerifyRows = in.VerifyRows
	job.SetTags(in.Tags)
	if err = job.SetSpec(in.Spec); err != nil {
		apiErr(w, err)
		return
	}
	if b, err := jsonMarshal(in.NotifyWebhooks); err == nil {
		job.NotifyWebhooks = b
	}
	job.Version = old.Version + 1

	if err = a.DB.Save(job).Error; err != nil {
		apiErr(w, err)
		return
	}
	a.saveVersion(job, "updated")
	a.DB.Audit("update", "job", job.ID, job.Name)
	if err = a.Scheduler.AddJob(job); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, job)
}

func (a *App) handleJobsDelete(w http.ResponseWriter, r *http.Request) {
	job, err := a.DB.GetJob(pathVar(r, "id"))
	if err != nil {
		apiErr(w, err)
		return
	}
	if err = a.DB.Delete(job).Error; err != nil {
		apiErr(w, err)
		return
	}
	a.DB.Where("job_id = ?", job.ID).Delete(&JobVersion{})
	a.Scheduler.RemoveJob(job.ID)
	a.DB.Audit("delete", "job", job.ID, job.Name)
	writeJSON(w, 200, map[string]any{"deleted": true})
}

// handleJobsAnalyze runs the pre-flight analysis on a saved job.
func (a *App) handleJobsAnalyze(w http.ResponseWriter, r *http.Request) {
	job, err := a.DB.GetJob(pathVar(r, "id"))
	if err != nil {
		apiErr(w, err)
		return
	}
	analysis, err := a.AnalyzeSpec(job.Spec())
	if err != nil {
		apiErr(w, err)
		return
	}
	writeJSON(w, 200, analysis)
}

// handleAnalyze runs the pre-flight analysis on an ad-hoc spec (wizard).
func (a *App) handleAnalyze(w http.ResponseWriter, r *http.Request) {
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
	analysis, err := a.AnalyzeSpec(in.Spec)
	if err != nil {
		apiErr(w, err)
		return
	}
	writeJSON(w, 200, analysis)
}

func (a *App) handleJobsRun(w http.ResponseWriter, r *http.Request) {
	job, err := a.DB.GetJob(pathVar(r, "id"))
	if err != nil {
		apiErr(w, err)
		return
	}
	switch a.Executor.Enqueue(job, "manual") {
	case EnqueueRejectedRunning:
		// single-instance guard: one queued-or-running execution per job
		a.DB.Audit("run", "job", job.ID, "rejected duplicate trigger: already running")
		body := map[string]any{"error": "job already has a run in progress", "status": 409}
		for _, h := range a.Runner.Active() {
			if h.JobID == job.ID {
				body["run_id"] = h.ID
				break
			}
		}
		writeJSON(w, 409, body)
		return
	case EnqueueRejectedFull, EnqueueRejectedStopped:
		writeErr(w, 503, "execution queue is full")
		return
	}
	a.DB.Audit("run", "job", job.ID, "manual trigger")
	a.Executor.PublishJobUpdate(job.ID)
	writeJSON(w, 202, map[string]any{"queued": true, "job": job.Name})
}

func (a *App) handleJobsDuplicate(w http.ResponseWriter, r *http.Request) {
	job, err := a.DB.GetJob(pathVar(r, "id"))
	if err != nil {
		apiErr(w, err)
		return
	}
	dup := *job
	dup.ID = newID()
	dup.Name = uniqueName(a, job.Name+"-copy")
	dup.ScheduleEnabled = false
	dup.NextRunAt = nil
	dup.ConsecutiveFailures = 0
	dup.AutoPaused = false
	dup.Version = 1
	dup.CreatedAt, dup.UpdatedAt = time.Now(), time.Now()
	dup.LastRunAt = nil
	if err = a.DB.Create(&dup).Error; err != nil {
		apiErr(w, err)
		return
	}
	a.saveVersion(&dup, "duplicated from "+job.Name)
	a.DB.Audit("create", "job", dup.ID, "duplicate of "+job.Name)
	writeJSON(w, 201, &dup)
}

func uniqueName(a *App, base string) string {
	name := base
	for i := 2; ; i++ {
		if _, err := a.DB.GetJob(name); err != nil {
			return name
		}
		name = g.F("%s-%d", base, i)
	}
}

/* ------------------------------ YAML I/O --------------------------------- */

func (a *App) handleJobsExport(w http.ResponseWriter, r *http.Request) {
	job, err := a.DB.GetJob(pathVar(r, "id"))
	if err != nil {
		apiErr(w, err)
		return
	}
	yamlStr, err := job.Spec().ToYAML()
	if err != nil {
		apiErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"name": job.Name, "yaml": yamlStr})
}

func (a *App) handleJobsImport(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name string `json:"name,omitempty"`
		YAML string `json:"yaml"`
	}
	if err := decodeBody(r, &in); err != nil {
		apiErr(w, err)
		return
	}
	spec, err := SpecFromReplicationYAML(in.YAML)
	if err != nil {
		apiErr(w, err)
		return
	}
	name := in.Name
	if name == "" {
		name = "imported-" + g.RandString(g.AlphaRunesLower, 6)
	}
	job := &Job{
		ID:                newID(),
		Name:              uniqueName(a, name),
		SchemaPolicy:      "auto",
		ConcurrencyPolicy: "skip",
		RetryBackoffSec:   60,
		Version:           1,
	}
	if err = job.SetSpec(spec); err != nil {
		apiErr(w, err)
		return
	}
	if err = a.DB.Create(job).Error; err != nil {
		apiErr(w, err)
		return
	}
	a.saveVersion(job, "imported")
	a.DB.Audit("import", "job", job.ID, job.Name)
	writeJSON(w, 201, job)
}

/* ------------------------------- versions -------------------------------- */

func (a *App) saveVersion(job *Job, comment string) {
	a.DB.Create(&JobVersion{JobID: job.ID, Version: job.Version, SpecJSON: job.SpecJSON, Comment: comment})
}

func (a *App) handleJobVersions(w http.ResponseWriter, r *http.Request) {
	job, err := a.DB.GetJob(pathVar(r, "id"))
	if err != nil {
		apiErr(w, err)
		return
	}
	var versions []JobVersion
	a.DB.Where("job_id = ?", job.ID).Order("version DESC").Find(&versions)
	writeJSON(w, 200, versions)
}

func (a *App) handleJobRollback(w http.ResponseWriter, r *http.Request) {
	job, err := a.DB.GetJob(pathVar(r, "id"))
	if err != nil {
		apiErr(w, err)
		return
	}
	var in struct {
		Version int `json:"version"`
	}
	if err = decodeBody(r, &in); err != nil {
		apiErr(w, err)
		return
	}

	var ver JobVersion
	if err = a.DB.Where("job_id = ? AND version = ?", job.ID, in.Version).First(&ver).Error; err != nil {
		apiErr(w, g.Error(err, "version %d not found", in.Version))
		return
	}

	job.SpecJSON = ver.SpecJSON
	job.Version++
	a.DB.Save(job)
	a.saveVersion(job, g.F("rollback to v%d", in.Version))
	a.DB.Audit("rollback", "job", job.ID, g.F("to v%d", in.Version))
	writeJSON(w, 200, job)
}

/* ------------------------------- schedule -------------------------------- */

func (a *App) handleJobSchedule(w http.ResponseWriter, r *http.Request) {
	job, err := a.DB.GetJob(pathVar(r, "id"))
	if err != nil {
		apiErr(w, err)
		return
	}
	var in struct {
		Cron    string `json:"cron"`
		TZ      string `json:"tz,omitempty"`
		Enabled *bool  `json:"enabled,omitempty"`
	}
	if err = decodeBody(r, &in); err != nil {
		apiErr(w, err)
		return
	}
	if in.Cron != "" {
		job.ScheduleCron = in.Cron
	}
	if in.TZ != "" {
		job.ScheduleTZ = in.TZ
	}
	if in.Enabled != nil {
		job.ScheduleEnabled = *in.Enabled
		if *in.Enabled {
			job.AutoPaused = false
			job.ConsecutiveFailures = 0
		}
	}
	if job.ScheduleEnabled && job.ScheduleCron == "" {
		writeErr(w, 400, "cron is required to enable a schedule")
		return
	}
	if err = a.DB.Save(job).Error; err != nil {
		apiErr(w, err)
		return
	}
	if err = a.Scheduler.AddJob(job); err != nil {
		apiErr(w, err)
		return
	}
	action := "pause"
	if job.ScheduleEnabled {
		action = "resume"
	}
	a.DB.Audit(action, "job", job.ID, job.ScheduleCron)
	writeJSON(w, 200, job)
}

func (a *App) handleJobNextRuns(w http.ResponseWriter, r *http.Request) {
	job, err := a.DB.GetJob(pathVar(r, "id"))
	if err != nil {
		apiErr(w, err)
		return
	}
	var times []*time.Time
	for n := 1; n <= 5; n++ {
		t := a.Scheduler.NextRun(job, n)
		if t == nil {
			break
		}
		times = append(times, t)
	}
	writeJSON(w, 200, map[string]any{"next_runs": times})
}

func (a *App) handleSchedulesUpcoming(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, a.Scheduler.Upcoming(10))
}

/* ------------------------------ batch create ----------------------------- */

type batchInput struct {
	Mode        string     `json:"mode"` // per_table (default) | single_job
	JobName     string     `json:"job_name,omitempty"`
	SourceConn  string     `json:"source_conn"`
	TargetConn  string     `json:"target_conn"`
	Defaults    StreamSpec `json:"defaults"`
	Tables      []struct {
		Source string   `json:"source"`
		Target string   `json:"target,omitempty"`
		PrimaryKey []string `json:"primary_key,omitempty"`
		UpdateKey  string   `json:"update_key,omitempty"`
	} `json:"tables"`
	ScheduleCron string   `json:"schedule_cron,omitempty"`
	ScheduleTZ   string   `json:"schedule_tz,omitempty"`
}

func (a *App) handleJobsBatchCreate(w http.ResponseWriter, r *http.Request) {
	var in batchInput
	if err := decodeBody(r, &in); err != nil {
		apiErr(w, err)
		return
	}
	if len(in.Tables) == 0 {
		writeErr(w, 400, "tables is required")
		return
	}
	if in.SourceConn == "" || in.TargetConn == "" {
		writeErr(w, 400, "source_conn and target_conn are required")
		return
	}

	var created []*Job

	if in.Mode == "single_job" {
		spec := JobSpec{SourceConn: in.SourceConn, TargetConn: in.TargetConn, Defaults: in.Defaults}
		for _, t := range in.Tables {
			spec.Streams = append(spec.Streams, StreamSpec{
				SourceObject: t.Source,
				TargetObject: t.Target,
				PrimaryKey:   t.PrimaryKey,
				UpdateKey:    t.UpdateKey,
			})
		}
		job := &Job{
			ID: newID(), Name: uniqueName(a, orDefault(in.JobName, "batch")),
			SchemaPolicy: "auto", ConcurrencyPolicy: "skip", RetryBackoffSec: 60, Version: 1,
			ScheduleCron: in.ScheduleCron, ScheduleTZ: in.ScheduleTZ,
			ScheduleEnabled: in.ScheduleCron != "",
		}
		if err := job.SetSpec(spec); err != nil {
			apiErr(w, err)
			return
		}
		if err := a.DB.Create(job).Error; err != nil {
			apiErr(w, err)
			return
		}
		a.saveVersion(job, "batch created")
		a.Scheduler.AddJob(job)
		created = append(created, job)
	} else {
		for _, t := range in.Tables {
			spec := JobSpec{
				SourceConn: in.SourceConn, TargetConn: in.TargetConn, Defaults: in.Defaults,
				Streams: []StreamSpec{{
					SourceObject: t.Source, TargetObject: t.Target,
					PrimaryKey: t.PrimaryKey, UpdateKey: t.UpdateKey,
				}},
			}
			job := &Job{
				ID: newID(), Name: uniqueName(a, orDefault(in.JobName, "batch")+"-"+t.Source),
				SchemaPolicy: "auto", ConcurrencyPolicy: "skip", RetryBackoffSec: 60, Version: 1,
				ScheduleCron: in.ScheduleCron, ScheduleTZ: in.ScheduleTZ,
				ScheduleEnabled: in.ScheduleCron != "",
			}
			if err := job.SetSpec(spec); err != nil {
				apiErr(w, err)
				return
			}
			if err := a.DB.Create(job).Error; err != nil {
				apiErr(w, err)
				return
			}
			a.saveVersion(job, "batch created")
			a.Scheduler.AddJob(job)
			created = append(created, job)
		}
	}

	a.DB.Audit("create", "job", "", g.F("batch created %d jobs", len(created)))
	writeJSON(w, 201, map[string]any{"created": created, "count": len(created)})
}

/* -------------------------------- helpers --------------------------------- */

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func orDefaultInt(v, def int) int {
	if v == 0 {
		return def
	}
	return v
}

func jsonMarshal(v any) (string, error) {
	b, err := json.Marshal(v)
	return string(b), err
}
