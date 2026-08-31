package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework"
)

const meridianDir = "../../examples/meridian"
const meridianYML = "../../examples/meridian/gofastr.yml"

// ensureMeridianEnv materializes the gitignored .env the generator emits
// alongside the meridian app. A fresh checkout doesn't have it; the seed
// password lives ONLY there, by design, so packing the committed tree
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
// path where they differ, far more useful than a giant DeepEqual dump.
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
// blueprint and re-parsing it yields an identical Blueprint. It runs against
// EVERY committed example blueprint, not a hand-picked one: the test used to
// run Meridian alone, and Meridian happens to be the one example that
// declares no middleware/plugins/helpers — the fixture guarding the inverse
// claim was the fixture least able to observe it failing (#318).
func TestPack_SerializerRoundTrip(t *testing.T) {
	matches, err := filepath.Glob("../../examples/*/gofastr.yml")
	if err != nil {
		t.Fatalf("glob examples: %v", err)
	}
	if len(matches) == 0 {
		t.Fatal("no example blueprints found")
	}
	for _, path := range matches {
		path := path
		t.Run(filepath.Base(filepath.Dir(path)), func(t *testing.T) {
			a, err := decodeBlueprintFile(path)
			if err != nil {
				t.Fatalf("parse %s: %v", path, err)
			}
			yml, err := encodeBlueprintYAML(a)
			if err != nil {
				t.Fatalf("serialize: %v", err)
			}
			b, err := decodeBlueprintString(yml)
			if err != nil {
				t.Fatalf("re-parse serialized yaml: %v\n--- yaml ---\n%s", err, yml)
			}
			if !reflect.DeepEqual(a, b) {
				t.Errorf("round-trip mismatch.\n%s\n--- serialized yaml ---\n%s", firstBlueprintDiff(a, b), yml)
			}
		})
	}
}

// TestPack_StubDescriptionRoundTrips exercises stubsToAny's map form: a stub
// with a description serializes as {name, description} and must decode back
// equal. No committed example sets a stub description, so the example-driven
// round-trip above cannot see this path.
func TestPack_StubDescriptionRoundTrips(t *testing.T) {
	a := Blueprint{
		App:        BlueprintApp{Name: "probe"},
		Middleware: []BlueprintNamedStub{{Name: "logger", Description: "logs each request"}},
		Plugins:    []BlueprintNamedStub{{Name: "metrics", Description: "counts things"}},
		Helpers:    []BlueprintNamedStub{{Name: "slugify", Description: "makes slugs"}},
	}
	yml, err := encodeBlueprintYAML(a)
	if err != nil {
		t.Fatalf("serialize: %v", err)
	}
	b, err := decodeBlueprintString(yml)
	if err != nil {
		t.Fatalf("re-parse serialized yaml: %v\n--- yaml ---\n%s", err, yml)
	}
	if !reflect.DeepEqual(a, b) {
		t.Errorf("stub description round-trip mismatch.\n%s\n--- serialized yaml ---\n%s", firstBlueprintDiff(a, b), yml)
	}
}

// TestPack_SeedCountWeightsRoundTrips guards the #330 fix: seed count and
// weights are decoded into BlueprintSeedEntity (blueprint.go decodeBlueprintSeed)
// and were then dropped by the serializer — the same decoded-but-never-emitted
// shape as #318. No committed example uses count/weights, which is exactly how
// the omission stayed invisible to the example round-trip.
func TestPack_SeedCountWeightsRoundTrips(t *testing.T) {
	yml := `seed:
  - entity: ticket
    count: 7
    weights:
      status:
        open: 5
        closed: 1
    rows:
      - title: first
`
	a, err := decodeBlueprintString(yml)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(a.Seed) != 1 || a.Seed[0].Count != 7 || a.Seed[0].Weights["status"]["open"] != 5 {
		t.Fatalf("fixture does not exercise the fix: %+v", a.Seed)
	}
	out, err := encodeBlueprintYAML(a)
	if err != nil {
		t.Fatalf("serialize: %v", err)
	}
	b, err := decodeBlueprintString(out)
	if err != nil {
		t.Fatalf("re-parse serialized yaml: %v\n--- yaml ---\n%s", err, out)
	}
	if !reflect.DeepEqual(a, b) {
		t.Errorf("seed count/weights round-trip mismatch.\n%s\n--- serialized yaml ---\n%s", firstBlueprintDiff(a, b), out)
	}
}

// TestPack_EntityRenamesRoundTrip guards the other omission the construct
// guard caught: entities.<name>.renames is decoded into
// EntityDeclaration.Renames and was never emitted back.
func TestPack_EntityRenamesRoundTrip(t *testing.T) {
	yml := `entities:
  - name: posts
    fields:
      - name: title
        type: string
    renames:
      title: heading
`
	a, err := decodeBlueprintString(yml)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(a.Entities) != 1 || a.Entities[0].Renames["title"] != "heading" {
		t.Fatalf("fixture does not exercise the fix: %+v", a.Entities)
	}
	out, err := encodeBlueprintYAML(a)
	if err != nil {
		t.Fatalf("serialize: %v", err)
	}
	b, err := decodeBlueprintString(out)
	if err != nil {
		t.Fatalf("re-parse serialized yaml: %v\n--- yaml ---\n%s", err, out)
	}
	if !reflect.DeepEqual(a, b) {
		t.Errorf("entity renames round-trip mismatch.\n%s\n--- serialized yaml ---\n%s", firstBlueprintDiff(a, b), out)
	}
}

