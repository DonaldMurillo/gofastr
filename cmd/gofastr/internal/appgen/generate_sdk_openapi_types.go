package appgen

import (
	"fmt"
	"go/token"
	"slices"
	"sort"
	"strings"
)

// oaType is one schema as the OpenAPI SDK types it, the model both the Go
// and the TypeScript renderers read, so the two SDKs type a value alike.
type oaType struct {
	// Kind is string, integer, number, boolean, array, map, struct or raw
	// (any JSON value, kept as written).
	Kind string
	Elem *oaType // array items, map values
	Name string  // struct
}

// oaField is one property of a struct type.
type oaField struct {
	Wire, Go           string
	Type               *oaType
	Required, Nullable bool
	Description        string
}

type oaStruct struct {
	Name               string
	Fields             []oaField
	Additional         *oaType
	AdditionalNullable bool
}

// oaTypeSet names and collects the struct types a document needs: a
// component schema keeps its own name, an inline object takes the name
// its position gives it (GenerateBarcodeRequest, PetOwner). A name taken
// twice is an error, never a renamed type.
type oaTypeSet struct {
	root    map[string]any
	structs map[string]*oaStruct
	order   []string
	byRef   map[string]string
	// refNullable holds whether each named component admits null, for
	// every use after the first.
	refNullable map[string]bool
	owner       map[string]string
	// inline holds the refs being typed in place (a component that is
	// not a named struct): one met again inside itself is raw JSON.
	inline map[string]bool
	// inlineTypes holds each component already typed in place.
	inlineTypes map[string]oaInlineType
}

type oaInlineType struct {
	t        *oaType
	nullable bool
}

// oaMaxStructs caps the struct types one document may derive: a document
// past it is refused rather than generated into a package nobody can
// build, or a process out of memory.
var oaMaxStructs = 5000

// singleAllOf types {allOf: [one member]} as that member, the nullable
// reference Swashbuckle and NSwag write ({allOf: [{$ref: X}], nullable:
// true}), keeping the wrapper's nullability. ok is false for any other
// schema, and for an allOf whose wrapper adds properties.
func (ts *oaTypeSet) singleAllOf(schema map[string]any, hint string) (t *oaType, nullable, ok bool, err error) {
	raw, present := schema["allOf"]
	if !present {
		return nil, false, false, nil
	}
	members, isList := raw.([]any)
	if !isList {
		return nil, false, true, fmt.Errorf("allOf: want a list, got %T", raw)
	}
	if len(members) != 1 {
		return nil, false, false, nil
	}
	member, isMap := members[0].(map[string]any)
	if !isMap {
		return nil, false, false, nil
	}
	for _, k := range []string{"properties", "additionalProperties", "items", "oneOf", "anyOf"} {
		if _, has := schema[k]; has {
			return nil, false, false, nil
		}
	}
	t, n, err := ts.typeOf(member, hint)
	return t, n || oaNullable(schema), true, err
}

// oaReservedTypes are the package-level names the generated SDKs declare
// themselves, or that client.d.ts reads from the global scope.
var oaReservedTypes = []string{
	"APIError", "APIPrefix", "ApiError", "Array", "ArrayBuffer", "ArrayBufferView",
	"Blob", "Client", "NewClient", "Promise", "Record",
}

func newOATypeSet(root map[string]any) *oaTypeSet {
	ts := &oaTypeSet{root: root, structs: map[string]*oaStruct{}, byRef: map[string]string{}, refNullable: map[string]bool{}, owner: map[string]string{}, inline: map[string]bool{}, inlineTypes: map[string]oaInlineType{}}
	for _, n := range oaReservedTypes {
		ts.owner[n] = "the SDK itself"
	}
	return ts
}

// claim takes a package-level type name for owner.
func (ts *oaTypeSet) claim(name, owner string) error {
	if !token.IsIdentifier(name) {
		return fmt.Errorf("%s derives the type name %q, which is not a Go identifier", owner, name)
	}
	if prev, taken := ts.owner[name]; taken {
		return fmt.Errorf("%s derives the type name %s, already declared by %s; rename one in the document (no auto-renaming)", owner, name, prev)
	}
	ts.owner[name] = owner
	return nil
}

