package server

// conns_io_test.go covers connection import (preview + commit) and export,
// including the secret-masking invariants: previews and exports never
// contain secret values, imports treat ****** as "no value provided".

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

const importSampleYAML = `connections:
  my pg:
    type: postgres
    host: 127.0.0.1
    port: 5432
    username: admin
    password: s3cr3t
  ora masked:
    type: oracle
    host: db.example.com
    port: 1521
    username: scott
    password: '******'
  broken:
    type: not_a_db
    host: x
`

func findEntry(t *testing.T, entries []ImportEntry, name string) ImportEntry {
	t.Helper()
	for _, e := range entries {
		if e.Name == name {
			return e
		}
	}
	t.Fatalf("entry %s not found in %+v", name, entries)
	return ImportEntry{}
}

func TestImportPreviewContent(t *testing.T) {
	app := newTestApp(t)

	entries, err := app.Conns.PreviewImportContent(importSampleYAML)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("want 3 entries, got %d: %+v", len(entries), entries)
	}

	pg := findEntry(t, entries, "MY_PG")
	if !pg.Valid || pg.Exists {
		t.Fatalf("MY_PG should be valid+new: %+v", pg)
	}
	if pg.Type != "postgres" {
		t.Fatalf("MY_PG type = %q", pg.Type)
	}
	if len(pg.SecretFields) != 1 || pg.SecretFields[0] != "password" {
		t.Fatalf("MY_PG secret fields = %+v", pg.SecretFields)
	}
	if pg.MaskedSecret {
		t.Fatal("MY_PG password is real, not masked")
	}

	ora := findEntry(t, entries, "ORA_MASKED")
	if !ora.Valid || !ora.MaskedSecret {
		t.Fatalf("ORA_MASKED should be valid+masked: %+v", ora)
	}

	broken := findEntry(t, entries, "BROKEN")
	if broken.Valid || broken.Error == "" {
		t.Fatalf("BROKEN should be invalid with an error: %+v", broken)
	}

	// previews must never contain the secret value
	b, _ := json.Marshal(entries)
	if strings.Contains(string(b), "s3cr3t") {
		t.Fatalf("preview leaked a secret: %s", b)
	}
}

func TestImportContentCommitAndConflicts(t *testing.T) {
	app := newTestApp(t)

	imported, skipped, err := app.Conns.ImportContent(importSampleYAML, []string{"my pg"}, "skip")
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if len(imported) != 1 || imported[0] != "MY_PG" || len(skipped) != 0 {
		t.Fatalf("imported=%v skipped=%v", imported, skipped)
	}

	rec, err := app.Conns.Get("MY_PG")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if rec.Props()["host"] != "127.0.0.1" {
		t.Fatalf("props: %+v", rec.Props())
	}
	if strings.Contains(rec.PropsJSON, "s3cr3t") {
		t.Fatal("plaintext password must not be stored in props")
	}
	if sec, _ := app.Vault.LoadSecret(secretPath("MY_PG", "password")); sec != "s3cr3t" {
		t.Fatalf("password not in vault: %q", sec)
	}

	// re-import with skip: skipped, record untouched
	imported, skipped, _ = app.Conns.ImportContent(importSampleYAML, []string{"my pg"}, "skip")
	if len(imported) != 0 || len(skipped) != 1 || skipped[0] != "MY_PG" {
		t.Fatalf("skip conflict: imported=%v skipped=%v", imported, skipped)
	}

	// overwrite with a masked password: non-secret props replaced, stored secret kept
	overwriteYAML := "connections:\n  my pg:\n    type: postgres\n    host: newhost\n    port: 5432\n    username: admin\n    password: '******'\n"
	imported, skipped, err = app.Conns.ImportContent(overwriteYAML, []string{"my pg"}, "overwrite")
	if err != nil {
		t.Fatalf("overwrite: %v", err)
	}
	if len(imported) != 1 || len(skipped) != 0 {
		t.Fatalf("overwrite imported=%v skipped=%v", imported, skipped)
	}
	rec, _ = app.Conns.Get("MY_PG")
	if rec.Props()["host"] != "newhost" {
		t.Fatalf("overwrite did not update props: %+v", rec.Props())
	}
	if sec, _ := app.Vault.LoadSecret(secretPath("MY_PG", "password")); sec != "s3cr3t" {
		t.Fatal("masked password must keep the stored secret")
	}

	// preview now flags it as existing
	entries, _ := app.Conns.PreviewImportContent(importSampleYAML)
	if e := findEntry(t, entries, "MY_PG"); !e.Exists {
		t.Fatal("MY_PG should be flagged as existing after import")
	}
}

