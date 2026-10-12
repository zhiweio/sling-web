// conns.go manages UI connections: encrypted persistence, testing,
// schema discovery and data preview. Connections materialize into the
// process-local ENV_YAML variable so sling core (GetLocalConns) can
// resolve them at run time; plaintext never touches disk.
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/flarco/g"
	"github.com/slingdata-io/sling-cli/core/dbio"
	"github.com/slingdata-io/sling-cli/core/dbio/connection"
	"github.com/slingdata-io/sling-cli/core/dbio/database"
	"github.com/slingdata-io/sling-cli/core/env"
	"github.com/spf13/cast"
	"gopkg.in/yaml.v3"
)

/* --------------------------- properties catalog --------------------------- */

// PropDef is one form field of a connection type.
type PropDef struct {
	Name        string   `json:"name"`
	Type        string   `json:"type"` // text|integer|boolean|dropdown|password
	Title       string   `json:"title,omitempty"`
	Description string   `json:"description,omitempty"`
	Examples    []string `json:"examples,omitempty"`
	Default     any      `json:"default,omitempty"`
	Options     []string `json:"options,omitempty"`
	Secret      bool     `json:"secret,omitempty"`
	Required    bool     `json:"required,omitempty"`
}

// ConnTypeDef describes a connection type for the form builder.
type ConnTypeDef struct {
	Type        string    `json:"type"`
	Title       string    `json:"title"`
	Kind        string    `json:"kind"` // database|file|api
	Required    []string  `json:"required,omitempty"`
	URLTemplate string    `json:"url_template,omitempty"`
	Docs        string    `json:"docs,omitempty"`
	Properties  []PropDef `json:"properties"`
}

var (
	catalogOnce sync.Once
	catalog     []ConnTypeDef
	catalogErr  error
)

// ConnTypeCatalog parses core/dbio/templates/_properties.yaml — the same
// file the CLI uses — into a form catalog for the frontend.
func ConnTypeCatalog() ([]ConnTypeDef, error) {
	catalogOnce.Do(func() {
		body, err := dbio.ReadTemplateFile("_properties.yaml")
		if err != nil {
			catalogErr = g.Error(err, "could not read _properties.yaml")
			return
		}
		raw := map[string]map[string]any{}
		if err = yaml.Unmarshal(body, &raw); err != nil {
			catalogErr = g.Error(err, "could not parse _properties.yaml")
			return
		}
		for typeName, def := range raw {
			if typeName == "base" {
				continue
			}
			ct := ConnTypeDef{
				Type:        typeName,
				Title:       cast.ToString(def["title"]),
				Kind:        cast.ToString(def["kind"]),
				URLTemplate: cast.ToString(def["url_template"]),
				Docs:        cast.ToString(def["docs"]),
			}
			for _, r := range cast.ToStringSlice(def["required"]) {
				if r != "name" {
					ct.Required = append(ct.Required, r)
				}
			}
			props, ok := def["properties"].(map[string]any)
			if !ok {
				continue
			}
			names := make([]string, 0, len(props))
			for p := range props {
				names = append(names, p)
			}
			// keep yaml order: name first, then the rest as encountered
			var ordered []string
			if _, has := props["name"]; has {
				ordered = append(ordered, "name")
			}
			for _, p := range names {
				if p != "name" {
					ordered = append(ordered, p)
				}
			}
			for _, p := range ordered {
				pm, _ := props[p].(map[string]any)
				pd := PropDef{
					Name:        p,
					Type:        cast.ToString(pm["type"]),
					Title:       cast.ToString(pm["title"]),
					Description: cast.ToString(pm["description"]),
					Examples:    cast.ToStringSlice(pm["examples"]),
					Default:     pm["default"],
				}
				if s := cast.ToString(pm["secret"]); s == "true" || cast.ToBool(pm["secret"]) {
					pd.Secret = true
					pd.Type = "password"
				}
				if opts, ok := pm["options"].([]any); ok {
					for _, o := range opts {
						pd.Options = append(pd.Options, cast.ToString(o))
					}
				}
				if p == "name" {
					pd.Title = "Connection Name"
					if pd.Description == "" {
						pd.Description = "No spaces; will be upper-cased."
					}
				}
				for _, r := range ct.Required {
					if r == p {
						pd.Required = true
					}
				}
				ct.Properties = append(ct.Properties, pd)
			}
			catalog = append(catalog, ct)
		}
		sort.Slice(catalog, func(i, j int) bool {
			ki, kj := catalog[i].Kind, catalog[j].Kind
			if ki != kj {
				return kindOrder(ki) < kindOrder(kj)
			}
			return catalog[i].Title < catalog[j].Title
		})
	})
	return catalog, catalogErr
}

