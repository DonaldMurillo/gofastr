package localdb

import (
	"encoding/json"
	"strings"
	"testing"
)

func mustPanic(t *testing.T, want string, fn func()) {
	t.Helper()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatalf("expected panic containing %q, got none", want)
		}
		if msg, _ := r.(string); !strings.Contains(msg, want) {
			t.Fatalf("panic = %v, want it to contain %q", r, want)
		}
	}()
	fn()
}

func TestManifestShape(t *testing.T) {
	reset()
	t.Cleanup(reset)

	db := New("pokedex")
	members := db.Store("members", AutoKey(),
		Index("by_created", "createdAt"),
		UniqueIndex("by_slot", "team", "slot"),
		MultiEntryIndex("by_type", "types"))
	db.Store("settings", KeyPath("meta.key"))

	if members.Name() != "members" || members.DB() != "pokedex" || members.KeyPath() != "id" {
		t.Fatalf("store accessors = %q %q %q", members.Name(), members.DB(), members.KeyPath())
	}
	if !members.HasIndex("by_created") || members.HasIndex("nope") {
		t.Fatal("HasIndex disagrees with the declaration")
	}

	var got map[string]any
	if err := json.Unmarshal(ManifestJSON(), &got); err != nil {
		t.Fatalf("manifest is not JSON: %v", err)
	}
	want := `{"pokedex":{"stores":{"members":{"keyPath":"id","autoKey":true,"indexes":{"by_created":{"keyPath":["createdAt"]},"by_slot":{"keyPath":["team","slot"],"unique":true},"by_type":{"keyPath":["types"],"multiEntry":true}}},"settings":{"keyPath":"meta.key"}}}}`
	if string(ManifestJSON()) != want {
		t.Fatalf("manifest =\n%s\nwant\n%s", ManifestJSON(), want)
	}
	if names := Names(); len(names) != 1 || names[0] != "pokedex" {
		t.Fatalf("Names = %v", names)
	}
}

func TestManifestEmptyIsNil(t *testing.T) {
	reset()
	t.Cleanup(reset)
	if ManifestJSON() != nil {
		t.Fatal("no declarations must emit no manifest")
	}
}

// Every refusal is a startup panic: a misdeclared database is a
// programmer error found where it was made, not a page that opens a
// half-applied schema.
func TestDeclarationRefusals(t *testing.T) {
	reset()
	t.Cleanup(reset)

	mustPanic(t, "invalid database name", func() { New("Pokedex") })
	mustPanic(t, "invalid database name", func() { New("") })
	mustPanic(t, "invalid database name", func() { New(strings.Repeat("a", 65)) })
	mustPanic(t, "invalid database name", func() { New("a.b") })

	db := New("dup")
	mustPanic(t, "declared twice", func() { New("dup") })
	db.Store("s")
	mustPanic(t, "declared twice", func() { db.Store("s") })
	mustPanic(t, "invalid store name", func() { db.Store("S p") })

	mustPanic(t, "reserved", func() { db.Store("p1", KeyPath("__proto__")) })
	mustPanic(t, "reserved", func() { db.Store("p2", KeyPath("a.constructor")) })
	mustPanic(t, "reserved", func() { db.Store("p3", Index("ix", "prototype")) })
	mustPanic(t, "not an ASCII identifier", func() { db.Store("p4", KeyPath("a..b")) })
	mustPanic(t, "not an ASCII identifier", func() { db.Store("p5", KeyPath("1abc")) })
	mustPanic(t, "not an ASCII identifier", func() { db.Store("p6", KeyPath("näme")) })
	mustPanic(t, "1-128 bytes", func() { db.Store("p7", KeyPath(strings.Repeat("a", 129))) })

	mustPanic(t, "single-segment", func() { db.Store("p8", KeyPath("a.b"), AutoKey()) })
	mustPanic(t, "invalid index name", func() { db.Store("p9", Index("By Name", "name")) })
	mustPanic(t, "at least one key path", func() { db.Store("p10", Index("ix")) })
	mustPanic(t, "declared twice", func() { db.Store("p11", Index("ix", "a"), Index("ix", "b")) })
	mustPanic(t, "exactly one key path", func() {
		db.Store("p12", addIndex("ix", &indexSpec{KeyPath: []string{"a", "b"}, MultiEntry: true}))
	})
}

// ManifestJSON is cached per render path; a declaration made after a
// read must still reach the next read.
func TestManifestCacheFollowsDeclarations(t *testing.T) {
	reset()
	t.Cleanup(reset)
	db := New("cache")
	db.Store("a")
	first := string(ManifestJSON())
	db.Store("b")
	if second := string(ManifestJSON()); second == first || !strings.Contains(second, `"b"`) {
		t.Fatalf("manifest after a new store = %s (before: %s)", second, first)
	}
	New("cache2")
	if !strings.Contains(string(ManifestJSON()), `"cache2"`) {
		t.Fatal("a new database is missing from the manifest")
	}
}
