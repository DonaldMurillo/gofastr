package migrate

import (
	"strings"
	"testing"
	"testing/fstest"
)

func TestRoutineDownDialect(t *testing.T) {
	for _, test := range []struct {
		upSuffix   string
		downSuffix string
		refused    bool
	}{
		{".pg", ".sqlite", true},
		{".sqlite", ".pg", true},
		{"", ".pg", true},
		{"", ".sqlite", true},
		{".pg", ".pg", false},
		{".sqlite", ".sqlite", false},
		{".pg", "", false},
		{".sqlite", "", false},
		{"", "", false},
	} {
		t.Run(test.upSuffix+"/"+test.downSuffix, func(t *testing.T) {
			fsys := fstest.MapFS{
				"r/metrics" + test.upSuffix + ".sql":        {Data: []byte("SELECT 1")},
				"r/metrics" + test.downSuffix + ".down.sql": {Data: []byte("SELECT 2")},
			}
			routines, err := RoutinesFS(fsys, "r")
			if test.refused {
				if err == nil || !strings.Contains(err.Error(), "dialect") {
					t.Fatalf("mismatched Down accepted: routines=%v, err=%v", routines, err)
				}
				return
			}
			if err != nil || len(routines) != 1 || routines[0].Down != "SELECT 2" {
				t.Fatalf("compatible Down: routines=%v, err=%v", routines, err)
			}
		})
	}
}