func TestImportContentBareForms(t *testing.T) {
	app := newTestApp(t)

	// bare top-level map (no connections: wrapper) + bare URL entry;
	// an unrelated env block must not be mistaken for a connection
	bare := "my pg:\n  type: postgres\n  host: h\n  port: 5432\n  username: u\n  password: p\nmy sqlite: sqlite://C:/data/db.sqlite\nenv:\n  VAR: 1\n"
	entries, err := app.Conns.PreviewImportContent(bare)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("want 2 entries (env block ignored), got %+v", entries)
	}
	pg := findEntry(t, entries, "MY_PG")
	if !pg.Valid || len(pg.SecretFields) != 1 {
		t.Fatalf("MY_PG: %+v", pg)
	}
	lite := findEntry(t, entries, "MY_SQLITE")
	if !lite.Valid || lite.Type != "sqlite" {
		t.Fatalf("MY_SQLITE should resolve via URL scheme: %+v", lite)
	}
}

func TestExportConnsMasksSecrets(t *testing.T) {
	app := newTestApp(t)
	_, err := app.Conns.Upsert(UpsertInput{
		Name:        "EXP",
		Type:        "postgres",
		Description: "export test",
		Tags:        []string{"team-a"},
		Props:       map[string]any{"host": "h1", "port": "5432", "username": "u", "password": "topsecret"},
	})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}

	for _, format := range []string{"yaml", "json"} {
		name, body, count, err := app.Conns.ExportConns(format)
		if err != nil {
			t.Fatalf("export %s: %v", format, err)
		}
		if count != 1 || name != "connections."+format {
			t.Fatalf("export %s: name=%s count=%d", format, name, count)
		}
		if strings.Contains(body, "topsecret") {
			t.Fatalf("export %s leaked the secret:\n%s", format, body)
		}
		if !strings.Contains(body, secretMask) {
			t.Fatalf("export %s must mask the password:\n%s", format, body)
		}
		if !strings.Contains(body, "h1") || !strings.Contains(body, "export test") {
			t.Fatalf("export %s missing props/description:\n%s", format, body)
		}

		// round-trip: the export previews as existing; the masked password
		// is flagged (not listed as an importable secret)
		entries, err := app.Conns.PreviewImportContent(body)
		if err != nil {
			t.Fatalf("re-preview %s: %v", format, err)
		}
		e := findEntry(t, entries, "EXP")
		if !e.Exists || !e.Valid || !e.MaskedSecret {
			t.Fatalf("round-trip %s: %+v", format, e)
		}
	}
}

func TestConnsImportExportHandlers(t *testing.T) {
	app := newTestApp(t)
	ts := httptest.NewServer(app.Handler())
	defer ts.Close()

	// preview via HTTP
	code, body := apiPost(t, ts.URL, "/api/conns/import", map[string]any{"content": importSampleYAML, "dry_run": true})
	if code != 200 || !strings.Contains(body, `"name":"MY_PG"`) {
		t.Fatalf("preview: %d %s", code, body)
	}
	if strings.Contains(body, "s3cr3t") {
		t.Fatalf("preview response leaked a secret: %s", body)
	}

	// commit via HTTP
	code, body = apiPost(t, ts.URL, "/api/conns/import", map[string]any{"content": importSampleYAML, "names": []string{"my pg"}})
	if code != 200 || !strings.Contains(body, `"imported":["MY_PG"]`) {
		t.Fatalf("commit: %d %s", code, body)
	}

	// export via HTTP (exact route must beat /api/conns/{name})
	code, body = apiGet(t, ts.URL, "/api/conns/export?format=yaml")
	if code != 200 || !strings.Contains(body, secretMask) || strings.Contains(body, "s3cr3t") {
		t.Fatalf("export: %d %s", code, body)
	}
	code, body = apiGet(t, ts.URL, "/api/conns/export?format=xml")
	if code != 400 {
		t.Fatalf("bad format must 400, got %d", code)
	}

	// invalid body / on_conflict rejected
	code, _ = apiPost(t, ts.URL, "/api/conns/import", map[string]any{"dry_run": true})
	if code != 400 {
		t.Fatalf("dry-run without content must 400, got %d", code)
	}
	code, _ = apiPost(t, ts.URL, "/api/conns/import", map[string]any{"content": "a: 1", "on_conflict": "nuke"})
	if code != 400 {
		t.Fatalf("bad on_conflict must 400, got %d", code)
	}

	// legacy body-less call still works (env scan; nothing importable in
	// the test home, but machine env vars may surface conns)
	code, body = apiPost(t, ts.URL, "/api/conns/import", nil)
	if code != 200 || !strings.Contains(body, `"imported"`) {
		t.Fatalf("legacy env import: %d %s", code, body)
	}
}
