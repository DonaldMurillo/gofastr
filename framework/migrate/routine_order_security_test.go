package migrate

import (
	"strings"
	"testing"
	"testing/fstest"
)

func TestRoutineSuffixOrder(t *testing.T) {
	for _, name := range []string{
		"metrics.down.pg.sql",
		"refresh.down.sqlite.sql",
		"audit.log_fn.down.pg.sql",
		"refresh.down.sqlite.down.sql",
	} {
		t.Run(name, func(t *testing.T) {
			fsys := fstest.MapFS{
				"r/base.sql": {Data: []byte("SELECT 1")},
				"r/" + name:  {Data: []byte("DROP VIEW metrics")},
			}
			if routines, err := RoutinesFS(fsys, "r"); err == nil || !strings.Contains(err.Error(), name) {
				t.Fatalf("misordered marker accepted: routines=%v, err=%v", routines, err)
			}
		})
	}
	for _, name := range []string{"down.pg.sql", "countdown.sqlite.sql", "audit.log_fn.pg.sql"} {
		t.Run(name, func(t *testing.T) {
			if _, err := RoutinesFS(fstest.MapFS{"r/" + name: {Data: []byte("SELECT 1")}}, "r"); err != nil {
				t.Fatal(err)
			}
		})
	}
}
