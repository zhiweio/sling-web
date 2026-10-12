// analyze.go — pre-run analysis: for every source table in a job, count
// columns/rows/storage, aggregate totals, and estimate duration and load
// level so users can assess the impact on source/target before running.
package server

import (
	"bufio"
	"context"
	"os"
	"strings"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/flarco/g"
	"github.com/slingdata-io/sling-cli/core/dbio"
	"github.com/slingdata-io/sling-cli/core/dbio/connection"
	"github.com/slingdata-io/sling-cli/core/dbio/database"
	"github.com/spf13/cast"
)

/* ------------------------------- payloads -------------------------------- */

// Analysis is the full pre-run analysis result.
type Analysis struct {
	SourceConn string           `json:"source_conn"`
	TargetConn string           `json:"target_conn"`
	Streams    []StreamAnalysis `json:"streams"`
	Totals     AnalysisTotals   `json:"totals"`
	AnalyzedAt time.Time        `json:"analyzed_at"`
}

// StreamAnalysis is per-source-object stats.
type StreamAnalysis struct {
	SourceObject string     `json:"source_object"`
	TargetObject string     `json:"target_object,omitempty"`
	Kind         string     `json:"kind"` // table|file
	Columns      []ColBrief `json:"columns,omitempty"`
	ColumnCount  int        `json:"column_count"`
	Rows         *int64     `json:"rows,omitempty"`       // nil = unknown (timeout/unsupported)
	SizeBytes    *int64     `json:"size_bytes,omitempty"` // nil = unknown
	TargetExists bool       `json:"target_exists,omitempty"`
	TargetRows   *int64     `json:"target_rows,omitempty"`
	Warnings     []string   `json:"warnings,omitempty"`
	Error        string     `json:"error,omitempty"`
}

// ColBrief is one column of the source table.
type ColBrief struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// AnalysisTotals aggregates the stream stats.
type AnalysisTotals struct {
	Tables           int    `json:"tables"`
	Columns          int    `json:"columns"`
	Rows             int64  `json:"rows"`               // sum of known rows
	RowsKnown        bool   `json:"rows_known"`         // false if any stream unknown
	SizeBytes        int64  `json:"size_bytes"`
	SizeKnown        bool   `json:"size_known"`
	NewTargets       int    `json:"new_targets"`        // target tables to be created
	EstimatedSeconds int64  `json:"estimated_seconds"`  // heuristic
	Load             string `json:"load"` // low|medium|high
}

// heuristic throughputs for the duration/load estimate (conservative)
const (
	estRowsPerSec   = 25_000
	estBytesPerSec  = 10 * 1024 * 1024
	largeTableRows  = 10_000_000
	largeTableBytes = 10 * 1024 * 1024 * 1024
)

/* -------------------------------- analyze --------------------------------- */

