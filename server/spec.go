// spec.go defines JobSpec, the UI-facing, plain-language job definition the
// wizard edits, and compiles it into sling-native task configs, replication
// configs and CLI-compatible YAML.
//
// UI modes map to sling write modes:
//   replace  -> full-refresh (drop + create + insert)
//   truncate -> truncate     (truncate + insert)
//   append   -> incremental + update_key   (only new rows)
//   upsert   -> incremental + primary_key  (MERGE)
//   snapshot -> snapshot     (append + _sling_loaded_at)
package server

import (
	"strings"

	"github.com/flarco/g"
	"github.com/slingdata-io/sling-cli/core/dbio"
	"github.com/slingdata-io/sling-cli/core/dbio/iop"
	"github.com/slingdata-io/sling-cli/core/sling"
	"gopkg.in/yaml.v3"
)

// JobSpec is the persisted job definition edited by the wizard.
type JobSpec struct {
	SourceConn string          `json:"source_conn"`
	TargetConn string          `json:"target_conn"`
	Defaults   StreamSpec      `json:"defaults"`
	Streams    []StreamSpec    `json:"streams"`
	Metadata   MetadataOptions `json:"metadata,omitempty"`
}

// StreamSpec is one stream (table mapping) inside a job.
// Empty fields inherit from JobSpec.Defaults.
type StreamSpec struct {
	Name         string         `json:"name,omitempty"` // display name (defaults to source object)
	SourceObject string         `json:"source_object,omitempty"`
	TargetObject string         `json:"target_object,omitempty"`
	Mode         string         `json:"mode,omitempty"` // ""|replace|truncate|append|upsert|snapshot
	PrimaryKey   []string       `json:"primary_key,omitempty"`
	UpdateKey    string         `json:"update_key,omitempty"`
	Select       []string       `json:"select,omitempty"`
	Disabled     bool           `json:"disabled,omitempty"`
	Options      *StreamOptions `json:"options,omitempty"`
}

// StreamOptions exposes the useful subset of sling source/target options.
type StreamOptions struct {
	// target
	AddNewColumns    *bool  `json:"add_new_columns,omitempty"`
	AdjustColumnType *bool  `json:"adjust_column_type,omitempty"`
	ColumnCasing     string `json:"column_casing,omitempty"` // source|target|snake|camel|upper|lower|normalize
	FileFormat       string `json:"file_format,omitempty"`   // csv|parquet|json|avro (file targets)
	PreSQL           string `json:"pre_sql,omitempty"`
	PostSQL          string `json:"post_sql,omitempty"`
	// source
	Where     string `json:"where,omitempty"`
	Limit     int    `json:"limit,omitempty"`
	ChunkSize int    `json:"chunk_size,omitempty"`
}

// MetadataOptions toggles sling metadata columns.
type MetadataOptions struct {
	LoadedAt  bool `json:"loaded_at,omitempty"`
	SyncedAt  bool `json:"synced_at,omitempty"`
	RowNum    bool `json:"row_num,omitempty"`
	RowID     bool `json:"row_id,omitempty"`
	ExecID    bool `json:"exec_id,omitempty"`
	StreamURL bool `json:"stream_url,omitempty"`
}

// UIModes is the ordered list of user-facing modes.
var UIModes = []string{"replace", "truncate", "append", "upsert", "snapshot"}

func ptrBool(b bool) *bool { return &b }
func ptrInt(i int) *int    { return &i }
func strPtr(s string) *string { return &s }

func columnCasingPtr(s string) *iop.ColumnCasing {
	c := iop.ColumnCasing(strings.ToLower(s))
	return &c
}

// EffectiveMode returns the stream mode falling back to defaults.
func (s JobSpec) EffectiveMode(st StreamSpec) string {
	if st.Mode != "" {
		return st.Mode
	}
	return s.Defaults.Mode
}

// EffectiveKeys returns the effective primary key / update key of a stream.
func (s JobSpec) EffectiveKeys(st StreamSpec) (pk []string, uk string) {
	if len(st.PrimaryKey) > 0 {
		pk = st.PrimaryKey
	} else {
		pk = s.Defaults.PrimaryKey
	}
	if st.UpdateKey != "" {
		uk = st.UpdateKey
	} else {
		uk = s.Defaults.UpdateKey
	}
	return pk, uk
}

