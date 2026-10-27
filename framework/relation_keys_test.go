package framework

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/hook"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
	"github.com/DonaldMurillo/gofastr/framework/owner"
)

// relApp migrates ents and serves them as the user named by X-User.
func relApp(t *testing.T, db *sql.DB, ents ...*entity.Entity) *App {
	t.Helper()
	reg := NewRegistry()
	for _, e := range ents {
		if err := reg.Register(e); err != nil {
			t.Fatal(err)
		}
	}
	if err := AutoMigrate(db, reg); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
	app := NewApp(WithDB(db), WithoutDefaultMiddleware())
	app.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := r.Header.Get("X-User")
			if id == "" {
				id = "test-user"
			}
			next.ServeHTTP(w, r.WithContext(handler.SetUser(r.Context(), relUser(id))))
		})
	})
	for _, e := range ents {
		app.Entity(e.GetName(), e.Config)
	}
	return app
}

type relUser string

func (u relUser) GetID() string { return string(u) }

// relDo serves one request as user and returns its decoded "data".
func relDo(t *testing.T, app *App, user, method, path string, body any) any {
	t.Helper()
	rec := serveKeyedAs(t, app, user, method, path, body)
	if rec.Code >= 300 {
		t.Fatalf("%s %s = %d %s", method, path, rec.Code, rec.Body)
	}
	var env struct{ Data any }
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	return env.Data
}

func serveKeyedAs(t *testing.T, app *App, user, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	if user != "" {
		req.Header.Set("X-User", user)
	}
	rec := httptest.NewRecorder()
	app.Router().ServeHTTP(rec, req)
	return rec
}

func relID(t *testing.T, app *App, path string, body map[string]any) string {
	t.Helper()
	data, _ := relDo(t, app, "", "POST", path, body).(map[string]any)
	return fmt.Sprint(data["id"])
}

// relList returns field of every row in rows, sorted.
func relList(rows any, field string) []string {
	var out []string
	switch list := rows.(type) {
	case []any:
		for _, r := range list {
			out = append(out, fmt.Sprint(relField(r, field)))
		}
	case []map[string]any:
		for _, r := range list {
			out = append(out, fmt.Sprint(r[field]))
		}
	}
	slices.Sort(out)
	return out
}

func relField(v any, path ...string) any {
	for _, p := range path {
		m, _ := v.(map[string]any)
		v = m[p]
	}
	return v
}

func schoolEntities(enrollmentScope *entity.ScopeConfig) []*entity.Entity {
	fields := []schema.Field{
		{Name: "student_id", Type: schema.String, Required: true},
		{Name: "course_id", Type: schema.String, Required: true},
		{Name: "grade", Type: schema.String},
	}
	if enrollmentScope != nil && enrollmentScope.OwnerField != "" {
		fields = append(fields, schema.Field{Name: enrollmentScope.OwnerField, Type: schema.String})
	}
	return []*entity.Entity{
		entity.Define("student", entity.EntityConfig{
			Table:  "students",
			Fields: []schema.Field{{Name: "name", Type: schema.String}},
			Relations: []entity.Relation{
				entity.HasMany("enrollments", "enrollment", "student_id"),
				entity.ManyToMany("courses", "course", "enrollments", "student_id", "course_id"),
			},
		}.WithTimestamps(false)),
		entity.Define("course", entity.EntityConfig{
			Table:  "courses",
			Fields: []schema.Field{{Name: "title", Type: schema.String}},
		}.WithTimestamps(false)),
		entity.Define("enrollment", entity.EntityConfig{
			Table:      "enrollments",
			Fields:     fields,
			PrimaryKey: []string{"student_id", "course_id"},
			Relations: []entity.Relation{
				entity.BelongsTo("student", "student", "student_id"),
				entity.BelongsTo("course", "course", "course_id"),
			},
			Scope: enrollmentScope,
		}.WithTimestamps(false)),
	}
}