func kindOrder(k string) int {
	switch k {
	case "database":
		return 0
	case "file":
		return 1
	case "api":
		return 2
	}
	return 3
}

/* ------------------------------ conn service ----------------------------- */

// ConnService manages UI connections.
type ConnService struct {
	db     *DB
	vault  *Vault
	broker *Broker

	mu      sync.Mutex // serializes materialization
	writeMu sync.Mutex // serializes Create/Update lookups + writes
}

func NewConnService(db *DB, vault *Vault, broker *Broker) *ConnService {
	return &ConnService{db: db, vault: vault, broker: broker}
}

// isSecretField classifies a property name using the catalog (secret flags)
// with env.SecretKeys as fallback.
func isSecretField(connType, field string) bool {
	cats, err := ConnTypeCatalog()
	if err == nil {
		for _, c := range cats {
			if c.Type == connType {
				for _, p := range c.Properties {
					if p.Name == field {
						return p.Secret
					}
				}
				return false
			}
		}
	}
	for _, k := range env.SecretKeys {
		if strings.EqualFold(k, field) {
			return true
		}
	}
	return false
}

// UpsertInput is the payload for creating/updating a connection.
type UpsertInput struct {
	Name        string         `json:"name"`
	Type        string         `json:"type"`
	Description string         `json:"description,omitempty"`
	Tags        []string       `json:"tags,omitempty"`
	Props       map[string]any `json:"props"` // may include secret fields (write-only)
}

// Upsert creates or updates a connection. Secret props are encrypted;
// empty-string secrets keep the stored value.
// normalizeConnType maps properties-catalog type keys to the dbio type sling
// core understands — the catalog calls local filesystem connections "local"
// while dbio (and Materialize's file:// URL special case) calls them "file".
func normalizeConnType(t string) string {
	if strings.ToLower(strings.TrimSpace(t)) == "local" {
		return "file"
	}
	return strings.TrimSpace(t)
}

// Create adds a new connection. A duplicate name is rejected instead of
// silently overwriting the existing connection's props and credentials.
func (cs *ConnService) Create(in UpsertInput) (*ConnectionRecord, error) {
	cs.writeMu.Lock()
	defer cs.writeMu.Unlock()

	name := strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(in.Name), " ", "_"))
	if name == "" {
		return nil, g.Error("connection name is required")
	}
	if _, err := cs.Get(name); err == nil {
		return nil, g.Error("connection %s already exists", name)
	}
	return cs.Upsert(in)
}

// Update modifies an existing connection. The name is immutable (jobs
// reference connections by name) and so is the type.
func (cs *ConnService) Update(name string, in UpsertInput) (*ConnectionRecord, error) {
	cs.writeMu.Lock()
	defer cs.writeMu.Unlock()

	name = strings.ToUpper(strings.TrimSpace(name))
	if in.Name != "" {
		if inName := strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(in.Name), " ", "_")); inName != name {
			return nil, g.Error("invalid rename: connection %s cannot be renamed (jobs reference it by name)", name)
		}
	}
	rec, err := cs.Get(name)
	if err != nil {
		return nil, g.Error(err, "connection not found: %s", name)
	}
	if t := normalizeConnType(in.Type); t != "" && t != rec.Type {
		return nil, g.Error("invalid type change for connection %s: %s to %s", name, rec.Type, t)
	}
	if in.Type == "" {
		in.Type = rec.Type
	}
	return cs.Upsert(in)
}

