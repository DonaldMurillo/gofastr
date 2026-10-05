package ui

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
	"github.com/DonaldMurillo/gofastr/framework/ui/theme"
)

// The kit-marking gate (owned styles, DESIGN-owned-styles "Kit
// marking"). An owned style's @scope stops at [data-cui-internal], so
// a kit component must put that attribute on the topmost element of
// every subtree that holds no caller content, and never on an element
// that holds some. Owners can then place a kit root and style the
// content they pass into its slots, and can never reach the kit's own
// markup.
//
// The gate renders every exported component of framework/ui and
// framework/headless with a sentinel element in every slot — every
// render.HTML and []render.HTML field, every variadic child, every
// func returning render.HTML, and every part the component's headless
// spec declares fillable — and asserts two things over the output:
//
//  1. no sentinel sits under a [data-cui-internal] element;
//  2. every element that is not a top-level root, not a sentinel's
//     ancestor, and not inside a sentinel is under one.
//
// kitComponents is the table the gate renders; TestKitMarkingCoversEveryComponent
// holds it to every exported function in the two packages that
// returns render.HTML.

const sentinelAttr = "data-kit-sentinel"

// kitComponent is one constructor. prep, when set, adjusts the
// reflection-filled arguments before the call: a component whose
// zero-valued required field panics names that field here.
type kitComponent struct {
	name string
	fn   any
	prep func(args []reflect.Value)
	// required names slot fields the component refuses to render
	// without; the empty pass keeps a sentinel in them.
	required []string
	// content, when set, says why the markup the component renders is
	// the caller's content (a document converted from a string): every
	// element stays in reach, and nothing may be marked.
	content string
}

// setAll sets every field named name, at any depth of the filled
// arguments (through structs, pointers and slices), to val; a nil val
// zeroes the field.
func setAll(args []reflect.Value, name string, val any) {
	var walk func(v reflect.Value)
	walk = func(v reflect.Value) {
		switch v.Kind() {
		case reflect.Pointer:
			if !v.IsNil() {
				walk(v.Elem())
			}
		case reflect.Slice:
			for i := range v.Len() {
				walk(v.Index(i))
			}
		case reflect.Struct:
			for i := range v.NumField() {
				sf := v.Type().Field(i)
				if !sf.IsExported() {
					continue
				}
				if sf.Name == name && val == nil {
					v.Field(i).SetZero()
					continue
				}
				if sf.Name == name && reflect.TypeOf(val).ConvertibleTo(sf.Type) {
					v.Field(i).Set(reflect.ValueOf(val).Convert(sf.Type))
					continue
				}
				walk(v.Field(i))
			}
		}
	}
	for _, a := range args {
		walk(a)
	}
}

// prepSet is a prep that sets named fields, in pairs (name, value).
func prepSet(pairs ...any) func([]reflect.Value) {
	return func(args []reflect.Value) {
		for i := 0; i+1 < len(pairs); i += 2 {
			setAll(args, pairs[i].(string), pairs[i+1])
		}
	}
}

// prepZero zeroes the named top-level fields of the first argument: a
// filled optional pointer that would override the field the test set.
func prepZero(names ...string) func([]reflect.Value) {
	return func(args []reflect.Value) {
		for _, n := range names {
			args[0].FieldByName(n).SetZero()
		}
	}
}

// kitMarkingExempt lists exported render.HTML functions that are not
// components, each with the reason.
var kitMarkingExempt = map[string]string{
	"headless.El":  "the element builder every component uses; it has no structure of its own",
	"headless.Own": "marks markup a composing component built; it renders nothing of its own",
}

