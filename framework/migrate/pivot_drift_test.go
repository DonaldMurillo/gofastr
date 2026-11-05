package migrate

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/internal/pgtest"
)

func pivotDriftPlan(local, foreign string, keyType schema.FieldType) Plan {
	return Plan{Registry: secTypeReg{
		"posts": entity.Define("posts", entity.EntityConfig{
			Fields:    []schema.Field{{Name: "id", Type: schema.String}},
			Relations: []entity.Relation{entity.ManyToMany("tags", "tags", "post_tags", local, foreign)},
		}.WithTimestamps(false)),
		"tags": entity.Define("tags", entity.EntityConfig{
			Fields: []schema.Field{{Name: "id", Type: keyType}},
		}.WithTimestamps(false)),
	}}
}

func pivotKeyChangePlan(key string) Plan {
	return pivotKeyChangePlanFor("posts", key)
}

// pivotKeyChangePlanFor names the owning table, so a case-kept table
// ("Posts") proves the rewritten-key lookup folds the name the way the
// Postgres catalog does.
func pivotKeyChangePlanFor(table, key string) Plan {
	plan := pivotDriftPlan("post_id", "tag_id", schema.String)
	registry := plan.Registry.(secTypeReg)
	posts := registry["posts"]
	if table != "posts" {
		config := posts.Config
		config.Table = table
		registry["posts"] = entity.Define("posts", config)
		posts = registry["posts"]
	}
	posts.Config.Fields = append(posts.Config.Fields, schema.Field{Name: "slug", Type: schema.String, Required: true})
	posts.Config.PrimaryKey = []string{key}
	return plan
}

func TestDiffSchemaPostgresPivotKeyChange(t *testing.T) {
	for _, table := range []string{"posts", "Posts"} {
		t.Run(table, func(t *testing.T) { diffPivotKeyChange(t, table) })
	}
}