func (cs *ConnService) Upsert(in UpsertInput) (*ConnectionRecord, error) {
	name := strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(in.Name), " ", "_"))
	if name == "" {
		return nil, g.Error("connection name is required")
	}

	connType := normalizeConnType(in.Type)
	def := map[string]any{}
	for _, c := range mustCatalog() {
		if c.Type == connType {
			for _, p := range c.Properties {
				if p.Default != nil && !p.Secret {
					def[p.Name] = p.Default
				}
			}
			break
		}
	}

	rec := &ConnectionRecord{}
	err := cs.db.Where("name = ?", name).First(rec).Error
	existing := err == nil

	// start from existing non-secret props (update path)
	nonSecret := map[string]any{}
	if existing {
		if rec.Type != "" && connType != "" && rec.Type != connType {
			return nil, g.Error("connection %s already exists with type %s", name, rec.Type)
		}
		for k, v := range rec.Props() {
			nonSecret[k] = v
		}
	}

	secretFields := []string{}
	if existing {
		secretFields = rec.SecretFieldList()
	}

	for k, v := range in.Props {
		if k == "name" || k == "type" {
			continue
		}
		sv := ""
		switch t := v.(type) {
		case string:
			sv = t
		default:
			if v != nil {
				sv = fmt.Sprint(v)
			}
		}
		if isSecretField(connType, k) {
			if sv == "" {
				continue // keep stored secret
			}
			if err = cs.vault.SaveSecret(secretPath(name, k), sv); err != nil {
				return nil, g.Error(err, "could not save secret %s", k)
			}
			if !contains(secretFields, k) {
				secretFields = append(secretFields, k)
			}
			continue
		}
		if sv == "" && def[k] != nil {
			delete(nonSecret, k)
			continue
		}
		if sv == "" {
			delete(nonSecret, k)
			continue
		}
		nonSecret[k] = sv
	}

	propsB, _ := json.Marshal(nonSecret)
	secB, _ := json.Marshal(secretFields)
	tagsB, _ := json.Marshal(in.Tags)

	rec.Name = name
	rec.Type = connType
	rec.Description = in.Description
	rec.PropsJSON = string(propsB)
	rec.SecretFields = string(secB)
	rec.TagsJSON = string(tagsB)

	var saveErr error
	if existing {
		saveErr = cs.db.Save(rec).Error
	} else {
		saveErr = cs.db.Create(rec).Error
	}
	if saveErr != nil {
		return nil, g.Error(saveErr, "could not save connection %s", name)
	}

	cs.db.Audit(map[bool]string{true: "update", false: "create"}[existing], "connection", name, "")
	cs.Materialize()
	if cs.broker != nil {
		cs.broker.Publish(Event{Type: "connection", Payload: rec})
	}
	return rec, nil
}

// Get returns a connection record.
func (cs *ConnService) Get(name string) (*ConnectionRecord, error) {
	rec := &ConnectionRecord{}
	err := cs.db.Where("name = ?", strings.ToUpper(name)).First(rec).Error
	if err != nil {
		return nil, g.Error(err, "connection not found: %s", name)
	}
	return rec, nil
}

// List returns all connections (secrets never included).
func (cs *ConnService) List() (recs []ConnectionRecord, err error) {
	if err = cs.db.Order("name ASC").Find(&recs).Error; err != nil {
		return nil, g.Error(err, "could not list connections")
	}
	return recs, nil
}

// Delete removes a connection and its secrets. Fails when jobs still use it.
func (cs *ConnService) Delete(name string) error {
	name = strings.ToUpper(name)
	usedBy, err := cs.Usage(name)
	if err != nil {
		return err
	}
	if len(usedBy) > 0 {
		return g.Error("connection %s is used by jobs: %s. Delete or re-point them first.", name, strings.Join(usedBy, ", "))
	}
	rec, err := cs.Get(name)
	if err != nil {
		return err
	}
	if err = cs.db.Delete(rec).Error; err != nil {
		return g.Error(err, "could not delete connection %s", name)
	}
	if err = cs.vault.DeleteSecrets(secretPath(name, "")); err != nil {
		return err
	}
	cs.db.Audit("delete", "connection", name, "")
	cs.Materialize()
	if cs.broker != nil {
		cs.broker.Publish(Event{Type: "connection", Payload: map[string]any{"deleted": name}})
	}
	return nil
}

// Usage lists job names referencing a connection.
func (cs *ConnService) Usage(connName string) (jobs []string, err error) {
	jobs = []string{} // non-nil so JSON renders [] not null
	var all []Job
	if err = cs.db.Find(&all).Error; err != nil {
		return nil, g.Error(err, "could not scan jobs")
	}
	for _, j := range all {
		spec := j.Spec()
		if strings.EqualFold(spec.SourceConn, connName) || strings.EqualFold(spec.TargetConn, connName) {
			jobs = append(jobs, j.Name)
		}
	}
	return jobs, nil
}

// decryptedProps returns props with secrets decrypted (memory only).
func (cs *ConnService) decryptedProps(rec *ConnectionRecord) (map[string]any, error) {
	props := rec.Props()
	for _, f := range rec.SecretFieldList() {
		val, err := cs.vault.LoadSecret(secretPath(rec.Name, f))
		if err != nil {
			return nil, err
		}
		if val != "" {
			props[f] = val
		}
	}
	return props, nil
}

