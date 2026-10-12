// maintenance.go runs periodic housekeeping: retention pruning (runs, logs,
// notifications, audit entries, job versions) and automatic database snapshot
// backups with rotation. Settings are re-read on every tick so runtime changes
// apply without a restart, and the service stops cleanly on shutdown.
package server

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/flarco/g"
	"github.com/robfig/cron/v3"
)

// Maintenance is the periodic housekeeping service.
type Maintenance struct {
	db     *DB
	notify *Notifier

	// defaultRetentionDays is the --retention flag value, used as the
	// retention_days setting fallback.
	defaultRetentionDays int

	mu           sync.Mutex
	started      bool
	backupCron   *cron.Cron
	stopCh       chan struct{}
	done         chan struct{}
	stopOnce     sync.Once
	lastNotified string // day (YYYY-MM-DD) of the last prune notification
}

func NewMaintenance(db *DB, notify *Notifier, defaultRetentionDays int) *Maintenance {
	return &Maintenance{
		db:                   db,
		notify:               notify,
		defaultRetentionDays: defaultRetentionDays,
		stopCh:               make(chan struct{}),
		done:                 make(chan struct{}),
	}
}

// Start launches the prune loop (first pass right after boot, then hourly)
// and the automatic backup schedule.
func (m *Maintenance) Start() {
	m.mu.Lock()
	if m.started {
		m.mu.Unlock()
		return
	}
	m.started = true
	m.mu.Unlock()

	go m.pruneLoop()
	m.ReloadBackupSchedule()
}

// Stop halts the prune loop and the backup cron. Safe to call repeatedly;
// waits up to 3s for the prune loop to exit.
func (m *Maintenance) Stop() {
	m.stopOnce.Do(func() { close(m.stopCh) })
	m.mu.Lock()
	started, backupCron := m.started, m.backupCron
	m.backupCron = nil
	m.mu.Unlock()
	if backupCron != nil {
		backupCron.Stop()
	}
	if started {
		select {
		case <-m.done:
		case <-time.After(3 * time.Second):
		}
	}
}

func (m *Maintenance) pruneLoop() {
	defer close(m.done)
	m.pruneOnce()
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-m.stopCh:
			return
		case <-ticker.C:
			m.pruneOnce()
		}
	}
}

// pruneOnce applies the retention policy current settings spell out.
func (m *Maintenance) pruneOnce() {
	runsDays := m.defaultRetentionDays
	if n, err := strconv.Atoi(m.db.GetSetting("retention_days", "")); err == nil {
		runsDays = n
	}
	res, err := m.db.Prune(PruneConfig{
		Runs:          runsDays,
		Notifications: settingInt(m.db, "retention.notifications_days", 30),
		Audit:         settingInt(m.db, "retention.audit_days", 365),
	})
	if err != nil {
		g.Warn("retention prune failed: %s", err)
		return
	}
	total := res.Runs + res.RunLogs + res.Notifications + res.AuditEntries + res.JobVersions
	if total == 0 {
		return
	}
	summary := fmt.Sprintf("runs=%d logs=%d notifications=%d audit=%d versions=%d",
		res.Runs, res.RunLogs, res.Notifications, res.AuditEntries, res.JobVersions)
	g.Info("retention pruned: %s", summary)
	m.db.Audit("prune", "setting", "", summary)

	// notify at most once a day so steady-state expiry does not spam the center
	if m.notify != nil {
		if today := time.Now().Format("2006-01-02"); today != m.lastNotified {
			m.lastNotified = today
			m.notify.System("info", "自动清理完成 / Retention prune", summary)
		}
	}
}

