package main

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

const meridianDir = "../../examples/meridian"
const meridianYML = "../../examples/meridian/gofastr.yml"

// ensureMeridianEnv materializes the gitignored .env the generator emits
// alongside the meridian app. A fresh checkout doesn't have it — the seed
// password lives ONLY there, by design — so packing the committed tree
// cannot recover the blueprint's secrets without it. Leaves an existing
// .env (a developer's own generate output) untouched.
func ensureMeridianEnv(t *testing.T, bp Blueprint) {
	t.Helper()
	path := filepath.Join(meridianDir, ".env")
	if _, err := os.Stat(path); err == nil {
		return
	}
	env := renderBlueprintEnv(bp)
	if env == "" {
		return
	}
	if err := os.WriteFile(path, []byte(env), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	t.Cleanup(func() { _ = os.Remove(path) })
}

// TestPack_ReadEntities recovers the entity declarations from the generated
// meridian app's register.go and asserts they equal the parsed YAML's entities
// (order, fields, types, access, indices, relations, properties).
func TestPack_ReadEntities(t *testing.T) {
	a, err := decodeBlueprintFile(meridianYML)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	got, err := packReadEntities(meridianDir)
	if err != nil {
		t.Fatalf("packReadEntities: %v", err)
	}
	if !reflect.DeepEqual(a.Entities, got) {
		t.Errorf("entities mismatch:\n%s", firstBlueprintDiff(a.Entities, got))
	}
}

// firstBlueprintDiff walks two values and returns a description of the first
// path where they differ — far more useful than a giant DeepEqual dump.
func firstBlueprintDiff(a, b any) string {
	if d := diffValue(reflect.ValueOf(a), reflect.ValueOf(b), "blueprint"); d != "" {
		return d
	}
	return "(values are deep-equal)"
}

func diffValue(a, b reflect.Value, path string) string {
	if !a.IsValid() || !b.IsValid() {
		if a.IsValid() != b.IsValid() {
			return fmt.Sprintf("%s: validity differs", path)
		}
		return ""
	}
	if a.Type() != b.Type() {
		return fmt.Sprintf("%s: type %v != %v", path, a.Type(), b.Type())
	}
	switch a.Kind() {
	case reflect.Ptr, reflect.Interface:
		if a.IsNil() || b.IsNil() {
			if a.IsNil() != b.IsNil() {
				return fmt.Sprintf("%s: nil-ness differs (%v vs %v)", path, a.IsNil(), b.IsNil())
			}
			return ""
		}
		return diffValue(a.Elem(), b.Elem(), path)
	case reflect.Struct:
		for i := 0; i < a.NumField(); i++ {
			if d := diffValue(a.Field(i), b.Field(i), path+"."+a.Type().Field(i).Name); d != "" {
				return d
			}
		}
	case reflect.Slice, reflect.Array:
		if a.Len() != b.Len() {
			return fmt.Sprintf("%s: len %d != %d", path, a.Len(), b.Len())
		}
		for i := 0; i < a.Len(); i++ {
			if d := diffValue(a.Index(i), b.Index(i), fmt.Sprintf("%s[%d]", path, i)); d != "" {
				return d
			}
		}
	case reflect.Map:
		if a.Len() != b.Len() {
			return fmt.Sprintf("%s: map len %d != %d (keys %v vs %v)", path, a.Len(), b.Len(), a.MapKeys(), b.MapKeys())
		}
		for _, k := range a.MapKeys() {
			bv := b.MapIndex(k)
			if !bv.IsValid() {
				return fmt.Sprintf("%s: key %v missing in second", path, k)
			}
			if d := diffValue(a.MapIndex(k), bv, fmt.Sprintf("%s[%v]", path, k)); d != "" {
				return d
			}
		}
	default:
		if !reflect.DeepEqual(a.Interface(), b.Interface()) {
			return fmt.Sprintf("%s: %#v != %#v", path, a.Interface(), b.Interface())
		}
	}
	return ""
}

// TestPack_SerializerRoundTrip is the core invariant: serializing a parsed
// blueprint and re-parsing it yields an identical Blueprint. It runs against the
// real Meridian flagship, so it exercises the full breadth of constructs (app +
// auth/admin/theme/dark, 5 entities with access/indices/relations, the marketing
// + app + auth screens with hero/section/card/stat_card+source/charts/entity
// blocks, nav, seed). When a new blueprint feature is added, the decoder AND
// encodeBlueprintYAML must both learn it or this fails.
func TestPack_SerializerRoundTrip(t *testing.T) {
	a, err := decodeBlueprintFile("../../examples/meridian/gofastr.yml")
	if err != nil {
		t.Fatalf("parse meridian.yml: %v", err)
	}
	yml := encodeBlueprintYAML(a)
	b, err := decodeBlueprintString(yml)
	if err != nil {
		t.Fatalf("re-parse serialized yaml: %v\n--- yaml ---\n%s", err, yml)
	}
	if !reflect.DeepEqual(a, b) {
		t.Errorf("round-trip mismatch.\n%s\n--- serialized yaml ---\n%s", firstBlueprintDiff(a, b), yml)
	}
}

func TestPack_ReadSeedAndNav(t *testing.T) {
	a, err := decodeBlueprintFile(meridianYML)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	seed, err := packReadSeed(meridianDir)
	if err != nil {
		t.Fatalf("packReadSeed: %v", err)
	}
	if !reflect.DeepEqual(a.Seed, seed) {
		t.Errorf("seed mismatch:\n%s", firstBlueprintDiff(a.Seed, seed))
	}
	nav, err := packReadNav(meridianDir)
	if err != nil {
		t.Fatalf("packReadNav: %v", err)
	}
	if !reflect.DeepEqual(a.Nav, nav) {
		t.Errorf("nav mismatch:\n%s", firstBlueprintDiff(a.Nav, nav))
	}
}

func TestPack_ReadApp(t *testing.T) {
	a, err := decodeBlueprintFile(meridianYML)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	ensureMeridianEnv(t, a)
	app, err := packReadApp(meridianDir)
	if err != nil {
		t.Fatalf("packReadApp: %v", err)
	}
	if !reflect.DeepEqual(a.App, app) {
		t.Errorf("app mismatch:\n%s", firstBlueprintDiff(a.App, app))
	}
}

func TestPack_ReadScreens(t *testing.T) {
	a, _ := decodeBlueprintFile(meridianYML)
	got, err := packReadScreens(meridianDir)
	if err != nil {
		t.Fatalf("packReadScreens: %v", err)
	}
	if !reflect.DeepEqual(a.Screens, got) {
		t.Errorf("screens mismatch:\n%s", firstBlueprintDiff(a.Screens, got))
	}
}

// TestPack_MeridianRoundTrip is the acceptance gate: packing the generated
// Meridian app reconstructs a Blueprint equal to the one parsed from the
// authored gofastr.yml. As features are added, this catches generator/pack
// divergence.
func TestPack_MeridianRoundTrip(t *testing.T) {
	a, err := decodeBlueprintFile(meridianYML)
	if err != nil {
		t.Fatalf("parse meridian.yml: %v", err)
	}
	ensureMeridianEnv(t, a)
	b, err := packBlueprint(meridianDir)
	if err != nil {
		t.Fatalf("packBlueprint: %v", err)
	}
	if !reflect.DeepEqual(a, b) {
		t.Errorf("round-trip mismatch:\n%s", firstBlueprintDiff(a, b))
	}
}
