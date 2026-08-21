package datexport_test

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/datexport"
)

// The registry stores entries verbatim, identifier validation (SafeIdent /
// MustIdent) is deliberately NOT here; it happens at the SQL-building sites
// in framework/export_data.go and framework/erase_data.go. These tests cover
// the guarantees the registry itself makes: last-writer-wins dedup by Name,
// Name-sorted copies out, defensive slice/map copies in both directions, and
// the resolver lookup that drives EraseUserData's fail-loud path.

func TestRegisterAndAll(t *testing.T) {
	datexport.Reset(t)

	datexport.Register(datexport.DataExporter{
		Name: "auth_sessions", Source: "auth", Table: "auth_sessions",
		PrimaryKey: "id", Columns: []string{"id", "user_id"},
	})

	got := datexport.All()
	if len(got) != 1 {
		t.Fatalf("len(All())=%d, want 1", len(got))
	}
	e := got[0]
	if e.Name != "auth_sessions" || e.Source != "auth" || e.Table != "auth_sessions" ||
		e.PrimaryKey != "id" || len(e.Columns) != 2 {
		t.Fatalf("entry mismatch: %+v", e)
	}
}

func TestAllSortedByName(t *testing.T) {
	datexport.Reset(t)

	datexport.Register(datexport.DataExporter{Name: "zeta", Table: "z"})
	datexport.Register(datexport.DataExporter{Name: "alpha", Table: "a"})
	datexport.Register(datexport.DataExporter{Name: "mid", Table: "m"})

	got := datexport.All()
	if len(got) != 3 || got[0].Name != "alpha" || got[1].Name != "mid" || got[2].Name != "zeta" {
		t.Fatalf("All() not sorted by Name: %+v", got)
	}
}

func TestRegisterDupNameReplaces(t *testing.T) {
	datexport.Reset(t)

	datexport.Register(datexport.DataExporter{
		Name: "jobs", Table: "jobs_old", Columns: []string{"id"},
	})
	datexport.Register(datexport.DataExporter{
		Name: "jobs", Table: "jobs_new", Columns: []string{"id", "state"},
	})

	got := datexport.All()
	if len(got) != 1 {
		t.Fatalf("duplicate Name grew the registry: %d entries", len(got))
	}
	if got[0].Table != "jobs_new" || len(got[0].Columns) != 2 {
		t.Fatalf("last-writer-wins violated: %+v", got[0])
	}
}

func TestRegisterCopiesColumns(t *testing.T) {
	datexport.Reset(t)

	cols := []string{"id", "email"}
	datexport.Register(datexport.DataExporter{Name: "users", Table: "users", Columns: cols})
	cols[0] = "mutated"

	if got := datexport.All()[0].Columns[0]; got != "id" {
		t.Fatalf("caller slice aliased into registry: Columns[0]=%q", got)
	}
}

func TestAllReturnsCopies(t *testing.T) {
	datexport.Reset(t)

	datexport.Register(datexport.DataExporter{Name: "users", Table: "users", Columns: []string{"id"}})
	out := datexport.All()
	out[0].Columns[0] = "mutated"
	out[0].Table = "mutated"

	fresh := datexport.All()[0]
	if fresh.Columns[0] != "id" || fresh.Table != "users" {
		t.Fatalf("All() result aliased into registry: %+v", fresh)
	}
}

func TestUnregister(t *testing.T) {
	datexport.Reset(t)

	datexport.Register(datexport.DataExporter{Name: "users", Table: "users"})
	if !datexport.Unregister("users") {
		t.Fatal("Unregister of a present entry returned false")
	}
	if datexport.Unregister("users") {
		t.Fatal("Unregister of an absent entry returned true")
	}
	if got := datexport.All(); len(got) != 0 {
		t.Fatalf("entry survived Unregister: %+v", got)
	}
}

func TestEraserDupNameReplaces(t *testing.T) {
	datexport.Reset(t)

	datexport.RegisterEraser(datexport.DataEraser{
		Name: "sessions", Table: "sessions", Column: "user_id",
		Mode: datexport.EraseDelete,
	})
	datexport.RegisterEraser(datexport.DataEraser{
		Name: "sessions", Table: "sessions_v2", Column: "owner_id",
		Mode: datexport.EraseAnonymize, ScrubColumns: []string{"owner_id"},
	})

	got := datexport.AllErasers()
	if len(got) != 1 {
		t.Fatalf("duplicate Name grew the eraser registry: %d entries", len(got))
	}
	if got[0].Table != "sessions_v2" || got[0].Mode != datexport.EraseAnonymize {
		t.Fatalf("last-writer-wins violated: %+v", got[0])
	}
}