// Materialize renders all connections into ENV_YAML (process memory) so
// sling core can resolve them; then clears the GetLocalConns cache.
func (cs *ConnService) Materialize() error {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	recs, err := cs.List()
	if err != nil {
		return err
	}
	conns := map[string]any{}
	for i := range recs {
		rec := &recs[i]
		props, err := cs.decryptedProps(rec)
		if err != nil {
			g.Debug("could not decrypt secrets for %s: %s", rec.Name, err)
			continue
		}
		entry := map[string]any{"type": rec.Type}
		// Local file connections are rootless in sling: give the configured
		// path to the client as its URL so browsing & streams resolve under it.
		if rec.Type == "file" {
			if path := cast.ToString(props["path"]); path != "" {
				entry["url"] = "file://" + strings.TrimSuffix(path, "/") + "/"
			}
		}
		for k, v := range props {
			entry[k] = v
		}
		conns[rec.Name] = entry
	}
	body, err := yaml.Marshal(map[string]any{"connections": conns})
	if err != nil {
		return g.Error(err, "could not render ENV_YAML")
	}
	os.Setenv("ENV_YAML", string(body))
	connection.GetLocalConns(true) // refresh the 10s discovery cache now
	return nil
}

// TestResult is the outcome of a connection test.
type TestResult struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
	Ms      int64  `json:"ms"`
}

// Test tests a stored connection by name and persists the outcome.
func (cs *ConnService) Test(name string) (*TestResult, error) {
	rec, err := cs.Get(name)
	if err != nil {
		return nil, err
	}
	props, err := cs.decryptedProps(rec)
	if err != nil {
		return nil, err
	}
	return cs.testRecord(rec, props, true)
}

// TestProps tests unsaved form values (wizard live feedback). It must not
// persist: rec is a name/type stub, and saving it would clobber the stored
// record's props and secret references with empty values.
func (cs *ConnService) TestProps(in UpsertInput) (*TestResult, error) {
	name := strings.ToUpper(strings.TrimSpace(in.Name))
	props := map[string]any{}
	for k, v := range in.Props {
		if k == "name" || k == "type" {
			continue
		}
		if sv, ok := v.(string); ok && sv == "" && isSecretField(in.Type, k) {
			// fall back to stored secret for edit flows
			if stored, err := cs.vault.LoadSecret(secretPath(name, k)); err == nil && stored != "" {
				props[k] = stored
			}
			continue
		}
		props[k] = v
	}
	rec := &ConnectionRecord{Name: name, Type: normalizeConnType(in.Type)}
	return cs.testRecord(rec, props, false)
}

func (cs *ConnService) testRecord(rec *ConnectionRecord, props map[string]any, persist bool) (*TestResult, error) {
	dbType, ok := dbio.ValidateType(strings.ToLower(rec.Type))
	if !ok {
		return nil, g.Error("unknown connection type: %s", rec.Type)
	}
	conn, err := connection.NewConnection(rec.Name, dbType, props)
	if err != nil {
		return nil, g.Error(err, "could not build connection %s", rec.Name)
	}

	start := time.Now()
	testOk, err := conn.Test()
	ms := time.Since(start).Milliseconds()

	res := &TestResult{Success: testOk, Ms: ms}
	if err != nil {
		res.Error = friendlyConnErr(err.Error())
	}

	rec.LastTestStatus = map[bool]string{true: "success", false: "error"}[testOk]
	rec.LastTestAt = timePtr(time.Now())
	rec.LastTestMs = ms
	rec.LastTestErr = res.Error
	if persist {
		cs.db.Save(rec)
	}
	return res, nil
}

func timePtr(t time.Time) *time.Time { return &t }

// friendlyConnErr strips driver noise from connection errors.
func friendlyConnErr(msg string) string {
	for _, marker := range []string{"\n"} {
		msg = strings.ReplaceAll(msg, marker, " ")
	}
	if len(msg) > 500 {
		msg = msg[:500] + "…"
	}
	return msg
}

/* ------------------------------- discovery -------------------------------- */

// DiscoverResult is the unified browse payload for the schema tree.
type DiscoverResult struct {
	Kind      string          `json:"kind"` // database|file|api
	Schemas   []SchemaBrief   `json:"schemas,omitempty"`
	Nodes     []FileNodeBrief `json:"nodes,omitempty"`
	Endpoints []string        `json:"endpoints,omitempty"`
}

