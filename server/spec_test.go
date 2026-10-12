package server

// spec_test.go — tests for EffectiveTarget same-name resolution, multi-stream
// compilation and the per-table primary-keys API (batch migration support).

import (
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/slingdata-io/sling-cli/core/sling"
)

func TestEffectiveTarget(t *testing.T) {
	cases := []struct {
		source, target string
		want           string
		wantErr        bool
	}{
		{source: "public.users", target: "staging.users", want: "staging.users"},
		{source: "public.users", target: "", want: "public.users"},                                    // same-name default
		{source: "public.*", target: "", want: "{stream_schema}.{stream_table}"},                      // wildcard template
		{source: "s3://bucket/data/*.csv", target: "", want: "{stream_file_path}"},                    // file wildcard
		{source: "select * from t", target: "", wantErr: true},                                        // query needs explicit
		{source: "sql: select 1", target: "", wantErr: true},                                          // query marker
		{source: "file:///tmp/data.csv", target: "", wantErr: true},                                   // concrete file needs explicit
	}
	for _, c := range cases {
		got, err := EffectiveTarget(c.source, c.target)
		if c.wantErr {
			if err == nil {
				t.Errorf("EffectiveTarget(%q, %q) expected error, got %q", c.source, c.target, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("EffectiveTarget(%q, %q) error: %v", c.source, c.target, err)
			continue
		}
		if got != c.want {
			t.Errorf("EffectiveTarget(%q, %q) = %q, want %q", c.source, c.target, got, c.want)
		}
	}
}

func newSpecApp(t *testing.T) (*App, string) {
	t.Helper()
	dir := testEnv(t)
	app, err := New(Options{DBPath: filepath.Join(dir, "test.db"), Concurrency: 1})
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	t.Cleanup(app.Shutdown)
	return app, dir
}

func upsertSqlitePair(t *testing.T, app *App, dir string) {
	t.Helper()
	srcDb := filepath.Join(dir, "src.db")
	createSqliteFixture(t, srcDb, map[string]string{
		"orders":    "create table orders (id integer primary key, name text)",
		"customers": "create table customers (customer_id integer primary key, email text)",
		"payments":  "create table payments (id integer, seq integer, amount integer, primary key (id, seq))",
		"archive":   "create table archive (id integer primary key, payload text)",
	})
	if _, err := app.Conns.Upsert(UpsertInput{Name: "SPEC_SRC", Type: "sqlite", Props: map[string]any{"instance": srcDb}}); err != nil {
		t.Fatal(err)
	}
	if _, err := app.Conns.Upsert(UpsertInput{Name: "SPEC_TGT", Type: "sqlite", Props: map[string]any{"instance": filepath.Join(dir, "tgt.db")}}); err != nil {
		t.Fatal(err)
	}
}

// unquote strips the dialect quoting Prepare() applies to object names.
func unquote(s string) string {
	return strings.ReplaceAll(s, `"`, "")
}

func TestCompileTaskDefaultTarget(t *testing.T) {
	app, dir := newSpecApp(t)
	upsertSqlitePair(t, app, dir)

	spec := JobSpec{
		SourceConn: "SPEC_SRC", TargetConn: "SPEC_TGT",
		Defaults: StreamSpec{Mode: "replace"},
		Streams:  []StreamSpec{{SourceObject: "main.orders"}}, // no TargetObject
	}
	cfg, err := spec.CompileTask()
	if err != nil {
		t.Fatalf("compile task: %v", err)
	}
	if got := unquote(cfg.Target.Object); got != "main.orders" {
		t.Fatalf("default target object = %q, want main.orders", got)
	}
}

func TestCompileReplicationDefaultTargetsAndKeys(t *testing.T) {
	app, dir := newSpecApp(t)
	upsertSqlitePair(t, app, dir)

	spec := JobSpec{
		SourceConn: "SPEC_SRC", TargetConn: "SPEC_TGT",
		Defaults: StreamSpec{Mode: "upsert", PrimaryKey: []string{"id"}},
		Streams: []StreamSpec{
			{SourceObject: "main.orders"},                                            // same-name + defaults key
			{SourceObject: "main.customers", PrimaryKey: []string{"customer_id"}},   // per-stream key override
			{SourceObject: "main.payments", PrimaryKey: []string{"id", "seq"}},      // composite key
			{SourceObject: "main.archive", TargetObject: "main.archive_2024"},       // explicit target
		},
	}
	r, err := spec.CompileReplication()
	if err != nil {
		t.Fatalf("compile replication: %v", err)
	}
	if len(r.Tasks) != 4 {
		t.Fatalf("tasks = %d, want 4", len(r.Tasks))
	}
	byStream := map[string]*sling.Config{}
	for _, cfg := range r.Tasks {
		byStream[cfg.StreamName] = cfg
	}
	if c := byStream["main.orders"]; unquote(c.Target.Object) != "main.orders" {
		t.Fatalf("orders default object = %q, want main.orders", c.Target.Object)
	}
	if c := byStream["main.customers"]; unquote(c.Target.Object) != "main.customers" {
		t.Fatalf("customers default object = %q, want main.customers", c.Target.Object)
	} else if pk := c.Source.PrimaryKey(); len(pk) != 1 || pk[0] != "customer_id" {
		t.Fatalf("customers per-stream pk = %v, want [customer_id]", pk)
	}
	if c := byStream["main.payments"]; c != nil {
		pk := c.Source.PrimaryKey()
		if len(pk) != 2 || pk[0] != "id" || pk[1] != "seq" {
			t.Fatalf("payments composite pk = %v, want [id seq]", pk)
		}
	} else {
		t.Fatal("payments stream missing from compiled tasks")
	}
	if c := byStream["main.orders"]; len(c.Source.PrimaryKey()) != 1 || c.Source.PrimaryKey()[0] != "id" {
		t.Fatalf("orders inherited pk = %v, want [id]", c.Source.PrimaryKey())
	}
	if c := byStream["main.archive"]; unquote(c.Target.Object) != "main.archive_2024" {
		t.Fatalf("archive explicit object = %q, want main.archive_2024", c.Target.Object)
	}
}

func TestCompileReplicationQueryWithoutTargetFails(t *testing.T) {
	app, dir := newSpecApp(t)
	upsertSqlitePair(t, app, dir)

	spec := JobSpec{
		SourceConn: "SPEC_SRC", TargetConn: "SPEC_TGT",
		Defaults: StreamSpec{Mode: "replace"},
		Streams:  []StreamSpec{{SourceObject: "select * from main.orders"}},
	}
	if _, err := spec.CompileReplication(); err == nil {
		t.Fatal("query stream without target should fail compilation")
	}
}

func TestToYAMLWritesExplicitObjects(t *testing.T) {
	spec := JobSpec{
		SourceConn: "SRC", TargetConn: "TGT",
		Defaults: StreamSpec{Mode: "replace"},
		Streams: []StreamSpec{
			{SourceObject: "public.users"},                                                    // same-name
			{SourceObject: "public.orders", TargetObject: "staging.orders_2024"},             // explicit
		},
	}
	out, err := spec.ToYAML()
	if err != nil {
		t.Fatalf("to yaml: %v", err)
	}
	// CLI replications fail on object-less streams, so same-name defaults must
	// be written out explicitly
	if !strings.Contains(out, "object: public.users") {
		t.Fatalf("yaml missing explicit same-name object:\n%s", out)
	}
	if !strings.Contains(out, "object: staging.orders_2024") {
		t.Fatalf("yaml missing explicit object:\n%s", out)
	}

	// round-trip: export must re-import into an equivalent spec
	back, err := SpecFromReplicationYAML(out)
	if err != nil {
		t.Fatalf("import yaml: %v", err)
	}
	if len(back.Streams) != 2 {
		t.Fatalf("round-trip streams = %d, want 2", len(back.Streams))
	}
	for _, st := range back.Streams {
		if st.TargetObject == "" {
			t.Fatalf("round-trip stream %q lost its target object", st.SourceObject)
		}
	}
}

func TestLocalConnTypeNormalized(t *testing.T) {
	app, dir := newSpecApp(t)

	// the properties catalog calls local filesystem connections "local";
	// records must be stored/tested as the dbio type "file"
	rec, err := app.Conns.Upsert(UpsertInput{Name: "LOCAL_SRC", Type: "local", Props: map[string]any{"path": dir}})
	if err != nil {
		t.Fatalf("upsert local conn: %v", err)
	}
	if rec.Type != "file" {
		t.Fatalf("stored type = %q, want file", rec.Type)
	}
	res, err := app.Conns.TestProps(UpsertInput{Name: "LOCAL_SRC", Type: "local", Props: map[string]any{"path": dir}})
	if err != nil {
		t.Fatalf("test local conn: %v", err)
	}
	if !res.Success {
		t.Fatalf("local conn test failed: %s", res.Error)
	}
}

func TestConnsPrimaryKeys(t *testing.T) {
	app, dir := newSpecApp(t)
	upsertSqlitePair(t, app, dir)

	// service layer
	keys, err := app.Conns.PrimaryKeys("SPEC_SRC", []string{"main.orders", "main.customers", "main.missing"})
	if err != nil {
		t.Fatalf("primary keys: %v", err)
	}
	if got := keys["main.orders"]; len(got) != 1 || got[0] != "id" {
		t.Fatalf("orders pk = %v, want [id]", got)
	}
	if got := keys["main.customers"]; len(got) != 1 || got[0] != "customer_id" {
		t.Fatalf("customers pk = %v, want [customer_id]", got)
	}
	if _, ok := keys["main.missing"]; ok {
		t.Fatal("missing table should be absent from result")
	}

	// http layer
	ts := httptest.NewServer(app.Handler())
	defer ts.Close()
	code, body := apiPost(t, ts.URL, "/api/conns/SPEC_SRC/primary-keys", map[string]any{
		"tables": []string{"main.orders"},
	})
	if code != 200 {
		t.Fatalf("primary-keys endpoint = %d: %s", code, body)
	}
	if !strings.Contains(body, `"main.orders":["id"]`) {
		t.Fatalf("primary-keys body: %s", body)
	}
}