// A join entity with a payload column is an entity in its own right: it
// includes both ends, each end includes it, and a many_to_many reads
// through it.
func TestJoinEntityRelations(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := relApp(t, db, schoolEntities(nil)...)
		s1 := relID(t, app, "/students", map[string]any{"name": "Ann"})
		math := relID(t, app, "/courses", map[string]any{"title": "Math"})
		art := relID(t, app, "/courses", map[string]any{"title": "Art"})
		relDo(t, app, "", "POST", "/enrollments", map[string]any{"student_id": s1, "course_id": math, "grade": "A"})
		relDo(t, app, "", "POST", "/enrollments", map[string]any{"student_id": s1, "course_id": art, "grade": "B"})

		one := relDo(t, app, "", "GET", "/enrollments/"+s1+"/"+math+"?include=student,course", nil)
		if got := relField(one, "course", "title"); got != "Math" {
			t.Errorf("enrollment course = %v, want Math", got)
		}
		if got := relField(one, "student", "name"); got != "Ann" {
			t.Errorf("enrollment student = %v, want Ann", got)
		}
		rows, _ := relDo(t, app, "", "GET", "/enrollments?include=course", nil).([]any)
		var titles []string
		for _, r := range rows {
			titles = append(titles, fmt.Sprint(relField(r, "course", "title")))
		}
		slices.Sort(titles)
		if !slices.Equal(titles, []string{"Art", "Math"}) {
			t.Errorf("list include course = %v", titles)
		}

		enr := relField(relDo(t, app, "", "GET", "/students/"+s1+"?include=enrollments.course", nil), "enrollments")
		if got := relList(enr, "grade"); !slices.Equal(got, []string{"A", "B"}) {
			t.Errorf("student enrollments = %v", got)
		}
		courses := relField(relDo(t, app, "", "GET", "/students/"+s1+"?include=courses", nil), "courses")
		if got := relList(courses, "title"); !slices.Equal(got, []string{"Art", "Math"}) {
			t.Errorf("student courses = %v", got)
		}

		for path, want := range map[string]int{
			"/enrollments?student.name=Ann&grade=A": 1,
			"/enrollments?course.title=Nope":        0,
			"/students?enrollments.grade=B":         1,
			"/students?enrollments.grade=Z":         0,
			"/students?courses.title=Art":           1,
			"/students?courses.title=Nope":          0,
		} {
			if got := len(relDo(t, app, "", "GET", path, nil).([]any)); got != want {
				t.Errorf("GET %s = %d rows, want %d", path, got, want)
			}
		}
	})
}

// An AfterList hook that replaces a composite-keyed child's rows folds
// back onto the included rows by their key tuple.
func TestCompositeChildIncludeHook(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := relApp(t, db, schoolEntities(nil)...)
		app.HookRegistry("enrollment").RegisterHook(hook.AfterList, func(_ context.Context, data any) error {
			if p, ok := data.(*hook.ListPayload); ok {
				for i, row := range p.Results {
					p.Results[i] = map[string]any{"studentId": row["studentId"], "courseId": row["courseId"], "grade": "MASKED"}
				}
			}
			return nil
		})
		s1 := relID(t, app, "/students", map[string]any{"name": "Ann"})
		for _, title := range []string{"Math", "Art"} {
			c := relID(t, app, "/courses", map[string]any{"title": title})
			relDo(t, app, "", "POST", "/enrollments", map[string]any{"student_id": s1, "course_id": c, "grade": "A"})
		}
		enr := relField(relDo(t, app, "", "GET", "/students/"+s1+"?include=enrollments", nil), "enrollments")
		if got := relList(enr, "grade"); !slices.Equal(got, []string{"MASKED", "MASKED"}) {
			t.Errorf("included grades = %v, want both masked", got)
		}
	})
}

// wireSchool is schoolEntities with enrollment's key published as
// learner and class.
func wireSchool() []*entity.Entity {
	ents := schoolEntities(nil)
	ents[2] = entity.Define("enrollment", entity.EntityConfig{
		Table: "enrollments",
		Fields: []schema.Field{
			{Name: "student_id", Type: schema.String, Required: true, WireName: "learner"},
			{Name: "course_id", Type: schema.String, Required: true, WireName: "class"},
			{Name: "grade", Type: schema.String},
		},
		PrimaryKey: []string{"student_id", "course_id"},
		Relations:  []entity.Relation{entity.BelongsTo("student", "student", "student_id")},
	}.WithTimestamps(false))
	return ents
}