// SchemaBrief is a database schema branch.
type SchemaBrief struct {
	Name   string        `json:"name"`
	Tables []TableBrief  `json:"tables"`
}

// TableBrief is a table or view leaf.
type TableBrief struct {
	Name   string        `json:"name"`
	IsView bool          `json:"is_view,omitempty"`
	Columns []ColumnBrief `json:"columns,omitempty"`
}

// FileNodeBrief is a file-system leaf.
type FileNodeBrief struct {
	Path      string `json:"path"`
	Name      string `json:"name"`
	IsDir     bool   `json:"is_dir,omitempty"`
	Size      int64  `json:"size,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

// Discover browses a stored connection (schemas/tables/files).
func (cs *ConnService) Discover(name, pattern string, level database.SchemataLevel) (*DiscoverResult, error) {
	if err := cs.Materialize(); err != nil {
		return nil, err
	}
	entries := connection.GetLocalConns(true)
	if entry := entries.Get(strings.ToUpper(name)); entry.Name == "" {
		return nil, g.Error("connection not found: %s", name)
	}

	opts := &connection.DiscoverOptions{Pattern: pattern, Level: level}
	nodes, schemata, endpoints, err := entries.Discover(strings.ToUpper(name), opts)
	if err != nil {
		return nil, g.Error(err, "could not discover %s", name)
	}

	res := &DiscoverResult{}
	if schemata.Databases != nil && len(schemata.Databases) > 0 {
		res.Kind = "database"
		dbNames := make([]string, 0, len(schemata.Databases))
		for dbName := range schemata.Databases {
			dbNames = append(dbNames, dbName)
		}
		sort.Strings(dbNames)
		for _, dbName := range dbNames {
			db := schemata.Databases[dbName]
			for _, schemaName := range sortedKeys(db.Schemas) {
				schema := db.Schemas[schemaName]
				brief := SchemaBrief{Name: schema.Name}
				for _, tableName := range sortedKeys(schema.Tables) {
					tbl := schema.Tables[tableName]
					tb := TableBrief{Name: tbl.Name, IsView: tbl.IsView}
					for _, col := range tbl.Columns {
						tb.Columns = append(tb.Columns, ColumnBrief{Name: col.Name, Type: string(col.Type)})
					}
					brief.Tables = append(brief.Tables, tb)
				}
				res.Schemas = append(res.Schemas, brief)
			}
		}
	}
	if nodes != nil && len(nodes) > 0 {
		res.Kind = "file"
		for i := range nodes {
			node := &nodes[i]
			updated := ""
			if node.Updated > 0 {
				updated = time.Unix(node.Updated, 0).Format("2006-01-02 15:04")
			}
			res.Nodes = append(res.Nodes, FileNodeBrief{
				Path: node.URI, Name: node.Name(), IsDir: node.IsDir,
				Size: int64(node.Size), UpdatedAt: updated,
			})
		}
	}
	if endpoints != nil && len(endpoints) > 0 {
		res.Kind = "api"
		for _, ep := range endpoints {
			res.Endpoints = append(res.Endpoints, ep.Name)
		}
	}
	if res.Kind == "" {
		res.Kind = "database"
	}
	return res, nil
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// PrimaryKeys returns the primary key columns for each requested table of a
// database connection. Batch upserts need per-table keys (they usually differ
// across tables), so the wizard fetches them on demand for the selected
// tables only. Tables without a primary key (or unreadable ones) map to an
// empty/absent entry rather than failing the whole request.
func (cs *ConnService) PrimaryKeys(name string, tables []string) (map[string][]string, error) {
	if len(tables) == 0 {
		return map[string][]string{}, nil
	}
	if err := cs.Materialize(); err != nil {
		return nil, err
	}
	entries := connection.GetLocalConns(true)
	entry := entries.Get(strings.ToUpper(name))
	if entry.Name == "" {
		return nil, g.Error("connection not found: %s", name)
	}
	if entry.Connection.Type.Kind() != dbio.KindDatabase {
		return nil, g.Error("primary keys are only supported for database connections")
	}

	dbConn, err := entry.Connection.AsDatabase(connection.AsConnOptions{UseCache: true})
	if err != nil {
		return nil, g.Error(err, "could not open database connection")
	}
	defer dbConn.Close()
	if err = dbConn.Connect(10); err != nil {
		return nil, g.Error(err, "could not connect")
	}

	keys := make(map[string][]string, len(tables))
	for _, table := range tables {
		data, err := dbConn.GetPrimaryKeys(table)
		if err != nil {
			continue // per-table failure is not fatal
		}
		type pkCol struct {
			name string
			pos  int
		}
		cols := []pkCol{}
		for _, rec := range data.Records() {
			colName := cast.ToString(rec["column_name"])
			if colName == "" {
				continue
			}
			cols = append(cols, pkCol{name: colName, pos: cast.ToInt(rec["position"])})
		}
		sort.Slice(cols, func(i, j int) bool { return cols[i].pos < cols[j].pos })
		names := make([]string, 0, len(cols))
		for _, c := range cols {
			names = append(names, c.name)
		}
		if len(names) > 0 {
			keys[table] = names
		}
	}
	return keys, nil
}

// PreviewRows returns a sample of rows from a table/file for the data
// preview panel.
func (cs *ConnService) PreviewRows(name, object string, limit int) (cols []map[string]any, rows []map[string]any, err error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	if err = cs.Materialize(); err != nil {
		return nil, nil, err
	}
	entries := connection.GetLocalConns(true)
	entry := entries.Get(strings.ToUpper(name))
	if entry.Name == "" {
		return nil, nil, g.Error("connection not found: %s", name)
	}
	if entry.Connection.Type.Kind() != dbio.KindDatabase {
		return nil, nil, g.Error("preview is only supported for database connections")
	}

	dbConn, err := entry.Connection.AsDatabase(connection.AsConnOptions{})
	if err != nil {
		return nil, nil, g.Error(err, "could not open database connection")
	}
	defer dbConn.Close()

	table, err := database.ParseTableName(object, dbConn.GetType())
	if err != nil {
		return nil, nil, g.Error(err, "could not parse table name: "+object)
	}
	sqlText := g.F("select * from %s", table.FDQN())
	data, err := dbConn.QueryContext(context.Background(), sqlText, map[string]any{"limit": limit})
	if err != nil {
		return nil, nil, g.Error(err, "could not query %s", object)
	}
	for _, col := range data.Columns {
		cols = append(cols, map[string]any{"name": col.Name, "type": string(col.Type)})
	}
	rows = data.Records()
	if len(rows) > limit {
		rows = rows[:limit]
	}
	return cols, rows, nil
}

/* -------------------------------- import --------------------------------- */

// ImportEnvYAML imports connections found in env.yaml / environment
// variables into the encrypted store so the vault becomes the source of
// truth. Existing managed connections are left untouched.
func (cs *ConnService) ImportEnvYAML(dryRun bool) (imported []string, err error) {
	imported = []string{} // non-nil so JSON renders [] not null
	// discover without our materialized conns shadowing the file
	os.Unsetenv("ENV_YAML")
	entries := connection.GetLocalConns(true)
	defer cs.Materialize()

	for _, entry := range entries {
		if entry.Name == "LOCAL" || strings.Contains(entry.Source, "env-var env yaml") {
			continue // built-in or our own materialized conns
		}
		if _, err := cs.Get(entry.Name); err == nil {
			continue // already managed
		}

		props := map[string]any{}
		for k, v := range entry.Connection.Data {
			if k == "type" || k == "url" {
				continue
			}
			if isSecretField(entry.Connection.Type.String(), k) {
				if sv := cast.ToString(v); sv != "" {
					if e := cs.vault.SaveSecret(secretPath(entry.Name, k), sv); e != nil {
						return imported, e
					}
				}
				continue
			}
			props[k] = v
		}

		propsB, _ := json.Marshal(props)
		rec := &ConnectionRecord{
			Name:      entry.Name,
			Type:      entry.Connection.Type.String(),
			PropsJSON: string(propsB),
		}
		if !dryRun {
			if e := cs.db.Create(rec).Error; e != nil {
				return imported, e
			}
		}
		imported = append(imported, entry.Name)
	}
	if !dryRun {
		cs.Materialize()
		cs.db.Audit("import", "connection", "", strings.Join(imported, ","))
	}
	return imported, nil
}

/* -------------------------------- helpers -------------------------------- */

func mustCatalog() []ConnTypeDef {
	c, _ := ConnTypeCatalog()
	return c
}

func contains(arr []string, s string) bool {
	for _, a := range arr {
		if a == s {
			return true
		}
	}
	return false
}
