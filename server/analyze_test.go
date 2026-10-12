package server

// analyze_test.go — pre-flight analysis integration tests on SQLite.

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/slingdata-io/sling-cli/core/dbio/database"
)

func newAnalyzeApp(t *testing.T) (*App, string) {
	t.Helper()
	dir := testEnv(t)
	app, err := New(Options{DBPath: filepath.Join(dir, "test.db"), Concurrency: 1})
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	t.Cleanup(app.Shutdown)
	return app, dir
}

func TestAnalyzeSpecTablesAndTotals(t *testing.T) {
	app, dir := newAnalyzeApp(t)

	// source sqlite with two tables
	srcDb := filepath.Join(dir, "src.db")
	createSqliteFixture(t, srcDb, map[string]string{
		"orders":   "create table orders (id integer primary key, name text, amount integer); insert into orders values (1,'a',10),(2,'b',20),(3,'c',30)",
		"customers": "create table customers (id integer primary key, email text); insert into customers values (1,'x@y')",
	})

	// target sqlite (empty → tables will be created)
	tgtDb := filepath.Join(dir, "tgt.db")

	if _, err := app.Conns.Upsert(UpsertInput{Name: "ANA_SRC", Type: "sqlite", Props: map[string]any{"instance": srcDb}}); err != nil {
		t.Fatalf("upsert src conn: %v", err)
	}
	if _, err := app.Conns.Upsert(UpsertInput{Name: "ANA_TGT", Type: "sqlite", Props: map[string]any{"instance": tgtDb}}); err != nil {
		t.Fatalf("upsert tgt conn: %v", err)
	}

	spec := JobSpec{
		SourceConn: "ANA_SRC",
		TargetConn: "ANA_TGT",
		Defaults:   StreamSpec{Mode: "replace"},
		Streams: []StreamSpec{
			{SourceObject: "main.orders", TargetObject: "main.orders"},
			{SourceObject: "main.customers", TargetObject: "main.customers"},
		},
	}

	res, err := app.AnalyzeSpec(spec)
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}

	if len(res.Streams) != 2 {
		t.Fatalf("streams: %+v", res.Streams)
	}
	orders := res.Streams[0]
	if orders.Error != "" {
		t.Fatalf("orders error: %s", orders.Error)
	}
	if orders.ColumnCount != 3 {
		t.Fatalf("orders columns = %d, want 3", orders.ColumnCount)
	}
	if orders.Rows == nil || *orders.Rows != 3 {
		t.Fatalf("orders rows = %v, want 3", orders.Rows)
	}
	if orders.TargetExists {
		t.Fatal("target should not exist yet")
	}

	totals := res.Totals
	if totals.Tables != 2 || totals.Columns != 5 {
		t.Fatalf("totals tables/cols = %d/%d, want 2/5", totals.Tables, totals.Columns)
	}
	if totals.Rows != 4 {
		t.Fatalf("totals rows = %d, want 4", totals.Rows)
	}
	// sqlite has no cheap per-table size catalog: size may be unknown
	if !totals.RowsKnown {
		t.Fatalf("totals known flags: %+v", totals)
	}
	if totals.NewTargets != 2 {
		t.Fatalf("new targets = %d, want 2", totals.NewTargets)
	}
	if totals.Load != "low" || totals.EstimatedSeconds < 1 {
		t.Fatalf("load heuristic: %+v", totals)
	}
}

func TestAnalyzeSpecWildcardExpansion(t *testing.T) {
	app, dir := newAnalyzeApp(t)
	srcDb := filepath.Join(dir, "src.db")
	createSqliteFixture(t, srcDb, map[string]string{
		"t1": "create table t1 (id integer); insert into t1 values (1),(2)",
		"t2": "create table t2 (id integer); insert into t2 values (1)",
	})
	if _, err := app.Conns.Upsert(UpsertInput{Name: "ANA_SRC", Type: "sqlite", Props: map[string]any{"instance": srcDb}}); err != nil {
		t.Fatal(err)
	}
	if _, err := app.Conns.Upsert(UpsertInput{Name: "ANA_TGT", Type: "sqlite", Props: map[string]any{"instance": filepath.Join(dir, "tgt.db")}}); err != nil {
		t.Fatal(err)
	}

	spec := JobSpec{
		SourceConn: "ANA_SRC", TargetConn: "ANA_TGT",
		Defaults: StreamSpec{Mode: "replace"},
		Streams:  []StreamSpec{{SourceObject: "main.t*"}},
	}
	res, err := app.AnalyzeSpec(spec)
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	if len(res.Streams) != 2 {
		t.Fatalf("wildcard should expand to 2 streams, got %d: %+v", len(res.Streams), res.Streams)
	}
	if res.Totals.Tables != 2 || res.Totals.Rows != 3 {
		t.Fatalf("wildcard totals: %+v", res.Totals)
	}
}

func TestAnalyzeLocalCsvFile(t *testing.T) {
	app, dir := newAnalyzeApp(t)
	csv := filepath.Join(dir, "data.csv")
	if err := writeTestFile(csv, "id,name\n1,a\n2,b\n3,c\n4,d\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := app.Conns.Upsert(UpsertInput{Name: "ANA_F", Type: "file", Props: map[string]any{"path": dir}}); err != nil {
		t.Fatal(err)
	}
	if _, err := app.Conns.Upsert(UpsertInput{Name: "ANA_TGT", Type: "sqlite", Props: map[string]any{"instance": filepath.Join(dir, "tgt.db")}}); err != nil {
		t.Fatal(err)
	}

	spec := JobSpec{
		SourceConn: "ANA_F", TargetConn: "ANA_TGT",
		Defaults: StreamSpec{Mode: "replace"},
		Streams:  []StreamSpec{{SourceObject: "file://" + csv, TargetObject: "main.data"}},
	}
	res, err := app.AnalyzeSpec(spec)
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	s := res.Streams[0]
	if s.Error != "" {
		t.Fatalf("csv stream error: %s", s.Error)
	}
	if s.Kind != "file" || s.ColumnCount != 2 {
		t.Fatalf("csv columns: %+v", s)
	}
	if s.Rows == nil || *s.Rows != 4 {
		t.Fatalf("csv rows = %v, want 4", s.Rows)
	}
	if s.SizeBytes == nil || *s.SizeBytes == 0 {
		t.Fatalf("csv size missing: %+v", s)
	}
}

func TestAnalyzeHTTPSpecValidation(t *testing.T) {
	app, _ := newAnalyzeApp(t)
	ts := httptest.NewServer(app.Handler())
	defer ts.Close()

	// empty spec → 400
	if code, _ := apiPost(t, ts.URL, "/api/analyze", map[string]any{"spec": JobSpec{}}); code != 400 {
		t.Fatalf("empty spec = %d, want 400", code)
	}
}

/* ------------------------------- helpers ---------------------------------- */

// createSqliteFixture creates a sqlite file and runs statements (each value
// may contain multiple semicolon-separated statements).
func createSqliteFixture(t *testing.T, path string, tables map[string]string) {
	t.Helper()
	conn, err := database.NewConn("sqlite://"+path+"?mode=rwc", "silent=true")
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	if err = conn.Connect(10); err != nil {
		t.Fatalf("connect fixture: %v", err)
	}
	defer conn.Close()
	for _, ddl := range tables {
		if _, err = conn.ExecMulti(ddl); err != nil {
			t.Fatalf("fixture ddl failed: %v\n%s", err, ddl)
		}
	}
}

func writeTestFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0644)
}