func TestKitMarkingCoversEveryComponent(t *testing.T) {
	listed := map[string]bool{}
	for _, c := range kitComponents {
		if listed[c.name] {
			t.Errorf("%s is listed twice", c.name)
		}
		listed[c.name] = true
	}
	var missing []string
	found := map[string]bool{}
	for _, pkg := range []struct{ dir, prefix string }{{".", ""}, {"../headless", "headless."}} {
		for _, name := range exportedHTMLFuncs(t, pkg.dir) {
			n := pkg.prefix + name
			found[n] = true
			if !listed[n] && kitMarkingExempt[n] == "" {
				missing = append(missing, n)
			}
		}
	}
	sort.Strings(missing)
	for _, n := range missing {
		t.Errorf("component %s is not in kitComponents: add it, so the marking gate renders it", n)
	}
	for n := range listed {
		if !found[n] {
			t.Errorf("kitComponents lists %s, which is not an exported render.HTML function", n)
		}
	}
}

// exportedHTMLFuncs returns every exported package-level function in
// dir's non-test files whose only result is render.HTML.
func exportedHTMLFuncs(t *testing.T, dir string) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	var out []string
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		af, err := parser.ParseFile(fset, f, src, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range af.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Recv != nil || !fd.Name.IsExported() || fd.Type.TypeParams != nil {
				continue
			}
			res := fd.Type.Results
			if res == nil || len(res.List) != 1 || len(res.List[0].Names) > 1 {
				continue
			}
			if sel, ok := res.List[0].Type.(*ast.SelectorExpr); ok && sel.Sel.Name == "HTML" {
				if id, ok := sel.X.(*ast.Ident); ok && id.Name == "render" {
					out = append(out, fd.Name.Name)
				}
			}
		}
	}
	return out
}

// TestKitMarksInternalSubtrees renders each component twice: with a
// sentinel in every slot, and with every slot empty. The two ends
// decide different marks. A wrapper that holds an optional slot and
// some text (an alert's header: its icon and its title) is a slot
// ancestor when the slot is filled, so the mark goes on the text; with
// the slot empty the wrapper holds only the component's own markup, so
// the mark goes on the wrapper, and a second mark inside it is an
// error.
func TestKitMarksInternalSubtrees(t *testing.T) {
	for _, c := range kitComponents {
		t.Run(c.name, func(t *testing.T) {
			for _, empty := range []bool{false, true} {
				out, err := renderWithSentinels(c, empty)
				if err != nil {
					t.Fatalf("render (empty slots: %v): %v", empty, err)
				}
				if c.content != "" {
					if strings.Contains(string(out), "data-cui-internal") {
						t.Errorf("%s renders the caller's content (%s), so nothing in it may be marked internal", c.name, c.content)
					}
					continue
				}
				for _, v := range markingViolations(string(out)) {
					if empty {
						v = "with every slot empty, " + v
					}
					t.Error(v)
				}
			}
		})
	}
}

// renderWithSentinels calls c.fn with every slot filled, or with every
// slot empty. Plain string fields are filled either way (see
// stringFor), so optional text parts render and are checked.
func renderWithSentinels(c kitComponent, empty bool) (out render.HTML, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	f := &sentinelFiller{fillable: fillableParts(c.name), empty: empty}
	fn := reflect.ValueOf(c.fn)
	args := f.args(fn.Type())
	if empty {
		for _, name := range c.required {
			setAll(args, name, render.HTML(`<i `+sentinelAttr+`="required"></i>`))
		}
	}
	if c.prep != nil {
		c.prep(args)
	}
	var res []reflect.Value
	if fn.Type().IsVariadic() {
		res = fn.CallSlice(args)
	} else {
		res = fn.Call(args)
	}
	return res[0].Interface().(render.HTML), nil
}

// fillableParts returns the parts a headless spec lets a caller fill,
// for a component rendered through headless.Parts.
func fillableParts(name string) []headless.Part {
	sp, ok := headless.SpecOf(strings.TrimPrefix(name, "headless."))
	if !ok {
		return nil
	}
	return sp.Fillable
}

var (
	htmlType   = reflect.TypeFor[render.HTML]()
	partsType  = reflect.TypeFor[headless.Parts]()
	slotsType  = reflect.TypeFor[headless.Slots]()
	stringType = reflect.TypeFor[string]()
)

