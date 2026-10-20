package unmounted

import "testing"

func TestListIsSortedBySource(t *testing.T) {
	t.Cleanup(func() { copies = map[string]Copy{} })
	Register(Copy{Source: "github.com/DonaldMurillo/gofastr/core/yaml", Version: "v0.91.0", Path: "example.com/app/unmounted/core/yaml"})
	Register(Copy{Source: "github.com/DonaldMurillo/gofastr/battery/auth", Version: "dev", Path: "example.com/app/unmounted/battery/auth"})
	got := List()
	if len(got) != 2 || got[0].Source != "github.com/DonaldMurillo/gofastr/battery/auth" || got[1].Version != "v0.91.0" {
		t.Fatalf("List = %+v", got)
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
