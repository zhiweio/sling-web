// handlers_misc.go serves health/version/stats/settings/audit/backup.
package server

import (
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"time"

	"github.com/flarco/g"
	"github.com/slingdata-io/sling-cli/core"
	"github.com/spf13/cast"
)

func goVersion() string { return runtime.Version() }

func (a *App) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (a *App) handleVersion(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{
		"version":    a.Opt.Version,
		"core":       core.Version,
		"go":         goVersion(),
		"started_at": a.startedAt,
		"data_dir":   homeDir(),
	})
}

func (a *App) handleStats(w http.ResponseWriter, r *http.Request) {
	var jobCount, connCount, activeRuns int64
	a.DB.Model(&Job{}).Where("is_template = ?", false).Count(&jobCount)
	a.DB.Model(&ConnectionRecord{}).Count(&connCount)
	activeRuns = int64(len(a.Runner.Active()))

	// 14-day daily aggregates for the dashboard trend
	type dayStat struct {
		Day     string `json:"day"`
		Runs    int64  `json:"runs"`
		Success int64  `json:"success"`
		Failed  int64  `json:"failed"`
		Rows    int64  `json:"rows"`
	}
	var trend []dayStat
	rows, err := a.DB.Raw(`
		SELECT date(start_time) AS day,
		       COUNT(*) AS runs,
		       SUM(CASE WHEN status IN ('success','warning') THEN 1 ELSE 0 END) AS success,
		       SUM(CASE WHEN status IN ('error','terminated') THEN 1 ELSE 0 END) AS failed,
		       SUM(rows) AS rows
		FROM runs
		WHERE start_time >= datetime('now', '-13 days')
		GROUP BY day ORDER BY day
	`).Rows()
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var ds dayStat
			var day string
			var success, failed, runCount, rowCount interface{}
			var dayAny interface{}
			_ = dayAny
			if err := rows.Scan(&day, &runCount, &success, &failed, &rowCount); err == nil {
				ds.Day = day
				ds.Runs = toInt64(runCount)
				ds.Success = toInt64(success)
				ds.Failed = toInt64(failed)
				ds.Rows = toInt64(rowCount)
				trend = append(trend, ds)
			}
		}
	}

	writeJSON(w, 200, map[string]any{
		"jobs":        jobCount,
		"connections": connCount,
		"active_runs": activeRuns,
		"upcoming":    a.Scheduler.Upcoming(10),
		"trend":       trend,
	})
}

func toInt64(v interface{}) int64 {
	switch t := v.(type) {
	case int64:
		return t
	case int:
		return int64(t)
	case []byte:
		return cast.ToInt64(string(t))
	case string:
		return cast.ToInt64(t)
	case nil:
		return 0
	}
	return cast.ToInt64(v)
}

/* -------------------------------- settings -------------------------------- */

var settingKeys = map[string]bool{
	"ui.language":               true,
	"ui.theme":                  true,
	"ui.locale":                 true,
	"retention_days":            true,
	"retention.notifications_days": true,
	"retention.audit_days":      true,
	"stream_concurrency":        true,
	"notify.on_success":         true,
	"notify.on_failure":         true,
	"backup.auto_enabled":       true,
	"backup.cron":               true,
	"backup.keep_count":         true,
}

// settingDefault returns the value shown (and used) when a setting is unset.
func (a *App) settingDefault(key string) string {
	switch key {
	case "retention_days":
		return strconv.Itoa(a.Opt.RetentionDays)
	case "retention.notifications_days":
		return "30"
	case "retention.audit_days":
		return "365"
	case "notify.on_success":
		return "false"
	case "notify.on_failure":
		return "true"
	case "backup.auto_enabled":
		return "true"
	case "backup.cron":
		return "0 3 * * *"
	case "backup.keep_count":
		return "7"
	}
	return ""
}

func (a *App) handleSettingsGet(w http.ResponseWriter, r *http.Request) {
	out := map[string]string{}
	for key := range settingKeys {
		out[key] = a.DB.GetSetting(key, a.settingDefault(key))
	}
	writeJSON(w, 200, out)
}

func (a *App) handleSettingsPut(w http.ResponseWriter, r *http.Request) {
	var in map[string]string
	if err := decodeBody(r, &in); err != nil {
		apiErr(w, err)
		return
	}
	for key, val := range in {
		if !settingKeys[key] {
			writeErr(w, 400, "unknown setting: "+key)
			return
		}
		if err := a.DB.SetSetting(key, val); err != nil {
			apiErr(w, err)
			return
		}
	}
	// backup schedule changes take effect immediately
	if _, changed := in["backup.auto_enabled"]; changed {
		a.Maintenance.ReloadBackupSchedule()
	} else if _, changed := in["backup.cron"]; changed {
		a.Maintenance.ReloadBackupSchedule()
	}
	a.DB.Audit("update", "setting", "", g.Marshal(in))
	writeJSON(w, 200, in)
}

/* --------------------------------- audit ---------------------------------- */

func (a *App) handleAudit(w http.ResponseWriter, r *http.Request) {
	f := AuditFilter{
		Action:     r.URL.Query().Get("action"),
		EntityType: r.URL.Query().Get("entity_type"),
		EntityID:   r.URL.Query().Get("entity_id"),
	}
	f.Limit, _ = httpAtoi(r.URL.Query().Get("limit"))
	f.Offset, _ = httpAtoi(r.URL.Query().Get("offset"))
	entries, total, err := a.DB.ListAuditEntries(f)
	if err != nil {
		apiErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"entries": entries, "total": total})
}

/* --------------------------------- backup --------------------------------- */

func (a *App) handleBackup(w http.ResponseWriter, r *http.Request) {
	name := g.F("sling-server-backup-%s.db", time.Now().Format("20060102-150405"))
	dest := filepath.Join(homeDir(), "backups", name)
	if err := a.DB.BackupInto(dest); err != nil {
		apiErr(w, err)
		return
	}
	a.DB.Audit("backup", "setting", dest, "")
	f, err := os.Open(dest)
	if err != nil {
		apiErr(w, err)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", g.F(`attachment; filename="%s"`, name))
	http.ServeContent(w, r, name, time.Now(), f)
}