// The fold-back reads the child's key under the child's own wire names.
func TestCompositeChildHookWireNames(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := relApp(t, db, wireSchool()...)
		app.HookRegistry("enrollment").RegisterHook(hook.AfterList, func(_ context.Context, data any) error {
			if p, ok := data.(*hook.ListPayload); ok {
				for i, row := range p.Results {
					p.Results[i] = map[string]any{"learner": row["learner"], "class": row["class"], "grade": "MASKED"}
				}
			}
			return nil
		})
		s1 := relID(t, app, "/students", map[string]any{"name": "Ann"})
		c := relID(t, app, "/courses", map[string]any{"title": "Math"})
		relDo(t, app, "", "POST", "/enrollments", map[string]any{"learner": s1, "class": c, "grade": "A"})
		enr := relField(relDo(t, app, "", "GET", "/students/"+s1+"?include=enrollments", nil), "enrollments")
		if got := relList(enr, "grade"); !slices.Equal(got, []string{"MASKED"}) {
			t.Errorf("included grades = %v, want masked", got)
		}
		// One hop deeper, the rows still carry enrollment's own names.
		one := relDo(t, app, "", "GET", "/enrollments/"+s1+"/"+c+"?include=student.enrollments", nil)
		nested := relField(one, "student", "enrollments")
		if got := relList(nested, "learner"); !slices.Equal(got, []string{s1}) {
			t.Errorf("nested learner = %v, want [%s]", got, s1)
		}
		if got := relList(nested, "grade"); !slices.Equal(got, []string{"MASKED"}) {
			t.Errorf("nested grades = %v, want masked", got)
		}
		// Two mentions of one relation, only the second going deeper.
		inc := "student(name=Ann),student(name_like=Ann).enrollments"
		rec := serveKeyedAs(t, app, "", "GET", "/enrollments/"+s1+"/"+c+"?include="+inc, nil)
		if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), `"studentId"`) {
			t.Errorf("include=%s = %d %s", inc, rec.Code, rec.Body)
		}
	})
}

// Two mentions of one relation whose subtrees differ only below a
// child they share convert every depth with its own target's names.
func TestNestedIncludeWireNames(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := relApp(t, db, wireSchool()...)
		s1 := relID(t, app, "/students", map[string]any{"name": "Ann"})
		c := relID(t, app, "/courses", map[string]any{"title": "Math"})
		relDo(t, app, "", "POST", "/enrollments", map[string]any{"learner": s1, "class": c, "grade": "A"})
		inc := "student.enrollments(grade=A).student,student.enrollments(grade_like=A).student.enrollments"
		rec := serveKeyedAs(t, app, "", "GET", "/enrollments/"+s1+"/"+c+"?include="+inc, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("include=%s = %d %s", inc, rec.Code, rec.Body)
		}
		var env struct{ Data any }
		if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
			t.Fatal(err)
		}
		var deep []string
		for _, e := range relField(env.Data, "student", "enrollments").([]any) {
			if inner, ok := relField(e, "student", "enrollments").([]any); ok {
				for _, d := range inner {
					deep = append(deep, fmt.Sprint(relField(d, "learner")))
				}
			}
		}
		if len(deep) == 0 || slices.Contains(deep, "<nil>") {
			t.Errorf("enrollments three hops down = %v, want each under learner: %s", deep, rec.Body)
		}
	})
}

