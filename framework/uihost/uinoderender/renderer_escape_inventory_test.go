package uinoderender

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/uinodev1"
)

// Property: the escapeSurfaces inventory in renderer_security_test.go is
// COMPLETE. escapeSurfaces is a hand-written table; without this check a
// new free-text string field could be added to a prop struct and wired
// through the renderer with no entry, and TestModuleStringsEscapeAtEveryProp
// would still pass — it only exercises the entries it has. This test kills
// that drift by deriving the authoritative field set from the uinodev1
// sources instead of restating it:
//
//   - componentDecoders (registry.go) is the closed component → prop-struct
//     mapping; a component cannot decode without an entry there.
//   - propsMarker() receivers seal the Props union; a struct without the
//     method cannot implement Props.
//   - every string field reachable from a prop struct is enumerated,
//     recursively through nested structs and slices (DetailItem,
//     DataColumn, DataCell, ...).
//
// Reflection cannot replace the parse: Go cannot enumerate a package's
// types at runtime, and a hand-written list of prop structs would just
// move the drift. The sources named above are the same authority the
// reviewer named.
//
// Coverage is MEASURED, not trusted: each escapeSurfaces entry's own tree
// builder is driven with a probe string, the tree is Validated, and the
// validated tree is reflect-walked to find which string field(s) the probe
// landed in. An entry cannot claim a field it does not actually exercise,
// and a stale entry names a field that no longer exists.
//
// Fields legitimately absent from escapeSurfaces live in escapeExemptions
// below, each with a written reason AND a machine-checked premise: the
// validator must REJECT a hostile payload in that field while ACCEPTING
// the same tree with a valid value — proving the rejection comes from the
// field's own guard (enum / URL check), not from a rotten tree. If that
// guard is ever relaxed, the premise breaks loudly here and the field must
// move into escapeSurfaces.
//
// Scope: props string fields only. Node.ActionRef is a module-controlled
// string outside the prop structs; the renderer never emits it verbatim
// (it is resolved to a host-assigned URL through ActionResolver) and the
// validator bounds its shape. If ActionRef ever becomes renderable
// verbatim it needs its own surface entry.

// escapeProbe is injected through each escapeSurfaces tree builder to
// discover which prop string field the entry actually exercises. Pure
// alphanumerics so it passes every content check that only bounds length.
const escapeProbe = "escinvprobe7"

// escapeExemptions: schema string fields with no escapeSurfaces entry.
// Enum fields: the validator rejects every value outside the fixed set,
// so a payload cannot reach Render — the reject/accept pair below pins
// that at test time. URL fields: guarded by IsValidHostRelative and also
// pinned end-to-end by TestURLPropsRejectSchemePayloads.
//
// The `field` value must equal the Go identity produced by the schema
// walk (Type.Field.ElemType.Field...); the failure messages of
// TestEscapeInventoryMatchesSchema print the exact string to paste.
var escapeExemptions = []struct {
	field  string
	reason string
	valid  string                    // a value that MUST validate in this field
	tree   func(value string) string // same tree with value in the exempted field
}{
	{"StackProps.Direction", "enum: horizontal|vertical; any other value fails Validate before Render", "vertical", func(v string) string {
		return `{"component":"stack","props":{"direction":"` + v + `"}}`
	}},
	{"StackProps.Align", "enum: start|center|end|stretch", "center", func(v string) string {
		return `{"component":"stack","props":{"align":"` + v + `"}}`
	}},
	{"ClusterProps.Align", "enum: start|center|end|stretch", "center", func(v string) string {
		return `{"component":"cluster","props":{"align":"` + v + `"}}`
	}},
	{"CardProps.Elevation", "enum: flat|low|high", "low", func(v string) string {
		return `{"component":"card","props":{"elevation":"` + v + `"}}`
	}},
	{"BadgeProps.Tone", "enum: neutral|positive|negative|warning|info", "info", func(v string) string {
		return `{"component":"badge","props":{"text":"T","tone":"` + v + `"}}`
	}},
	{"StatCardProps.Trend", "enum: up|down|flat", "up", func(v string) string {
		return `{"component":"stat-card","props":{"label":"L","value":"1","trend":"` + v + `"}}`
	}},
	{"ButtonProps.Variant", "enum: primary|secondary|ghost|danger", "ghost", func(v string) string {
		return `{"component":"button","props":{"label":"L","variant":"` + v + `"},"action_ref":"a"}`
	}},
	{"LinkProps.To", "URL guard: host-relative same-origin only (IsValidHostRelative); also pinned by TestURLPropsRejectSchemePayloads", "/ok", func(v string) string {
		return `{"component":"link","props":{"text":"T","to":"` + v + `"}}`
	}},
	{"ImageProps.Src", "URL guard: host-relative same-origin only (IsValidHostRelative); also pinned by TestURLPropsRejectSchemePayloads", "/ok.png", func(v string) string {
		return `{"component":"image","props":{"src":"` + v + `","alt":"A"}}`
	}},
}

