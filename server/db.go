// Package server implements the sling-server Browser/Server workbench:
// a standalone HTTP API + embedded web UI over the sling core engine.
//
// db.go owns the persistence layer. SQLite (WAL) is the single source of
// truth for jobs, schedules, runs, logs, connections and encrypted secrets.
// Core CLI files (env.yaml) are never written with sensitive data.
package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/flarco/g"
	"github.com/slingdata-io/sling-cli/core/dbio/database"
	"github.com/slingdata-io/sling-cli/core/sling"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// DB is the server store handle.
type DB struct {
	*gorm.DB
	Path string `json:"path"`
}

// OpenDB opens (creating if needed) the server SQLite database at path.
func OpenDB(path string) (*DB, error) {
	if path == "" {
		path = DefaultDBPath()
	}
	if strings.HasPrefix(path, "sqlite://") {
		path = strings.TrimPrefix(path, "sqlite://")
	}
	os.MkdirAll(filepath.Dir(path), 0755)

	dbURL := g.F("sqlite://%s?cache=shared&mode=rwc&_journal_mode=WAL&_busy_timeout=10000", path)
	conn, err := database.NewConn(dbURL, "silent=true")
	if err != nil {
		return nil, g.Error(err, "could not open server db at %s", path)
	}

	gdb, err := conn.GetGormConn(&gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, g.Error(err, "could not connect server db at %s", path)
	}

	// SQLite performs best for a single-writer workload; keep the pool small.
	sqlDB, _ := gdb.DB()
	if sqlDB != nil {
		sqlDB.SetMaxOpenConns(1)
	}

	db := &DB{DB: gdb, Path: path}
	if err = db.Migrate(); err != nil {
		return nil, err
	}
	return db, nil
}

// DefaultDBPath returns the default server db location (~/.sling/server.db),
// distinct from the CLI's .sling.db so the two never contend.
func DefaultDBPath() string {
	return filepath.Join(homeDir(), "server.db")
}

// Close releases the underlying sql pool (required on Windows before
// deleting the db file, e.g. in tests).
func (db *DB) Close() error {
	if sqlDB, err := db.DB.DB(); err == nil {
		return sqlDB.Close()
	}
	return nil
}

// Migrate creates / upgrades the server tables.
func (db *DB) Migrate() error {
	tables := []interface{}{
		&Job{},
		&JobVersion{},
		&Run{},
		&RunLog{},
		&ConnectionRecord{},
		&SecretRecord{},
		&AppSetting{},
		&AuditEntry{},
		&SchemaSnapshot{},
		&Notification{},
		&NotifyChannel{},
		&AdminUser{},
	}
	for _, t := range tables {
		if err := db.AutoMigrate(t); err != nil {
			return g.Error(err, "could not migrate server table %T", t)
		}
	}
	return nil
}

/* ---------------------------------- Job ---------------------------------- */