type sentinelFiller struct {
	fillable []headless.Part
	texts    int
	empty    bool // every slot empty instead of a sentinel
	n        int
}

func (f *sentinelFiller) sentinel() render.HTML {
	if f.empty {
		return ""
	}
	f.n++
	return render.HTML(fmt.Sprintf(`<i %s="%d"></i>`, sentinelAttr, f.n))
}

func (f *sentinelFiller) args(ft reflect.Type) []reflect.Value {
	args := make([]reflect.Value, ft.NumIn())
	for i := range ft.NumIn() {
		in := ft.In(i)
		if ft.IsVariadic() && i == ft.NumIn()-1 {
			args[i] = f.slice(in, 0)
			continue
		}
		args[i] = f.value(in, 0)
	}
	return args
}

func (f *sentinelFiller) value(t reflect.Type, depth int) reflect.Value {
	v := reflect.New(t).Elem()
	if depth > 4 {
		return v
	}
	switch {
	case t == htmlType:
		v.Set(reflect.ValueOf(f.sentinel()))
	case t == partsType:
		v.Set(reflect.ValueOf(f.parts()))
	case t == stringType:
		v.SetString("text")
	case t.Kind() == reflect.Slice:
		v.Set(f.slice(t, depth))
	case t.Kind() == reflect.Struct:
		for i := range t.NumField() {
			sf := t.Field(i)
			if !sf.IsExported() {
				continue
			}
			if sf.Type == stringType {
				v.Field(i).SetString(f.stringFor(sf.Name))
				continue
			}
			v.Field(i).Set(f.value(sf.Type, depth+1))
		}
	case t.Kind() == reflect.Pointer && t.Elem().Kind() == reflect.Struct:
		p := reflect.New(t.Elem())
		p.Elem().Set(f.value(t.Elem(), depth+1))
		v.Set(p)
	case t.Kind() == reflect.Func && t.NumOut() == 1 && t.Out(0) == htmlType:
		v.Set(reflect.MakeFunc(t, func([]reflect.Value) []reflect.Value {
			return []reflect.Value{reflect.ValueOf(f.sentinel())}
		}))
	}
	return v
}

// stringFor fills a plain string field by its name: a URL-shaped field
// gets a same-origin path, an enum-shaped one stays zero (its default),
// and every other field gets distinct text, so optional text parts
// render and ids stay unique.
func (f *sentinelFiller) stringFor(field string) string {
	for _, suf := range []string{"Endpoint", "Action", "Href", "URL", "Src", "Path"} {
		if strings.HasSuffix(field, suf) {
			return "/x"
		}
	}
	switch field {
	case "Tone", "Direction", "State", "Type", "Align", "Method", "TitleTag", "ZIndexTier",
		"GroupMarkup", "Machine", "Variant", "Radio", "Mode", "Collapse", "Attr":
		return ""
	}
	f.texts++
	return fmt.Sprintf("text%d", f.texts)
}

// slice returns a one-element slice of t's element type, filled;
// slices of scalars other than render.HTML stay empty.
func (f *sentinelFiller) slice(t reflect.Type, depth int) reflect.Value {
	et := t.Elem()
	switch {
	case et == htmlType && !f.empty, et.Kind() == reflect.Struct, et.Kind() == reflect.Pointer && et.Elem().Kind() == reflect.Struct:
		s := reflect.MakeSlice(t, 1, 1)
		s.Index(0).Set(f.value(et, depth+1))
		return s
	}
	return reflect.MakeSlice(t, 0, 0)
}

func (f *sentinelFiller) parts() headless.Parts {
	if len(f.fillable) == 0 || f.empty {
		return headless.Parts{}
	}
	slots := headless.Slots{}
	for _, p := range f.fillable {
		slots[p] = f.sentinel()
	}
	return headless.Parts{Slots: slots}
}

var _ = slotsType