// EffectiveTarget resolves the target object for a stream. An explicit
// TargetObject always wins. Otherwise tables default to the same name as the
// source: wildcard table streams use the {stream_schema}/{stream_table}
// template so each expanded table maps to its own name, wildcard file streams
// mirror each file path. SQL queries and concrete file paths have no sensible
// default and must specify a target explicitly.
func (s JobSpec) EffectiveTarget(st StreamSpec) (string, error) {
	return EffectiveTarget(st.SourceObject, st.TargetObject)
}

// EffectiveTarget is the source/target pair resolution shared by compilation,
// YAML export and analysis. The wizard mirrors it client-side for placeholders.
func EffectiveTarget(sourceObject, targetObject string) (string, error) {
	switch {
	case targetObject != "":
		return targetObject, nil
	case IsQuery(sourceObject):
		return "", g.Error("target object is required for SQL query stream: %s", sourceObject)
	case HasWildcard(sourceObject) && strings.Contains(sourceObject, "/"):
		return "{stream_file_path}", nil
	case HasWildcard(sourceObject):
		return "{stream_schema}.{stream_table}", nil
	case strings.Contains(sourceObject, "/"):
		return "", g.Error("target object is required for file stream: %s", sourceObject)
	default:
		return sourceObject, nil
	}
}

// slingMode maps a UI mode + keys to the sling write mode.
func slingMode(uiMode string, pk []string, uk string) sling.Mode {
	switch uiMode {
	case "truncate":
		return sling.TruncateMode
	case "snapshot":
		return sling.SnapshotMode
	case "append":
		// append = incremental by watermark; falls back to full-refresh when
		// the stream has no update key yet (the wizard requires one).
		if uk == "" {
			return sling.FullRefreshMode
		}
		return sling.IncrementalMode
	case "upsert":
		if len(pk) == 0 {
			return sling.FullRefreshMode
		}
		return sling.IncrementalMode
	default: // replace
		return sling.FullRefreshMode
	}
}

// IsSingleTask reports whether the spec compiles to a single sling task
// (one stream, no wildcard) rather than a replication.
func (s JobSpec) IsSingleTask() bool {
	active := 0
	for _, st := range s.Streams {
		if st.Disabled {
			continue
		}
		if HasWildcard(st.SourceObject) {
			return false
		}
		active++
	}
	return active == 1
}

// HasWildcard reports whether an object reference contains a glob pattern.
func HasWildcard(object string) bool {
	return strings.ContainsAny(object, "*?")
}

// IsQuery reports whether the source object is an inline SQL query.
func IsQuery(object string) bool {
	trimmed := strings.TrimSpace(object)
	return strings.HasPrefix(trimmed, "sql:") ||
		strings.HasPrefix(strings.ToLower(trimmed), "select ")
}

// stripQueryPrefix removes the "sql:" marker.
func stripQueryPrefix(object string) string {
	return strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(object), "sql:"), "SQL:")
}

/* ------------------------------ compilation ------------------------------ */

func (o *StreamOptions) sourceOpts() *sling.SourceOptions {
	so := &sling.SourceOptions{}
	if o == nil {
		return so
	}
	if o.Limit > 0 {
		so.Limit = ptrInt(o.Limit)
	}
	return so
}

func (o *StreamOptions) targetOpts() *sling.TargetOptions {
	to := &sling.TargetOptions{}
	if o == nil {
		return to
	}
	if o.AddNewColumns != nil {
		to.AddNewColumns = o.AddNewColumns
	}
	if o.AdjustColumnType != nil {
		to.AdjustColumnType = o.AdjustColumnType
	}
	if o.ColumnCasing != "" {
		to.ColumnCasing = columnCasingPtr(o.ColumnCasing)
	}
	if o.FileFormat != "" {
		to.Format = dbio.FileType(strings.ToLower(o.FileFormat))
	}
	if o.PreSQL != "" {
		to.PreSQL = strPtr(o.PreSQL)
	}
	if o.PostSQL != "" {
		to.PostSQL = strPtr(o.PostSQL)
	}
	return to
}