// Job is a persisted, schedulable data movement definition.
type Job struct {
	ID          string `json:"id" gorm:"primaryKey;size:40"`
	Name        string `json:"name" gorm:"uniqueIndex;size:190"`
	Description string `json:"description,omitempty"`
	Folder      string `json:"folder,omitempty" gorm:"index;size:190"`
	TagsJSON    string `json:"tags_json,omitempty" gorm:"column:tags"`

	// SpecJSON holds the compiled-from-wizard JobSpec (see spec.go).
	SpecJSON string `json:"spec_json,omitempty" gorm:"column:spec"`

	// Scheduling
	ScheduleCron    string `json:"schedule_cron,omitempty" gorm:"column:schedule_cron;size:64"`
	ScheduleTZ      string `json:"schedule_tz,omitempty" gorm:"column:schedule_tz;size:64"`
	ScheduleEnabled bool   `json:"schedule_enabled" gorm:"column:schedule_enabled"`
	NextRunAt       *time.Time `json:"next_run_at,omitempty" gorm:"index"`

	// Execution policies
	SchemaPolicy     string `json:"schema_policy,omitempty" gorm:"column:schema_policy;size:16;default:auto"` // auto|manual|stop
	ConcurrencyPolicy string `json:"concurrency_policy,omitempty" gorm:"column:concurrency_policy;size:16;default:skip"` // deprecated: single-instance enforced at enqueue; column kept for schema compat
	RetryMax         int    `json:"retry_max,omitempty" gorm:"column:retry_max;default:0"`
	RetryBackoffSec  int    `json:"retry_backoff_sec,omitempty" gorm:"column:retry_backoff_sec;default:60"`
	VerifyRows       bool   `json:"verify_rows,omitempty" gorm:"column:verify_rows"`
	NotifyWebhooks   string `json:"notify_webhooks,omitempty" gorm:"column:notify_webhooks"` // JSON array of URLs

	// Runtime state
	LastRunAt           *time.Time `json:"last_run_at,omitempty" gorm:"column:last_run_at"`
	ConsecutiveFailures int        `json:"consecutive_failures,omitempty" gorm:"column:consecutive_failures;default:0"`
	AutoPaused          bool       `json:"auto_paused,omitempty" gorm:"column:auto_paused"` // set after N consecutive failures
	Version             int        `json:"version,omitempty" gorm:"column:version;default:1"`
	IsTemplate          bool       `json:"is_template,omitempty" gorm:"column:is_template"`

	CreatedAt time.Time `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt time.Time `json:"updated_at" gorm:"autoUpdateTime"`
}

func (j Job) Tags() []string {
	var tags []string
	json.Unmarshal([]byte(j.TagsJSON), &tags)
	return tags
}

func (j *Job) SetTags(tags []string) {
	b, _ := json.Marshal(tags)
	j.TagsJSON = string(b)
}

func (j Job) Spec() (spec JobSpec) {
	json.Unmarshal([]byte(j.SpecJSON), &spec)
	return spec
}

func (j *Job) SetSpec(spec JobSpec) error {
	b, err := json.Marshal(spec)
	if err != nil {
		return g.Error(err, "could not marshal job spec")
	}
	j.SpecJSON = string(b)
	return nil
}

func (j Job) Webhooks() (urls []string) {
	json.Unmarshal([]byte(j.NotifyWebhooks), &urls)
	return urls
}

// JobVersion is an immutable snapshot of a job's spec for the timeline UI.
type JobVersion struct {
	ID        int64  `json:"id" gorm:"primaryKey;autoIncrement"`
	JobID     string `json:"job_id" gorm:"index"`
	Version   int    `json:"version"`
	SpecJSON  string `json:"spec_json" gorm:"column:spec"`
	Comment   string `json:"comment,omitempty"`
	CreatedAt time.Time `json:"created_at" gorm:"autoCreateTime;index"`
}

/* ---------------------------------- Run ---------------------------------- */

// Run is one execution of a job (or an ad-hoc task).
type Run struct {
	ID       string `json:"id" gorm:"primaryKey;size:40"` // exec id
	JobID    string `json:"job_id,omitempty" gorm:"index;size:40"`
	JobName  string `json:"job_name,omitempty" gorm:"index;size:190"`
	Type     string `json:"type" gorm:"size:16;default:task"` // task|replication
	Trigger  string `json:"trigger" gorm:"size:16;default:manual"` // manual|schedule|retry|api
	Attempt  int    `json:"attempt,omitempty" gorm:"default:1"`

	Status    sling.ExecStatus `json:"status" gorm:"index"`
	Error     string           `json:"error,omitempty"`
	Rows      uint64           `json:"rows,omitempty"`
	Bytes     uint64           `json:"bytes,omitempty"`
	DurationMs int64           `json:"duration_ms,omitempty"`

	// StreamSummaryJSON is a compact per-stream status list for replications.
	StreamSummaryJSON string `json:"stream_summary_json,omitempty" gorm:"column:stream_summary"`

	LogTail string `json:"log_tail,omitempty" gorm:"column:log_tail"` // last ~4KB for quick display

	StartTime time.Time  `json:"start_time" gorm:"autoCreateTime;index"`
	EndTime   *time.Time `json:"end_time,omitempty" gorm:"index"`
	Pid       int        `json:"pid,omitempty"`
	Version   string     `json:"version,omitempty"`
}

func (r Run) StreamSummary() (ss []StreamStatus) {
	json.Unmarshal([]byte(r.StreamSummaryJSON), &ss)
	return ss
}

// StreamStatus is the per-stream rollup shown in the runs table.
type StreamStatus struct {
	Stream  string          `json:"stream"`
	Object  string          `json:"object,omitempty"`
	Status  sling.ExecStatus `json:"status"`
	Rows    uint64          `json:"rows,omitempty"`
	Bytes   uint64          `json:"bytes,omitempty"`
	Error   string          `json:"error,omitempty"`
}

// RunLog is one persisted log line of a run. Capped per-run by the runner.
type RunLog struct {
	ID     int64  `json:"id" gorm:"primaryKey;autoIncrement"`
	RunID  string `json:"run_id" gorm:"index:idx_runlog_run_seq,priority:1;size:40"`
	Seq    int64  `json:"seq" gorm:"index:idx_runlog_run_seq,priority:2"`
	Ts     time.Time `json:"ts"`
	Level  int8   `json:"level"`
	Text   string `json:"text"`
}

/* ------------------------------ Connections ------------------------------ */

// ConnectionRecord is a UI-managed connection. PropsJSON holds non-secret
// fields; secret fields live in SecretRecord, AES-GCM encrypted, and are
// referenced here by field name only. Plaintext never touches disk.
type ConnectionRecord struct {
	Name        string `json:"name" gorm:"primaryKey;size:190"`
	Type        string `json:"type" gorm:"index;size:32"`
	Description string `json:"description,omitempty"`
	PropsJSON   string `json:"props_json,omitempty" gorm:"column:props"` // non-secret props
	SecretFields string `json:"secret_fields,omitempty" gorm:"column:secret_fields"` // JSON []string of encrypted field names
	TagsJSON    string `json:"tags_json,omitempty" gorm:"column:tags"`

	LastTestStatus string     `json:"last_test_status,omitempty" gorm:"column:last_test_status;size:16"`
	LastTestAt     *time.Time `json:"last_test_at,omitempty" gorm:"column:last_test_at"`
	LastTestMs     int64      `json:"last_test_ms,omitempty" gorm:"column:last_test_ms"`
	LastTestErr    string     `json:"last_test_err,omitempty" gorm:"column:last_test_err"`

	CreatedAt time.Time `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt time.Time `json:"updated_at" gorm:"autoUpdateTime"`
}