// TestEscapeInventoryMatchesSchema fails when a schema string field has no
// escapeSurfaces entry and no justified exemption, when an entry or
// exemption is stale, or when an exemption's premise (validator rejects
// hostile content in that field) stops holding.
func TestEscapeInventoryMatchesSchema(t *testing.T) {
	sc := parseUIPackage(t)

	// Wiring bijection: Component consts ↔ componentDecoders ↔ sealed
	// union. Drift in the wiring itself is the same failure class as
	// inventory drift, so it fails here too.
	for comp, propsName := range sc.decoders {
		if !sc.propStructs[propsName] {
			t.Errorf("componentDecoders[%s] decodes into %s, which has no propsMarker method: not in the sealed Props union", comp, propsName)
		}
		if _, ok := sc.wireNames[comp]; !ok {
			t.Errorf("componentDecoders key %s is not a string-valued Component const in uinodev1", comp)
		}
	}
	for comp, wire := range sc.wireNames {
		if !strings.HasPrefix(comp, "Comp") {
			continue
		}
		if _, ok := sc.decoders[comp]; !ok {
			t.Errorf("Component const %s (= %q) has no componentDecoders entry: undecodable enum value", comp, wire)
		}
	}
	for s := range sc.propStructs {
		decoded := false
		for _, p := range sc.decoders {
			if p == s {
				decoded = true
				break
			}
		}
		if !decoded {
			t.Errorf("prop struct %s implements the sealed Props union but no component decodes it (dead type or missing componentDecoders entry)", s)
		}
	}

	// Authoritative string-field set.
	fields := schemaStringFields(t, sc)
	authoritative := map[string]string{} // goPath -> wirePath
	for _, f := range fields {
		if prev, dup := authoritative[f.goPath]; dup && prev != f.wirePath {
			t.Fatalf("schema walk produced ambiguous identity %s (%s vs %s)", f.goPath, prev, f.wirePath)
		}
		authoritative[f.goPath] = f.wirePath
	}

	// Measured coverage: drive each entry's tree with the probe and see
	// where it lands in the validated tree.
	covered := map[string]string{} // field goPath -> first entry name that probed it
	for _, sf := range escapeSurfaces {
		tt, err := uinodev1.Validate([]byte(sf.tree(escapeJSONString(escapeProbe))), uinodev1.DefaultLimits())
		if err != nil {
			t.Errorf("escapeSurfaces entry %q: probe tree no longer validates: %v", sf.name, err)
			continue
		}
		paths := probeFieldPaths(tt.Root.Props, escapeProbe)
		if len(paths) == 0 {
			t.Errorf("escapeSurfaces entry %q: probe payload landed in no prop string field; the entry exercises nothing", sf.name)
			continue
		}
		for _, p := range paths {
			if _, dup := covered[p]; !dup {
				covered[p] = sf.name
			}
		}
	}

	// Exemptions: reason present, premise machine-checked.
	exempted := map[string]bool{}
	for _, ex := range escapeExemptions {
		if ex.reason == "" {
			t.Errorf("exemption for %s has no written reason", ex.field)
		}
		exempted[ex.field] = true
		if _, err := uinodev1.Validate([]byte(ex.tree(escapeJSONString(markupPayload))), uinodev1.DefaultLimits()); err == nil {
			t.Errorf("exemption %s: validator ACCEPTED a hostile payload in this field — the guard the exemption relies on no longer rejects; move the field into escapeSurfaces", ex.field)
		}
		if _, err := uinodev1.Validate([]byte(ex.tree(ex.valid)), uinodev1.DefaultLimits()); err != nil {
			t.Errorf("exemption %s: control tree with valid value %q does not validate (%v) — the reject above may be an unrelated tree defect; fix the exemption's tree/valid pair", ex.field, ex.valid, err)
		}
	}

	// Completeness — the point of this test.
	var missing []string
	for goPath, wirePath := range authoritative {
		_, isCovered := covered[goPath]
		_, isExempt := exempted[goPath]
		switch {
		case isCovered && isExempt:
			t.Errorf("field %s (%s) is both covered by escapeSurfaces entry %q and exempted — pick one", wirePath, goPath, covered[goPath])
		case !isCovered && !isExempt:
			missing = append(missing, fmt.Sprintf("%s  (Go: %s)", wirePath, goPath))
		}
	}
	t.Logf("escape inventory: %d schema string fields across %d components; %d covered by escapeSurfaces, %d exempted",
		len(authoritative), len(sc.decoders), len(covered), len(exempted))
	if len(missing) > 0 {
		sort.Strings(missing)
		t.Errorf("schema string fields with NO escaping coverage and NO exemption — add an escapeSurfaces entry (renderer_security_test.go) or a justified escapeExemptions entry:\n  %s", strings.Join(missing, "\n  "))
	}

	// Staleness: entries/exemptions naming fields that no longer exist.
	for p, entry := range covered {
		if _, ok := authoritative[p]; !ok {
			t.Errorf("escapeSurfaces entry %q probed %s, which is not a string field in the current schema — stale entry", entry, p)
		}
	}
	for p := range exempted {
		if _, ok := authoritative[p]; !ok {
			t.Errorf("exemption for %s does not match any string field in the current schema — stale exemption", p)
		}
	}
}

