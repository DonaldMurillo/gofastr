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

// A plain routine name cannot contain dots: the trailing segment reads as
// an unknown dialect suffix. A dialect tag makes a dotted name load.
func TestDottedPlainNameRefused(t *testing.T) {
	for _, name := range []string{"audit.log_fn.sql", "audit.log_fn.down.sql", "a.b.c.sql"} {
		t.Run(name, func(t *testing.T) {
			fsys := fstest.MapFS{"r/" + name: {Data: []byte("SELECT 1")}}
			if _, err := RoutinesFS(fsys, "r"); err == nil || !strings.Contains(err.Error(), "unknown dialect suffix") {
				t.Fatalf("dotted plain name: err = %v, want unknown dialect suffix", err)
			}
		})
	}
	routines, err := RoutinesFS(fstest.MapFS{
		"r/audit.log_fn.sqlite.sql":      {Data: []byte("SELECT 1")},
		"r/audit.log_fn.sqlite.down.sql": {Data: []byte("SELECT 2")},
	}, "r")
	if err != nil || len(routines) != 1 || routines[0].Name != "audit.log_fn" || routines[0].Down != "SELECT 2" {
		t.Fatalf("tagged dotted name: routines=%+v, err=%v", routines, err)
	}
}