// TestPack_QuotedValuesRoundTripExactBytes guards the #330 quoting fix: a
// quoted value goes through core/yaml's parseQuoted → strconv.Unquote, which
// replaces invalid UTF-8 bytes with U+FFFD. Before quoteYAMLString escaped
// them, "a\xffb'" re-parsed as "a\ufffdb'" — corruption the quoting itself
// introduced (pre-#323 the value was emitted bare and survived). Non-printable
// runes get the same treatment; printable multi-byte runes must NOT be
// escaped — mangling café to \u00e9 would round-trip but ship unreadable YAML.
func TestPack_QuotedValuesRoundTripExactBytes(t *testing.T) {
	values := []string{
		"a\xffb'",             // invalid UTF-8 byte (the reported corruption)
		"x\x01y'",             // non-printable control rune
		"d\x7fe'",             // DEL
		"n\u00a0m'",           // NBSP: non-printable per unicode.IsPrint, needs \uNNNN
		"caf\u00e9'",          // printable multi-byte: must pass through literally
		"\u65e5\u672c\u8a9e'", // CJK: same
	}
	rows := make([]map[string]any, len(values))
	for i, v := range values {
		rows[i] = map[string]any{"note": v}
	}
	a := Blueprint{Seed: []BlueprintSeedEntity{{Entity: "notes", Rows: rows}}}
	yml, err := encodeBlueprintYAML(a)
	if err != nil {
		t.Fatalf("serialize: %v", err)
	}
	for _, lit := range []string{"café", "日本語"} {
		if !strings.Contains(yml, lit) {
			t.Errorf("serialized yaml escaped the printable multi-byte rune %q instead of emitting it literally:\n%s", lit, yml)
		}
	}
	b, err := decodeBlueprintString(yml)
	if err != nil {
		t.Fatalf("re-parse serialized yaml: %v\n--- yaml ---\n%s", err, yml)
	}
	if !reflect.DeepEqual(a, b) {
		t.Errorf("quoted-value round-trip mismatch.\n%s\n--- serialized yaml ---\n%s", firstBlueprintDiff(a, b), yml)
	}
}

// TestPackSerializerCoversEveryBlueprintField is the durable half of the
// #318 fix. topLevelOrder listed middleware/plugins/helpers all along while
// blueprintToMap silently dropped them; nothing compared the two lists. This
// derives the comparison from the struct itself, so it needs no hand-kept
// fixture: every field Blueprint can carry must make the serializer emit its
// top-level key, and the emitted key set must equal set(topLevelOrder)
// exactly. A key ordered but never emitted is the #318 regression; a key
// emitted but not ordered silently ships unsorted.
//
// Boundary, stated honestly: this guards struct→serializer→order agreement.
// It cannot see decodeBlueprint. A construct added to the decoder and the
// struct but not the serializer stays invisible here; that side is guarded
// by TestPack_SerializerRoundTrip, for every construct a committed example
// actually uses.
func TestPackSerializerCoversEveryBlueprintField(t *testing.T) {
	rt := reflect.TypeOf(Blueprint{})
	emitted := map[string]bool{}
	zero := blueprintToMap(Blueprint{})
	for i := range rt.NumField() {
		bpv := reflect.New(rt).Elem()
		probeLeafValue(t, bpv.Field(i))
		keys := blueprintToMap(bpv.Interface().(Blueprint))
		assertFieldMovesOutput(t, rt.Field(i).Name, zero, keys, "blueprintToMap")
		for k := range keys {
			emitted[k] = true
		}
	}
	assertOrderMatchesEmitted(t, emitted, topLevelOrder, "topLevelOrder")
}

// The app section has the same failure mode one level down: decodeApp read
// description/base_url/public_openapi that appToMap never emitted. No
// committed example sets them, so the example round-trip cannot see this
// class either.
func TestPackSerializerCoversEveryAppField(t *testing.T) {
	rt := reflect.TypeOf(BlueprintApp{})
	emitted := map[string]bool{}
	zero := appToMap(BlueprintApp{})
	for i := range rt.NumField() {
		appv := reflect.New(rt).Elem()
		probeLeafValue(t, appv.Field(i))
		keys := appToMap(appv.Interface().(BlueprintApp))
		assertFieldMovesOutput(t, rt.Field(i).Name, zero, keys, "appToMap")
		for k := range keys {
			emitted[k] = true
		}
	}
	assertOrderMatchesEmitted(t, emitted, appOrder, "appOrder")
}

