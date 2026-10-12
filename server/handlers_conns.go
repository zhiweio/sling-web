// handlers_conns.go serves the connection center APIs.
package server

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/slingdata-io/sling-cli/core/dbio/database"
)

func (a *App) handleConnsList(w http.ResponseWriter, r *http.Request) {
	recs, err := a.Conns.List()
	if err != nil {
		apiErr(w, err)
		return
	}
	for i := range recs {
		recs[i].PropsJSON = ""       // raw props stay internal
		recs[i].SecretFields = ""    // not needed by list view
	}
	writeJSON(w, 200, recs)
}

func (a *App) handleConnTypes(w http.ResponseWriter, r *http.Request) {
	cats, err := ConnTypeCatalog()
	if err != nil {
		apiErr(w, err)
		return
	}
	writeJSON(w, 200, cats)
}

func (a *App) handleConnsGet(w http.ResponseWriter, r *http.Request) {
	rec, err := a.Conns.Get(pathVar(r, "name"))
	if err != nil {
		apiErr(w, err)
		return
	}
	// safe view: props without secrets, secrets as field-name list
	props := rec.Props()
	for _, f := range rec.SecretFieldList() {
		if _, ok := props[f]; ok {
			props[f] = secretMask
		}
	}
	body := map[string]any{
		"name":            rec.Name,
		"type":            rec.Type,
		"description":     rec.Description,
		"tags":            rec.Tags(),
		"props":           props,
		"secret_fields":   rec.SecretFieldList(),
		"last_test":       map[string]any{"status": rec.LastTestStatus, "at": rec.LastTestAt, "ms": rec.LastTestMs, "error": rec.LastTestErr},
		"used_by":         mustUsage(a, rec.Name),
		"created_at":      rec.CreatedAt,
		"updated_at":      rec.UpdatedAt,
	}
	writeJSON(w, 200, body)
}

func mustUsage(a *App, name string) []string {
	jobs, err := a.Conns.Usage(name)
	if err != nil {
		return nil
	}
	return jobs
}

func (a *App) handleConnsCreate(w http.ResponseWriter, r *http.Request) {
	var in UpsertInput
	if err := decodeBody(r, &in); err != nil {
		apiErr(w, err)
		return
	}
	rec, err := a.Conns.Create(in)
	if err != nil {
		apiErr(w, err)
		return
	}
	writeJSON(w, 201, rec)
}

func (a *App) handleConnsUpdate(w http.ResponseWriter, r *http.Request) {
	var in UpsertInput
	if err := decodeBody(r, &in); err != nil {
		apiErr(w, err)
		return
	}
	rec, err := a.Conns.Update(pathVar(r, "name"), in)
	if err != nil {
		apiErr(w, err)
		return
	}
	writeJSON(w, 200, rec)
}

func (a *App) handleConnsDelete(w http.ResponseWriter, r *http.Request) {
	if err := a.Conns.Delete(pathVar(r, "name")); err != nil {
		apiErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"deleted": true})
}

func (a *App) handleConnsTest(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name  string         `json:"name"`           // stored connection
		Props map[string]any `json:"props,omitempty"` // or unsaved props
		Type  string         `json:"type,omitempty"`
	}
	if err := decodeBody(r, &in); err != nil {
		apiErr(w, err)
		return
	}

	var res *TestResult
	var err error
	if in.Props != nil || in.Type != "" {
		res, err = a.Conns.TestProps(UpsertInput{Name: in.Name, Type: in.Type, Props: in.Props})
	} else {
		res, err = a.Conns.Test(in.Name)
	}
	if err != nil {
		apiErr(w, err)
		return
	}
	writeJSON(w, 200, res)
}

func (a *App) handleConnsDiscover(w http.ResponseWriter, r *http.Request) {
	pattern := r.URL.Query().Get("pattern")
	levelStr := strings.ToLower(r.URL.Query().Get("level"))
	level := database.SchemataLevelTable
	switch levelStr {
	case "schema":
		level = database.SchemataLevelSchema
	case "column", "columns":
		level = database.SchemataLevelColumn
	}
	res, err := a.Conns.Discover(pathVar(r, "name"), pattern, level)
	if err != nil {
		apiErr(w, err)
		return
	}
	writeJSON(w, 200, res)
}

func (a *App) handleConnsPreview(w http.ResponseWriter, r *http.Request) {
	object := r.URL.Query().Get("table")
	if object == "" {
		object = r.URL.Query().Get("object")
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	cols, rows, err := a.Conns.PreviewRows(pathVar(r, "name"), object, limit)
	if err != nil {
		apiErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"columns": cols, "rows": rows})
}

func (a *App) handleConnsPrimaryKeys(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Tables []string `json:"tables"`
	}
	if err := decodeBody(r, &in); err != nil {
		apiErr(w, err)
		return
	}
	if len(in.Tables) > 500 {
		writeErr(w, 400, "too many tables (max 500)")
		return
	}
	keys, err := a.Conns.PrimaryKeys(pathVar(r, "name"), in.Tables)
	if err != nil {
		apiErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"keys": keys})
}

// handleConnsImport previews (dry_run) or commits an import. Sources:
// uploaded YAML/JSON content, or the machine's sling environment
// (source="env"; also the legacy body-less call). Existing names are
// skipped unless on_conflict="overwrite".
func (a *App) handleConnsImport(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Content    string   `json:"content,omitempty"`
		Source     string   `json:"source,omitempty"` // "env" to scan the machine's sling environment
		DryRun     bool     `json:"dry_run,omitempty"`
		Names      []string `json:"names,omitempty"`
		OnConflict string   `json:"on_conflict,omitempty"` // skip (default) | overwrite
	}
	raw, err := io.ReadAll(r.Body)
	defer r.Body.Close()
	if err != nil {
		apiErr(w, err)
		return
	}
	if len(raw) > 0 {
		if err = json.Unmarshal(raw, &in); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
	}
	if len(in.Content) > 1<<20 {
		writeErr(w, http.StatusBadRequest, "content too large (max 1MB)")
		return
	}
	if in.OnConflict == "" {
		in.OnConflict = "skip"
	}
	if in.OnConflict != "skip" && in.OnConflict != "overwrite" {
		writeErr(w, http.StatusBadRequest, "on_conflict must be skip or overwrite")
		return
	}

	if in.DryRun {
		var found []ImportEntry
		if in.Source == "env" {
			found, err = a.Conns.PreviewImportEnv()
		} else if in.Content == "" {
			writeErr(w, http.StatusBadRequest, "content is required")
			return
		} else {
			found, err = a.Conns.PreviewImportContent(in.Content)
		}
		if err != nil {
			apiErr(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"found": found})
		return
	}

	var imported, skipped []string
	if in.Content != "" {
		imported, skipped, err = a.Conns.ImportContent(in.Content, in.Names, in.OnConflict)
	} else {
		imported, skipped, err = a.Conns.ImportEnv(in.Names, in.OnConflict)
	}
	if err != nil {
		apiErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"imported": imported, "skipped": skipped})
}

// handleConnsExport downloads every managed connection with secrets
// masked; the document can be re-imported here or dropped into env.yaml.
func (a *App) handleConnsExport(w http.ResponseWriter, r *http.Request) {
	format := r.URL.Query().Get("format")
	if format == "" {
		format = "yaml"
	}
	if format != "yaml" && format != "json" {
		writeErr(w, http.StatusBadRequest, "format must be yaml or json")
		return
	}
	name, body, count, err := a.Conns.ExportConns(format)
	if err != nil {
		apiErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"name": name, "body": body, "count": count})
}