// A three-way association keyed by all three of its links.
func TestTernaryEntityRelations(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		named := func(name, table string, rels ...entity.Relation) *entity.Entity {
			return entity.Define(name, entity.EntityConfig{
				Table:     table,
				Fields:    []schema.Field{{Name: "name", Type: schema.String}},
				Relations: rels,
			}.WithTimestamps(false))
		}
		member := entity.Define("membership", entity.EntityConfig{
			Table: "memberships",
			Fields: []schema.Field{
				{Name: "user_id", Type: schema.String, Required: true},
				{Name: "project_id", Type: schema.String, Required: true},
				{Name: "role_id", Type: schema.String, Required: true},
			},
			PrimaryKey: []string{"user_id", "project_id", "role_id"},
			Relations: []entity.Relation{
				entity.BelongsTo("user", "member", "user_id"),
				entity.BelongsTo("project", "project", "project_id"),
				entity.BelongsTo("role", "role", "role_id"),
			},
		}.WithTimestamps(false))
		app := relApp(t, db,
			named("member", "members", entity.HasMany("memberships", "membership", "user_id")),
			named("project", "projects"),
			named("role", "roles"),
			member,
		)
		u := relID(t, app, "/members", map[string]any{"name": "Ann"})
		p := relID(t, app, "/projects", map[string]any{"name": "Apollo"})
		admin := relID(t, app, "/roles", map[string]any{"name": "admin"})
		dev := relID(t, app, "/roles", map[string]any{"name": "dev"})
		relDo(t, app, "", "POST", "/memberships", map[string]any{"user_id": u, "project_id": p, "role_id": admin})
		relDo(t, app, "", "POST", "/memberships", map[string]any{"user_id": u, "project_id": p, "role_id": dev})

		one := relDo(t, app, "", "GET", "/memberships/"+u+"/"+p+"/"+admin+"?include=user,project,role", nil)
		for path, want := range map[string]string{"user": "Ann", "project": "Apollo", "role": "admin"} {
			if got := relField(one, path, "name"); got != want {
				t.Errorf("membership %s = %v, want %s", path, got, want)
			}
		}
		ms := relField(relDo(t, app, "", "GET", "/members/"+u+"?include=memberships.role", nil), "memberships")
		var roles []string
		for _, m := range ms.([]any) {
			roles = append(roles, fmt.Sprint(relField(m, "role", "name")))
		}
		slices.Sort(roles)
		if !slices.Equal(roles, []string{"admin", "dev"}) {
			t.Errorf("member roles = %v", roles)
		}
		for path, want := range map[string]int{
			"/memberships?role.name=admin":        1,
			"/members?memberships.role_id=" + dev: 1,
			"/members?memberships.role_id=none":   0,
		} {
			if got := len(relDo(t, app, "", "GET", path, nil).([]any)); got != want {
				t.Errorf("GET %s = %d rows, want %d", path, got, want)
			}
		}
	})
}

// A many_to_many reads its pivot through the entity that owns the pivot,
// so a soft-deleted link stays hidden and another owner's link stays out.
func TestPivotEntityScopes(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		t.Run("soft delete", func(t *testing.T) {
			ents := schoolEntities(&entity.ScopeConfig{SoftDelete: true})
			app := relApp(t, db, ents...)
			s1 := relID(t, app, "/students", map[string]any{"name": "Ann"})
			math := relID(t, app, "/courses", map[string]any{"title": "Math"})
			art := relID(t, app, "/courses", map[string]any{"title": "Art"})
			relDo(t, app, "", "POST", "/enrollments", map[string]any{"student_id": s1, "course_id": math})
			relDo(t, app, "", "POST", "/enrollments", map[string]any{"student_id": s1, "course_id": art})
			if rec := serveKeyedAs(t, app, "", "DELETE", "/enrollments/"+s1+"/"+art, nil); rec.Code >= 300 {
				t.Fatalf("DELETE = %d %s", rec.Code, rec.Body)
			}

			courses := relField(relDo(t, app, "", "GET", "/students/"+s1+"?include=courses", nil), "courses")
			if got := relList(courses, "title"); !slices.Equal(got, []string{"Math"}) {
				t.Errorf("include courses = %v, want [Math]", got)
			}
			// A second mention with its own filter is a node of its own;
			// it reads the pivot through the same scopes.
			courses = relField(relDo(t, app, "", "GET", "/students/"+s1+"?include=courses(title=Math),courses(title=Art)", nil), "courses")
			if got := relList(courses, "title"); !slices.Equal(got, []string{"Math"}) {
				t.Errorf("include courses twice = %v, want [Math]", got)
			}
			if got := len(relDo(t, app, "", "GET", "/students?courses.title=Art", nil).([]any)); got != 0 {
				t.Errorf("filter through a deleted link matched %d students", got)
			}
			reg := NewRegistry()
			for _, e := range ents {
				_ = reg.Register(e)
			}
			loaded, err := EagerLoad(context.Background(), db, ents[0], ents[0].Config.Relations[1:], []string{s1}, reg)
			if err != nil {
				t.Fatal(err)
			}
			if got := relList(loaded[s1]["courses"], "title"); !slices.Equal(got, []string{"Math"}) {
				t.Errorf("EagerLoad courses = %v, want [Math]", got)
			}
		})
		t.Run("owner", func(t *testing.T) {
			prev := owner.GetExtractor()
			owner.SetExtractor(func(ctx context.Context) (any, bool) {
				if u, ok := handler.GetUser(ctx); ok && u != nil {
					return u.(relUser).GetID(), true
				}
				return nil, false
			})
			t.Cleanup(func() { owner.SetExtractor(prev) })
			app := relApp(t, db, schoolEntities(&entity.ScopeConfig{OwnerField: "owner_id"})...)
			s1 := relID(t, app, "/students", map[string]any{"name": "Ann"})
			math := relID(t, app, "/courses", map[string]any{"title": "Math"})
			art := relID(t, app, "/courses", map[string]any{"title": "Art"})
			relDo(t, app, "alice", "POST", "/enrollments", map[string]any{"student_id": s1, "course_id": math})
			relDo(t, app, "bob", "POST", "/enrollments", map[string]any{"student_id": s1, "course_id": art})

			courses := relField(relDo(t, app, "alice", "GET", "/students/"+s1+"?include=courses", nil), "courses")
			if got := relList(courses, "title"); !slices.Equal(got, []string{"Math"}) {
				t.Errorf("alice include courses = %v, want [Math]", got)
			}
			if got := len(relDo(t, app, "alice", "GET", "/students?courses.title=Art", nil).([]any)); got != 0 {
				t.Errorf("alice filter through bob's link matched %d students", got)
			}
			if got := len(relDo(t, app, "bob", "GET", "/students?courses.title=Art", nil).([]any)); got != 1 {
				t.Errorf("bob filter through his own link matched %d students, want 1", got)
			}
		})
	})
}

