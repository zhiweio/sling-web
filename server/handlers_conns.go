// handlers_conns.go serves the connection center APIs.
package server

import (
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
			props[f] = "******"
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

func (a *App) handleConnsImport(w http.ResponseWriter, r *http.Request) {
	dryRun := r.URL.Query().Get("dry") == "1"
	imported, err := a.Conns.ImportEnvYAML(dryRun)
	if err != nil {
		apiErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"imported": imported})
}
