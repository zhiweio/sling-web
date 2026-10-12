package database_test

// Live-Oracle integration tests for the array-DML bulk loader
// (OracleConn.ArrayDMLStream). External test package: the live path goes
// through core/dbio/connection (the production URL parsing), which imports
// the database package and would create an import cycle from inside it.
//
// Gate: set ORACLE_TEST_URL to a disposable instance, e.g.
//   ORACLE_TEST_URL='oracle://sling_test:SlingTest123@localhost:11521/FREEPDB1'
// Tests create and drop SLING_ADML_* tables in that schema. The million-row
// case takes a few minutes; run with -timeout 20m.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
	"testing"
	"time"

	"github.com/flarco/g"
	"github.com/slingdata-io/sling-cli/core/dbio"
	"github.com/slingdata-io/sling-cli/core/dbio/connection"
	"github.com/slingdata-io/sling-cli/core/dbio/database"
	"github.com/slingdata-io/sling-cli/core/dbio/iop"
	"github.com/spf13/cast"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func oracleTestURL(t *testing.T) string {
	t.Helper()
	u := os.Getenv("ORACLE_TEST_URL")
	if u == "" {
		t.Skip("ORACLE_TEST_URL not set; skipping live Oracle array-DML tests")
	}
	return u
}

// newOracleArrayDMLTestConn returns a connected OracleConn with sqlldr
// lookup forced to fail, so BulkImportStream deterministically exercises
// the embedded array-DML loader regardless of the host machine. It goes
// through connection.NewConnection (the production URL parsing path).
func oracleTestSchema(t *testing.T) string {
	t.Helper()
	u := oracleTestURL(t)
	rest := strings.TrimPrefix(u, "oracle://")
	return strings.ToUpper(rest[:strings.Index(rest, ":")])
}

func oracleTable(t *testing.T, name string) string {
	t.Helper()
	return fmt.Sprintf("%s.%s", oracleTestSchema(t), name)
}

func newOracleArrayDMLTestConn(t *testing.T) *database.OracleConn {
	t.Helper()
	conn, err := connection.NewConnection("sling_adml_test", dbio.TypeDbOracle, g.M("url", oracleTestURL(t)))
	require.NoError(t, err)
	dbConn, err := conn.AsDatabase(connection.AsConnOptions{UseCache: false})
	require.NoError(t, err)
	oracle := dbConn.(*database.OracleConn)
	require.NoError(t, oracle.Connect(30))
	t.Cleanup(func() { oracle.Close() })
	oracle.SetProp("sqlldr_path", filepath.Join(os.TempDir(), "no-such-sqlldr"))
	return oracle
}

func oracleDDL(t *testing.T, conn *database.OracleConn, statements ...string) {
	t.Helper()
	for _, q := range statements {
		if _, err := conn.Self().Exec(q); err != nil {
			t.Fatalf("ddl failed: %s\n%s", err, q)
		}
	}
}

func dropOracleTable(t *testing.T, conn *database.OracleConn, name string) {
	t.Helper()
	_, _ = conn.Self().Exec(fmt.Sprintf("drop table %s purge", name))
}

func oracleCount(t *testing.T, conn *database.OracleConn, sql string) int {
	t.Helper()
	data, err := conn.Self().Query(sql)
	require.NoError(t, err)
	recs := data.Records()
	require.Len(t, recs, 1)
	return cast.ToInt(recs[0]["cnt"])
}

// oracleGenStream lazily generates n rows so million-row cases never hold
// the dataset in memory.
func oracleGenStream(t *testing.T, gctx *g.Context, cols iop.Columns, n int, gen func(i int) []any) *iop.Datastream {
	t.Helper()
	i := 0
	ds := iop.NewDatastreamIt(gctx.Ctx, cols, func(it *iop.Iterator) bool {
		if i >= n {
			return false
		}
		it.Row = gen(i)
		i++
		return true
	})
	require.NoError(t, ds.Start())
	return ds
}