// A Relation field naming an entity with an integer key holds an integer:
// Postgres refuses a TEXT foreign key to it.
func TestRelationFieldToIntegerKey(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		team := entity.Define("team", entity.EntityConfig{
			Table: "teams",
			Fields: []schema.Field{
				{Name: "id", Type: schema.Int, AutoGenerate: schema.AutoIncrement},
				{Name: "name", Type: schema.String},
			},
			Relations: []entity.Relation{entity.HasMany("players", "player", "team_id")},
		}.WithTimestamps(false))
		player := entity.Define("player", entity.EntityConfig{
			Table: "players",
			Fields: []schema.Field{
				{Name: "name", Type: schema.String},
				{Name: "team_id", Type: schema.Relation, To: "team"},
			},
		}.WithTimestamps(false))
		app := relApp(t, db, team, player)
		tid := relID(t, app, "/teams", map[string]any{"name": "Owls"})
		relDo(t, app, "", "POST", "/players", map[string]any{"name": "Ann", "team_id": tid})

		// The field declares a belongs_to named after itself.
		rows := relDo(t, app, "", "GET", "/players?include=team_id", nil).([]any)
		if len(rows) != 1 || relField(rows[0], "teamId", "name") != "Owls" {
			t.Errorf("players include team = %v", rows)
		}
		// The column holds an integer, so a filter value is checked as one.
		for _, path := range []string{"/players?team_id=abc", "/teams?players.team_id=abc", "/teams?include=players(team_id=abc)"} {
			if rec := serveKeyedAs(t, app, "", "GET", path, nil); rec.Code != http.StatusBadRequest {
				t.Errorf("%s = %d, want 400: %s", path, rec.Code, rec.Body)
			}
		}
		if got := len(relDo(t, app, "", "GET", "/players?team_id="+tid, nil).([]any)); got != 1 {
			t.Errorf("?team_id=%s matched %d players, want 1", tid, got)
		}
		players := relField(relDo(t, app, "", "GET", "/teams/"+tid+"?include=players", nil), "players")
		if got := relList(players, "name"); !slices.Equal(got, []string{"Ann"}) {
			t.Errorf("team players = %v", got)
		}

		reg := NewRegistry()
		_ = reg.Register(team)
		_ = reg.Register(player)
		changes, err := migrate.DiffSchema(context.Background(), db, reg)
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range changes {
			t.Errorf("diff after migrate: %s", c.Summary)
		}
	})
}