// TestPackSerializerCoversEveryConstructField extends the two guards above
// one level down: into the structs inside each slice-of-struct construct,
// and (since the pointer/nested-struct extension) into the structs nested
// inside those. The top-level guards prove a construct KEY appears when the
// Blueprint field is set; they cannot see fields dropped inside the element,
// because the key's mere presence already differs from the zero output. That
// is the seed.Count/Weights shape exactly (#330): decoded into
// BlueprintSeedEntity, dropped by blueprintToMap, invisible to every guard
// above and to every committed example. Each construct field must move the
// serializer output — slice elements probed alone, nested structs probed
// leave-one-out — and the keys emitted must match the construct's order
// list where one exists. Before the extension, deleting any single
// emission line inside scope, pagination, auth, or pwa left this guard
// green: the constructs' parent keys moved the output, the fields inside
// them were never probed, and only a committed example using the field
// (SerializerRoundTrip) stood in the way — coverage by accident.
func TestPackSerializerCoversEveryConstructField(t *testing.T) {
	for _, spec := range constructSpecs() {
		t.Run(spec.name, func(t *testing.T) {
			zbp, zeroElem := spec.build()
			// base is what every field probe is compared against. Slice
			// constructs probe one field alone and compare against the
			// zero element's output — the empty construct container is
			// already present on both sides, so only the probed field's
			// emission can differ. Nested structs cannot work that way:
			// their container only emits when a gate sibling is set (pwa
			// needs Enabled, screen access needs Auth or Role, auth needs
			// any of four fields), so probing jwt_secret alone never opens
			// the container and the guard would fail a healthy serializer.
			// Nested specs invert the probe: every leaf set — container
			// guaranteed open — then exactly one field zeroed per probe.
			// The zeroed field's absence must be visible in the output; a
			// dropped emission line makes the probe identical to base,
			// which is the red.
			base := blueprintToMap(*zbp)
			if spec.leaveOneOut {
				probeLeafValue(t, zeroElem)
				base = blueprintToMap(*zbp)
			}
			emitted := map[string]bool{}
			rt := zeroElem.Type()
			for i := range rt.NumField() {
				bp, elem := spec.build()
				if spec.leaveOneOut {
					probeLeafValue(t, elem)
					elem.Field(i).Set(reflect.Zero(rt.Field(i).Type))
				} else {
					probeLeafValue(t, elem.Field(i))
				}
				out := blueprintToMap(*bp)
				field := spec.name + "." + rt.Field(i).Name
				if reason, ok := constructOmissions[field]; ok {
					// The exemption must be total: the probed field may not
					// leak into the output either.
					if !reflect.DeepEqual(base, out) {
						t.Errorf("%s is exempted (%s) but probing it changes the output: an exempted field must be dropped completely", field, reason)
					}
					continue
				}
				assertFieldMovesOutput(t, field, base, out, "blueprintToMap")
				if !spec.leaveOneOut {
					if m := spec.at(out); m != nil {
						for k := range m {
							emitted[k] = true
						}
					}
				}
			}
			if spec.leaveOneOut {
				if m := spec.at(base); m != nil {
					for k := range m {
						emitted[k] = true
					}
				}
			}
			switch {
			case spec.order == nil:
				if got := orderFor(spec.key); got != nil {
					t.Errorf("orderFor(%q) = %v but the %s spec carries no order list; pass it so emitted keys are checked against it", spec.key, got, spec.name)
				}
			case spec.orderSubset:
				// accessOrder spans two constructs (screen access emits
				// auth/role, entity access emits read/create/update/delete),
				// so set equality cannot hold per construct. The inverse
				// direction — every listed key emitted by SOME construct —
				// is pinned by the per-field probes above: each list key
				// maps to a struct field, and a field that stops emitting
				// fails its own leave-one-out probe.
				ordered := map[string]bool{}
				for _, k := range spec.order {
					ordered[k] = true
				}
				for k := range emitted {
					if !ordered[k] {
						t.Errorf("%s does not order %q, which the serializer emits; keys outside the order ship unsorted", spec.orderName, k)
					}
				}
			default:
				assertOrderMatchesEmitted(t, emitted, spec.order, spec.orderName)
			}
		})
	}
}

// KNOWN FRONTIER — what this guard still cannot see, measured, not guessed.
//
// The extension above reaches slice elements and one level of nested struct.
// It does NOT reach structs nested inside a block's props. Deleting any of
// these single emission lines from pack.go leaves the whole pack + kiln +
// generator surface green today:
//
//	pwa.enabled
//	p.eyebrow, p.title, p.subtitle          (block props)
//	src.entity, src.agg, src.field          (props source)
//	plan.name, plan.price, plan.period      (props pricing plan)
//
// None is a live bug: every one of those keys is emitted correctly right now.
// What is missing is anything that would notice if it stopped — the #318
// shape, two levels further down than #330 reached.
//
// This list exists because the previous extension was declared to close the
// class and did not. If you extend the guard again, re-derive this list by
// deleting emission lines rather than trusting it, and shrink it here.

// constructOmissions names construct fields that deliberately do NOT reach
// the serializer output, with the reason. An omission listed here is part
// of the contract; an omission not listed here fails the guard above.
var constructOmissions = map[string]string{
	"entities.Endpoints": "derived runtime wiring, not an authoring key: the decoder splits entity-level endpoints into decl.Endpoints (Method/Path/Name/Description, MCP hard-false, handler dropped) and a top-level Blueprint.Endpoints stub carrying the full authoring form (entity, handler, mcp). Emitting decl.Endpoints back under the entity would duplicate every endpoint on re-parse.",
	"indices.Expression": "framework.Index is shared with hand-written Go configs, which support expression indexes; the blueprint grammar's indices allow-list is name/columns/unique only (decodeIndices), so Expression can never be authored in YAML and emitting it would fail rejectUnknownKeys on re-parse.",
}

// constructSpec: how to build a Blueprint holding one zero element of a
// construct, and where that element's emitted map lands in blueprintToMap's
// output. at returns nil when the element emits a non-map (a bare stub
// scalar), so key collection skips it.
type constructSpec struct {
	name      string
	order     []string
	orderName string
	// key is the construct's emitted YAML container key. Consumed only when
	// order is nil, to pin that the writer really has no order list for it
	// (orderedKeys then sorts it alphabetically): a nil order is a checked
	// contract, not a forgotten field. If orderFor learns the key, the spec
	// must grow the list.
	key string
	// leaveOneOut marks a spec whose element is a nested struct reached
	// through a parent (App.Auth, Screen.Access, Entity.Scope, …) rather
	// than a slice element. These probe leave-one-out instead of
	// probe-alone; see the test body for why probe-alone false-reds here.
	leaveOneOut bool
	// orderSubset marks an order list shared by several constructs
	// (accessOrder spans screen access and entity access): assert only that
	// every emitted key is ordered, not set equality.
	orderSubset bool
	build       func() (*Blueprint, reflect.Value)
	at          func(map[string]any) map[string]any
}

