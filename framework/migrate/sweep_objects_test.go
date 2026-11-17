package migrate

import (
	"testing"
	"testing/fstest"

	"github.com/DonaldMurillo/gofastr/core/schema"
)

func TestSweepObjectsViewEntityNeedsAKey(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("a view with Columns and no PrimaryKey column built an entity")
		}
	}()
	View{Name: "v", Select: "SELECT 1 AS n", Columns: []Column{{Name: "n", Type: schema.Int}}}.ToEntity()
}

func TestSweepObjectsViewEntityIsNormalized(t *testing.T) {
	ent := View{Name: "v", Select: "SELECT 1 AS a, 2 AS b", Columns: []Column{
		{Name: "a", Type: schema.Int, PrimaryKey: true},
		{Name: "b", Type: schema.Int, PrimaryKey: true},
	}}.ToEntity()
	c := ent.Config
	if !c.Unmanaged || c.Scope == nil || c.Pagination == nil || c.Exposure == nil {
		t.Fatalf("view entity config not normalized: %+v", c)
	}
	if len(c.PrimaryKey) != 2 || c.PrimaryKey[0] != "a" || c.PrimaryKey[1] != "b" {
		t.Errorf("composite key = %v, want [a b]", c.PrimaryKey)
	}
}

func TestSweepObjectsViewDependentsAreTransitive(t *testing.T) {
	ordered := topoSortViews([]View{
		{Name: "a", DependsOn: []string{"t"}},
		{Name: "b", DependsOn: []string{"a"}},
		{Name: "c", DependsOn: []string{"b"}},
	})
	deps := viewDependents(ordered)
	if got := deps["a"]; len(got) != 2 || got[0].Name != "b" || got[1].Name != "c" {
		t.Errorf("dependents of a = %v, want [b c]", got)
	}
}

func TestSweepObjectsRoutinesFSRefusals(t *testing.T) {
	for name, files := range map[string]map[string]string{
		"only non-sql files":      {"r/README.md": "notes"},
		"empty down":              {"r/foo.sql": "SELECT 1", "r/foo.down.sql": " \n"},
		"two downs":               {"r/foo.pg.sql": "SELECT 1", "r/foo.down.sql": "SELECT 2", "r/foo.pg.down.sql": "SELECT 3"},
		"down without up":         {"r/foo.sql": "SELECT 1", "r/bar.down.sql": "SELECT 2"},
		"down of another dialect": {"r/foo.pg.sql": "SELECT 1", "r/foo.sqlite.down.sql": "SELECT 2"},
	} {
		fsys := fstest.MapFS{}
		for p, body := range files {
			fsys[p] = &fstest.MapFile{Data: []byte(body)}
		}
		if _, err := RoutinesFS(fsys, "r"); err == nil {
			t.Errorf("%s: RoutinesFS accepted it", name)
		}
	}
}

func TestSweepObjectsRoutinesFSSkipsDotfilesAndDirs(t *testing.T) {
	fsys := fstest.MapFS{
		"r/foo.sql":       {Data: []byte("SELECT 1")},
		"r/.#foo.sql":     {Data: []byte("")},
		"r/sub.sql/x.sql": {Data: []byte("SELECT 2")},
		"r/bar.sql":       {Data: []byte("\xef\xbb\xbfSELECT 3")},
	}
	got, err := RoutinesFS(fsys, "r")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Name != "bar" || got[0].Up != "SELECT 3" || got[1].Name != "foo" {
		t.Errorf("RoutinesFS = %+v, want bar (BOM stripped) then foo", got)
	}
}