// --- schema derivation (go/ast over core-ui/uinodev1 sources) ----------

// stringField is one schema-derived escaping surface candidate.
type stringField struct {
	goPath   string // e.g. DataTableProps.Columns.DataColumn.Key
	wirePath string // e.g. data-table.columns.key
}

// uiSchema is everything the walk needs from the parsed package.
type uiSchema struct {
	wireNames   map[string]string          // const ident (CompStack) -> wire name ("stack")
	structs     map[string]*ast.StructType // all struct declarations
	propStructs map[string]bool            // structs with a propsMarker method
	decoders    map[string]string          // Comp ident -> props struct name
}

// uinodev1Dir locates the uinodev1 package sources relative to this file.
func uinodev1Dir(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed")
	}
	dir := filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "core-ui", "uinodev1")
	if _, err := os.Stat(filepath.Join(dir, "types.go")); err != nil {
		t.Fatalf("cannot locate core-ui/uinodev1 sources at %s: %v", dir, err)
	}
	return dir
}

// parseUIPackage parses every non-test source file of uinodev1 and
// collects the declarations the inventory derives from. Any shape it does
// not recognize is a hard failure: the walker must never silently skip.
func parseUIPackage(t *testing.T) uiSchema {
	t.Helper()
	entries, err := os.ReadDir(uinodev1Dir(t))
	if err != nil {
		t.Fatalf("reading uinodev1 sources: %v", err)
	}
	fset := token.NewFileSet()
	sc := uiSchema{
		wireNames:   map[string]string{},
		structs:     map[string]*ast.StructType{},
		propStructs: map[string]bool{},
		decoders:    map[string]string{},
	}
	foundDecoders := false
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(uinodev1Dir(t), name), nil, 0)
		if err != nil {
			t.Fatalf("parsing %s: %v — the escape inventory derives from these sources", name, err)
		}
		for _, d := range f.Decls {
			switch d := d.(type) {
			case *ast.FuncDecl:
				if d.Recv == nil || d.Name.Name != "propsMarker" || len(d.Recv.List) != 1 {
					continue
				}
				if id, ok := receiverIdent(d.Recv.List[0].Type); ok {
					sc.propStructs[id] = true
				}
			case *ast.GenDecl:
				switch d.Tok {
				case token.CONST:
					for _, s := range d.Specs {
						vs, ok := s.(*ast.ValueSpec)
						if !ok || len(vs.Names) != 1 || len(vs.Values) != 1 {
							continue
						}
						lit, ok := vs.Values[0].(*ast.BasicLit)
						if !ok || lit.Kind != token.STRING {
							continue
						}
						if w, err := strconv.Unquote(lit.Value); err == nil {
							sc.wireNames[vs.Names[0].Name] = w
						}
					}
				case token.TYPE:
					for _, s := range d.Specs {
						ts, ok := s.(*ast.TypeSpec)
						if !ok {
							continue
						}
						if st, ok := ts.Type.(*ast.StructType); ok {
							sc.structs[ts.Name.Name] = st
						}
					}
				case token.VAR:
					for _, s := range d.Specs {
						vs, ok := s.(*ast.ValueSpec)
						if !ok {
							continue
						}
						for i, n := range vs.Names {
							if n.Name != "componentDecoders" || i >= len(vs.Values) {
								continue
							}
							lit, ok := vs.Values[i].(*ast.CompositeLit)
							if !ok {
								t.Fatalf("componentDecoders is not a composite literal; update the escape-inventory parser")
							}
							for _, elt := range lit.Elts {
								kv, ok := elt.(*ast.KeyValueExpr)
								if !ok {
									t.Fatalf("componentDecoders entry is not key:value; update the escape-inventory parser")
								}
								comp, ok := identName(kv.Key)
								if !ok {
									t.Fatalf("componentDecoders key has unmodeled shape (%T); update the escape-inventory parser", kv.Key)
								}
								idx, ok := kv.Value.(*ast.IndexExpr)
								if !ok {
									t.Fatalf("componentDecoders[%s] does not use decodeAs[T]; update the escape-inventory parser", comp)
								}
								fn, _ := identName(idx.X)
								typ, ok := identName(idx.Index)
								if fn != "decodeAs" || !ok {
									t.Fatalf("componentDecoders[%s] does not use decodeAs[T]; update the escape-inventory parser", comp)
								}
								sc.decoders[comp] = typ
							}
							foundDecoders = true
						}
					}
				}
			}
		}
	}
	if !foundDecoders {
		t.Fatal("componentDecoders not found in core-ui/uinodev1; the escape-inventory check derives the component→props mapping from it")
	}
	return sc
}

