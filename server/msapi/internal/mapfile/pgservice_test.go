package mapfile

import (
	"os"
	"strings"
	"testing"

	"msapi/internal/model"
)

func TestPGService(t *testing.T) {
	dir := t.TempDir()
	stores := map[string]model.PostGISConn{
		"zeta": {PostGISConfig: model.PostGISConfig{Host: "db2", Port: 5433, Database: "g", User: "r", SSLMode: "require"}},
		"city_db": {
			PostGISConfig: model.PostGISConfig{Host: "postgres", Port: 5432, Database: "gisdb", User: "map_reader", SSLMode: "prefer"},
			Password:      "pw",
		},
	}
	if err := WritePGService(dir, stores, -1); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(PGServicePath(dir))
	if err != nil {
		t.Fatal(err)
	}
	s := string(got)
	if strings.Index(s, "[city_db]") > strings.Index(s, "[zeta]") {
		t.Error("sections should be sorted")
	}
	for _, want := range []string{"host=postgres\n", "port=5432\n", "dbname=gisdb\n", "user=map_reader\n",
		"password=pw\n", "options=-c default_transaction_read_only=on\n", "sslmode=require\n"} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in:\n%s", want, s)
		}
	}
	if strings.Count(s, "password=") != 1 {
		t.Error("store without password must not get a password line")
	}
}
