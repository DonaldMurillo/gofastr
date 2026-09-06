package featureflag

import (
	"context"
	"testing"
)

// Property: MemoryStore.All's documented contract — "The result is a copy.
// Mutations don't affect the store" — must hold for EVERY field of Flag,
// including the Users/Tenants/Envs slice fields whose backing arrays are the
// allow-lists evaluate() consults on every request.
// Surfaces: core/featureflag/flag.go::All (struct copy shares slice backing)
// and ::Set (stores the caller's Flag without cloning its slices — the same
// aliasing from the other direction), consumed together by evaluate() for
// every featureflag.Bool call.
// Guard history: All() copied the Flag struct but not its slices, and Set()
// stored the caller's slices uncloned, so in-place edits on either side were
// visible on the other: annotating the snapshot returned by All (the shape
// its own SECURITY comment invites — scrubbing the PII allow-lists before an
// admin render) rewrote the LIVE flag definition, and mutating a Flag after
// Set changed what every subsequent evaluate() consults. Both directions also
// made those slices a data race between evaluate() readers and any writer
// holding the "copy". Set/Get/All now deep-copy the slice fields at both
// boundaries (cloneFlag).
func TestAllSnapshotDoesNotShareSlices(t *testing.T) {
	store := NewMemoryStore()
	ctx := featureflagCtx(context.Background(), "admin", "t1", "prod")

	if err := store.Set(Flag{
		Key:     "beta",
		Enabled: true,
		Users:   []string{"alice@example.com", "bob@example.com"},
		Tenants: []string{"t-prod"},
		Envs:    []string{"prod"},
	}); err != nil {
		t.Fatal(err)
	}

	// Direction 1: the snapshot All returns must not alias the store. The
	// documented consumer of All is an admin listing that post-processes
	// exactly these PII-bearing fields.
	snap := store.All()
	if len(snap) != 1 {
		t.Fatalf("All: len %d", len(snap))
	}
	snap[0].Users[0] = "SCRUBBED@example.invalid"
	snap[0].Tenants[0] = "SCRUBBED"

	var live Flag
	got, gerr := store.Get(ctx, "beta")
	if gerr != nil || got == nil {
		t.Fatalf("Get after snapshot edit: %v %v", got, gerr)
	}
	live = *got
	if live.Users[0] != "alice@example.com" || live.Tenants[0] != "t-prod" {
		t.Errorf("SECURITY: [flag-snapshot-alias] editing the All() snapshot rewrote the live flag: Users[0]=%q Tenants[0]=%q — All documents \"The result is a copy. Mutations don't affect the store\", but the slice fields alias the store's allow-lists, so an admin render that scrubs PII in place corrupts what evaluate() admits",
			live.Users[0], live.Tenants[0])
	}

	// Direction 2: Set must not alias the caller's Flag. The stored
	// definition changes when the caller reuses its slice afterwards.
	users := []string{"carol@example.com"}
	if err := store.Set(Flag{Key: "beta2", Enabled: true, Users: users}); err != nil {
		t.Fatal(err)
	}
	users[0] = "mallory@example.com"
	got2, gerr := store.Get(ctx, "beta2")
	if gerr != nil || got2 == nil {
		t.Fatalf("Get beta2: %v %v", got2, gerr)
	}
	if got2.Users[0] != "carol@example.com" {
		t.Errorf("SECURITY: [flag-set-alias] mutating the caller's slice AFTER Set rewrote the stored allow-list to %q — Set stores the slice backing uncloned, so the live flag definition is caller-owned memory that evaluate() keeps consulting",
			got2.Users[0])
	}

	// False-positive guard: the struct-copy half of All already works —
	// Enabled on the snapshot must not feed back either.
	snap2 := store.All()
	for i := range snap2 {
		snap2[i].Enabled = false
	}
	if g, _ := store.Get(ctx, "beta"); g != nil && !g.Enabled {
		t.Error("value-field edits on the snapshot leaked into the store; only the slice fields may alias")
	}
}