var oaRaw = &oaType{Kind: "raw"}

// typeOf types schema, naming an inline object hint. nullable reports a
// schema that also admits null.
func (ts *oaTypeSet) typeOf(schema map[string]any, hint string) (t *oaType, nullable bool, err error) {
	if schema == nil {
		return oaRaw, false, nil
	}
	if ref, isRef := schema["$ref"].(string); isRef {
		if name, seen := ts.byRef[ref]; seen {
			return &oaType{Kind: "struct", Name: name}, ts.refNullable[ref], nil
		}
		resolved, err := oaResolve(ts.root, schema)
		if err != nil {
			return nil, false, err
		}
		if comp, ok := strings.CutPrefix(ref, "#/components/schemas/"); ok && oaStructShaped(resolved) {
			if name := oaGoName(comp); oaValidIdent(name) && oaFieldsNameable(resolved) {
				if err := ts.claim(name, fmt.Sprintf("component schema %q", comp)); err != nil {
					return nil, false, err
				}
				ts.byRef[ref] = name
				ts.refNullable[ref] = oaNullable(resolved)
				if err := ts.addStruct(name, resolved); err != nil {
					return nil, false, err
				}
				return &oaType{Kind: "struct", Name: name}, oaNullable(resolved), nil
			}
		}
		if cached, typed := ts.inlineTypes[ref]; typed {
			return cached.t, cached.nullable, nil
		}
		if ts.inline[ref] {
			return oaRaw, false, nil
		}
		// A component typed in place is typed once, its inline objects
		// named after it, so every use shares one type and a document
		// whose components fan out does not mint a type per path.
		if comp, ok := strings.CutPrefix(ref, "#/components/schemas/"); ok && oaValidIdent(oaGoName(comp)) {
			hint = oaGoName(comp)
		}
		ts.inline[ref] = true
		t, nullable, err := ts.typeOf(resolved, hint)
		delete(ts.inline, ref)
		if err == nil {
			ts.inlineTypes[ref] = oaInlineType{t, nullable}
		}
		return t, nullable, err
	}
	if t, nullable, ok, err := ts.singleAllOf(schema, hint); ok {
		return t, nullable, err
	}
	for _, union := range []string{"oneOf", "anyOf"} {
		raw, present := schema[union]
		if !present {
			continue
		}
		branches, isList := raw.([]any)
		if !isList {
			return nil, false, fmt.Errorf("%s: want a list, got %T", union, raw)
		}
		var nonNull []map[string]any
		for _, b := range branches {
			member, _ := b.(map[string]any)
			if member["type"] == "null" {
				nullable = true
				continue
			}
			nonNull = append(nonNull, member)
		}
		if len(nonNull) != 1 {
			return oaRaw, nullable, nil
		}
		t, n, err := ts.typeOf(nonNull[0], hint)
		return t, nullable || n, err
	}
	s, err := oaResolve(ts.root, schema)
	if err != nil {
		return nil, false, err
	}
	nullable = oaNullable(s)
	typ, _ := s["type"].(string)
	if list, isList := s["type"].([]any); isList {
		var types []string
		for _, e := range list {
			if name, _ := e.(string); name != "null" {
				types = append(types, name)
			}
		}
		if len(types) != 1 {
			return oaRaw, nullable, nil
		}
		typ = types[0]
	}
	switch typ {
	case "string", "integer", "number", "boolean":
		return &oaType{Kind: typ}, nullable, nil
	case "array":
		items, _ := s["items"].(map[string]any)
		if items == nil {
			return &oaType{Kind: "array", Elem: oaRaw}, nullable, nil
		}
		elem, _, err := ts.typeOf(items, hint+"Item")
		if err != nil {
			return nil, false, err
		}
		return &oaType{Kind: "array", Elem: elem}, nullable, nil
	case "object", "":
		if oaStructShaped(s) {
			if !oaFieldsNameable(s) {
				return oaRaw, nullable, nil
			}
			if err := ts.claim(hint, "an inline object schema"); err != nil {
				return nil, false, err
			}
			if err := ts.addStruct(hint, s); err != nil {
				return nil, false, err
			}
			return &oaType{Kind: "struct", Name: hint}, nullable, nil
		}
		if ap, isSchema := s["additionalProperties"].(map[string]any); isSchema && len(ap) > 0 {
			elem, _, err := ts.typeOf(ap, hint+"Value")
			if err != nil {
				return nil, false, err
			}
			return &oaType{Kind: "map", Elem: elem}, nullable, nil
		}
		if typ == "object" {
			return &oaType{Kind: "map", Elem: oaRaw}, nullable, nil
		}
	}
	return oaRaw, nullable, nil
}