// ReloadBackupSchedule (re)registers the automatic backup cron from current
// settings. Called on start and whenever backup settings change.
func (m *Maintenance) ReloadBackupSchedule() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.backupCron != nil {
		m.backupCron.Stop()
		m.backupCron = nil
	}
	if m.db.GetSetting("backup.auto_enabled", "true") != "true" {
		return
	}
	spec := m.db.GetSetting("backup.cron", "0 3 * * *")
	c := cron.New(cron.WithChain(cron.Recover(cron.DefaultLogger)))
	if _, err := c.AddFunc(spec, m.autoBackup); err != nil {
		g.Warn("invalid backup cron %q, automatic backup disabled: %s", spec, err)
		return
	}
	m.backupCron = c
	c.Start()
}

func (m *Maintenance) autoBackup() {
	name, err := m.createSnapshot("auto")
	if err != nil {
		g.Warn("automatic backup failed: %s", err)
		m.db.Audit("backup", "backup", "", "auto backup failed: "+err.Error())
		if m.notify != nil {
			m.notify.System("error", "自动备份失败 / Auto backup failed", err.Error())
		}
		return
	}
	g.Info("automatic backup created: %s", name)
	m.db.Audit("backup", "backup", name, "auto")
	m.rotateBackups(settingInt(m.db, "backup.keep_count", 7))
}

// createSnapshot writes a new VACUUM INTO snapshot; kind is "auto" or
// "manual" (controls the filename prefix).
func (m *Maintenance) createSnapshot(kind string) (string, error) {
	prefix := "sling-server-backup"
	if kind == "auto" {
		prefix = "sling-server-auto"
	}
	ts := time.Now().Format("20060102-150405")
	name := g.F("%s-%s.db", prefix, ts)
	dest := filepath.Join(backupsDir(), name)
	for i := 2; ; i++ { // same-second collisions get a -N suffix
		if _, err := os.Stat(dest); os.IsNotExist(err) {
			break
		}
		name = g.F("%s-%s-%d.db", prefix, ts, i)
		dest = filepath.Join(backupsDir(), name)
	}
	if err := m.db.BackupInto(dest); err != nil {
		return "", g.Error(err, "could not create backup snapshot")
	}
	return name, nil
}

// rotateBackups deletes the oldest auto snapshots beyond keep. Manual
// snapshots are never rotated; they are user-managed via the UI.
func (m *Maintenance) rotateBackups(keep int) {
	if keep <= 0 {
		return
	}
	autos, err := listBackupFiles("auto")
	if err != nil {
		g.Debug("could not list backups for rotation: %s", err)
		return
	}
	for i := keep; i < len(autos); i++ { // list is newest first
		if err := os.Remove(filepath.Join(backupsDir(), autos[i].Name)); err == nil {
			g.Info("rotated old backup: %s", autos[i].Name)
		}
	}
}

/* ------------------------------ backup files ------------------------------ */

// BackupFile describes one snapshot on disk.
type BackupFile struct {
	Name      string    `json:"name"`
	Size      int64     `json:"size"`
	CreatedAt time.Time `json:"created_at"` // file mtime
	Kind      string    `json:"kind"`       // auto|manual
}

// backupsDir is the directory holding snapshot files.
func backupsDir() string {
	return filepath.Join(homeDir(), "backups")
}

// listBackupFiles scans the backups directory, newest first. kind filters
// ("auto", "manual", "" = all); files we did not name are ignored.
func listBackupFiles(kind string) ([]BackupFile, error) {
	entries, err := os.ReadDir(backupsDir())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	files := []BackupFile{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".db") {
			continue
		}
		fk := ""
		switch {
		case strings.HasPrefix(e.Name(), "sling-server-auto-"):
			fk = "auto"
		case strings.HasPrefix(e.Name(), "sling-server-backup-"):
			fk = "manual"
		default:
			continue
		}
		if kind != "" && fk != kind {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, BackupFile{Name: e.Name(), Size: info.Size(), CreatedAt: info.ModTime(), Kind: fk})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].CreatedAt.After(files[j].CreatedAt) })
	return files, nil
}

// settingInt reads an integer setting, returning def when unset or invalid.
func settingInt(db *DB, key string, def int) int {
	if n, err := strconv.Atoi(db.GetSetting(key, "")); err == nil {
		return n
	}
	return def
}