func constructSpecs() []constructSpec {
	return []constructSpec{
		{
			name: "entities", order: entityOrder, orderName: "entityOrder",
			build: func() (*Blueprint, reflect.Value) {
				bp := &Blueprint{Entities: []framework.EntityDeclaration{{}}}
				return bp, reflect.ValueOf(&bp.Entities[0]).Elem()
			},
			at: func(out map[string]any) map[string]any { return digEmitted(out, "entities", 0) },
		},
		{
			name: "fields", order: fieldOrder, orderName: "fieldOrder",
			build: func() (*Blueprint, reflect.Value) {
				bp := &Blueprint{Entities: []framework.EntityDeclaration{{Fields: []framework.FieldDeclaration{{}}}}}
				return bp, reflect.ValueOf(&bp.Entities[0].Fields[0]).Elem()
			},
			at: func(out map[string]any) map[string]any { return digEmitted(out, "entities", 0, "fields", 0) },
		},
		{
			name: "relations", order: relationOrder, orderName: "relationOrder",
			build: func() (*Blueprint, reflect.Value) {
				bp := &Blueprint{Entities: []framework.EntityDeclaration{{Relations: []framework.Relation{{}}}}}
				return bp, reflect.ValueOf(&bp.Entities[0].Relations[0]).Elem()
			},
			at: func(out map[string]any) map[string]any { return digEmitted(out, "entities", 0, "relations", 0) },
		},
		{
			name: "indices", order: indexOrder, orderName: "indexOrder",
			build: func() (*Blueprint, reflect.Value) {
				bp := &Blueprint{Entities: []framework.EntityDeclaration{{Indices: []framework.Index{{}}}}}
				return bp, reflect.ValueOf(&bp.Entities[0].Indices[0]).Elem()
			},
			at: func(out map[string]any) map[string]any { return digEmitted(out, "entities", 0, "indices", 0) },
		},
		{
			name: "screens", order: screenOrder, orderName: "screenOrder",
			build: func() (*Blueprint, reflect.Value) {
				bp := &Blueprint{Screens: []BlueprintScreen{{}}}
				return bp, reflect.ValueOf(&bp.Screens[0]).Elem()
			},
			at: func(out map[string]any) map[string]any { return digEmitted(out, "screens", 0) },
		},
		{
			name: "body", order: blockOrder, orderName: "blockOrder",
			build: func() (*Blueprint, reflect.Value) {
				bp := &Blueprint{Screens: []BlueprintScreen{{Body: []BlueprintBlock{{}}}}}
				return bp, reflect.ValueOf(&bp.Screens[0].Body[0]).Elem()
			},
			at: func(out map[string]any) map[string]any { return digEmitted(out, "screens", 0, "body", 0) },
		},
		{
			name: "children", order: blockOrder, orderName: "blockOrder",
			build: func() (*Blueprint, reflect.Value) {
				bp := &Blueprint{Screens: []BlueprintScreen{{Body: []BlueprintBlock{{Children: []BlueprintBlock{{}}}}}}}
				return bp, reflect.ValueOf(&bp.Screens[0].Body[0].Children[0]).Elem()
			},
			at: func(out map[string]any) map[string]any {
				return digEmitted(out, "screens", 0, "body", 0, "children", 0)
			},
		},
		{
			name: "actions", order: actionOrder, orderName: "actionOrder",
			build: func() (*Blueprint, reflect.Value) {
				bp := &Blueprint{Screens: []BlueprintScreen{{Body: []BlueprintBlock{{Actions: []BlueprintAction{{}}}}}}}
				return bp, reflect.ValueOf(&bp.Screens[0].Body[0].Actions[0]).Elem()
			},
			at: func(out map[string]any) map[string]any {
				return digEmitted(out, "screens", 0, "body", 0, "actions", 0)
			},
		},
		{
			name: "transitions", order: transitionOrder, orderName: "transitionOrder",
			build: func() (*Blueprint, reflect.Value) {
				bp := &Blueprint{Screens: []BlueprintScreen{{Body: []BlueprintBlock{{Transitions: []BlueprintTransition{{}}}}}}}
				return bp, reflect.ValueOf(&bp.Screens[0].Body[0].Transitions[0]).Elem()
			},
			at: func(out map[string]any) map[string]any {
				return digEmitted(out, "screens", 0, "body", 0, "transitions", 0)
			},
		},
		{
			name: "nav", order: navOrder, orderName: "navOrder",
			build: func() (*Blueprint, reflect.Value) {
				bp := &Blueprint{Nav: []BlueprintNavItem{{}}}
				return bp, reflect.ValueOf(&bp.Nav[0]).Elem()
			},
			at: func(out map[string]any) map[string]any { return digEmitted(out, "nav", 0) },
		},
		{
			name: "items", order: navOrder, orderName: "navOrder",
			build: func() (*Blueprint, reflect.Value) {
				bp := &Blueprint{Nav: []BlueprintNavItem{{Items: []BlueprintNavItem{{}}}}}
				return bp, reflect.ValueOf(&bp.Nav[0].Items[0]).Elem()
			},
			at: func(out map[string]any) map[string]any { return digEmitted(out, "nav", 0, "items", 0) },
		},
		{
			name: "seed", order: seedOrder, orderName: "seedOrder",
			build: func() (*Blueprint, reflect.Value) {
				bp := &Blueprint{Seed: []BlueprintSeedEntity{{}}}
				return bp, reflect.ValueOf(&bp.Seed[0]).Elem()
			},
			at: func(out map[string]any) map[string]any { return digEmitted(out, "seed", 0) },
		},
		{
			name: "endpoints", order: endpointOrder, orderName: "endpointOrder",
			build: func() (*Blueprint, reflect.Value) {
				bp := &Blueprint{Endpoints: []BlueprintEndpoint{{}}}
				return bp, reflect.ValueOf(&bp.Endpoints[0]).Elem()
			},
			at: func(out map[string]any) map[string]any { return digEmitted(out, "endpoints", 0) },
		},
		{
			name: "stubs", order: stubOrder, orderName: "stubOrder",
			build: func() (*Blueprint, reflect.Value) {
				bp := &Blueprint{Middleware: []BlueprintNamedStub{{}}}
				return bp, reflect.ValueOf(&bp.Middleware[0]).Elem()
			},
			at: func(out map[string]any) map[string]any { return digEmitted(out, "middleware", 0) },
		},
		// ----- nested structs, probed leave-one-out (see constructSpec) -----
		{
			name: "auth", order: authOrder, orderName: "authOrder", key: "auth", leaveOneOut: true,
			build: func() (*Blueprint, reflect.Value) {
				bp := &Blueprint{App: BlueprintApp{Auth: BlueprintAuth{}}}
				return bp, reflect.ValueOf(&bp.App.Auth).Elem()
			},
			at: func(out map[string]any) map[string]any { return digEmitted(out, "app", "auth") },
		},
		{
			name: "admin", order: adminOrder, orderName: "adminOrder", key: "admin", leaveOneOut: true,
			build: func() (*Blueprint, reflect.Value) {
				bp := &Blueprint{App: BlueprintApp{Admin: BlueprintAdmin{}}}
				return bp, reflect.ValueOf(&bp.App.Admin).Elem()
			},
			at: func(out map[string]any) map[string]any { return digEmitted(out, "app", "admin") },
		},
		{
			// No order list exists for pwa: orderFor("pwa") is nil and the
			// writer sorts its keys alphabetically.
			name: "pwa", key: "pwa", leaveOneOut: true,
			build: func() (*Blueprint, reflect.Value) {
				bp := &Blueprint{App: BlueprintApp{PWA: BlueprintPWA{}}}
				return bp, reflect.ValueOf(&bp.App.PWA).Elem()
			},
			at: func(out map[string]any) map[string]any { return digEmitted(out, "app", "pwa") },
		},
		{
			// accessOrder is shared with exposure.access below: subset check.
			name: "access", order: accessOrder, orderName: "accessOrder", key: "access",
			leaveOneOut: true, orderSubset: true,
			build: func() (*Blueprint, reflect.Value) {
				bp := &Blueprint{Screens: []BlueprintScreen{{Access: BlueprintAccess{}}}}
				return bp, reflect.ValueOf(&bp.Screens[0].Access).Elem()
			},
			at: func(out map[string]any) map[string]any { return digEmitted(out, "screens", 0, "access") },
		},
		{
			name: "scope", key: "scope", leaveOneOut: true, // orderFor("scope") is nil: sorted
			build: func() (*Blueprint, reflect.Value) {
				bp := &Blueprint{Entities: []framework.EntityDeclaration{{Scope: &framework.ScopeDeclaration{}}}}
				return bp, reflect.ValueOf(bp.Entities[0].Scope).Elem()
			},
			at: func(out map[string]any) map[string]any { return digEmitted(out, "entities", 0, "scope") },
		},
		{
			name: "pagination", key: "pagination", leaveOneOut: true, // orderFor("pagination") is nil: sorted
			build: func() (*Blueprint, reflect.Value) {
				bp := &Blueprint{Entities: []framework.EntityDeclaration{{Pagination: &framework.PaginationDeclaration{}}}}
				return bp, reflect.ValueOf(bp.Entities[0].Pagination).Elem()
			},
			at: func(out map[string]any) map[string]any { return digEmitted(out, "entities", 0, "pagination") },
		},
		{
			name: "exposure", key: "exposure", leaveOneOut: true, // orderFor("exposure") is nil: sorted
			build: func() (*Blueprint, reflect.Value) {
				bp := &Blueprint{Entities: []framework.EntityDeclaration{{Exposure: &framework.ExposureDeclaration{}}}}
				return bp, reflect.ValueOf(bp.Entities[0].Exposure).Elem()
			},
			at: func(out map[string]any) map[string]any { return digEmitted(out, "entities", 0, "exposure") },
		},
		{
			name: "exposure.access", order: accessOrder, orderName: "accessOrder", key: "access",
			leaveOneOut: true, orderSubset: true,
			build: func() (*Blueprint, reflect.Value) {
				bp := &Blueprint{Entities: []framework.EntityDeclaration{{
					Exposure: &framework.ExposureDeclaration{Access: &framework.AccessDeclaration{}},
				}}}
				return bp, reflect.ValueOf(bp.Entities[0].Exposure.Access).Elem()
			},
			at: func(out map[string]any) map[string]any {
				return digEmitted(out, "entities", 0, "exposure", "access")
			},
		},
		{
			name: "read_scope", order: readScopeOrder, orderName: "readScopeOrder", key: "read_scope", leaveOneOut: true,
			build: func() (*Blueprint, reflect.Value) {
				bp := &Blueprint{Entities: []framework.EntityDeclaration{{
					Exposure: &framework.ExposureDeclaration{ReadScope: &framework.ReadScopeDeclaration{}},
				}}}
				return bp, reflect.ValueOf(bp.Entities[0].Exposure.ReadScope).Elem()
			},
			at: func(out map[string]any) map[string]any {
				return digEmitted(out, "entities", 0, "exposure", "read_scope")
			},
		},
		{
			name: "filter", order: predicateOrder, orderName: "predicateOrder", key: "filter", leaveOneOut: true,
			build: func() (*Blueprint, reflect.Value) {
				bp := &Blueprint{Entities: []framework.EntityDeclaration{{
					Exposure: &framework.ExposureDeclaration{
						ReadScope: &framework.ReadScopeDeclaration{Filter: []framework.RowPredicateDeclaration{{}}},
					},
				}}}
				return bp, reflect.ValueOf(&bp.Entities[0].Exposure.ReadScope.Filter[0]).Elem()
			},
			at: func(out map[string]any) map[string]any {
				return digEmitted(out, "entities", 0, "exposure", "read_scope", "filter", 0)
			},
		},
	}
}

