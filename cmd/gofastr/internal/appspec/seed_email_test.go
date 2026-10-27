package appspec

import "testing"

// The generator emits seed_email into FindByEmail and CreateUser
// literals. Login canonicalizes the address and FindByEmail matches it
// exactly, so a seed admin stored as "Ops@Example.com" could never sign
// in. The decoder stores the address login looks up.
func TestSeedEmailIsCanonical(t *testing.T) {
	spec, err := covT_decode(t, "app:\n  admin:\n    seed_email: \" Ops@Example.com \"\n")
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got, want := spec.App.Admin.SeedEmail, "ops@example.com"; got != want {
		t.Fatalf("SeedEmail = %q, want the login spelling %q", got, want)
	}
}