// A Relation key column holding an integer takes its id segment only in
// canonical decimal: Postgres would read "01" as 1.
func TestRelationKeyCanonical(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		team := entity.Define("team", entity.EntityConfig{
			Table: "teams",
			Fields: []schema.Field{
				{Name: "id", Type: schema.Int, AutoGenerate: schema.AutoIncrement},
				{Name: "name", Type: schema.String},
			},
		}.WithTimestamps(false))
		slot := entity.Define("slot", entity.EntityConfig{
			Table: "slots",
			Fields: []schema.Field{
				{Name: "team_id", Type: schema.Relation, To: "team", Required: true},
				{Name: "position", Type: schema.String, Required: true},
			},
			PrimaryKey: []string{"team_id", "position"},
		}.WithTimestamps(false))
		app := relApp(t, db, team, slot)
		tid := relID(t, app, "/teams", map[string]any{"name": "Owls"})
		relDo(t, app, "", "POST", "/slots", map[string]any{"team_id": tid, "position": "keeper"})

		if rec := serveKeyedAs(t, app, "", "GET", "/slots/"+tid+"/keeper", nil); rec.Code != http.StatusOK {
			t.Fatalf("GET canonical = %d %s", rec.Code, rec.Body)
		}
		for _, alias := range []string{"0" + tid, "+" + tid, tid + ".0"} {
			if rec := serveKeyedAs(t, app, "", "GET", "/slots/"+alias+"/keeper", nil); rec.Code != http.StatusNotFound {
				t.Errorf("GET /slots/%s/keeper = %d, want 404", alias, rec.Code)
			}
		}

		// A write spelling the reference another way stores it canonical,
		// so the canonical id still reaches it on SQLite's TEXT column.
		relDo(t, app, "", "POST", "/slots", map[string]any{"team_id": "0" + tid, "position": "wing"})
		if rec := serveKeyedAs(t, app, "", "GET", "/slots/"+tid+"/wing", nil); rec.Code != http.StatusOK {
			t.Errorf("GET a row written as 0%s = %d %s", tid, rec.Code, rec.Body)
		}
		if rec := serveKeyedAs(t, app, "", "POST", "/slots", map[string]any{"team_id": "owls", "position": "back"}); rec.Code != http.StatusBadRequest {
			t.Errorf("POST a non-integer reference = %d %s, want 400", rec.Code, rec.Body)
		}
	})
}

// A key that is itself a reference to an integer key holds an integer,
// and so does a reference to it.
func TestRelationKeyChain(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		account := entity.Define("account", entity.EntityConfig{
			Table: "accounts",
			Fields: []schema.Field{
				{Name: "id", Type: schema.Int, AutoGenerate: schema.AutoIncrement},
				{Name: "email", Type: schema.String},
			},
		}.WithTimestamps(false))
		profile := entity.Define("profile", entity.EntityConfig{
			Table: "profiles",
			Fields: []schema.Field{
				{Name: "account_id", Type: schema.Relation, To: "account", Required: true},
				{Name: "bio", Type: schema.String},
				// A self-reference to a key that is itself a reference.
				{Name: "manager_id", Type: schema.Relation, To: "profile"},
			},
			PrimaryKey: []string{"account_id"},
		}.WithTimestamps(false))
		note := entity.Define("note", entity.EntityConfig{
			Table: "notes",
			Fields: []schema.Field{
				{Name: "body", Type: schema.String},
				{Name: "profile_id", Type: schema.Relation, To: "profile"},
			},
		}.WithTimestamps(false))
		app := relApp(t, db, account, profile, note)
		aid := relID(t, app, "/accounts", map[string]any{"email": "ann@example.com"})
		relDo(t, app, "", "POST", "/profiles", map[string]any{"account_id": aid, "bio": "hi"})
		relDo(t, app, "", "POST", "/notes", map[string]any{"body": "first", "profile_id": "0" + aid})

		rows := relDo(t, app, "", "GET", "/notes?include=profile_id", nil).([]any)
		if len(rows) != 1 || relField(rows[0], "profileId", "bio") != "hi" {
			t.Errorf("notes include profile = %v", rows)
		}
		bid := relID(t, app, "/accounts", map[string]any{"email": "bo@example.com"})
		relDo(t, app, "", "POST", "/profiles", map[string]any{"account_id": bid, "bio": "bo", "manager_id": "0" + aid})
		bo := relDo(t, app, "", "GET", "/profiles/"+bid+"?include=manager_id", nil)
		if got := relField(bo, "managerId", "bio"); got != "hi" {
			t.Errorf("profile manager = %v, want hi", got)
		}
		reg := NewRegistry()
		for _, e := range []*entity.Entity{account, profile, note} {
			_ = reg.Register(e)
		}
		changes, err := migrate.DiffSchema(context.Background(), db, reg)
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range changes {
			t.Errorf("diff after migrate: %s", c.Summary)
		}
	})
}

