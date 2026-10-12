// handlers_backups.go serves the snapshot backup management APIs: list,
// create now, download and delete. Snapshots are plain files produced by
// VACUUM INTO (see DB.BackupInto); listing scans the backups directory so
// there is no bookkeeping table to drift out of sync with disk.
package server

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/flarco/g"
)

func (a *App) handleBackupsList(w http.ResponseWriter, r *http.Request) {
	files, err := listBackupFiles("")
	if err != nil {
		apiErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"backups": files, "dir": backupsDir()})
}

func (a *App) handleBackupsCreate(w http.ResponseWriter, r *http.Request) {
	name, err := a.Maintenance.createSnapshot("manual")
	if err != nil {
		apiErr(w, err)
		return
	}
	a.DB.Audit("backup", "backup", name, "manual")
	out := BackupFile{Name: name, Kind: "manual"}
	if st, err := os.Stat(filepath.Join(backupsDir(), name)); err == nil {
		out.Size, out.CreatedAt = st.Size(), st.ModTime()
	}
	writeJSON(w, 201, out)
}

func (a *App) handleBackupDownload(w http.ResponseWriter, r *http.Request) {
	name := pathVar(r, "name")
	if !validBackupName(name) {
		writeErr(w, http.StatusBadRequest, "invalid backup name")
		return
	}
	f, err := os.Open(filepath.Join(backupsDir(), name))
	if err != nil {
		apiErr(w, g.Error("backup not found"))
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		apiErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", g.F(`attachment; filename="%s"`, name))
	http.ServeContent(w, r, name, st.ModTime(), f)
}

func (a *App) handleBackupDelete(w http.ResponseWriter, r *http.Request) {
	name := pathVar(r, "name")
	if !validBackupName(name) {
		writeErr(w, http.StatusBadRequest, "invalid backup name")
		return
	}
	if err := os.Remove(filepath.Join(backupsDir(), name)); err != nil {
		if os.IsNotExist(err) {
			apiErr(w, g.Error("backup not found"))
			return
		}
		apiErr(w, err)
		return
	}
	a.DB.Audit("delete", "backup", name, "")
	writeJSON(w, 200, map[string]any{"deleted": true})
}

// validBackupName guards against path traversal: a bare filename with a
// sling-server backup prefix and .db suffix.
func validBackupName(name string) bool {
	if name == "" || strings.ContainsAny(name, `/\`) || strings.Contains(name, "..") {
		return false
	}
	return strings.HasPrefix(name, "sling-server-") && strings.HasSuffix(name, ".db")
}
