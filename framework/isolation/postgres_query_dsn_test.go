package isolation

import (
	"database/sql"
	"net/url"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/pgtest"
	"github.com/lib/pq"
)

func TestPostgresQueryDatabaseIsIsolated(t *testing.T) {
	clearIsolationEnv(t)
	rt, err := Resolve(linkedWorktree(t))
	if err != nil {
		t.Fatal(err)
	}
	db := pgtest.DB(t)
	var database, schema string
	if err := db.QueryRow(`SELECT current_database(), current_schema()`).Scan(&database, &schema); err != nil {
		t.Fatal(err)
	}
	base, err := url.Parse(pgtest.BaseDSN(t))
	if err != nil || (base.Scheme != "postgres" && base.Scheme != "postgresql") {
		t.Skip("requires a PostgreSQL URL")
	}

	for _, tc := range []struct {
		name, path, key, value string
	}{
		{"query-only", "/", "dbname", database},
		{"query-alias", "/other", "database", database},
		{"path-and-query", "/" + database, "dbname", "0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			u := *base
			u.Path, u.RawPath = tc.path, ""
			q := u.Query()
			q.Del("dbname")
			q.Del("database")
			q.Set(tc.key, tc.value)
			q.Set("application_name", "isolation & parity")
			u.RawQuery = q.Encode()
			dsn, err := pgtest.DSNWithSearchPath(u.String(), schema)
			if err != nil {
				t.Fatal(err)
			}
			source, err := sql.Open("postgres", dsn)
			if err != nil {
				t.Fatal(err)
			}
			defer source.Close()
			var connectedDB, connectedSchema string
			if err := source.QueryRowContext(t.Context(), `SELECT current_database(), current_schema()`).Scan(&connectedDB, &connectedSchema); err != nil {
				t.Fatal(err)
			}
			if connectedDB != database || connectedSchema != schema {
				t.Fatalf("source selected %q.%q, want %q.%q", connectedDB, connectedSchema, database, schema)
			}

			want := database + "_" + rt.ID()
			if len(want) > 63 {
				t.Skip("the live database name is too long for this fixture")
			}
			driver, rewritten, err := rt.Database("", dsn)
			if err != nil || driver != "postgres" {
				t.Fatalf("Database driver = %q, err = %v", driver, err)
			}
			cfg, err := pq.NewConfig(rewritten)
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Database != want || cfg.ApplicationName != "isolation & parity" || cfg.Runtime["search_path"] != schema {
				t.Errorf("rewritten database = %q, application = %q, schema = %q; want %q, isolation & parity, %q",
					cfg.Database, cfg.ApplicationName, cfg.Runtime["search_path"], want, schema)
			}
			if got := envMap(rt.Env([]string{"DATABASE_URL=" + dsn}))["DATABASE_URL"]; got != rewritten {
				t.Error("Env and Database rewrote the database URL differently")
			}
			if _, twice, err := rt.Database(driver, rewritten); err != nil || twice != rewritten {
				t.Errorf("Database changed the already-isolated URL or returned an error: %v", err)
			}
		})
	}
}