// oaStructShaped reports an object schema that declares properties.
func oaStructShaped(s map[string]any) bool {
	props, _ := s["properties"].(map[string]any)
	return len(props) > 0
}

// oaNullable reports nullable: true (OpenAPI 3.0) or "null" in a type list.
func oaNullable(s map[string]any) bool {
	if s["nullable"] == true {
		return true
	}
	list, _ := s["type"].([]any)
	return slices.Contains(list, any("null"))
}

// oaFieldsNameable reports whether every property becomes a Go field: its
// derived name is an identifier no other property derives. Such a wire
// name holds only letters, digits and the -_. and space oaGoName drops,
// all of which a json tag carries. An object failing it is typed raw, so
// no property is dropped or renamed.
func oaFieldsNameable(s map[string]any) bool {
	props, _ := s["properties"].(map[string]any)
	seen := map[string]bool{}
	for wire := range props {
		g := oaGoName(wire)
		if !oaValidIdent(g) || seen[g] {
			return false
		}
		seen[g] = true
	}
	return true
}

func (ts *oaTypeSet) addStruct(name string, s map[string]any) error {
	if len(ts.structs) >= oaMaxStructs {
		return fmt.Errorf("the document derives more than %d types; the SDK refuses it rather than generate a package that size", oaMaxStructs)
	}
	props, _ := s["properties"].(map[string]any)
	required := map[string]bool{}
	if rq, ok := s["required"].([]any); ok {
		for _, r := range rq {
			if rs, ok := r.(string); ok {
				required[rs] = true
			}
		}
	}
	st := &oaStruct{Name: name}
	ts.structs[name] = st
	ts.order = append(ts.order, name)
	wires := make([]string, 0, len(props))
	for w := range props {
		wires = append(wires, w)
	}
	sort.Strings(wires)
	for _, wire := range wires {
		ps, _ := props[wire].(map[string]any)
		g := oaGoName(wire)
		ft, nullable, err := ts.typeOf(ps, name+g)
		if err != nil {
			return fmt.Errorf("property %q of %s: %w", wire, name, err)
		}
		desc, _ := ps["description"].(string)
		st.Fields = append(st.Fields, oaField{Wire: wire, Go: g, Type: ft, Required: required[wire], Nullable: nullable, Description: desc})
	}
	switch additional := s["additionalProperties"].(type) {
	case bool:
		if additional {
			st.Additional = oaRaw
		}
	case map[string]any:
		var err error
		st.Additional, st.AdditionalNullable, err = ts.typeOf(additional, name+"AdditionalProperty")
		if err != nil {
			return fmt.Errorf("additionalProperties of %s: %w", name, err)
		}
	}
	return nil
}

// goExpr is the Go type of t.
func (t *oaType) goExpr() string {
	switch t.Kind {
	case "string":
		return "string"
	case "integer":
		return "int64"
	case "number":
		return "float64"
	case "boolean":
		return "bool"
	case "array":
		return "[]" + t.Elem.goExpr()
	case "map":
		return "map[string]" + t.Elem.goExpr()
	case "struct":
		return t.Name
	}
	return "json.RawMessage"
}

// goPointer reports whether a value of t that may be absent or null goes
// behind a pointer: a scalar, so the zero value stays sendable, and a
// struct, so a type may refer to itself.
func (t *oaType) goPointer() bool {
	switch t.Kind {
	case "string", "integer", "number", "boolean", "struct":
		return true
	}
	return false
}