func oracleSimpleColumns() iop.Columns {
	return iop.Columns{
		{Name: "id", Type: iop.BigIntType},
		{Name: "v", Type: iop.StringType},
	}
}

// TestOracleArrayDMLTypesRoundTrip loads every scalar type sling maps for
// Oracle (numbers with scale, binary_double, char padding, date, timestamps
// at two precisions, raw, unicode, quotes, newlines, NULLs, empty string)
// and verifies the values read back.
func TestOracleArrayDMLTypesRoundTrip(t *testing.T) {
	conn := newOracleArrayDMLTestConn(t)
	table := oracleTable(t, "sling_adml_types")
	dropOracleTable(t, conn, table)
	oracleDDL(t, conn, fmt.Sprintf(`create table %s (
		id number(10) not null,
		big_num number(20,2),
		dec_num number(12,6),
		f_num binary_double,
		vstr varchar2(500),
		chstr char(10),
		d date,
		ts6 timestamp(6),
		ts3 timestamp(3),
		raw16 raw(16),
		note varchar2(100)
	)`, table))

	cols := iop.Columns{
		{Name: "id", Type: iop.BigIntType},
		{Name: "big_num", Type: iop.FloatType},
		{Name: "dec_num", Type: iop.FloatType},
		{Name: "f_num", Type: iop.FloatType},
		{Name: "vstr", Type: iop.StringType},
		{Name: "chstr", Type: iop.StringType},
		{Name: "d", Type: iop.DateType},
		{Name: "ts6", Type: iop.TimestampType},
		{Name: "ts3", Type: iop.TimestampType},
		{Name: "raw16", Type: iop.BinaryType},
		{Name: "note", Type: iop.StringType},
	}

	base := time.Date(2026, 10, 12, 15, 30, 45, 0, time.UTC)
	const rows = 40
	fNums := []float64{1.5e-8, 1.23456789e10, -3.25, 0}
	gen := func(i int) []any {
		vstr := fmt.Sprintf("row-%d-中文-'sq'-\"dq\"-ok", i)
		if i%3 == 0 {
			vstr += "\nsecond line" // binds are binary: newlines must survive
		}
		if i%17 == 0 {
			vstr = strings.Repeat("x", 400) // near the varchar2 ceiling
		}
		var note any
		switch {
		case i%2 == 0:
			note = nil
		case i%4 == 1:
			note = "" // Oracle folds '' to NULL on every path
		default:
			note = fmt.Sprintf("note %d", i)
		}
		var vstrAny any = vstr
		if i%5 == 0 {
			vstrAny = nil
		}
		id := i
		if i == rows-1 {
			id = 9999999999 // number(10) ceiling
		}
		return []any{
			id,
			float64(i)*100 - 0.5,
			float64(i) / 7,
			fNums[i%4],
			vstrAny,
			fmt.Sprintf("%d", i),
			base.Add(time.Duration(i) * time.Hour).Truncate(time.Second),
			base.Add(time.Duration(i) * 137 * time.Microsecond),
			base.Add(time.Duration(i) * 11 * time.Millisecond),
			[]byte(fmt.Sprintf("%016d", i)),
			note,
		}
	}

	count, err := conn.ArrayDMLStream(table, oracleGenStream(t, g.NewContext(context.Background()), cols, rows, gen))
	require.NoError(t, err)
	assert.EqualValues(t, rows, count)

	data, err := conn.Self().Query(fmt.Sprintf("select * from %s order by id", table))
	require.NoError(t, err)
	recs := data.Records()
	require.Len(t, recs, rows)

	for i, rec := range recs {
		wantID := i
		if i == rows-1 {
			wantID = 9999999999
		}
		assert.Equal(t, cast.ToFloat64(wantID), cast.ToFloat64(rec["id"]), "id row %d", i)
		assert.InDelta(t, float64(i)*100-0.5, cast.ToFloat64(rec["big_num"]), 0.01, "big_num row %d", i)
		assert.InDelta(t, float64(i)/7, cast.ToFloat64(rec["dec_num"]), 1e-4, "dec_num row %d", i)
		assert.InDelta(t, fNums[i%4], cast.ToFloat64(rec["f_num"]), 1e-6, "f_num row %d", i)

		if i%5 == 0 {
			assert.Nil(t, rec["vstr"], "vstr should be NULL row %d", i)
		} else {
			want := fmt.Sprintf("row-%d-中文-'sq'-\"dq\"-ok", i)
			if i%3 == 0 {
				want += "\nsecond line"
			}
			if i%17 == 0 {
				want = strings.Repeat("x", 400)
			}
			assert.Equal(t, want, rec["vstr"], "vstr row %d", i)
		}
		assert.Equal(t, fmt.Sprintf("%-10s", fmt.Sprintf("%d", i)), rec["chstr"], "char padding row %d", i)

		d, ok := rec["d"].(time.Time)
		require.True(t, ok, "date row %d: %#v", i, rec["d"])
		assert.WithinDuration(t, base.Add(time.Duration(i)*time.Hour).Truncate(time.Second), d, 90*time.Second, "date row %d", i)
		ts6, ok := rec["ts6"].(time.Time)
		require.True(t, ok, "ts6 row %d: %#v", i, rec["ts6"])
		assert.WithinDuration(t, base.Add(time.Duration(i)*137*time.Microsecond), ts6, time.Second, "ts6 row %d", i)

		raw, ok := rec["raw16"].([]byte)
		require.True(t, ok, "raw16 row %d: %#v", i, rec["raw16"])
		assert.Equal(t, []byte(fmt.Sprintf("%016d", i)), raw, "raw16 row %d", i)

		// NULL and '' both read back as NULL (Oracle semantics, parity with cursor path)
		if i%2 == 0 || i%4 == 1 {
			assert.Nil(t, rec["note"], "note row %d", i)
		} else {
			assert.Equal(t, fmt.Sprintf("note %d", i), rec["note"], "note row %d", i)
		}
	}
}

