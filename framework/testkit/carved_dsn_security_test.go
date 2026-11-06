package testkit_test

import (
	"net/url"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/testkit"
	"github.com/lib/pq"
)

func TestCarvedDSNDropsDatabaseOverrides(t *testing.T) {
	for _, query := range []string{
		"dbname=postgres", "database=postgres", "dbname=postgres&dbname=other&database=postgres",
	} {
		t.Run(query, func(t *testing.T) {
			dsn, err := testkit.RewriteDBNameForTest("postgres://u:p@localhost/postgres?sslmode=disable&"+query, "carved_xyz")
			if err != nil {
				t.Fatal(err)
			}
			parsed, err := url.Parse(dsn)
			if err != nil {
				t.Fatal(err)
			}
			if parsed.Path != "/carved_xyz" || parsed.Query().Has("dbname") || parsed.Query().Has("database") || parsed.Query().Get("sslmode") != "disable" {
				t.Fatalf("carved DSN still overrides its database: %s", dsn)
			}
			connection, err := pq.ParseURL(dsn)
			if err != nil || !strings.Contains(connection, "dbname='carved_xyz'") || strings.Contains(connection, "postgres") {
				t.Fatalf("driver connection = %q, %v; want carved_xyz only", connection, err)
			}
		})
	}
}