func diffPivotKeyChange(t *testing.T, table string) {
	database, catalog := mock(t)
	catalog.ExpectQuery("SELECT version").WillReturnRows(sqlmock.NewRows([]string{"version"}).AddRow("PostgreSQL 16"))
	catalog.ExpectQuery("information_schema.columns").WillReturnRows(sqlmock.NewRows([]string{"table", "column", "type"}).
		AddRow(0, "id", "text").AddRow(0, "slug", "text").AddRow(1, "id", "text").
		AddRow(2, "post_id", "text").AddRow(2, "tag_id", "text"))
	catalog.ExpectQuery("con.contype = 'p'").WillReturnRows(sqlmock.NewRows([]string{"table", "constraint", "column"}).
		AddRow(0, "posts_pkey", "id").AddRow(1, "tags_pkey", "id"))
	catalog.ExpectQuery("con.contype = 'f'").WillReturnRows(sqlmock.NewRows([]string{"table", "constraint", "schema", "current", "reference", "column", "key", "action"}))
	catalog.ExpectQuery("con.contype = 'p'").WillReturnRows(sqlmock.NewRows([]string{"table", "constraint", "column"}).
		AddRow(0, "post_tags_pkey", "post_id").AddRow(0, "post_tags_pkey", "tag_id"))
	catalog.ExpectQuery("con.contype = 'f'").WillReturnRows(sqlmock.NewRows([]string{"table", "constraint", "schema", "current", "reference", "column", "key", "action"}).
		AddRow(0, "post_tags_post_id_fkey", "public", true, "posts", "post_id", "id", "c").
		AddRow(0, "post_tags_tag_id_fkey", "public", true, "tags", "tag_id", "id", "c"))
	changes, err := DiffSchema(context.Background(), database, pivotKeyChangePlanFor(table, "slug").Registry)
	if err != nil {
		t.Fatalf("supported primary-key change with generated pivot: %v", err)
	}
	var statements []string
	for _, change := range changes {
		statements = append(statements, change.SQL)
	}
	statement := strings.Join(statements, "\n")
	for _, want := range []string{"DROP CONSTRAINT", "ADD PRIMARY KEY (slug)", "ALTER TABLE post_tags ADD FOREIGN KEY (post_id) REFERENCES " + table + "(slug) ON DELETE CASCADE"} {
		if !strings.Contains(statement, want) {
			t.Errorf("key change omits %q: %s", want, statement)
		}
	}
	if err := catalog.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestDiffSchemaPostgresPivotKeyChangeLive(t *testing.T) {
	database := pgtest.DB(t)
	ctx := context.Background()
	if err := AutoMigrate(database, pivotKeyChangePlan("id").Registry); err != nil {
		t.Fatal(err)
	}
	changed := pivotKeyChangePlan("slug")
	changes, err := DiffSchema(ctx, database, changed.Registry)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ApplySchemaDiffWithOptions(ctx, database, changes, ApplyOptions{AllowDestructive: true}); err != nil {
		t.Fatal(err)
	}
	if changes, err := DiffSchema(ctx, database, changed.Registry); err != nil || len(changes) != 0 {
		t.Fatalf("post-plan pivot constraints have not converged: changes=%v, err=%v", changes, err)
	}
}

func TestPivotDriftRefused(t *testing.T) {
	base := pivotDriftPlan("post_id", "tag_id", schema.String)
	for _, dialect := range []Dialect{DialectSQLite, DialectPostgres} {
		for name, changed := range map[string]Plan{
			"local":   pivotDriftPlan("article_id", "tag_id", schema.String),
			"foreign": pivotDriftPlan("post_id", "label_id", schema.String),
			"type":    pivotDriftPlan("post_id", "tag_id", schema.Int),
		} {
			t.Run(string(dialect)+"/"+name, func(t *testing.T) {
				previous := SnapshotFromPlan(base, dialect)
				if _, _, _, err := GeneratePlan(changed, previous, dialect); err == nil || !strings.Contains(err.Error(), "post_tags") {
					t.Errorf("GeneratePlan = %v, want named pivot drift refusal", err)
				}
				if dialect != DialectSQLite {
					return
				}
				db := openMigrateSQLite(t)
				if err := AutoMigrate(db, base.Registry); err != nil {
					t.Fatal(err)
				}
				if _, err := DiffSchema(context.Background(), db, changed.Registry); err == nil || !strings.Contains(err.Error(), "post_tags") {
					t.Errorf("DiffSchema = %v, want named pivot drift refusal", err)
				}
				if err := AutoMigrate(db, changed.Registry); err == nil || !strings.Contains(err.Error(), "post_tags") {
					t.Errorf("AutoMigrate = %v, want named pivot drift refusal", err)
				}
			})
		}
		if _, _, _, err := GeneratePlan(base, SnapshotFromPlan(base, dialect), dialect); err != nil {
			t.Fatalf("unchanged pivot: %v", err)
		}
	}
}

func TestPivotConstraintDriftRefused(t *testing.T) {
	plan := pivotDriftPlan("post_id", "tag_id", schema.String)
	for name, constraints := range map[string]string{
		"missing primary key":  `FOREIGN KEY (post_id) REFERENCES posts(id) ON DELETE CASCADE, FOREIGN KEY (tag_id) REFERENCES tags(id) ON DELETE CASCADE`,
		"missing foreign keys": `PRIMARY KEY (post_id, tag_id)`,
		"missing cascade":      `PRIMARY KEY (post_id, tag_id), FOREIGN KEY (post_id) REFERENCES posts(id), FOREIGN KEY (tag_id) REFERENCES tags(id) ON DELETE CASCADE`,
		"wrong endpoint":       `PRIMARY KEY (post_id, tag_id), FOREIGN KEY (post_id) REFERENCES tags(id) ON DELETE CASCADE, FOREIGN KEY (tag_id) REFERENCES posts(id) ON DELETE CASCADE`,
	} {
		t.Run(name, func(t *testing.T) {
			database := openMigrateSQLite(t)
			if err := AutoMigrate(database, plan.Registry); err != nil {
				t.Fatal(err)
			}
			if _, err := database.Exec(`DROP TABLE post_tags; CREATE TABLE post_tags (post_id TEXT NOT NULL, tag_id TEXT NOT NULL, ` + constraints + `)`); err != nil {
				t.Fatal(err)
			}
			if changes, err := DiffSchema(context.Background(), database, plan.Registry); err == nil || !strings.Contains(err.Error(), "post_tags") || len(changes) != 0 {
				t.Errorf("DiffSchema = %v, %v; want explicit pivot constraint refusal", changes, err)
			}
			if err := AutoMigrate(database, plan.Registry); err == nil || !strings.Contains(err.Error(), "post_tags") {
				t.Errorf("AutoMigrate = %v; want explicit pivot constraint refusal", err)
			}
		})
	}
}

func TestSharedPivotEndpointDriftRefused(t *testing.T) {
	plan := pivotDriftPlan("post_id", "tag_id", schema.String)
	registry := plan.Registry.(secTypeReg)
	registry["articles"] = entity.Define("articles", entity.EntityConfig{
		Fields:    []schema.Field{{Name: "id", Type: schema.String}},
		Relations: []entity.Relation{entity.ManyToMany("tags", "tags", "post_tags", "post_id", "tag_id")},
	}.WithTimestamps(false))
	for _, dialect := range []Dialect{DialectSQLite, DialectPostgres} {
		if up, _, _, err := GeneratePlan(plan, SchemaSnapshot{}, dialect); err == nil || !strings.Contains(err.Error(), "post_tags") || up != "" {
			t.Errorf("GeneratePlan(%s) = %q, %v; want shared endpoint refusal", dialect, up, err)
		}
	}
	database := openMigrateSQLite(t)
	if _, err := DiffSchema(context.Background(), database, registry); err == nil || !strings.Contains(err.Error(), "post_tags") {
		t.Errorf("DiffSchema = %v; want shared endpoint refusal", err)
	}
	if err := AutoMigrate(database, registry); err == nil || !strings.Contains(err.Error(), "post_tags") {
		t.Errorf("AutoMigrate = %v; want shared endpoint refusal", err)
	}
}

func reciprocalPivotDriftPlan(local, foreign string) Plan {
	return Plan{Registry: secTypeReg{
		"posts": entity.Define("posts", entity.EntityConfig{
			Fields:    []schema.Field{{Name: "id", Type: schema.String}},
			Relations: []entity.Relation{entity.ManyToMany("tags", "tags", "post_tags", "post_id", "tag_id")},
		}.WithTimestamps(false)),
		"tags": entity.Define("tags", entity.EntityConfig{
			Fields:    []schema.Field{{Name: "id", Type: schema.Int}},
			Relations: []entity.Relation{entity.ManyToMany("posts", "posts", "post_tags", local, foreign)},
		}.WithTimestamps(false)),
	}}
}

func TestReciprocalPivotDriftRefused(t *testing.T) {
	base := reciprocalPivotDriftPlan("tag_id", "post_id")
	for _, test := range []struct {
		name, local, foreign string
	}{
		{name: "local column", local: "label_id", foreign: "post_id"},
		{name: "target column", local: "tag_id", foreign: "article_id"},
		{name: "endpoint types", local: "post_id", foreign: "tag_id"},
	} {
		t.Run(test.name, func(t *testing.T) {
			changed := reciprocalPivotDriftPlan(test.local, test.foreign)
			for _, dialect := range []Dialect{DialectSQLite, DialectPostgres} {
				for _, initial := range []bool{false, true} {
					t.Run(string(dialect)+"/initial="+strconv.FormatBool(initial), func(t *testing.T) {
						previous := SchemaSnapshot{}
						if !initial {
							previous = SnapshotFromPlan(base, dialect)
						}
						if up, down, _, err := GeneratePlan(changed, previous, dialect); err == nil || !strings.Contains(err.Error(), "post_tags") || up != "" || down != "" {
							t.Errorf("GeneratePlan = (%q, %q, %v), want named shared-pivot refusal without SQL", up, down, err)
						}
					})
				}
			}
			for _, initial := range []bool{false, true} {
				db := openMigrateSQLite(t)
				if !initial {
					if err := AutoMigrate(db, base.Registry); err != nil {
						t.Fatal(err)
					}
				}
				if changes, err := DiffSchema(context.Background(), db, changed.Registry); err == nil || !strings.Contains(err.Error(), "post_tags") || len(changes) != 0 {
					t.Errorf("DiffSchema initial=%v = (%v, %v), want named shared-pivot refusal", initial, changes, err)
				}
				if err := AutoMigrate(db, changed.Registry); err == nil || !strings.Contains(err.Error(), "post_tags") {
					t.Errorf("AutoMigrate initial=%v = %v, want named shared-pivot refusal", initial, err)
				}
				if initial {
					var tables int
					if err := db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name IN ('posts', 'tags', 'post_tags')").Scan(&tables); err != nil || tables != 0 {
						t.Errorf("refused initial boot created %d entity tables, error = %v", tables, err)
					}
				}
			}
		})
	}
}