// AnalyzeSpec inspects the spec's source objects (expanding wildcards) and
// the target side, returning per-stream and total metrics.
func (a *App) AnalyzeSpec(spec JobSpec) (*Analysis, error) {
	if err := a.Conns.Materialize(); err != nil {
		return nil, err
	}
	entries := connection.GetLocalConns(true)

	srcEntry := entries.Get(spec.SourceConn)
	if srcEntry.Name == "" {
		return nil, g.Error("source connection not found: %s", spec.SourceConn)
	}
	tgtEntry := entries.Get(spec.TargetConn)
	if tgtEntry.Name == "" {
		return nil, g.Error("target connection not found: %s", spec.TargetConn)
	}

	// target db handle (lazy) for existence checks
	var tgtDB database.Connection
	openTarget := func() database.Connection {
		if tgtDB != nil || tgtEntry.Connection.Type.Kind() != dbio.KindDatabase {
			return tgtDB
		}
		db, err := tgtEntry.Connection.AsDatabase(connection.AsConnOptions{UseCache: true})
		if err != nil {
			return nil
		}
		if err = db.Connect(10); err != nil {
			return nil
		}
		tgtDB = db
		return tgtDB
	}
	defer func() {
		if tgtDB != nil {
			tgtDB.Close()
		}
	}()

	res := &Analysis{
		SourceConn: spec.SourceConn,
		TargetConn: spec.TargetConn,
		AnalyzedAt: time.Now(),
	}

	for _, st := range streamsOfSpec(spec) {
		target, terr := EffectiveTarget(st.SourceObject, st.TargetObject)
		if terr != nil {
			res.Streams = append(res.Streams, StreamAnalysis{
				SourceObject: st.SourceObject, TargetObject: st.TargetObject,
				Kind: "table", Error: terr.Error(),
			})
			continue
		}

		if srcEntry.Connection.Type.Kind() == dbio.KindFile {
			sa := analyzeFileObject(srcEntry, st.SourceObject, target)
			if tgt := openTarget(); tgt != nil && sa.Error == "" {
				if t, err := database.ParseTableName(target, tgt.GetType()); err == nil {
					if exists, err := tgt.TableExists(t); err == nil && exists {
						sa.TargetExists = true
						if n, err := countWithTimeout(tgt, target, 30*time.Second); err == nil {
							sa.TargetRows = &n
						}
					}
				}
			}
			res.Streams = append(res.Streams, sa)
			continue
		}

		// database source: expand wildcards into concrete tables
		var objects []string
		if HasWildcard(st.SourceObject) {
			tables, err := expandWildcardTables(srcEntry, st.SourceObject)
			if err != nil {
				res.Streams = append(res.Streams, StreamAnalysis{
					SourceObject: st.SourceObject, TargetObject: target, Kind: "table", Error: err.Error(),
				})
				continue
			}
			objects = tables
		} else {
			objects = []string{st.SourceObject}
		}

		for _, obj := range objects {
			// resolve the same-name default per concrete table (a wildcard
			// stream's target template becomes each table's own name)
			objTarget, err := EffectiveTarget(obj, st.TargetObject)
			if err != nil {
				objTarget = target
			}
			res.Streams = append(res.Streams, analyzeTable(srcEntry, obj, objTarget, openTarget))
		}
	}

	res.Totals = aggregateAnalysis(res.Streams)
	return res, nil
}

func streamsOfSpec(spec JobSpec) []StreamSpec {
	out := []StreamSpec{}
	for _, s := range spec.Streams {
		if !s.Disabled && s.SourceObject != "" {
			out = append(out, s)
		}
	}
	return out
}

// expandWildcardTables lists concrete tables matching a pattern on a DB source.
func expandWildcardTables(entry connection.ConnEntry, pattern string) ([]string, error) {
	_, _, schemata, _, err := entry.Connection.Discover(&connection.DiscoverOptions{
		Pattern: pattern, Level: database.SchemataLevelTable,
	})
	if err != nil {
		return nil, g.Error(err, "could not expand %s", pattern)
	}
	names := []string{}
	for _, dbName := range sortedKeys(schemata.Databases) {
		db := schemata.Databases[dbName]
		for _, schemaName := range sortedKeys(db.Schemas) {
			schema := db.Schemas[schemaName]
			for _, tableName := range sortedKeys(schema.Tables) {
				names = append(names, schema.Name+"."+tableName)
			}
		}
	}
	if len(names) == 0 {
		return nil, g.Error("no tables match %s", pattern)
	}
	return names, nil
}

