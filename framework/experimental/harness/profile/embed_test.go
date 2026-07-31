package profile

import "testing"

func TestEmbeddedDefault(t *testing.T) {
	p, err := Embedded("default")
	if err != nil {
		t.Fatalf("Embedded(default): %v", err)
	}
	if p.Name == "" {
		t.Fatalf("embedded default profile has empty name")
	}
}

func TestEmbeddedFramework(t *testing.T) {
	p, err := Embedded("framework")
	if err != nil {
		t.Fatalf("Embedded(framework): %v", err)
	}
	if p.Name == "" {
		t.Fatalf("embedded framework profile has empty name")
	}
}

func TestEmbeddedUnknown(t *testing.T) {
	if _, err := Embedded("nope"); err == nil {
		t.Fatalf("expected error for unknown embedded profile")
	}
}