// digEmitted follows a path of map keys (string) and list indices (int)
// into a blueprintToMap result and returns the map at the end, or nil when
// the path is absent or ends at a non-map (a bare stub scalar).
func digEmitted(v any, path ...any) map[string]any {
	for _, p := range path {
		switch p := p.(type) {
		case string:
			m, ok := v.(map[string]any)
			if !ok {
				return nil
			}
			v, ok = m[p]
			if !ok {
				return nil
			}
		case int:
			l, ok := v.([]any)
			if !ok || p >= len(l) {
				return nil
			}
			v = l[p]
		}
	}
	m, _ := v.(map[string]any)
	return m
}

// assertFieldMovesOutput closes the hole the two set-agreement checks above
// leave open. They compare the serializer against the key order, so a construct
// BOTH are ignorant of cancels out and passes silently — which is the #318
// shape exactly: Middleware was decoded into the struct, never emitted, and no
// committed example carried it, so neither the order check nor the example
// round-trip could see it. The struct field is the ground truth, so populating
// any one field must change what the serializer writes.
//
// Comparing against the zero-value output rather than checking for a non-empty
// map is load-bearing: a zero BlueprintApp already emits api_prefix (the
// condition is != "api", which "" satisfies), so "emitted something" is true
// for every field and proves nothing. Watched both forms against a Widgets
// field that neither the serializer nor topLevelOrder knows about: the
// non-empty form passes, this one fails.
func assertFieldMovesOutput(t *testing.T, field string, zero, probed map[string]any, fn string) {
	t.Helper()
	if reflect.DeepEqual(zero, probed) {
		t.Errorf("%s writes the same output whether %s is set or not: the field decodes into the struct and serializes to nothing, and no key-order check can see that", fn, field)
	}
}

