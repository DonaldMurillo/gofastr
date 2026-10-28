package datexport_test

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/core/datexport"
)

// A store a host can build twice with two tables registers each under
// its own name: registration replaces by Name, so a shared constant left
// the first table out of every export and erasure (2026-10-28 review of
// the erasure registrations).
func TestScopedNameSeparatesTables(t *testing.T) {
	for _, tc := range []struct{ base, table, want string }{
		{"a2a_tasks", "a2a_tasks", "a2a_tasks"},
		{"a2a_tasks", "t1_a2a_tasks", "a2a_tasks-t1_a2a_tasks"},
		{"a2a_tasks", "main.a2a_tasks", "a2a_tasks-main-2ea2a_tasks"},
		{"a2a_tasks", "main-2ea2a_tasks", "a2a_tasks-main-2d2ea2a_tasks"},
		{"flags", "Flags", "flags-Flags"},
		{"flags", "drapeaux_é", "flags-drapeaux_-c3-a9"},
	} {
		if got := datexport.ScopedName(tc.base, tc.table); got != tc.want {
			t.Errorf("ScopedName(%q, %q) = %q, want %q", tc.base, tc.table, got, tc.want)
		}
	}
}

// Two stores on two tables both stay registered.
func TestScopedNamesBothRegister(t *testing.T) {
	datexport.Reset(t)
	t.Cleanup(func() { datexport.Reset(t) })
	for _, table := range []string{"a2a_tasks", "main.a2a_tasks"} {
		datexport.Register(datexport.DataExporter{
			Name: datexport.ScopedName("a2a_tasks", table), Source: "a2a", Table: table,
			PrimaryKey: "id", Columns: []string{"id"},
		})
	}
	if got := datexport.All(); len(got) != 2 {
		t.Fatalf("registered %d exporters, want 2: %+v", len(got), got)
	}
}