// usesRaw reports whether t's Go spelling names json.RawMessage.
func (t *oaType) usesRaw() bool {
	switch t.Kind {
	case "array", "map":
		return t.Elem.usesRaw()
	case "string", "integer", "number", "boolean", "struct":
		return false
	}
	return true
}

// tsExpr is the TypeScript type of t. An integer is number | string:
// parseJSON hands one past 2^53 back as its decimal string.
func (t *oaType) tsExpr() string {
	switch t.Kind {
	case "string":
		return "string"
	case "integer":
		return "number | string"
	case "number":
		return "number"
	case "boolean":
		return "boolean"
	case "array":
		return "Array<" + t.Elem.tsExpr() + ">"
	case "map":
		return "Record<string, " + t.Elem.tsExpr() + ">"
	case "struct":
		return t.Name
	}
	return "unknown"
}

func (ts *oaTypeSet) goRecursiveCodecs() map[string]bool {
	recursive := make(map[string]bool)
	for _, name := range ts.order {
		st := ts.structs[name]
		if st.Additional == nil {
			continue
		}
		seen := make(map[string]bool)
		var reaches func(*oaType) bool
		reaches = func(t *oaType) bool {
			if t == nil {
				return false
			}
			switch t.Kind {
			case "map", "array":
				return reaches(t.Elem)
			case "struct":
				if t.Name == name {
					return true
				}
				if seen[t.Name] {
					return false
				}
				seen[t.Name] = true
				nested := ts.structs[t.Name]
				return reaches(nested.Additional) || slices.ContainsFunc(nested.Fields, func(f oaField) bool { return reaches(f.Type) })
			default:
				return false
			}
		}
		if reaches(st.Additional) || slices.ContainsFunc(st.Fields, func(f oaField) bool { return reaches(f.Type) }) {
			recursive[name] = true
		}
	}
	return recursive
}

// renderGo emits every struct type, in the order they were named.
func (ts *oaTypeSet) renderGo(sb *strings.Builder, recursive map[string]bool) {
	for _, name := range ts.order {
		st := ts.structs[name]
		if !token.IsIdentifier(name) {
			continue
		}
		fmt.Fprintf(sb, "\n// %s is the schema %s.\ntype %s struct {\n", name, name, name)
		for _, f := range st.Fields {
			if d := oaCommentText(f.Description); d != "" {
				fmt.Fprintf(sb, "\t// %s\n", d)
			}
			typ := f.Type.goExpr()
			if (!f.Required || f.Nullable) && f.Type.goPointer() || f.Type.Kind == "struct" {
				typ = "*" + typ
			}
			tag := f.Wire
			if !f.Required {
				tag += ",omitempty"
			}
			fmt.Fprintf(sb, "\t%s %s `json:%q`\n", f.Go, typ, tag)
		}
		if st.Additional != nil {
			typ := st.Additional.goExpr()
			if st.AdditionalNullable && st.Additional.goPointer() {
				typ = "*" + typ
			}
			fmt.Fprintf(sb, "\t// AdditionalProperties holds JSON properties not declared by the schema.\n\tAdditionalProperties map[string]%s `json:\"-\"`\n}\n", typ)
			st.renderGoObjectCodec(sb, typ, recursive[name])
		} else {
			sb.WriteString("}\n")
		}
	}
	if len(recursive) > 0 {
		sb.WriteString(oaGoJSONCycleGuard)
	}
}