// analyzeTable gathers columns/rows/size for one DB table + target existence.
func analyzeTable(srcEntry connection.ConnEntry, object, target string, openTarget func() database.Connection) StreamAnalysis {
	sa := StreamAnalysis{SourceObject: object, TargetObject: target, Kind: "table"}

	dbConn, err := srcEntry.Connection.AsDatabase(connection.AsConnOptions{UseCache: true})
	if err != nil {
		sa.Error = err.Error()
		return sa
	}
	if err = dbConn.Connect(10); err != nil {
		sa.Error = err.Error()
		return sa
	}

	// columns
	if cols, err := dbConn.GetColumns(object); err != nil {
		sa.Warnings = append(sa.Warnings, "could not read columns: "+g.ErrMsgSimple(err))
	} else {
		sa.ColumnCount = len(cols)
		for _, c := range cols {
			sa.Columns = append(sa.Columns, ColBrief{Name: c.Name, Type: string(c.Type)})
		}
	}

	// row count (potentially slow on big tables → generous timeout, degrade)
	if n, err := countWithTimeout(dbConn, object, 90*time.Second); err != nil {
		sa.Warnings = append(sa.Warnings, "row count unavailable: "+g.ErrMsgSimple(err))
	} else {
		sa.Rows = &n
		if n >= largeTableRows {
			sa.Warnings = append(sa.Warnings,
				g.F("%s rows — large table, consider running off-peak", humanize.Comma(n)))
		}
	}

	// storage size (best-effort, per engine)
	if n, ok := tableSizeBytes(dbConn, object, srcEntry.Connection.Type); ok {
		sa.SizeBytes = &n
		if n >= largeTableBytes {
			sa.Warnings = append(sa.Warnings,
				g.F("%s — large volume, mind network and disk load", humanize.Bytes(uint64(n))))
		}
	}

	// target side
	if tgt := openTarget(); tgt != nil {
		if t, err := database.ParseTableName(target, tgt.GetType()); err == nil {
			if exists, err := tgt.TableExists(t); err == nil && exists {
				sa.TargetExists = true
				if n, err := countWithTimeout(tgt, target, 30*time.Second); err == nil {
					sa.TargetRows = &n
				}
			}
		}
	}

	return sa
}

func countWithTimeout(dbConn database.Connection, table string, timeout time.Duration) (int64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	type result struct {
		n   int64
		err error
	}
	ch := make(chan result, 1)
	go func() {
		n, err := dbConn.Self().GetCount(table)
		ch <- result{n, err}
	}()
	select {
	case r := <-ch:
		return r.n, r.err
	case <-ctx.Done():
		return 0, g.Error("timeout")
	}
}

// tableSizeBytes returns the on-disk size of a table for engines with a
// cheap catalog query; ok=false means unsupported/unknown. dbio queries do
// not bind parameters, so values are escaped single-quoted literals.
func tableSizeBytes(dbConn database.Connection, table string, connType dbio.Type) (int64, bool) {
	lit := func(v string) string { return "'" + strings.ReplaceAll(v, "'", "''") + "'" }

	var sql string
	switch connType {
	case dbio.TypeDbPostgres, dbio.TypeDbRedshift:
		sql = "select pg_total_relation_size(" + lit(table) + ")"
	case dbio.TypeDbMySQL, dbio.TypeDbMariaDB:
		parts := strings.SplitN(table, ".", 2)
		if len(parts) != 2 {
			return 0, false
		}
		sql = "select data_length + index_length from information_schema.tables where table_schema = " +
			lit(parts[0]) + " and table_name = " + lit(parts[1])
	case dbio.TypeDbSQLServer, dbio.TypeDbAzure:
		sql = "select sum(reserved_page_count) * 8 * 1024 from sys.dm_db_partition_stats where object_id = object_id(" + lit(table) + ")"
	case dbio.TypeDbDuckDb:
		name := table
		if i := strings.LastIndex(name, "."); i >= 0 {
			name = name[i+1:]
		}
		sql = "select estimated_size * 128 * 1024 from duckdb_tables() where table_name = " + lit(name) + " limit 1"
	default:
		return 0, false
	}

	data, err := dbConn.Self().Query(sql)
	if err != nil || len(data.Rows) == 0 || len(data.Rows[0]) == 0 {
		return 0, false
	}
	n := cast.ToInt64(data.Rows[0][0])
	return n, n > 0
}

/* --------------------------------- files ---------------------------------- */