func (c ConnectionRecord) Props() map[string]any {
	m := map[string]any{}
	json.Unmarshal([]byte(c.PropsJSON), &m)
	return m
}

func (c ConnectionRecord) SecretFieldList() (fields []string) {
	json.Unmarshal([]byte(c.SecretFields), &fields)
	return fields
}

func (c ConnectionRecord) Tags() (tags []string) {
	json.Unmarshal([]byte(c.TagsJSON), &tags)
	return tags
}

// SecretRecord is one encrypted secret value, addressed by path
// (e.g. "conn/mypg/password").
type SecretRecord struct {
	Path       string `json:"path" gorm:"primaryKey;size:255"`
	Ciphertext []byte `json:"ciphertext"` // nonce (12B) || AES-256-GCM ciphertext
	UpdatedAt  time.Time `json:"updated_at" gorm:"autoUpdateTime"`
}

/* --------------------------- Settings / audit ---------------------------- */

// AppSetting is a generic KV setting (concurrency, retention, ui prefs...).
type AppSetting struct {
	Key   string `json:"key" gorm:"primaryKey;size:190"`
	Value string `json:"value"`
}

// AuditEntry records config changes for the timeline & traceability.
type AuditEntry struct {
	ID         int64  `json:"id" gorm:"primaryKey;autoIncrement"`
	Action     string `json:"action" gorm:"index;size:32"` // create|update|delete|run|cancel|pause|resume|rollback|import|login...
	EntityType string `json:"entity_type" gorm:"index;size:32"` // job|connection|setting|run
	EntityID   string `json:"entity_id,omitempty" gorm:"index;size:190"`
	Detail     string `json:"detail,omitempty"`
	CreatedAt  time.Time `json:"created_at" gorm:"autoCreateTime;index"`
}