// TestOracleArrayDMLBatchBoundaries exercises flush boundaries around the
// 1000-row array batch, including the empty stream.
func TestOracleArrayDMLBatchBoundaries(t *testing.T) {
	conn := newOracleArrayDMLTestConn(t)
	table := oracleTable(t, "sling_adml_bounds")
	dropOracleTable(t, conn, table)
	oracleDDL(t, conn, fmt.Sprintf("create table %s (id number(10), v varchar2(50))", table))

	for _, n := range []int{0, 1, 999, 1000, 1001, 2500} {
		_, err := conn.Self().Exec(fmt.Sprintf("truncate table %s", table))
		require.NoError(t, err)

		stream := oracleGenStream(t, g.NewContext(context.Background()), oracleSimpleColumns(), n,
			func(i int) []any { return []any{i, fmt.Sprintf("v-%d", i)} })

		count, err := conn.ArrayDMLStream(table, stream)
		require.NoError(t, err, "n=%d", n)
		assert.EqualValues(t, n, count, "returned count for n=%d", n)
		assert.Equal(t, n, oracleCount(t, conn, fmt.Sprintf("select count(*) cnt from %s", table)), "db count for n=%d", n)
	}
}

// TestOracleArrayDMLMillionRows is the production-incident guard: a
// million-row load must complete without protocol or memory blowups and
// land every row intact.
func TestOracleArrayDMLMillionRows(t *testing.T) {
	if testing.Short() {
		t.Skip("million-row load skipped in -short mode")
	}
	conn := newOracleArrayDMLTestConn(t)
	table := oracleTable(t, "sling_adml_bulk")
	dropOracleTable(t, conn, table)
	oracleDDL(t, conn, fmt.Sprintf("create table %s (id number(10), amt number(12,2), v varchar2(120), ts timestamp(3))", table))

	cols := iop.Columns{
		{Name: "id", Type: iop.BigIntType},
		{Name: "amt", Type: iop.FloatType},
		{Name: "v", Type: iop.StringType},
		{Name: "ts", Type: iop.TimestampType},
	}
	const total = 1_000_000
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	gen := func(i int) []any {
		return []any{
			i,
			float64(i%100000) / 100,
			fmt.Sprintf("payload-%d-数据-%06d", i, i%999999),
			base.Add(time.Duration(i) * time.Millisecond),
		}
	}

	start := time.Now()
	count, err := conn.ArrayDMLStream(table, oracleGenStream(t, g.NewContext(context.Background()), cols, total, gen))
	elapsed := time.Since(start)
	require.NoError(t, err)
	assert.EqualValues(t, total, count)
	t.Logf("array dml: %d rows in %s (%.0f rows/s)", count, elapsed.Round(time.Millisecond), float64(count)/elapsed.Seconds())

	assert.Equal(t, total, oracleCount(t, conn, fmt.Sprintf("select count(*) cnt from %s", table)))

	data, err := conn.Self().Query(fmt.Sprintf(
		"select sum(id) sum_id, min(id) min_id, max(id) max_id from %s", table))
	require.NoError(t, err)
	rec := data.Records()[0]
	assert.InDelta(t, float64(total)*float64(total-1)/2, cast.ToFloat64(rec["sum_id"]), 1e3)
	assert.Equal(t, 0, cast.ToInt(rec["min_id"]))
	assert.Equal(t, total-1, cast.ToInt(rec["max_id"]))

	// spot check first & last payloads
	sample, err := conn.Self().Query(fmt.Sprintf(
		"select v from %s where id in (0, %d) order by id", table, total-1))
	require.NoError(t, err)
	recs := sample.Records()
	require.Len(t, recs, 2)
	assert.Equal(t, "payload-0-数据-000000", recs[0]["v"])
	assert.Equal(t, fmt.Sprintf("payload-%d-数据-%06d", total-1, (total-1)%999999), recs[1]["v"])
}