// analyzeFileObject stats a file stream: local files get header columns,
// line count (capped) and stat size; remote files get listed size only.
func analyzeFileObject(srcEntry connection.ConnEntry, object, target string) StreamAnalysis {
	sa := StreamAnalysis{SourceObject: object, TargetObject: target, Kind: "file"}

	// local path (file:// or plain path)
	path := strings.TrimPrefix(object, "file://")
	if path != "" && !strings.Contains(path, "://") {
		fi, statErr := os.Stat(path)
		if statErr != nil {
			sa.Error = "file not found: " + path
			return sa
		}
		if !fi.IsDir() {
			n := fi.Size()
			sa.SizeBytes = &n
			if strings.HasSuffix(strings.ToLower(path), ".csv") {
				if cols, rows, ok := quickCsvStats(path); ok {
					sa.ColumnCount = cols
					sa.Rows = &rows
					for i := 0; i < cols; i++ {
						sa.Columns = append(sa.Columns, ColBrief{Name: g.F("col_%d", i+1)})
					}
				}
			}
			if sa.Rows == nil && n >= 512*1024*1024 {
				sa.Warnings = append(sa.Warnings, "file is large, rows not counted (>512MB)")
			}
			return sa
		}
	}

	// remote / wildcard files: use discovery for sizes
	_, nodes, _, _, err := srcEntry.Connection.Discover(&connection.DiscoverOptions{
		Pattern: object, Level: database.SchemataLevelTable,
	})
	if err != nil {
		sa.Error = g.ErrMsgSimple(err)
		return sa
	}
	var total int64
	count := 0
	for i := range nodes {
		if nodes[i].IsDir {
			continue
		}
		total += int64(nodes[i].Size)
		count++
	}
	if count > 0 {
		sa.SizeBytes = &total
		sa.Warnings = append(sa.Warnings, g.F("%d files", count))
	} else {
		sa.Error = "no files match " + object
	}
	return sa
}

// quickCsvStats reads the header field count and a line count, capped at
// ~1GB of scanning to bound the analysis cost.
func quickCsvStats(path string) (cols int, rows int64, ok bool) {
	const maxScan = int64(1) << 30 // 1GB
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, false
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)
	if !sc.Scan() {
		return 0, 0, false // empty file
	}
	cols = len(strings.Split(strings.TrimRight(sc.Text(), "\r\n"), ","))
	if cols == 0 {
		return 0, 0, false
	}

	var scanned int64
	for sc.Scan() {
		rows++
		scanned += int64(len(sc.Bytes())) + 1
		if scanned > maxScan {
			break // huge file: stop counting, report what we have
		}
	}
	return cols, rows, true
}

/* ------------------------------- aggregate -------------------------------- */

func aggregateAnalysis(streams []StreamAnalysis) AnalysisTotals {
	t := AnalysisTotals{RowsKnown: true, SizeKnown: true}
	var estSecs int64

	for _, s := range streams {
		if s.Error != "" {
			continue
		}
		t.Tables++
		t.Columns += s.ColumnCount
		if s.Rows != nil {
			t.Rows += *s.Rows
			est := (*s.Rows + estRowsPerSec - 1) / estRowsPerSec
			if est > estSecs {
				estSecs = est
			}
		} else {
			t.RowsKnown = false
		}
		if s.SizeBytes != nil {
			t.SizeBytes += *s.SizeBytes
			est := (*s.SizeBytes + estBytesPerSec - 1) / estBytesPerSec
			if est > estSecs {
				estSecs = est
			}
		} else {
			t.SizeKnown = false
		}
		if !s.TargetExists {
			t.NewTargets++
		}
	}

	t.EstimatedSeconds = estSecs

	// load heuristic: combine volume and duration
	loadScore := 0
	switch {
	case t.Rows >= 100_000_000 || t.SizeBytes >= 100*1024*1024*1024:
		loadScore = 3
	case t.Rows >= 10_000_000 || t.SizeBytes >= 10*1024*1024*1024:
		loadScore = 2
	default:
		loadScore = 1
	}
	if estSecs >= 3600 {
		loadScore++
	}
	if loadScore >= 4 {
		t.Load = "high"
	} else if loadScore == 3 {
		t.Load = "medium"
	} else {
		t.Load = "low"
	}
	return t
}