// CompileTask builds a prepared sling Config for a single-stream spec.
func (s JobSpec) CompileTask() (cfg *sling.Config, err error) {
	var st StreamSpec
	for _, c := range s.Streams {
		if !c.Disabled {
			st = c
			break
		}
	}

	pk, uk := s.EffectiveKeys(st)
	obj, err := EffectiveTarget(st.SourceObject, st.TargetObject)
	if err != nil {
		return nil, err
	}

	cfg = &sling.Config{
		Mode: slingMode(s.EffectiveMode(st), pk, uk),
		Source: sling.Source{
			Conn:        s.SourceConn,
			UpdateKey:   uk,
			PrimaryKeyI: keyAny(pk),
			Select:      st.Select,
			Options:     st.Options.sourceOpts(),
		},
		Target: sling.Target{
			Conn:    s.TargetConn,
			Object:  obj,
			Options: st.Options.targetOpts(),
		},
		Env: map[string]string{},
	}

	if IsQuery(st.SourceObject) {
		cfg.Source.Query = stripQueryPrefix(st.SourceObject)
	} else {
		cfg.Source.Stream = st.SourceObject
	}
	if st.Options != nil && st.Options.Where != "" {
		cfg.Source.Where = st.Options.Where
	}
	applyMetadata(cfg, s.Metadata)

	if err = cfg.Prepare(); err != nil {
		return nil, g.Error(err, "could not prepare task config")
	}
	return cfg, nil
}

// CompileReplication builds a compiled sling ReplicationConfig from the spec.
// It round-trips through the exact YAML ToYAML exports, so the executed
// config is identical to what `sling run --replication` runs with an exported
// job — and so core's internal stream ordering / raw-maps state is properly
// initialized (a hand-built Streams map compiles to zero tasks).
func (s JobSpec) CompileReplication() (r *sling.ReplicationConfig, err error) {
	yamlText, err := s.ToYAML()
	if err != nil {
		return nil, err
	}
	cfg, err := sling.LoadReplicationConfig(yamlText)
	if err != nil {
		return nil, g.Error(err, "could not load replication config")
	}
	r = &cfg
	if err = r.Compile(nil); err != nil {
		return nil, g.Error(err, "could not compile replication config")
	}
	return r, nil
}

// keyAny normalizes a key list to the shape sling expects
// (single string for one key, list otherwise).
func keyAny(pk []string) any {
	if len(pk) == 0 {
		return nil
	}
	if len(pk) == 1 {
		return pk[0]
	}
	return pk
}

func applyMetadata(cfg *sling.Config, m MetadataOptions) {
	if m.LoadedAt {
		cfg.MetadataLoadedAt = ptrBool(true)
	}
	if m.SyncedAt {
		cfg.MetadataSyncedAt = ptrBool(true)
	}
	if m.RowNum {
		cfg.MetadataRowNum = true
	}
	if m.RowID {
		cfg.MetadataRowID = true
	}
	if m.ExecID {
		cfg.MetadataExecID = true
	}
	if m.StreamURL {
		cfg.MetadataStreamURL = true
	}
}

/* -------------------------------- YAML I/O ------------------------------- */