// schemaStringFields enumerates every string field reachable from every
// decoded prop struct, in deterministic wire-name order.
func schemaStringFields(t *testing.T, sc uiSchema) []stringField {
	t.Helper()
	comps := make([]string, 0, len(sc.decoders))
	for c := range sc.decoders {
		comps = append(comps, c)
	}
	sort.Slice(comps, func(i, j int) bool { return sc.wireNames[comps[i]] < sc.wireNames[comps[j]] })
	var out []stringField
	for _, comp := range comps {
		propsName := sc.decoders[comp]
		wire, ok := sc.wireNames[comp]
		if !ok {
			t.Errorf("componentDecoders key %s is not a string-valued const in uinodev1", comp)
			continue
		}
		st := sc.structs[propsName]
		if st == nil {
			t.Errorf("componentDecoders[%s] decodes into %s, but no such struct exists in uinodev1", comp, propsName)
			continue
		}
		walkSchemaStruct(t, sc, st, propsName, wire, &out)
	}
	return out
}

// nonStringScalars are field types the walk ignores deliberately: they
// cannot carry markup.
//
// "any" is deliberately absent. It is an alias for interface{}, which the
// walker treats as a hard failure below, but it parses as an *ast.Ident
// rather than an *ast.InterfaceType — so listing it here would classify
// an open-ended field as a bounded scalar and let a prop that can hold a
// string need neither coverage nor an exemption. Same type, opposite
// treatment, and the hole would be invisible: verified by adding an
// any-typed prop, which passed silently before this line was removed.
var nonStringScalars = map[string]bool{
	"int": true, "int8": true, "int16": true, "int32": true, "int64": true,
	"uint": true, "uint8": true, "uint16": true, "uint32": true, "uint64": true,
	"float32": true, "float64": true, "bool": true,
}

// walkSchemaStruct appends the string fields of st to out. Identity is the
// Go path (Type.Field.ElemType.Field...); nested struct hops append the
// element type name, mirroring probeFieldPaths exactly.
func walkSchemaStruct(t *testing.T, sc uiSchema, st *ast.StructType, goPrefix, wirePrefix string, out *[]stringField) {
	for _, fld := range st.Fields.List {
		if len(fld.Names) == 0 {
			t.Fatalf("embedded field in %s: the escape-inventory walker does not model embedding", goPrefix)
		}
		jsonName, skip := jsonName(fld)
		if skip {
			continue
		}
		for _, id := range fld.Names {
			goPath := goPrefix + "." + id.Name
			wirePath := wirePrefix + "." + jsonName
			elt := elemTypeExpr(fld.Type)
			switch x := elt.(type) {
			case *ast.Ident:
				switch {
				case x.Name == "string":
					*out = append(*out, stringField{goPath, wirePath})
				case nonStringScalars[x.Name]:
					// bounded numeric/bool prop: cannot carry markup
				case sc.structs[x.Name] != nil:
					walkSchemaStruct(t, sc, sc.structs[x.Name], goPath+"."+x.Name, wirePath, out)
				default:
					t.Fatalf("field %s has type %q, which the escape-inventory walker does not model; extend the walker before adding such a field", goPath, x.Name)
				}
			case *ast.MapType:
				if key, _ := identName(x.Key); key != "string" {
					t.Fatalf("field %s is a map with non-string key %q; extend the escape-inventory walker", goPath, key)
				}
				vident, ok := elemTypeExpr(x.Value).(*ast.Ident)
				if !ok {
					t.Fatalf("field %s is a map with unmodeled value shape; extend the escape-inventory walker", goPath)
				}
				switch {
				case vident.Name == "string":
					*out = append(*out, stringField{goPath, wirePath})
				case sc.structs[vident.Name] != nil:
					// map values keep the field path (keys are dropped on
					// the reflect side, so no element-type hop here)
					walkSchemaStruct(t, sc, sc.structs[vident.Name], goPath, wirePath, out)
				default:
					t.Fatalf("field %s is a map with value type %q, which the escape-inventory walker does not model", goPath, vident.Name)
				}
			case *ast.InterfaceType:
				t.Fatalf("field %s is an interface; the sealed wire type must not have open-ended fields", goPath)
			default:
				t.Fatalf("field %s has an unmodeled type shape (%T); extend the escape-inventory walker", goPath, elt)
			}
		}
	}
}