// markingViolations parses html and reports each marking error. A
// sentinel that does not render is not an error (a slot the config
// hides); a sentinel under an internal subtree is.
func markingViolations(html string) []string {
	roots := parseTagTree(html)
	var out []string
	var walk func(n *tagNode, isRoot, underMarked bool)
	walk = func(n *tagNode, isRoot, underMarked bool) {
		if _, ok := n.attrs[sentinelAttr]; ok {
			if underMarked {
				out = append(out, "slot content sits under a [data-cui-internal] element: "+n.path())
			}
			return // the slot's content is the caller's
		}
		_, marked := n.attrs["data-cui-internal"]
		if marked && isRoot {
			out = append(out, "the root is marked data-cui-internal; an owner must be able to place it: "+n.path())
		}
		// A mark inside a marked subtree is allowed: @scope stops at the
		// outer one, so the inner one is inert. A component composing
		// another (a Field around a Select, a wrapper around a whole
		// headless render) keeps the inner component's marks as they
		// are rather than growing a knob to switch them off.
		if !isRoot && !underMarked && !marked && !n.holdsSentinel() {
			out = append(out, "internal element not marked data-cui-internal: "+n.path())
			return // report the topmost only
		}
		for _, k := range n.kids {
			walk(k, false, underMarked || marked)
		}
	}
	for _, r := range roots {
		walk(r, true, false)
	}
	return out
}

type tagNode struct {
	tag    string
	attrs  map[string]string
	kids   []*tagNode
	parent *tagNode
}

func (n *tagNode) holdsSentinel() bool {
	if _, ok := n.attrs[sentinelAttr]; ok {
		return true
	}
	return slices.ContainsFunc(n.kids, (*tagNode).holdsSentinel)
}

// path names n by its ancestry, each step with its class when it has
// one, so a failure points at the part.
func (n *tagNode) path() string {
	var steps []string
	for m := n; m != nil; m = m.parent {
		s := m.tag
		if c := m.attrs["class"]; c != "" {
			s += "." + strings.ReplaceAll(c, " ", ".")
		}
		steps = append(steps, s)
	}
	slices.Reverse(steps)
	return strings.Join(steps, " > ")
}

var voidTags = map[string]bool{
	"area": true, "base": true, "br": true, "col": true, "embed": true, "hr": true, "img": true,
	"input": true, "link": true, "meta": true, "source": true, "track": true, "wbr": true,
}

var rawTextTags = map[string]bool{"script": true, "style": true, "textarea": true, "title": true}

// parseTagTree builds an element tree from well-formed component
// markup: start and end tags, void and self-closed elements, comments,
// and raw-text elements. Text is dropped.
func parseTagTree(src string) []*tagNode {
	var roots []*tagNode
	var cur *tagNode
	add := func(n *tagNode) {
		n.parent = cur
		if cur == nil {
			roots = append(roots, n)
		} else {
			cur.kids = append(cur.kids, n)
		}
	}
	for i := 0; i < len(src); {
		if src[i] != '<' {
			i++
			continue
		}
		switch {
		case strings.HasPrefix(src[i:], "<!--"):
			end := strings.Index(src[i:], "-->")
			if end < 0 {
				return roots
			}
			i += end + 3
		case strings.HasPrefix(src[i:], "<!"):
			end := strings.IndexByte(src[i:], '>')
			if end < 0 {
				return roots
			}
			i += end + 1
		case strings.HasPrefix(src[i:], "</"):
			end := strings.IndexByte(src[i:], '>')
			if end < 0 {
				return roots
			}
			name := strings.ToLower(strings.TrimSpace(src[i+2 : i+end]))
			for m := cur; m != nil; m = m.parent {
				if m.tag == name {
					cur = m.parent
					break
				}
			}
			i += end + 1
		default:
			n, next, selfClosed := parseStartTag(src, i)
			if n == nil {
				i++
				continue
			}
			add(n)
			i = next
			if rawTextTags[n.tag] {
				end := strings.Index(strings.ToLower(src[i:]), "</"+n.tag)
				if end < 0 {
					return roots
				}
				i += end
				gt := strings.IndexByte(src[i:], '>')
				i += gt + 1
				continue
			}
			if !selfClosed && !voidTags[n.tag] {
				cur = n
			}
		}
	}
	return roots
}