// TestOracleArrayDMLLOBFallsBack verifies a LOB target routes to the cursor
// path and still lands CLOB content intact.
func TestOracleArrayDMLLOBFallsBack(t *testing.T) {
	conn := newOracleArrayDMLTestConn(t)
	table := oracleTable(t, "sling_adml_lob")
	dropOracleTable(t, conn, table)
	oracleDDL(t, conn, fmt.Sprintf("create table %s (id number(10), v varchar2(100), c clob)", table))

	cols := iop.Columns{
		{Name: "id", Type: iop.BigIntType},
		{Name: "v", Type: iop.StringType},
		{Name: "c", Type: iop.StringType},
	}
	const total = 1500
	long := strings.Repeat("clob内容-", 60) // ~540 bytes
	count, err := conn.BulkImportStream(table, oracleGenStream(t, g.NewContext(context.Background()), cols, total,
		func(i int) []any {
			return []any{i, fmt.Sprintf("row-%d", i), long}
		}))
	require.NoError(t, err)
	assert.EqualValues(t, total, count)
	assert.Equal(t, total, oracleCount(t, conn, fmt.Sprintf("select count(*) cnt from %s", table)))

	// verify content via SQL length/substr: reading LOB values through the
	// query path needs LOB fetching; length + prefix prove the write landed
	sample, err := conn.Self().Query(fmt.Sprintf(
		"select length(c) len, substr(c, 1, 4) head from %s where id = %d", table, total-1))
	require.NoError(t, err)
	recs := sample.Records()
	require.Len(t, recs, 1)
	assert.Equal(t, utf8.RuneCountInString(long), cast.ToInt(recs[0]["len"]))
	assert.Equal(t, "clob", cast.ToString(recs[0]["head"]))
}