func (st *oaStruct) renderGoObjectCodec(sb *strings.Builder, valueType string, recursive bool) {
	wires := make([]string, 0, len(st.Fields))
	for _, f := range st.Fields {
		wires = append(wires, fmt.Sprintf("%q", f.Wire))
	}
	fmt.Fprintf(sb, "\nfunc (v %s) MarshalJSON() ([]byte, error) {\n", st.Name)
	if recursive {
		sb.WriteString(`	value := reflect.ValueOf(v)
	if openAPIJSONHasCycle(value, nil, make(map[openAPIJSONVisit]bool)) {
		return nil, &json.UnsupportedValueError{Value: value, Str: "encountered a cycle via " + value.Type().String()}
	}
`)
	}
	fmt.Fprintf(sb, `
	type plain %[1]s
	data, err := json.Marshal(plain(v))
	if err != nil || len(v.AdditionalProperties) == 0 {
		return data, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, err
	}
	for key, value := range v.AdditionalProperties {
		switch key {
		case %[2]s:
			continue
		}
		raw, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		fields[key] = raw
	}
	return json.Marshal(fields)
}

func (v *%[1]s) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	for key, raw := range fields {
		switch key {
`, st.Name, strings.Join(wires, ", "))
	for _, f := range st.Fields {
		fmt.Fprintf(sb, `		case %q:
			if err := json.Unmarshal(raw, &v.%s); err != nil {
				return err
			}
`, f.Wire, f.Go)
	}
	fmt.Fprintf(sb, `		default:
			var value %[1]s
			if err := json.Unmarshal(raw, &value); err != nil {
				return err
			}
			if v.AdditionalProperties == nil {
				v.AdditionalProperties = make(map[string]%[1]s)
			}
			v.AdditionalProperties[key] = value
		}
	}
	return nil
}
`, valueType)
}

const oaGoJSONCycleGuard = `
type openAPIJSONVisit struct {
	typ, owner reflect.Type
	ptr uintptr
	length int
}

func openAPIJSONHasCycle(v reflect.Value, owner reflect.Type, path map[openAPIJSONVisit]bool) bool {
	kind := v.Kind()
	switch kind {
	case reflect.Ptr, reflect.Map, reflect.Slice:
		if v.IsNil() || kind == reflect.Slice && v.Type().Elem().Kind() == reflect.Uint8 {
			return false
		}
		visit := openAPIJSONVisit{typ: v.Type(), owner: owner, ptr: v.Pointer()}
		if kind == reflect.Slice {
			visit.length = v.Len()
		}
		if path[visit] {
			return true
		}
		path[visit] = true
		defer delete(path, visit)
	}
	switch kind {
	case reflect.Ptr:
		return openAPIJSONHasCycle(v.Elem(), nil, path)
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			var owner reflect.Type
			if v.Type().Field(i).Tag.Get("json") == "-" {
				owner = v.Type()
			}
			if openAPIJSONHasCycle(v.Field(i), owner, path) {
				return true
			}
		}
	case reflect.Map:
		iter := v.MapRange()
		for iter.Next() {
			if owner != nil && openAPIJSONDeclares(owner, iter.Key().String()) {
				continue
			}
			if openAPIJSONHasCycle(iter.Value(), nil, path) {
				return true
			}
		}
	case reflect.Slice:
		for i := 0; i < v.Len(); i++ {
			if openAPIJSONHasCycle(v.Index(i), nil, path) {
				return true
			}
		}
	}
	return false
}

func openAPIJSONDeclares(typ reflect.Type, key string) bool {
	for i := 0; i < typ.NumField(); i++ {
		name, _, _ := strings.Cut(typ.Field(i).Tag.Get("json"), ",")
		if name != "-" && name == key {
			return true
		}
	}
	return false
}
`

// renderTS emits every struct type as an interface.
func (ts *oaTypeSet) renderTS(sb *strings.Builder) {
	for _, name := range ts.order {
		st := ts.structs[name]
		if !token.IsIdentifier(name) {
			continue
		}
		fmt.Fprintf(sb, "export interface %s {\n", name)
		for _, f := range st.Fields {
			if d := oaCommentText(f.Description); d != "" {
				fmt.Fprintf(sb, "  /** %s */\n", sdkCommentSafe(d))
			}
			opt := ""
			if !f.Required {
				opt = "?"
			}
			typ := f.Type.tsExpr()
			if f.Nullable {
				typ += " | null"
			}
			fmt.Fprintf(sb, "  %s%s: %s;\n", jsQuote(f.Wire), opt, typ)
		}
		sb.WriteString("}\n\n")
	}
}

// oaCommentText folds document text onto one comment line: whitespace
// runs, line breaks among them, become one space and other control
// characters are dropped.
func oaCommentText(s string) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 && r != '\n' && r != '\t' || r == 0x7f || r >= 0x80 && r <= 0x9f {
			return -1
		}
		return r
	}, s)
	return strings.Join(strings.Fields(s), " ")
}