// ToYAML renders the spec as a CLI-compatible sling replication YAML so any
// exported job runs with `sling run --replication file.yaml`.
func (s JobSpec) ToYAML() (string, error) {
	dpk, duk := s.EffectiveKeys(s.Defaults)
	defMode := slingMode(s.EffectiveMode(s.Defaults), dpk, duk)

	defaults := map[string]any{}
	if defMode != "" {
		defaults["mode"] = string(defMode)
	}
	if duk != "" {
		defaults["update_key"] = duk
	}
	if len(dpk) > 0 {
		defaults["primary_key"] = keyAny(dpk)
	}
	if om := optionsMap(s.Defaults.Options); len(om) > 0 {
		defaults["options"] = om
	}
	if len(s.Defaults.Select) > 0 {
		defaults["select"] = s.Defaults.Select
	}

	streams := map[string]any{}
	for _, st := range s.Streams {
		if st.Disabled {
			continue
		}
		spk, suk := s.EffectiveKeys(st)
		sm := map[string]any{}
		if m := slingMode(s.EffectiveMode(st), spk, suk); m != defMode {
			sm["mode"] = string(m)
		}
		if suk != "" && suk != duk {
			sm["update_key"] = suk
		}
		if len(spk) > 0 && strings.Join(spk, ",") != strings.Join(dpk, ",") {
			sm["primary_key"] = keyAny(spk)
		}
		if len(st.Select) > 0 {
			sm["select"] = st.Select
		}
		obj, terr := EffectiveTarget(st.SourceObject, st.TargetObject)
		if terr != nil {
			return "", terr
		}
		// always explicit: a CLI `sling run --replication` fails on an
		// object-less stream, so same-name defaults must be written out.
		sm["object"] = obj
		if IsQuery(st.SourceObject) {
			sm["sql"] = stripQueryPrefix(st.SourceObject)
		}
		if om := optionsMap(st.Options); len(om) > 0 {
			sm["options"] = om
		}
		name := st.Name
		if name == "" {
			name = st.SourceObject
		}
		streams[name] = sm
	}

	final := map[string]any{
		"source":   s.SourceConn,
		"target":   s.TargetConn,
		"defaults": defaults,
		"streams":  streams,
	}

	out, err := yaml.Marshal(final)
	if err != nil {
		return "", g.Error(err, "could not render job YAML")
	}
	return string(out), nil
}

// SpecFromReplicationYAML imports a CLI replication YAML into a JobSpec.
func SpecFromReplicationYAML(content string) (spec JobSpec, err error) {
	r, err := sling.LoadReplicationConfig(content)
	if err != nil {
		return spec, g.Error(err, "could not parse replication YAML")
	}
	spec = JobSpec{
		SourceConn: r.Source,
		TargetConn: r.Target,
	}
	uiMode := func(m sling.Mode, pk any, uk string) string {
		pks := castKeyArray(pk)
		switch m {
		case sling.TruncateMode:
			return "truncate"
		case sling.SnapshotMode:
			return "snapshot"
		case sling.IncrementalMode:
			if len(pks) > 0 {
				return "upsert"
			}
			return "append"
		default:
			return "replace"
		}
	}

	defPK := castKeyArray(r.Defaults.PrimaryKeyI)
	spec.Defaults = StreamSpec{
		Mode:       uiMode(r.Defaults.Mode, r.Defaults.PrimaryKeyI, r.Defaults.UpdateKey),
		PrimaryKey: defPK,
		UpdateKey:  r.Defaults.UpdateKey,
	}

	names := r.StreamsOrdered()
	for _, name := range names {
		st, ok := r.Streams[name]
		if !ok {
			continue
		}
		spec.Streams = append(spec.Streams, StreamSpec{
			Name:         name,
			SourceObject: name,
			TargetObject: st.Object,
			Mode:         uiMode(st.Mode, st.PrimaryKeyI, st.UpdateKey),
			PrimaryKey:   castKeyArray(st.PrimaryKeyI),
			UpdateKey:    st.UpdateKey,
		})
	}
	return spec, nil
}

// castKeyArray normalizes sling's any-shaped primary key to []string.
func castKeyArray(v any) (keys []string) {
	switch t := v.(type) {
	case nil:
	case string:
		if t != "" {
			keys = []string{t}
		}
	case []string:
		keys = t
	case []any:
		for _, item := range t {
			if s, ok := item.(string); ok && s != "" {
				keys = append(keys, s)
			}
		}
	}
	return keys
}

func optionsMap(o *StreamOptions) map[string]any {
	m := map[string]any{}
	if o == nil {
		return m
	}
	if o.AddNewColumns != nil {
		m["add_new_columns"] = *o.AddNewColumns
	}
	if o.AdjustColumnType != nil {
		m["adjust_column_type"] = *o.AdjustColumnType
	}
	if o.ColumnCasing != "" {
		m["column_casing"] = o.ColumnCasing
	}
	if o.PreSQL != "" {
		m["pre_sql"] = o.PreSQL
	}
	if o.PostSQL != "" {
		m["post_sql"] = o.PostSQL
	}
	if o.Where != "" {
		m["where"] = o.Where
	}
	return m
}