func TestEraserCopiesScrubColumns(t *testing.T) {
	datexport.Reset(t)

	scrub := []string{"email"}
	datexport.RegisterEraser(datexport.DataEraser{
		Name: "audit", Table: "audit", Column: "actor_id",
		Mode: datexport.EraseAnonymize, ScrubColumns: scrub,
	})
	scrub[0] = "mutated"

	out := datexport.AllErasers()
	if out[0].ScrubColumns[0] != "email" {
		t.Fatalf("caller slice aliased into registry: %q", out[0].ScrubColumns[0])
	}
	out[0].ScrubColumns[0] = "mutated"
	if got := datexport.AllErasers()[0].ScrubColumns[0]; got != "email" {
		t.Fatalf("AllErasers() result aliased into registry: %q", got)
	}
}

func TestAllErasersSortedByName(t *testing.T) {
	datexport.Reset(t)

	datexport.RegisterEraser(datexport.DataEraser{Name: "b", Table: "b", Column: "user_id"})
	datexport.RegisterEraser(datexport.DataEraser{Name: "a", Table: "a", Column: "user_id"})

	got := datexport.AllErasers()
	if len(got) != 2 || got[0].Name != "a" || got[1].Name != "b" {
		t.Fatalf("AllErasers() not sorted by Name: %+v", got)
	}
}

func TestUnregisterEraser(t *testing.T) {
	datexport.Reset(t)

	datexport.RegisterEraser(datexport.DataEraser{Name: "sessions", Table: "sessions", Column: "user_id"})
	if !datexport.UnregisterEraser("sessions") {
		t.Fatal("UnregisterEraser of a present entry returned false")
	}
	if datexport.UnregisterEraser("sessions") {
		t.Fatal("UnregisterEraser of an absent entry returned true")
	}
}

func TestResolverReplaceAndLookup(t *testing.T) {
	datexport.Reset(t)

	datexport.RegisterIdentityResolver(datexport.IdentityEmail, datexport.DataIdentityResolver{
		Table: "users_old", IDColumn: "id", ValueColumn: "email",
	})
	datexport.RegisterIdentityResolver(datexport.IdentityEmail, datexport.DataIdentityResolver{
		Table: "auth_users", IDColumn: "id", ValueColumn: "email",
	})

	r, ok := datexport.ResolveIdentity(datexport.IdentityEmail)
	if !ok {
		t.Fatal("registered resolver not found")
	}
	if r.Table != "auth_users" {
		t.Fatalf("last-writer-wins violated: %+v", r)
	}
}

func TestResolveIdentityMissing(t *testing.T) {
	datexport.Reset(t)

	// EraseUserData treats an eraser declaring an unresolvable identity as a
	// fail-loud misconfiguration; ok=false is what triggers it.
	if _, ok := datexport.ResolveIdentity(datexport.IdentityEmail); ok {
		t.Fatal("ResolveIdentity reported a resolver that was never registered")
	}
}

func TestUnregisterIdentityResolver(t *testing.T) {
	datexport.Reset(t)

	datexport.RegisterIdentityResolver(datexport.IdentityEmail, datexport.DataIdentityResolver{
		Table: "auth_users", IDColumn: "id", ValueColumn: "email",
	})
	if !datexport.UnregisterIdentityResolver(datexport.IdentityEmail) {
		t.Fatal("UnregisterIdentityResolver of a present kind returned false")
	}
	if datexport.UnregisterIdentityResolver(datexport.IdentityEmail) {
		t.Fatal("UnregisterIdentityResolver of an absent kind returned true")
	}
}

func TestAllIdentityResolversIsCopy(t *testing.T) {
	datexport.Reset(t)

	datexport.RegisterIdentityResolver(datexport.IdentityEmail, datexport.DataIdentityResolver{
		Table: "auth_users", IDColumn: "id", ValueColumn: "email",
	})
	out := datexport.AllIdentityResolvers()
	if len(out) != 1 {
		t.Fatalf("len(AllIdentityResolvers())=%d, want 1", len(out))
	}
	delete(out, datexport.IdentityEmail)

	if _, ok := datexport.ResolveIdentity(datexport.IdentityEmail); !ok {
		t.Fatal("mutating the returned map reached the registry")
	}
}

func TestResetClearsAllPlanes(t *testing.T) {
	datexport.Reset(t)

	datexport.Register(datexport.DataExporter{Name: "users", Table: "users"})
	datexport.RegisterEraser(datexport.DataEraser{Name: "users", Table: "users", Column: "id"})
	datexport.RegisterIdentityResolver(datexport.IdentityEmail, datexport.DataIdentityResolver{
		Table: "auth_users", IDColumn: "id", ValueColumn: "email",
	})

	datexport.Reset(t)

	if n := len(datexport.All()); n != 0 {
		t.Fatalf("%d exporters survived Reset", n)
	}
	if n := len(datexport.AllErasers()); n != 0 {
		t.Fatalf("%d erasers survived Reset", n)
	}
	if n := len(datexport.AllIdentityResolvers()); n != 0 {
		t.Fatalf("%d resolvers survived Reset", n)
	}
}