// SchemaSnapshot stores the last-seen source schema of a stream so the
// drift policy can diff against it before runs.
type SchemaSnapshot struct {
	ID        int64  `json:"id" gorm:"primaryKey;autoIncrement"`
	JobID     string `json:"job_id" gorm:"index;size:40"`
	Stream    string `json:"stream" gorm:"size:255"`
	ColumnsJSON string `json:"columns_json,omitempty" gorm:"column:columns"`
	CreatedAt time.Time `json:"created_at" gorm:"autoCreateTime"`
}

func (s SchemaSnapshot) Columns() (cols []ColumnBrief) {
	json.Unmarshal([]byte(s.ColumnsJSON), &cols)
	return cols
}

// ColumnBrief is the comparable projection of a source column.
type ColumnBrief struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// Notification is an in-app notification center item.
type Notification struct {
	ID        int64  `json:"id" gorm:"primaryKey;autoIncrement"`
	Type      string `json:"type" gorm:"index;size:32"`  // run_success|run_failed|run_cancelled|paused|system
	Level     string `json:"level" gorm:"size:16"`       // info|warning|error
	Title     string `json:"title"`
	Body      string `json:"body,omitempty"`
	MetaJSON  string `json:"meta_json,omitempty" gorm:"column:meta"`
	Read      bool   `json:"read" gorm:"index;default:false"`
	CreatedAt time.Time `json:"created_at" gorm:"autoCreateTime;index"`
}

// NotifyChannel is a global outbound notification channel: a WeCom / Feishu /
// DingTalk / Slack bot webhook, or a generic JSON receiver. The URL may embed
// credentials (e.g. the WeCom key) and is masked on every API read; provider
// signing secrets live in the vault under notify/channel/<id>/secret.
type NotifyChannel struct {
	ID         int64     `json:"id" gorm:"primaryKey;autoIncrement"`
	Name       string    `json:"name" gorm:"size:190"`
	Provider   string    `json:"provider" gorm:"size:16"` // wecom|feishu|dingtalk|slack|generic
	URL        string    `json:"url" gorm:"size:500"`
	EventsJSON string    `json:"events_json,omitempty" gorm:"column:events"` // JSON []string of event types; empty = all
	Enabled    bool      `json:"enabled" gorm:"default:true"`
	CreatedAt  time.Time `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt  time.Time `json:"updated_at" gorm:"autoUpdateTime"`
}

func (c NotifyChannel) Events() (events []string) {
	json.Unmarshal([]byte(c.EventsJSON), &events)
	return
}

// AdminUser is the single administrator account of the workbench. Only the
// bcrypt hash is persisted — plaintext passwords never touch disk.
type AdminUser struct {
	Username           string    `json:"username" gorm:"primaryKey;size:190"`
	PasswordHash       string    `json:"-" gorm:"size:255"`
	MustChangePassword bool      `json:"must_change_password" gorm:"column:must_change_password"`
	CreatedAt          time.Time `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt          time.Time `json:"updated_at" gorm:"autoUpdateTime"`
}

/* ------------------------------- Queries --------------------------------- */

// JobFilter narrows a jobs page.
type JobFilter struct {
	Search  string // name/description substring
	Folder  string
	Tag     string
	Paused  *bool
	Limit   int
	Offset  int
}

// ListJobs returns jobs matching the filter plus total count.
func (db *DB) ListJobs(f JobFilter) (jobs []Job, total int64, err error) {
	q := db.Model(&Job{})
	if f.Search != "" {
		s := "%" + strings.ToLower(f.Search) + "%"
		q = q.Where("lower(name) LIKE ? OR lower(description) LIKE ?", s, s)
	}
	if f.Folder != "" {
		q = q.Where("folder = ?", f.Folder)
	}
	if f.Tag != "" {
		q = q.Where("tags LIKE ?", "%\""+f.Tag+"\"%")
	}
	if f.Paused != nil {
		q = q.Where("schedule_enabled = ?", !*f.Paused)
	}
	if err = q.Count(&total).Error; err != nil {
		return nil, 0, g.Error(err, "could not count jobs")
	}
	limit := f.Limit
	if limit <= 0 {
		limit = 200
	}
	if err = q.Order("name ASC").Limit(limit).Offset(f.Offset).Find(&jobs).Error; err != nil {
		return nil, 0, g.Error(err, "could not list jobs")
	}
	return jobs, total, nil
}