// A cascade link to a target keyed on a reference to an integer key binds
// the link as that integer, however the caller spelled the number.
func TestCascadeLinkToRelationKey(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		account := entity.Define("account", entity.EntityConfig{
			Table: "accounts",
			Fields: []schema.Field{
				{Name: "id", Type: schema.Int, AutoGenerate: schema.AutoIncrement},
				{Name: "email", Type: schema.String},
			},
		}.WithTimestamps(false))
		profile := entity.Define("profile", entity.EntityConfig{
			Table: "profiles",
			Fields: []schema.Field{
				{Name: "account_id", Type: schema.Relation, To: "account", Required: true},
				{Name: "bio", Type: schema.String},
			},
			PrimaryKey: []string{"account_id"},
		}.WithTimestamps(false))
		team := entity.Define("team", entity.EntityConfig{
			Table:  "teams",
			Fields: []schema.Field{{Name: "name", Type: schema.String}},
			Relations: []entity.Relation{
				entity.ManyToMany("members", "profile", "team_members", "team_id", "profile_id").WithCascadeWrite(true),
			},
		}.WithTimestamps(false))
		app := relApp(t, db, account, profile, team)
		aid := relID(t, app, "/accounts", map[string]any{"email": "ann@example.com"})
		relDo(t, app, "", "POST", "/profiles", map[string]any{"account_id": aid, "bio": "hi"})
		n, err := strconv.ParseFloat(aid, 64)
		if err != nil {
			t.Fatal(err)
		}
		ch, err := app.CrudHandler("team")
		if err != nil {
			t.Fatal(err)
		}
		res, err := ch.CreateOne(context.Background(), map[string]any{"name": "Owls", "members": []any{n}})
		if err != nil {
			t.Fatalf("CreateOne linking %v: %v", n, err)
		}
		tid := fmt.Sprint(res["id"])
		members := relField(relDo(t, app, "", "GET", "/teams/"+tid+"?include=members", nil), "members")
		if got := relList(members, "bio"); !slices.Equal(got, []string{"hi"}) {
			t.Errorf("team members = %v, want [hi]", got)
		}
		if _, err := ch.CreateOne(context.Background(), map[string]any{"name": "Larks", "members": []any{"0" + aid}}); err != nil {
			t.Errorf("CreateOne linking %q: %v", "0"+aid, err)
		}
		// A float past 2^53 may be a rounded integer: refused, not bound.
		_, err = ch.CreateOne(context.Background(), map[string]any{"name": "Big", "members": []any{float64(1<<53 + 2)}})
		var ve *ValidationError
		if !errors.As(err, &ve) {
			t.Errorf("linking 2^53+2 as a float = %v, want a validation error", err)
		}
	})
}