// parseStartTag reads the start tag at src[i] ('<'). It returns nil
// when no tag name follows.
func parseStartTag(src string, i int) (*tagNode, int, bool) {
	j := i + 1
	for j < len(src) && (isNameByte(src[j])) {
		j++
	}
	if j == i+1 {
		return nil, i + 1, false
	}
	n := &tagNode{tag: strings.ToLower(src[i+1 : j]), attrs: map[string]string{}}
	for j < len(src) {
		for j < len(src) && isSpace(src[j]) {
			j++
		}
		if j >= len(src) {
			break
		}
		if src[j] == '>' {
			return n, j + 1, false
		}
		if strings.HasPrefix(src[j:], "/>") {
			return n, j + 2, true
		}
		k := j
		for k < len(src) && !isSpace(src[k]) && src[k] != '=' && src[k] != '>' && !strings.HasPrefix(src[k:], "/>") {
			k++
		}
		name := strings.ToLower(src[j:k])
		j = k
		val := ""
		if j < len(src) && src[j] == '=' {
			j++
			if j < len(src) && (src[j] == '"' || src[j] == '\'') {
				q := src[j]
				end := strings.IndexByte(src[j+1:], q)
				if end < 0 {
					return n, len(src), false
				}
				val = src[j+1 : j+1+end]
				j += end + 2
			} else {
				k := j
				for k < len(src) && !isSpace(src[k]) && src[k] != '>' {
					k++
				}
				val = src[j:k]
				j = k
			}
		}
		if name != "" {
			n.attrs[name] = val
		} else {
			j++
		}
	}
	return n, j, false
}

func isNameByte(b byte) bool {
	return b == '-' || b == ':' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9'
}

func isSpace(b byte) bool { return b == ' ' || b == '\t' || b == '\n' || b == '\r' || b == '\f' }

