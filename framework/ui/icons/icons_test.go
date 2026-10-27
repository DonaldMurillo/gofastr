package icons

import (
	"slices"
	"testing"
)

func TestNamesReturnsOwnedCanonicalCatalog(t *testing.T) {
	names := Names()
	if len(names) != 291 || !slices.IsSorted(names) {
		t.Fatalf("expected the complete pinned catalog of 291 sorted names, got %v", names)
	}
	for _, name := range names {
		if body, ok := Lookup(name); !ok || body == "" {
			t.Fatalf("catalog name %q has no drawing", name)
		}
	}
	names[0] = "changed"
	if Names()[0] == "changed" {
		t.Fatal("caller mutated the catalog")
	}
}

func TestAliasesAndUnknownNames(t *testing.T) {
	for alias, canonical := range Aliases() {
		got, ok := Lookup(alias)
		want, _ := Lookup(canonical)
		if !ok || got != want {
			t.Errorf("alias %q differs from %q", alias, canonical)
		}
	}
	aliases := Aliases()
	aliases["card"] = "changed"
	if Aliases()["card"] != CreditCard {
		t.Fatal("caller mutated alias storage")
	}
	if body, ok := Lookup("missing"); ok || body != "" {
		t.Fatal("unknown name has a drawing")
	}
}