// Relation columns with upper-case letters resolve on Postgres, which
// folds the unquoted DDL to lower case.
func TestMixedCaseRelationColumns(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		author := entity.Define("author", entity.EntityConfig{
			Table:  "authors",
			Fields: []schema.Field{{Name: "name", Type: schema.String}},
			Relations: []entity.Relation{
				entity.HasMany("books", "book", "authorId"),
				entity.ManyToMany("tags", "tag", "authorTags", "authorId", "tagId"),
			},
		}.WithTimestamps(false))
		book := entity.Define("book", entity.EntityConfig{
			Table: "books",
			Fields: []schema.Field{
				{Name: "title", Type: schema.String},
				{Name: "authorId", Type: schema.String},
			},
			Relations: []entity.Relation{entity.BelongsTo("author", "author", "authorId")},
		}.WithTimestamps(false))
		tag := entity.Define("tag", entity.EntityConfig{
			Table:  "tags",
			Fields: []schema.Field{{Name: "label", Type: schema.String}},
		}.WithTimestamps(false))
		app := relApp(t, db, author, book, tag)
		a := relID(t, app, "/authors", map[string]any{"name": "Ann"})
		relDo(t, app, "", "POST", "/books", map[string]any{"title": "Dune", "authorId": a})
		tg := relID(t, app, "/tags", map[string]any{"label": "scifi"})
		if _, err := db.Exec("INSERT INTO authorTags (authorId, tagId) VALUES ($1, $2)", a, tg); err != nil {
			t.Fatal(err)
		}

		rows := relDo(t, app, "", "GET", "/books?include=author", nil).([]any)
		if len(rows) != 1 || relField(rows[0], "author", "name") != "Ann" {
			t.Errorf("books include author = %v", rows)
		}
		got := relDo(t, app, "", "GET", "/authors/"+a+"?include=books,tags", nil)
		if titles := relList(relField(got, "books"), "title"); !slices.Equal(titles, []string{"Dune"}) {
			t.Errorf("author books = %v", titles)
		}
		if labels := relList(relField(got, "tags"), "label"); !slices.Equal(labels, []string{"scifi"}) {
			t.Errorf("author tags = %v", labels)
		}
		for path, want := range map[string]int{
			"/authors?books.title=Dune": 1,
			"/authors?tags.label=scifi": 1,
			"/books?author.name=Ann":    1,
		} {
			if got := len(relDo(t, app, "", "GET", path, nil).([]any)); got != want {
				t.Errorf("GET %s = %d rows, want %d", path, got, want)
			}
		}
	})
}

// A target keyed by a column other than id is joined on that column.
func TestRelationToNonIDKey(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		currency := entity.Define("currency", entity.EntityConfig{
			Table: "currencies",
			Fields: []schema.Field{
				{Name: "code", Type: schema.String, Required: true},
				{Name: "label", Type: schema.String},
			},
			PrimaryKey: []string{"code"},
		}.WithTimestamps(false))
		product := entity.Define("product", entity.EntityConfig{
			Table: "products",
			Fields: []schema.Field{
				{Name: "name", Type: schema.String},
				{Name: "currency_code", Type: schema.String},
			},
			Relations: []entity.Relation{
				entity.BelongsTo("currency", "currency", "currency_code"),
				entity.ManyToMany("accepts", "currency", "product_currencies", "product_id", "currency_code"),
			},
		}.WithTimestamps(false))
		app := relApp(t, db, currency, product)
		relDo(t, app, "", "POST", "/currencies", map[string]any{"code": "EUR", "label": "Euro"})
		relDo(t, app, "", "POST", "/currencies", map[string]any{"code": "USD", "label": "Dollar"})
		p := relID(t, app, "/products", map[string]any{"name": "Lamp", "currency_code": "EUR"})
		if _, err := db.Exec("INSERT INTO product_currencies (product_id, currency_code) VALUES ($1, 'USD')", p); err != nil {
			t.Fatal(err)
		}

		got := relDo(t, app, "", "GET", "/products/"+p+"?include=currency,accepts", nil)
		if l := relField(got, "currency", "label"); l != "Euro" {
			t.Errorf("product currency = %v, want Euro", l)
		}
		if labels := relList(relField(got, "accepts"), "label"); !slices.Equal(labels, []string{"Dollar"}) {
			t.Errorf("product accepts = %v, want [Dollar]", labels)
		}
		for path, want := range map[string]int{
			"/products?currency.label=Euro":  1,
			"/products?accepts.label=Dollar": 1,
			"/products?accepts.label=Euro":   0,
		} {
			if got := len(relDo(t, app, "", "GET", path, nil).([]any)); got != want {
				t.Errorf("GET %s = %d rows, want %d", path, got, want)
			}
		}
	})
}