func TestReciprocalPivotDeclarationsAgree(t *testing.T) {
	plan := reciprocalPivotDriftPlan("tag_id", "post_id")
	for _, dialect := range []Dialect{DialectSQLite, DialectPostgres} {
		up, _, next, err := GeneratePlan(plan, SchemaSnapshot{}, dialect)
		if err != nil || strings.Count(up, "CREATE TABLE IF NOT EXISTS post_tags (") != 1 {
			t.Fatalf("initial %s plan = (%q, %v), want one pivot creation", dialect, up, err)
		}
		if up, down, _, err := GeneratePlan(plan, next, dialect); err != nil || up != "" || down != "" {
			t.Fatalf("unchanged %s plan = (%q, %q, %v)", dialect, up, down, err)
		}
	}
	db := openMigrateSQLite(t)
	if err := AutoMigrate(db, plan.Registry); err != nil {
		t.Fatal(err)
	}
	if changes, err := DiffSchema(context.Background(), db, plan.Registry); err != nil || len(changes) != 0 {
		t.Fatalf("unchanged live diff = (%v, %v)", changes, err)
	}
	if err := AutoMigrate(db, plan.Registry); err != nil {
		t.Fatalf("unchanged boot: %v", err)
	}
	for _, statement := range []string{
		"INSERT INTO posts (id) VALUES ('post-1')",
		"INSERT INTO tags (id) VALUES (1)",
		"INSERT INTO post_tags (post_id, tag_id) VALUES ('post-1', 1)",
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM tags JOIN post_tags ON tags.id = post_tags.tag_id JOIN posts ON posts.id = post_tags.post_id").Scan(&count); err != nil || count != 1 {
		t.Fatalf("reciprocal join count = %d, error = %v", count, err)
	}
}