// probeLeafValue sets v to a non-zero value the serializers treat as
// "present". The exact value is irrelevant; emission is keyed on
// non-emptiness. Structs get every leaf set so Enabled-only emission
// conditions (auth, admin, pwa) fire regardless of field order.
func probeLeafValue(t *testing.T, v reflect.Value) {
	t.Helper()
	probeLeaf(t, v, 0)
}

// probeLeafDepthBound caps probeLeaf's recursion. Slice ELEMENTS are probed
// as well — a one-element slice holding a zero-valued element is exactly how
// seed.Count/Weights stayed invisible to the coverage guards — but
// BlueprintBlock.Children and BlueprintNavItem.Items contain themselves, so
// the walk needs a depth bound or it does not terminate.
const probeLeafDepthBound = 8

func probeLeaf(t *testing.T, v reflect.Value, depth int) {
	t.Helper()
	if !v.CanSet() {
		t.Fatalf("probe: field %s of %s cannot be set", v.String(), v.Type())
	}
	if depth > probeLeafDepthBound {
		return
	}
	switch v.Kind() {
	case reflect.String:
		v.SetString("probe")
	case reflect.Bool:
		v.SetBool(true)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v.SetInt(1)
	case reflect.Float32, reflect.Float64:
		v.SetFloat(1)
	case reflect.Map:
		m := reflect.MakeMap(v.Type())
		k := reflect.New(v.Type().Key()).Elem()
		ev := reflect.New(v.Type().Elem()).Elem()
		probeLeaf(t, k, depth+1)
		probeLeaf(t, ev, depth+1)
		m.SetMapIndex(k, ev)
		v.Set(m)
	case reflect.Slice:
		v.Set(reflect.MakeSlice(v.Type(), 1, 1))
		probeLeaf(t, v.Index(0), depth+1)
	case reflect.Ptr:
		// Recurse into the pointee, don't stop at allocating it: a zero
		// pointee only proves the nil-gated container opens, never that the
		// serializer sees the fields inside it. Pointees left zero are how
		// Scope/Exposure/Pagination fields stayed invisible to this walk.
		v.Set(reflect.New(v.Type().Elem()))
		probeLeaf(t, v.Elem(), depth+1)
	case reflect.Interface:
		// Only the empty interface accepts an arbitrary probe string. Named
		// interfaces (entity.Endpoint's http.Handler, mcp.ToolHandler) are
		// runtime wiring, never YAML; leave them nil rather than panic.
		if v.Type().NumMethod() == 0 {
			v.Set(reflect.ValueOf("probe"))
		}
	case reflect.Struct:
		for i := range v.NumField() {
			probeLeaf(t, v.Field(i), depth+1)
		}
	}
}