// kitComponents is every component the marking gate renders.
var kitComponents = []kitComponent{
	{name: "AnchoredRail", fn: AnchoredRail},
	{name: "AnimatedCounter", fn: AnimatedCounter},
	{name: "AspectRatioComponent", fn: AspectRatioComponent},
	{name: "AuthCard", fn: AuthCard},
	{name: "Avatar", fn: Avatar},
	{name: "AvatarGroup", fn: AvatarGroup},
	{name: "BackToTop", fn: BackToTop},
	{name: "Banner", fn: Banner},
	{name: "BarChart", fn: BarChart},
	{name: "Box", fn: Box},
	{name: "Breadcrumbs", fn: Breadcrumbs},
	{name: "Button", fn: Button},
	{name: "Callout", fn: Callout},
	{name: "Card", fn: Card},
	{name: "Carousel", fn: Carousel, required: []string{"Content"}},
	{name: "Center", fn: Center},
	{name: "Checkbox", fn: Checkbox},
	{name: "CheckboxGroup", fn: CheckboxGroup},
	{name: "Cluster", fn: Cluster},
	{name: "CodeBlock", fn: CodeBlock},
	{name: "CodeTabs", fn: CodeTabs},
	{name: "Collapsible", fn: Collapsible},
	{name: "ColorField", fn: ColorField},
	{name: "ColorPicker", fn: ColorPicker},
	{name: "Combobox", fn: Combobox},
	{name: "ConditionalField", fn: ConditionalField},
	{name: "Container", fn: Container},
	{name: "ContentRow", fn: ContentRow},
	{name: "Control", fn: Control},
	{name: "CopyButton", fn: CopyButton},
	{name: "Counter", fn: Counter, prep: prepZero("Slice")},
	{name: "DataTable", fn: DataTable, prep: prepSet("Pages", 3, "Page", 1)},
	{name: "DateField", fn: DateField},
	{name: "DetailList", fn: DetailList},
	{name: "DiffViewer", fn: DiffViewer},
	{name: "Divider", fn: Divider},
	{name: "EmptyState", fn: EmptyState},
	{name: "EmptyValue", fn: EmptyValue},
	{name: "FactBox", fn: FactBox},
	{name: "FileDropzone", fn: FileDropzone},
	{name: "FileUpload", fn: FileUpload},
	{name: "FilterChipBar", fn: FilterChipBar},
	{name: "FilterToolbar", fn: FilterToolbar},
	{name: "Form", fn: Form},
	{name: "FormField", fn: FormField},
	{name: "FormFieldFor", fn: FormFieldFor},
	{name: "FormRepeater", fn: FormRepeater},
	{name: "FormSection", fn: FormSection},
	{name: "Gallery", fn: Gallery},
	{name: "GlobalSearch", fn: GlobalSearch},
	{name: "Grid", fn: Grid},
	{name: "Hero", fn: Hero},
	{name: "HeroSplit", fn: HeroSplit},
	{name: "Icon", fn: Icon},
	{name: "InputGroup", fn: InputGroup, required: []string{"Input"}},
	{name: "JSONViewer", fn: JSONViewer},
	{name: "LineChart", fn: LineChart},
	{name: "Link", fn: Link},
	{name: "LinkButton", fn: LinkButton},
	{name: "ListDetail", fn: ListDetail, prep: prepSet("MobileSinglePane", true)},
	{name: "ListDetailPlaceholder", fn: ListDetailPlaceholder},
	{name: "Markdown", fn: Markdown, content: "the rendered document is the caller's prose, which a docs or article owner styles"},
	{name: "Menu", fn: Menu, prep: prepSet("Href", "", "RPC", "", "Action", nil)},
	{name: "MetricBand", fn: MetricBand},
	{name: "MultiSelect", fn: MultiSelect},
	{name: "Muted", fn: Muted},
	{name: "NetworkRetryBanner", fn: NetworkRetryBanner},
	{name: "Notification", fn: Notification},
	{name: "NumberField", fn: NumberField},
	{name: "NumberInput", fn: NumberInput},
	{name: "OptimisticAction", fn: OptimisticAction},
	{name: "OptimizedImage", fn: OptimizedImage, prep: prepSet("Width", 100, "Height", 100)},
	{name: "PageHeader", fn: PageHeader},
	{name: "Pagination", fn: Pagination, prep: prepSet("Pages", 3, "Page", 1)},
	{name: "PaneHost", fn: PaneHost, required: []string{"Primary"}},
	{name: "PasswordInput", fn: PasswordInput},
	{name: "PieChart", fn: PieChart},
	{name: "PipelineImage", fn: PipelineImage},
	{name: "PollingIndicator", fn: PollingIndicator},
	{name: "PricingCard", fn: PricingCard},
	{name: "Progress", fn: Progress},
	{name: "ProgressSteps", fn: ProgressSteps},
	{name: "Radio", fn: Radio},
	{name: "RadioGroup", fn: RadioGroup},
	{name: "RangeSlider", fn: RangeSlider},
	{name: "Rating", fn: Rating},
	{name: "RatingInput", fn: RatingInput},
	{name: "RecordSummary", fn: RecordSummary},
	{name: "Repeater", fn: Repeater},
	{name: "Responsive", fn: Responsive},
	{name: "SearchInput", fn: SearchInput},
	{name: "Section", fn: Section},
	{name: "SegmentedControl", fn: SegmentedControl, prep: func(args []reflect.Value) {
		opts := args[0].FieldByName("Options")
		opts.Set(reflect.Append(opts, opts.Index(0)))
		opts.Index(1).FieldByName("Value").SetString("other")
	}},
	{name: "Select", fn: Select},
	{name: "ShortcutHint", fn: ShortcutHint},
	{name: "SidebarBody", fn: SidebarBody, prep: prepSet("Href", "")},
	{name: "SidebarDrawerTrigger", fn: SidebarDrawerTrigger},
	{name: "SignOut", fn: SignOut},
	{name: "SignalToggle", fn: SignalToggle, prep: prepZero("Slice")},
	{name: "SkeletonAvatar", fn: SkeletonAvatar},
	{name: "SkeletonCard", fn: SkeletonCard},
	{name: "SkeletonLine", fn: SkeletonLine},
	{name: "SkeletonRow", fn: SkeletonRow},
	{name: "SkeletonTimeline", fn: SkeletonTimeline},
	{name: "SkipLink", fn: SkipLink},
	{name: "Slider", fn: Slider},
	{name: "SortableList", fn: SortableList},
	{name: "SortableListItems", fn: SortableListItems},
	{name: "Spacer", fn: Spacer},
	{name: "Sparkline", fn: Sparkline},
	{name: "Spinner", fn: Spinner},
	{name: "Stack", fn: Stack},
	{name: "StatCard", fn: StatCard},
	{name: "StatusBadge", fn: StatusBadge},
	{name: "StatusPill", fn: StatusPill},
	{name: "StepRail", fn: StepRail},
	{name: "StepWizard", fn: StepWizard},
	{name: "Sticky", fn: Sticky},
	{name: "Switch", fn: Switch},
	{name: "TableOfContents", fn: TableOfContents},
	{name: "Tabs", fn: Tabs, prep: prepZero("Slice")},
	{name: "Tag", fn: Tag},
	{name: "TagInput", fn: TagInput},
	{name: "TerminalBlock", fn: TerminalBlock},
	{name: "TerminalOK", fn: TerminalOK},
	{name: "TerminalOut", fn: TerminalOut},
	{name: "TextArea", fn: TextArea},
	{name: "TextField", fn: TextField},
	{name: "ThemeToggle", fn: ThemeToggle},
	{name: "Themed", fn: Themed, prep: func(args []reflect.Value) { args[0].Set(reflect.ValueOf(style.RegisterThemeOverride(theme.Default()))) }},
	{name: "TimePicker", fn: TimePicker},
	{name: "Timeline", fn: Timeline},
	{name: "ToggleAction", fn: ToggleAction},
	{name: "Toolbar", fn: Toolbar},
	{name: "Tooltip", fn: Tooltip},
	{name: "Tree", fn: Tree},
	{name: "ValidationSummary", fn: ValidationSummary},
	{name: "Workbench", fn: Workbench},
	{name: "headless.Alert", fn: headless.Alert},
	{name: "headless.BackToTop", fn: headless.BackToTop},
	{name: "headless.Badge", fn: headless.Badge},
	{name: "headless.Breadcrumbs", fn: headless.Breadcrumbs},
	{name: "headless.Button", fn: headless.Button},
	{name: "headless.Card", fn: headless.Card},
	{name: "headless.Carousel", fn: headless.Carousel},
	{name: "headless.Choice", fn: headless.Choice, prep: prepSet("Type", "checkbox")},
	{name: "headless.Cluster", fn: headless.Cluster},
	{name: "headless.Color", fn: headless.Color},
	{name: "headless.Combobox", fn: headless.Combobox},
	{name: "headless.ConditionalField", fn: headless.ConditionalField},
	{name: "headless.Container", fn: headless.Container},
	{name: "headless.Counter", fn: headless.Counter},
	{name: "headless.DetailList", fn: headless.DetailList, required: []string{"Value"}},
	{name: "headless.Disclosure", fn: headless.Disclosure, required: []string{"Summary"}},
	{name: "headless.Divider", fn: headless.Divider},
	{name: "headless.EmptyState", fn: headless.EmptyState},
	{name: "headless.Field", fn: headless.Field},
	{name: "headless.FieldRow", fn: headless.FieldRow},
	{name: "headless.Fieldset", fn: headless.Fieldset},
	{name: "headless.FileUpload", fn: headless.FileUpload},
	{name: "headless.Form", fn: headless.Form},
	{name: "headless.Gallery", fn: headless.Gallery},
	{name: "headless.Grid", fn: headless.Grid},
	{name: "headless.Group", fn: headless.Group},
	{name: "headless.Input", fn: headless.Input},
	{name: "headless.InputGroup", fn: headless.InputGroup},
	{name: "headless.JSONTree", fn: headless.JSONTree},
	{name: "headless.LightboxViewer", fn: headless.LightboxViewer},
	{name: "headless.Menu", fn: headless.Menu, prep: prepSet("Href", "", "RPC", "", "Action", nil)},
	{name: "headless.MultiSelect", fn: headless.MultiSelect},
	{name: "headless.NotificationBell", fn: headless.NotificationBell, prep: prepSet("Attr", "")},
	{name: "headless.NumberInput", fn: headless.NumberInput},
	{name: "headless.OptimisticAction", fn: headless.OptimisticAction},
	{name: "headless.PageHeader", fn: headless.PageHeader},
	{name: "headless.Pagination", fn: headless.Pagination, prep: prepSet("Pages", 3, "Page", 1)},
	{name: "headless.PaneHost", fn: headless.PaneHost, required: []string{"Primary"}},
	{name: "headless.Password", fn: headless.Password},
	{name: "headless.Progress", fn: headless.Progress},
	{name: "headless.Rail", fn: headless.Rail},
	{name: "headless.RangeSlider", fn: headless.RangeSlider},
	{name: "headless.Rating", fn: headless.Rating},
	{name: "headless.Repeater", fn: headless.Repeater},
	{name: "headless.Section", fn: headless.Section},
	{name: "headless.Select", fn: headless.Select},
	{name: "headless.Sidebar", fn: headless.Sidebar, prep: prepSet("Href", "")},
	{name: "headless.SidebarDrawerTrigger", fn: headless.SidebarDrawerTrigger},
	{name: "headless.SidebarRegion", fn: headless.SidebarRegion, prep: prepSet("Href", "")},
	{name: "headless.Skeleton", fn: headless.Skeleton},
	{name: "headless.Slider", fn: headless.Slider},
	{name: "headless.SortableItems", fn: headless.SortableItems},
	{name: "headless.SortableList", fn: headless.SortableList},
	{name: "headless.Spacer", fn: headless.Spacer, prep: prepSet("Grow", 1)},
	{name: "headless.Spinner", fn: headless.Spinner},
	{name: "headless.Stack", fn: headless.Stack},
	{name: "headless.StatCard", fn: headless.StatCard},
	{name: "headless.StepWizard", fn: headless.StepWizard},
	{name: "headless.Steps", fn: headless.Steps},
	{name: "headless.Switch", fn: headless.Switch},
	{name: "headless.SystemBanner", fn: headless.SystemBanner},
	{name: "headless.Table", fn: headless.Table},
	{name: "headless.TableOfContents", fn: headless.TableOfContents},
	{name: "headless.Tabs", fn: headless.Tabs},
	{name: "headless.Tag", fn: headless.Tag},
	{name: "headless.TagInput", fn: headless.TagInput},
	{name: "headless.Textarea", fn: headless.Textarea},
	{name: "headless.Timeline", fn: headless.Timeline},
	{name: "headless.Toast", fn: headless.Toast},
	{name: "headless.ToastStack", fn: headless.ToastStack},
	{name: "headless.ToastTemplate", fn: headless.ToastTemplate},
	{name: "headless.ToggleAction", fn: headless.ToggleAction},
	{name: "headless.Toolbar", fn: headless.Toolbar},
	{name: "headless.ToolbarGroup", fn: headless.ToolbarGroup},
	{name: "headless.ToolbarSearch", fn: headless.ToolbarSearch},
	{name: "headless.ToolbarSpacer", fn: headless.ToolbarSpacer},
	{name: "headless.Tree", fn: headless.Tree},
	{name: "headless.ValidationSummary", fn: headless.ValidationSummary},
}