// TestOracleArrayDMLErrorPropagation: a bad row mid-stream must surface the
// driver error; already-committed batches stay (full-refresh loads into a
// temp table in production, so residue is harmless).
func TestOracleArrayDMLErrorPropagation(t *testing.T) {
	conn := newOracleArrayDMLTestConn(t)
	table := oracleTable(t, "sling_adml_err")
	dropOracleTable(t, conn, table)
	oracleDDL(t, conn, fmt.Sprintf("create table %s (id number(10) not null, v varchar2(10))", table))

	const total = 2500
	count, err := conn.ArrayDMLStream(table, oracleGenStream(t, g.NewContext(context.Background()), oracleSimpleColumns(), total,
		func(i int) []any {
			v := fmt.Sprintf("v-%d", i)
			if i == 1500 {
				v = strings.Repeat("x", 20) // ORA-12899 in the second batch
			}
			return []any{i, v}
		}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "array-dml")
	assert.EqualValues(t, 2000, count) // rows counted before the failing flush
	// Oracle array DML is not batch-atomic: rows of the failing batch around
	// the offending one can stay applied (observed 1999 of 2000 landed).
	// Full-refresh flows load into a temp table, so residue never reaches the
	// production table.
	dbCount := oracleCount(t, conn, fmt.Sprintf("select count(*) cnt from %s", table))
	assert.GreaterOrEqual(t, dbCount, 1000, "first committed batch must be present")
	assert.LessOrEqual(t, dbCount, 2000, "failing batch must not fully apply")
}

// TestOracleArrayDMLCancelMidStream cancels the stream context mid-load and
// expects a context error with partial data persisted.
func TestOracleArrayDMLCancelMidStream(t *testing.T) {
	conn := newOracleArrayDMLTestConn(t)
	table := oracleTable(t, "sling_adml_cancel")
	dropOracleTable(t, conn, table)
	oracleDDL(t, conn, fmt.Sprintf("create table %s (id number(10), v varchar2(50))", table))

	gctx := g.NewContext(context.Background())
	go func() {
		time.Sleep(300 * time.Millisecond)
		gctx.Cancel()
	}()

	const total = 200_000
	count, err := conn.ArrayDMLStream(table, oracleGenStream(t, gctx, oracleSimpleColumns(), total,
		func(i int) []any { return []any{i, fmt.Sprintf("v-%d", i)} }))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "context canceled")
	assert.Less(t, count, uint64(total))
}

// TestOracleArrayDMLPropGating: both opt-outs must keep loading correctly
// through the cursor path.
func TestOracleArrayDMLPropGating(t *testing.T) {
	conn := newOracleArrayDMLTestConn(t)
	table := oracleTable(t, "sling_adml_gate")
	dropOracleTable(t, conn, table)
	oracleDDL(t, conn, fmt.Sprintf("create table %s (id number(10), v varchar2(50))", table))

	for _, tc := range []struct {
		name  string
		props map[string]string
	}{
		{"use_array_dml_false", map[string]string{"use_array_dml": "false"}},
		{"allow_bulk_import_false", map[string]string{"allow_bulk_import": "false"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for k, v := range tc.props {
				conn.SetProp(k, v)
				defer conn.SetProp(k, "")
			}
			_, err := conn.Self().Exec(fmt.Sprintf("truncate table %s", table))
			require.NoError(t, err)

			count, err := conn.BulkImportStream(table, oracleGenStream(t, g.NewContext(context.Background()), oracleSimpleColumns(), 100,
				func(i int) []any { return []any{i, fmt.Sprintf("v-%d", i)} }))
			require.NoError(t, err)
			assert.EqualValues(t, 100, count)
			assert.Equal(t, 100, oracleCount(t, conn, fmt.Sprintf("select count(*) cnt from %s", table)))
		})
	}
}
