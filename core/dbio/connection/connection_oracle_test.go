package connection

// connection_oracle_test.go — 回归测试：oracle URL 的 path 形式与 service_name
// 参数形式都必须能连上（path 被 connection.setURL 归入 `sid` prop 后，必须按
// service name 语义传给 go-ora，否则 ORA-12505）。由 ORACLE_URL 环境变量门控，
// 形如 oracle://user:pass@host:port/SERVICE。

import (
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/flarco/g"
)

func TestOracleURLFormsConnect(t *testing.T) {
	origURL := os.Getenv("ORACLE_URL")
	if origURL == "" {
		t.Skip("ORACLE_URL not set")
	}

	u, err := url.Parse(origURL)
	if err != nil {
		t.Fatalf("could not parse ORACLE_URL: %v", err)
	}
	service := strings.TrimPrefix(u.Path, "/")
	if service == "" {
		t.Fatal("ORACLE_URL must carry a service in its path (oracle://host:port/SERVICE)")
	}

	// 参数形式：oracle://host:port/?service_name=SERVICE
	u2 := *u
	u2.Path = "/"
	u2.RawQuery = "service_name=" + url.QueryEscape(service)
	forms := map[string]string{
		"path":         origURL,
		"service_name": u2.String(),
	}

	for name, connURL := range forms {
		entry, err := NewConnectionFromURL(g.F("ORA_PROBE_%s", strings.ToUpper(name)), connURL)
		if err != nil {
			t.Errorf("[%s] NewConnectionFromURL(%s) error: %v", name, connURL, err)
			continue
		}
		conn, err := entry.AsDatabase()
		if err != nil {
			t.Errorf("[%s] AsDatabase error: %v", name, err)
			continue
		}
		if err = conn.Connect(10); err != nil {
			t.Errorf("[%s] %s connect error: %v", name, connURL, err)
			conn.Close()
			continue
		}
		data, err := conn.Query("select 1 as ok from dual")
		if err != nil || len(data.Rows) == 0 {
			t.Errorf("[%s] query error: %v", name, err)
		} else {
			t.Logf("[%s] OK val=%v", name, data.Rows[0][0])
		}
		conn.Close()
	}
}