// jsonName returns the wire name for a field (json tag, comma options
// stripped). skip reports a `json:"-"` field that never crosses the wire.
// Without a tag it falls back to the lowercased Go field name.
func jsonName(f *ast.Field) (name string, skip bool) {
	fallback := ""
	if len(f.Names) > 0 {
		fallback = strings.ToLower(f.Names[0].Name)
	}
	if f.Tag == nil {
		return fallback, false
	}
	tag, err := strconv.Unquote(f.Tag.Value)
	if err != nil {
		return fallback, false
	}
	for _, part := range strings.Fields(tag) {
		if !strings.HasPrefix(part, "json:") {
			continue
		}
		n := strings.Trim(strings.TrimPrefix(part, "json:"), `"`)
		if n == "-" {
			return "", true
		}
		if i := strings.IndexByte(n, ','); i >= 0 {
			n = n[:i]
		}
		if n != "" {
			return n, false
		}
	}
	return fallback, false
}

// elemTypeExpr strips slice/array/pointer/paren wrappers from a field
// type, yielding the element type expression.
func elemTypeExpr(x ast.Expr) ast.Expr {
	switch t := x.(type) {
	case *ast.ArrayType:
		return elemTypeExpr(t.Elt)
	case *ast.StarExpr:
		return elemTypeExpr(t.X)
	case *ast.ParenExpr:
		return elemTypeExpr(t.X)
	default:
		return x
	}
}

// identName resolves Ident and SelectorExpr to their plain name.
func identName(x ast.Expr) (string, bool) {
	switch t := x.(type) {
	case *ast.Ident:
		return t.Name, true
	case *ast.SelectorExpr:
		return t.Sel.Name, true
	default:
		return "", false
	}
}

// receiverIdent resolves a method receiver type to its plain struct name.
func receiverIdent(x ast.Expr) (string, bool) {
	if star, ok := x.(*ast.StarExpr); ok {
		x = star.X
	}
	return identName(x)
}

// probeFieldPaths reports the Go field paths (same identity scheme the
// schema walk produces) inside props whose string value equals probe.
func probeFieldPaths(props any, probe string) []string {
	var out []string
	var walk func(v reflect.Value, path string)
	walk = func(v reflect.Value, path string) {
		switch v.Kind() {
		case reflect.String:
			if v.String() == probe {
				out = append(out, path)
			}
		case reflect.Struct:
			for i := range v.NumField() {
				f := v.Type().Field(i)
				if !f.IsExported() {
					continue
				}
				fv := v.Field(i)
				fp := path + "." + f.Name
				if fv.Kind() == reflect.Struct && fv.Type().Name() != "" {
					fp += "." + fv.Type().Name()
				}
				walk(fv, fp)
			}
		case reflect.Slice, reflect.Array:
			for i := range v.Len() {
				ev := v.Index(i)
				ep := path
				if ev.Kind() == reflect.Struct && ev.Type().Name() != "" {
					ep = path + "." + ev.Type().Name()
				}
				walk(ev, ep)
			}
		case reflect.Map:
			for _, k := range v.MapKeys() {
				walk(v.MapIndex(k), path) // values carry the field path; keys dropped
			}
		case reflect.Pointer:
			if v.IsNil() {
				return
			}
			pv := v.Elem()
			pp := path
			if pv.Kind() == reflect.Struct && pv.Type().Name() != "" {
				pp = path + "." + pv.Type().Name()
			}
			walk(pv, pp)
		}
	}
	rv := reflect.ValueOf(props)
	walk(rv, rv.Type().Name())
	return out
}
