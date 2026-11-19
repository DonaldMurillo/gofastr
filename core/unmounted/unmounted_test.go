package unmounted

import "testing"

func TestListIsSortedBySource(t *testing.T) {
	t.Cleanup(func() { copies = map[string]Copy{} })
	const app = "example.com/app/unmounted/"
	for _, c := range []Copy{
		{Source: "github.com/DonaldMurillo/gofastr/core/yaml", Version: "v0.91.0", Path: app + "core/yaml"},
		{Source: "github.com/DonaldMurillo/gofastr/battery/auth", Version: "dev", Path: app + "z/auth"},
		{Source: "github.com/DonaldMurillo/gofastr/battery/auth", Version: "dev", Path: app + "battery/auth"},
	} {
		Register(c)
	}
	want := []string{app + "battery/auth", app + "z/auth", app + "core/yaml"}
	got := List()
	if len(got) != len(want) {
		t.Fatalf("List = %+v, want %d copies", got, len(want))
	}
	for i, c := range got {
		if c.Path != want[i] {
			t.Fatalf("List[%d] = %s, want %s: copies sort by Source, then Path", i, c.Path, want[i])
		}
	}
}

// One source can have a copy per path; the same path registered again
// replaces the earlier copy.
func TestRegisterKeysByPath(t *testing.T) {
	t.Cleanup(func() { copies = map[string]Copy{} })
	Register(Copy{Source: "s", Version: "v0.90.0", Path: "b"})
	Register(Copy{Source: "s", Version: "v0.90.0", Path: "a"})
	Register(Copy{Source: "s", Version: "v0.91.0", Path: "b"})
	got := List()
	if len(got) != 2 || got[0].Path != "a" || got[1].Path != "b" || got[1].Version != "v0.91.0" {
		t.Fatalf("List = %+v", got)
	}
}