func assertOrderMatchesEmitted(t *testing.T, emitted map[string]bool, order []string, name string) {
	t.Helper()
	ordered := map[string]bool{}
	for _, k := range order {
		ordered[k] = true
	}
	for k := range emitted {
		if !ordered[k] {
			t.Errorf("%s does not order %q, which the serializer emits; keys outside the order ship unsorted", name, k)
		}
	}
	for k := range ordered {
		if !emitted[k] {
			t.Errorf("%s lists %q but the serializer never emits it — the #318 regression (ordered, decoded, dropped)", name, k)
		}
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

// TestPackSelfReferentialHelperNoHang verifies the hop-depth bound in
// reverseEntityResource: a self-referential or mutually-recursive zero-arg
// helper must break the walk, not loop forever.
func TestPackSelfReferentialHelperNoHang(t *testing.T) {
	src := `package screens
func a() interface{} { return a() }
func b() interface{} { return c() }
func c() interface{} { return b() }
`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "screens.go", src, 0)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	helpers := packHelperReturns(f)

	// Construct a().List(ctx): the call shape reverseEntityResource walks.
	selfRef := &ast.CallExpr{
		Fun: &ast.SelectorExpr{
			X:   &ast.CallExpr{Fun: &ast.Ident{Name: "a"}},
			Sel: &ast.Ident{Name: "List"},
		},
		Args: []ast.Expr{&ast.Ident{Name: "ctx"}},
	}
	// Mutually recursive: b() → c() → b() → …
	mutual := &ast.CallExpr{
		Fun: &ast.SelectorExpr{
			X:   &ast.CallExpr{Fun: &ast.Ident{Name: "b"}},
			Sel: &ast.Ident{Name: "List"},
		},
		Args: []ast.Expr{&ast.Ident{Name: "ctx"}},
	}

	for name, call := range map[string]*ast.CallExpr{"self-ref": selfRef, "mutual": mutual} {
		done := make(chan struct{})
		go func(call *ast.CallExpr) {
			defer close(done)
			if _, ok := reverseBlock(call, helpers); ok {
				t.Errorf("%s: expected not-reversible", name)
			}
		}(call)
		select {
		case <-done:
			// returned: bound works
		case <-time.After(3 * time.Second):
			t.Fatalf("%s: reverseBlock hung (hop-depth bound missing)", name)
		}
	}
}

// materializeBlueprint renders bp to a temp dir and returns the dir: the
// on-disk shape gofastr pack reads back.
func materializeBlueprint(t *testing.T, bp Blueprint) string {
	t.Helper()
	files, err := renderBlueprintFiles(bp)
	if err != nil {
		t.Fatalf("renderBlueprintFiles: %v", err)
	}
	dir := t.TempDir()
	for _, f := range files {
		p := filepath.Join(dir, f.name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(f.content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// TestPack_TimestampsEntityRoundTrips guards that an entity declared with
// timestamps: true survives generate→pack. The generator emits the config as
// EntityConfig{...}.WithTimestamps(true), so pack must see through the method
// wrapper to recover the fields. Otherwise the whole entity config is lost.
func TestPack_TimestampsEntityRoundTrips(t *testing.T) {
	ts := true
	bp := Blueprint{
		App: BlueprintApp{Name: "TS", Module: "example.com/ts", DBDriver: "sqlite", DBURL: "file:x.db"},
		Entities: []framework.EntityDeclaration{{
			Name:       "posts",
			Timestamps: &ts,
			Fields: []framework.FieldDeclaration{
				{Name: "title", Type: "string", Required: true},
			},
		}},
	}
	dir := materializeBlueprint(t, bp)
	got, err := packReadEntities(dir)
	if err != nil {
		t.Fatalf("packReadEntities: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("recovered %d entities, want 1", len(got))
	}
	if got[0].Timestamps == nil || !*got[0].Timestamps {
		t.Errorf("timestamps lost: %+v", got[0].Timestamps)
	}
	if len(got[0].Fields) != 1 || got[0].Fields[0].Name != "title" {
		t.Fatalf("fields lost through the WithTimestamps wrapper: %+v", got[0].Fields)
	}
}

func TestPack_GroupedEntityConfigsRoundTrip(t *testing.T) {
	crud := false
	bp := Blueprint{
		App: BlueprintApp{Name: "Grouped", Module: "example.com/grouped", DBDriver: "sqlite", DBURL: "file:x.db"},
		Entities: []framework.EntityDeclaration{{
			Name: "notes", Fields: []framework.FieldDeclaration{{Name: "title", Type: "string"}},
			Scope:      &framework.ScopeDeclaration{OwnerField: "user_id", SoftDelete: true},
			Pagination: &framework.PaginationDeclaration{CursorFields: []string{"created_at", "id"}, MaxListLimit: 50},
			Exposure:   &framework.ExposureDeclaration{CRUD: &crud, MCP: true, Access: &framework.AccessDeclaration{Read: "notes:read"}},
		}},
	}
	dir := materializeBlueprint(t, bp)
	got, err := packReadEntities(dir)
	if err != nil {
		t.Fatalf("packReadEntities: %v", err)
	}
	if len(got) != 1 || got[0].Scope == nil || got[0].Pagination == nil || got[0].Exposure == nil {
		t.Fatalf("grouped configs lost: %#v", got)
	}
	if got[0].Scope.OwnerField != "user_id" || !got[0].Scope.SoftDelete || got[0].Pagination.MaxListLimit != 50 {
		t.Fatalf("grouped values changed: %#v", got[0])
	}
	if got[0].Exposure.CRUD == nil || *got[0].Exposure.CRUD || !got[0].Exposure.MCP || got[0].Exposure.Access.Read != "notes:read" {
		t.Fatalf("grouped exposure changed: %#v", got[0].Exposure)
	}
	m := entityToMap(got[0])
	if _, ok := m["scope"]; !ok {
		t.Fatalf("packed YAML map flattened scope: %#v", m)
	}
	if _, ok := m["pagination"]; !ok {
		t.Fatalf("packed YAML map flattened pagination: %#v", m)
	}
	if _, ok := m["exposure"]; !ok {
		t.Fatalf("packed YAML map flattened exposure: %#v", m)
	}
	for _, flat := range []string{
		"soft_delete", "multi_tenant", "tenant_field", "owner_field",
		"cross_owner_read", "cursor_field", "cursor_fields", "max_list_limit",
		"crud", "mcp", "public", "access",
	} {
		if _, ok := m[flat]; ok {
			t.Errorf("packed YAML map contains removed flat key %q: %#v", flat, m)
		}
	}
}

// TestPack_PublicEntityRoundTrips guards that a public entity survives
// generate and pack without moving exposure.public back to a flat key.
func TestPack_PublicEntityRoundTrips(t *testing.T) {
	bp := Blueprint{
		App:      BlueprintApp{Name: "Pub", Module: "example.com/pub", DBDriver: "sqlite", DBURL: "file:x.db"},
		Entities: []framework.EntityDeclaration{{Scope: &framework.ScopeDeclaration{}, Pagination: &framework.PaginationDeclaration{}, Name: "posts", Exposure: &framework.ExposureDeclaration{Public: true}, Fields: []framework.FieldDeclaration{{Name: "title", Type: "string", Required: true}}}},
	}
	dir := materializeBlueprint(t, bp)
	got, err := packReadEntities(dir)
	if err != nil {
		t.Fatalf("packReadEntities: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("recovered %d entities, want 1", len(got))
	}
	if !got[0].Exposure.Public {
		t.Errorf("public flag lost on read-back: Public=%v", got[0].Exposure.Public)
	}
	m := entityToMap(got[0])
	exposure, ok := m["exposure"].(map[string]any)
	if !ok || exposure["public"] != true {
		t.Errorf("entityToMap exposure = %#v, want public: true", m["exposure"])
	}
	if _, ok := m["public"]; ok {
		t.Errorf("entityToMap emitted removed flat public key: %#v", m)
	}
}

// TestPack_ScreenlessAppRoundTrips guards that packing a valid generated app
// with entities but no screens does not error on a missing screens.go.
func TestPack_ScreenlessAppRoundTrips(t *testing.T) {
	bp := Blueprint{
		App: BlueprintApp{Name: "NoScreens", Module: "example.com/noscreens", DBDriver: "sqlite", DBURL: "file:x.db"},
		Entities: []framework.EntityDeclaration{{
			Name:   "posts",
			Fields: []framework.FieldDeclaration{{Name: "title", Type: "string", Required: true}},
		}},
	}
	dir := materializeBlueprint(t, bp)
	packed, err := packBlueprint(dir)
	if err != nil {
		t.Fatalf("packBlueprint on a screenless app errored: %v", err)
	}
	if len(packed.Screens) != 0 {
		t.Errorf("recovered %d screens, want 0", len(packed.Screens))
	}
	if len(packed.Entities) != 1 {
		t.Errorf("recovered %d entities, want 1", len(packed.Entities))
	}
}

// packReadNav reads sidebarConfig's AST, so it depends on the SHAPE the
// generator emits. When the sidebar started resolving its auth control per
// request, that builder changed from a single `return ui.SidebarConfig{…}` to
// `cfg := ui.SidebarConfig{…}` … `return cfg`, and the reader, which asserted
// the returned expression was a composite literal, began reporting no nav at
// all. It reported it silently: "no nav" and "nav could not be read" are the
// same answer.
//
// Meridian is hand-maintained, so its own round-trip test only caught this
// once meridian was edited to match. This drives the GENERATOR's output
// directly, which is the shape every blueprint actually ships.
func TestPackReadsNavFromAGeneratedAuthApp(t *testing.T) {
	bp := Blueprint{
		App: BlueprintApp{
			Name: "Navy", Module: "example.com/navy",
			DBDriver: "sqlite", DBURL: "file:navy.db",
			// Auth on is what makes the generator emit the per-request
			// builder, the shape that broke the reader.
			Auth: BlueprintAuth{Enabled: true},
		},
		Entities: []framework.EntityDeclaration{{
			Name:   "notes",
			Fields: []framework.FieldDeclaration{{Name: "title", Type: "string"}},
		}},
		Nav: []BlueprintNavItem{
			{Label: "Notes", Href: "/notes"},
			{Label: "Admin Console", Href: "/admin", Role: "admin"},
		},
	}
	dir := materializeBlueprint(t, bp)

	nav, err := packReadNav(dir)
	if err != nil {
		t.Fatalf("packReadNav: %v", err)
	}
	if len(nav) != len(bp.Nav) {
		t.Fatalf("packReadNav returned %d items, want %d — the reader lost the generator's sidebar shape:\n%#v", len(nav), len(bp.Nav), nav)
	}
	for i, want := range bp.Nav {
		if nav[i].Label != want.Label || nav[i].Href != want.Href || nav[i].Role != want.Role {
			t.Errorf("nav[%d] = %+v, want %+v", i, nav[i], want)
		}
	}
}