// GetJob fetches a job by id or name.
func (db *DB) GetJob(idOrName string) (job *Job, err error) {
	job = &Job{}
	if e := db.Where("id = ? OR name = ?", idOrName, idOrName).First(job).Error; e != nil {
		return nil, g.Error(e, "job not found: %s", idOrName)
	}
	return job, nil
}

// RunFilter narrows a runs page.
type RunFilter struct {
	JobID  string
	Status string
	Trigger string
	From   *time.Time
	To     *time.Time
	Limit  int
	Offset int
}

// ListRuns returns runs matching the filter plus total count.
func (db *DB) ListRuns(f RunFilter) (runs []Run, total int64, err error) {
	q := db.Model(&Run{})
	if f.JobID != "" {
		q = q.Where("job_id = ?", f.JobID)
	}
	if f.Status != "" {
		q = q.Where("status = ?", f.Status)
	}
	if f.Trigger != "" {
		q = q.Where("trigger = ?", f.Trigger)
	}
	if f.From != nil {
		q = q.Where("start_time >= ?", *f.From)
	}
	if f.To != nil {
		q = q.Where("start_time <= ?", *f.To)
	}
	if err = q.Count(&total).Error; err != nil {
		return nil, 0, g.Error(err, "could not count runs")
	}
	limit := f.Limit
	if limit <= 0 {
		limit = 50
	}
	if err = q.Order("start_time DESC").Limit(limit).Offset(f.Offset).Find(&runs).Error; err != nil {
		return nil, 0, g.Error(err, "could not list runs")
	}
	return runs, total, nil
}

// AppendLog persists one log line for a run with its sequence number.
func (db *DB) AppendLog(runID string, seq int64, ts time.Time, level int8, text string) error {
	return db.Create(&RunLog{RunID: runID, Seq: seq, Ts: ts, Level: level, Text: text}).Error
}

// GetLogs returns the logs of a run from offsetSeq (exclusive), ascending.
func (db *DB) GetLogs(runID string, offsetSeq int64, limit int) (logs []RunLog, err error) {
	if limit <= 0 || limit > 10000 {
		limit = 2000
	}
	if err = db.Where("run_id = ? AND seq > ?", runID, offsetSeq).
		Order("seq ASC").Limit(limit).Find(&logs).Error; err != nil {
		return nil, g.Error(err, "could not get run logs")
	}
	return logs, nil
}

// GetSetting reads a setting, returning fallback when unset.
func (db *DB) GetSetting(key, fallback string) string {
	s := AppSetting{Key: key}
	if err := db.First(&s, "key = ?", key).Error; err != nil {
		return fallback
	}
	return s.Value
}

// SetSetting upserts a setting.
func (db *DB) SetSetting(key, value string) error {
	return db.Save(&AppSetting{Key: key, Value: value}).Error
}

// Audit appends an audit entry; errors are non-fatal (logged only).
func (db *DB) Audit(action, entityType, entityID, detail string) {
	if err := db.Create(&AuditEntry{Action: action, EntityType: entityType, EntityID: entityID, Detail: detail}).Error; err != nil {
		g.Debug("could not write audit entry: %s", err)
	}
}

// AuditFilter narrows an audit log page.
type AuditFilter struct {
	Action     string
	EntityType string
	EntityID   string
	Limit      int
	Offset     int
}

// ListAuditEntries returns audit entries matching the filter plus total count,
// newest first.
func (db *DB) ListAuditEntries(f AuditFilter) (entries []AuditEntry, total int64, err error) {
	q := db.Model(&AuditEntry{})
	if f.Action != "" {
		q = q.Where("action = ?", f.Action)
	}
	if f.EntityType != "" {
		q = q.Where("entity_type = ?", f.EntityType)
	}
	if f.EntityID != "" {
		q = q.Where("entity_id = ?", f.EntityID)
	}
	if err = q.Count(&total).Error; err != nil {
		return nil, 0, g.Error(err, "could not count audit entries")
	}
	limit := f.Limit
	if limit <= 0 {
		limit = 200
	}
	if err = q.Order("created_at DESC").Limit(limit).Offset(f.Offset).Find(&entries).Error; err != nil {
		return nil, 0, g.Error(err, "could not list audit entries")
	}
	return entries, total, nil
}

// PruneConfig carries per-table retention windows in days; zero = keep forever.
type PruneConfig struct {
	Runs          int // runs + run_logs + job_versions (versions keep a minimum of 10 per job)
	Notifications int // read notifications only; unread are never pruned
	Audit         int
}

// PruneResult reports per-table deletion counts.
type PruneResult struct {
	Runs          int64 `json:"runs"`
	RunLogs       int64 `json:"run_logs"`
	Notifications int64 `json:"notifications"`
	AuditEntries  int64 `json:"audit_entries"`
	JobVersions   int64 `json:"job_versions"`
}

// minJobVersionsKept is the floor of job_versions rows preserved per job no
// matter how old, so rollback history never disappears entirely.
const minJobVersionsKept = 10

// Prune deletes expired data per the retention windows. Run logs are deleted
// via the run_ids of pruned runs plus an orphan sweep, so no log rows are left
// behind for deleted runs.
func (db *DB) Prune(cfg PruneConfig) (res PruneResult, err error) {
	if cfg.Runs > 0 {
		cutoff := time.Now().AddDate(0, 0, -cfg.Runs)
		var ids []string
		if err = db.Model(&Run{}).
			Where("start_time < ? AND end_time IS NOT NULL", cutoff).
			Pluck("id", &ids).Error; err != nil {
			return res, g.Error(err, "could not select expired runs")
		}
		if len(ids) > 0 {
			r := db.Where("run_id IN ?", ids).Delete(&RunLog{})
			if r.Error != nil {
				return res, r.Error
			}
			res.RunLogs = r.RowsAffected
			r = db.Where("id IN ?", ids).Delete(&Run{})
			if r.Error != nil {
				return res, r.Error
			}
			res.Runs = r.RowsAffected
		}
		// orphan sweep: logs whose run row is gone (interrupted runs etc.)
		r := db.Where("run_id NOT IN (SELECT id FROM runs)").Delete(&RunLog{})
		if r.Error != nil {
			return res, r.Error
		}
		res.RunLogs += r.RowsAffected

		// job versions: older versions expire, but keep the latest N per job
		r = db.Exec(`
			DELETE FROM job_versions
			WHERE created_at < ?
			  AND id NOT IN (
			    SELECT id FROM (
			      SELECT id, ROW_NUMBER() OVER (
			             PARTITION BY job_id ORDER BY created_at DESC, id DESC) AS rn
			      FROM job_versions
			    ) WHERE rn <= ?
			  )
		`, cutoff, minJobVersionsKept)
		if r.Error != nil {
			return res, r.Error
		}
		res.JobVersions = r.RowsAffected
	}
	if cfg.Notifications > 0 {
		cutoff := time.Now().AddDate(0, 0, -cfg.Notifications)
		r := db.Where("read = ? AND created_at < ?", true, cutoff).Delete(&Notification{})
		if r.Error != nil {
			return res, r.Error
		}
		res.Notifications = r.RowsAffected
	}
	if cfg.Audit > 0 {
		cutoff := time.Now().AddDate(0, 0, -cfg.Audit)
		r := db.Where("created_at < ?", cutoff).Delete(&AuditEntry{})
		if r.Error != nil {
			return res, r.Error
		}
		res.AuditEntries = r.RowsAffected
	}
	return res, nil
}

// BackupInto makes a consistent snapshot copy of the database via VACUUM INTO.
func (db *DB) BackupInto(destPath string) error {
	os.MkdirAll(filepath.Dir(destPath), 0755)
	return db.Exec("VACUUM INTO ?", destPath).Error
}
