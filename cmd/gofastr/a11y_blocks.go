package main

import (
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/urlsafe"
)

// a11ySnapshotMaxBytes caps the snapshot `generate screen --from-a11y`
// reads. A full-page snapshot of a busy app is a few hundred KiB.
const a11ySnapshotMaxBytes = 4 << 20

// a11yScreenBody reads an aria snapshot (path "-" is stdin) and maps it to
// a screen body whose forms post to route. Mapping warnings go to stdout,
// or to stderr in --json mode so stdout stays machine-readable.
func a11yScreenBody(path, route string, jsonMode bool) ([]BlueprintBlock, error) {
	var r io.Reader = os.Stdin
	if path != "-" {
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		r = f
	}
	src, err := io.ReadAll(io.LimitReader(r, a11ySnapshotMaxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(src) > a11ySnapshotMaxBytes {
		return nil, fmt.Errorf("snapshot is larger than %d bytes", a11ySnapshotMaxBytes)
	}
	nodes, err := parseAriaSnapshot(string(src))
	if err != nil {
		return nil, err
	}
	blocks, warnings := mapA11yToBlocks(nodes, route)
	for _, w := range warnings {
		if jsonMode {
			fmt.Fprintln(os.Stderr, "warning: "+w)
			continue
		}
		warn("%s", w)
	}
	if len(blocks) == 0 {
		return nil, fmt.Errorf("the snapshot has nothing to render: no headings, text, links, or controls")
	}
	return blocks, nil
}

// a11yMapper turns an aria snapshot into blueprint screen blocks, each a
// framework/ui catalog component. It never emits markup or CSS of its own:
// a role with no catalog counterpart is inlined or skipped, and the skip is
// reported so the author can finish that part by hand.
type a11yMapper struct {
	action    string // form action: the screen's own route
	warnings  []string
	warned    map[string]bool
	names     map[string]bool // field names in use: ui fields use the name as the element id
	tables    map[string]int  // caption → tables so far: the caption derives the table's id
	formDepth int             // >0 while mapping a form's contents: forms do not nest
	titled    bool
}

func newA11yMapper(action string) *a11yMapper {
	return &a11yMapper{action: action, warned: map[string]bool{}, names: map[string]bool{}, tables: map[string]int{}}
}

// mapA11yToBlocks maps a parsed snapshot to screen body blocks. action is
// the route generated forms post to. Warnings name every node the mapping
// dropped or flattened.
func mapA11yToBlocks(nodes []*ariaNode, action string) ([]BlueprintBlock, []string) {
	m := newA11yMapper(action)
	blocks := m.siblings(nodes)
	return blocks, m.warnings
}

func (m *a11yMapper) warn(key, format string, args ...any) {
	if m.warned[key] {
		return
	}
	m.warned[key] = true
	m.warnings = append(m.warnings, fmt.Sprintf(format, args...))
}

// a11yTransparentRoles carry no meaning of their own in a screen body:
// their children are spliced into the parent.
var a11yTransparentRoles = map[string]bool{
	"generic": true, "none": true, "presentation": true, "main": true,
	"document": true, "application": true, "tabpanel": true, "labeltext": true, "superscript": true, "subscript": true,
}

// a11yLandmarkRoles become a ui.Section when they carry a name; an
// unnamed one is transparent.
var a11yLandmarkRoles = map[string]bool{
	"region": true, "article": true, "complementary": true, "group": true,
	"search": true, "figure": true,
}

// a11yTextRoles render as a paragraph of their text.
var a11yTextRoles = map[string]bool{
	"text": true, "paragraph": true, "statictext": true, "blockquote": true,
	"code": true, "strong": true, "emphasis": true, "caption": true,
	"term": true, "definition": true, "note": true, "time": true,
	"status": true, "log": true, "mark": true, "insertion": true, "deletion": true,
}

// a11yFieldRoles are the controls a run is grouped into a form around.
var a11yFieldRoles = map[string]bool{
	"textbox": true, "searchbox": true, "spinbutton": true, "checkbox": true,
	"switch": true, "combobox": true, "listbox": true, "radiogroup": true, "radio": true,
}

// flatten splices transparent containers so that controls wrapped in
// layout divs still land side by side for form grouping.
func (m *a11yMapper) flatten(nodes []*ariaNode) []*ariaNode {
	out := make([]*ariaNode, 0, len(nodes))
	for _, n := range nodes {
		transparent := a11yTransparentRoles[n.Role] ||
			(a11yLandmarkRoles[n.Role] && n.Name == "")
		if transparent && len(n.Children) > 0 {
			if n.Text != "" {
				out = append(out, &ariaNode{Role: "text", Text: n.Text, Line: n.Line})
			}
			out = append(out, m.flatten(n.Children)...)
			continue
		}
		if transparent && n.Text != "" {
			out = append(out, &ariaNode{Role: "text", Text: n.Text, Line: n.Line})
			continue
		}
		if transparent {
			continue
		}
		out = append(out, n)
	}
	return out
}

// siblings maps one sibling list. Outside a form, a run of controls holding
// at least one input becomes a form block; inside one, the controls are its
// fields directly.
func (m *a11yMapper) siblings(nodes []*ariaNode) []BlueprintBlock {
	nodes = dropRedundantLabels(m.flatten(nodes))
	var out []BlueprintBlock
	for i := 0; i < len(nodes); {
		n := nodes[i]
		if m.formDepth == 0 {
			if part, _ := a11yFormPart(n); part {
				if end, ok := a11yFormRun(nodes, i); ok {
					out = append(out, m.form(nodes[i:end]))
					i = end
					continue
				}
			}
		}
		if n.Role == "radio" {
			j := i
			for j < len(nodes) && nodes[j].Role == "radio" {
				j++
			}
			out = append(out, m.radioGroup("", n.Line, nodes[i:j])...)
			i = j
			continue
		}
		out = append(out, m.node(n)...)
		i++
		// A table right under a heading with the same text keeps its
		// caption for assistive technology but stops repeating it on screen.
		if last := len(out) - 1; last >= 1 && out[last].Kind == "data_table" && out[last-1].Type == "heading" &&
			strings.EqualFold(out[last-1].Text, blueprintProp(out[last], "caption")) {
			out[last].Props["caption_hidden"] = true
		}
		// A paragraph right under the page title is its subtitle.
		if last := len(out) - 1; last >= 0 && out[last].Kind == "page_header" && out[last].Props["subtitle"] == nil &&
			i < len(nodes) && (nodes[i].Role == "paragraph" || nodes[i].Role == "text") &&
			len(nodes[i].Children) == 0 && nodes[i].Label() != "" {
			out[last].Props["subtitle"] = nodes[i].Label()
			i++
		}
	}
	return out
}

// a11yFormPart reports whether n belongs in a form run: a control, or a
// named group (a fieldset) holding one. field is true when n takes input,
// which a row of plain buttons does not.
func a11yFormPart(n *ariaNode) (part, field bool) {
	switch {
	case a11yFieldRoles[n.Role]:
		return true, true
	case n.Role == "button":
		return true, false
	case a11yLandmarkRoles[n.Role] && a11yHasField(n):
		return true, true
	}
	return false, false
}

func a11yHasField(n *ariaNode) bool {
	for _, c := range n.Children {
		if a11yFieldRoles[c.Role] || a11yHasField(c) {
			return true
		}
	}
	return false
}

// a11yFormRun returns the end of the form run that starts at nodes[i]:
// the controls, and the help, hint and error text between them, up to the
// last control. A paragraph between two fields is the first field's help
// line, so it must not split one form in two and strand its submit
// button. ok is false when the run takes no input (a row of buttons).
func a11yFormRun(nodes []*ariaNode, i int) (end int, ok bool) {
	end = i
	for j := i; j < len(nodes); j++ {
		n := nodes[j]
		if part, field := a11yFormPart(n); part {
			ok = ok || field
			end = j + 1
			continue
		}
		if (a11yTextRoles[n.Role] && len(n.Children) == 0) || n.Role == "alert" {
			continue
		}
		break
	}
	return end, ok
}

// dropRedundantLabels removes a text node that only repeats the name of
// the control right after it: the visible <label> a snapshot lists beside
// its input. The generated field renders its own label.
func dropRedundantLabels(nodes []*ariaNode) []*ariaNode {
	out := make([]*ariaNode, 0, len(nodes))
	for i, n := range nodes {
		if i+1 < len(nodes) && (n.Role == "text" || n.Role == "statictext") && len(n.Children) == 0 &&
			a11yFieldRoles[nodes[i+1].Role] {
			label := strings.TrimSuffix(strings.TrimSpace(n.Label()), ":")
			label = strings.TrimSuffix(strings.TrimSpace(label), "*")
			if strings.EqualFold(strings.TrimSpace(label), nodes[i+1].Label()) {
				continue
			}
		}
		out = append(out, n)
	}
	return out
}

func (m *a11yMapper) node(n *ariaNode) []BlueprintBlock {
	switch {
	case n.Role == "banner" || n.Role == "contentinfo":
		m.warn("role:"+n.Role, "skipped %s (line %d): the app layout renders the site header and footer", n.Role, n.Line)
		return nil
	case n.Role == "heading":
		return m.heading(n)
	case a11yTextRoles[n.Role]:
		if len(n.Children) > 0 {
			// A paragraph holding a link (`Already a member? Sign in`) keeps
			// its pieces together.
			parts := m.siblings(n.Children)
			if n.Text != "" {
				parts = append([]BlueprintBlock{{Type: "text", Text: n.Text}}, parts...)
			}
			return a11yGroup(parts)
		}
		if n.Label() == "" {
			return nil
		}
		return []BlueprintBlock{{Type: "text", Text: n.Label()}}
	case n.Role == "alert":
		return []BlueprintBlock{m.alert(n)}
	case n.Role == "link":
		return []BlueprintBlock{m.link(n)}
	case n.Role == "button":
		return []BlueprintBlock{m.button(n, "button")}
	case n.Role == "form":
		if m.formDepth > 0 {
			m.warn(fmt.Sprintf("form:%d", n.Line), "form (line %d) sits inside another form: its fields joined the outer one, since HTML forms do not nest", n.Line)
			return m.siblings(n.Children)
		}
		return []BlueprintBlock{m.form(n.Children)}
	case n.Role == "radiogroup":
		m.warnDropped(n, "radios", "radio")
		return m.radioGroup(n.Name, n.Line, a11yDescendants(n, "radio"))
	case a11yFieldRoles[n.Role]:
		return []BlueprintBlock{m.field(n)}
	case n.Role == "navigation":
		return []BlueprintBlock{m.navigation(n)}
	case n.Role == "list" || n.Role == "directory":
		return []BlueprintBlock{m.list(n)}
	case n.Role == "table" || n.Role == "grid" || n.Role == "treegrid":
		if b, ok := m.table(n); ok {
			return []BlueprintBlock{b}
		}
		return nil
	case n.Role == "separator":
		return []BlueprintBlock{{Kind: "divider"}}
	case a11yLandmarkRoles[n.Role] || n.Role == "dialog" || n.Role == "alertdialog":
		// A fieldset of radios is the radio group, its legend the name.
		if radios, ok := a11yOnlyRadios(n); ok {
			return m.radioGroup(n.Name, n.Line, radios)
		}
		return m.section(n)
	case n.Role == "img" || n.Role == "image":
		m.warn("role:img", "skipped img %q (line %d): a snapshot carries no image source; add the image by hand", n.Name, n.Line)
		return nil
	case len(n.Children) > 0:
		m.warn("role:"+n.Role, "%s (line %d) has no catalog component: its contents were inlined", n.Role, n.Line)
		return m.siblings(n.Children)
	case n.Label() != "":
		m.warn("role:"+n.Role, "%s %q (line %d) has no catalog component: kept as text", n.Role, n.Label(), n.Line)
		return []BlueprintBlock{{Type: "text", Text: n.Label()}}
	}
	m.warn("role:"+n.Role, "skipped %s (line %d): no catalog component and no text", n.Role, n.Line)
	return nil
}

// a11yOnlyRadios returns a group's radios when radios are all it holds,
// besides a text line repeating its name (the legend).
func a11yOnlyRadios(n *ariaNode) ([]*ariaNode, bool) {
	var radios []*ariaNode
	for _, c := range dropRedundantLabels(newA11yMapper("").flatten(n.Children)) {
		switch {
		case c.Role == "radio":
			radios = append(radios, c)
		case (c.Role == "text" || c.Role == "statictext") && strings.EqualFold(c.Label(), n.Name):
		default:
			return nil, false
		}
	}
	return radios, len(radios) > 0
}

// a11yGroup keeps several pieces together: inline ones (text, links,
// buttons) on one line, anything block-level stacked. One piece needs no
// wrapper.
func a11yGroup(parts []BlueprintBlock) []BlueprintBlock {
	if len(parts) <= 1 {
		return parts
	}
	for _, p := range parts {
		inline := p.Kind == "action_button" || (p.Kind == "" && (p.Type == "text" || p.Type == "link"))
		if !inline {
			return []BlueprintBlock{{Kind: "stack", Props: map[string]any{"gap": "xs"}, Children: parts}}
		}
	}
	return []BlueprintBlock{{Kind: "cluster", Props: map[string]any{"gap": "xs", "align": "baseline"}, Children: parts}}
}

func (m *a11yMapper) heading(n *ariaNode) []BlueprintBlock {
	if n.Label() == "" {
		m.warn(fmt.Sprintf("heading:%d", n.Line), "skipped heading (line %d): it has no text", n.Line)
		return nil
	}
	level, _ := strconv.Atoi(n.Attrs["level"])
	if level < 1 || level > 6 {
		level = 2
	}
	if level == 1 {
		if !m.titled {
			m.titled = true
			return []BlueprintBlock{{Kind: "page_header", Props: map[string]any{"title": n.Label()}}}
		}
		// The page header is the page's one h1.
		m.warn(fmt.Sprintf("h1:%d", n.Line), "heading %q (line %d) is a second level-1 heading: made level 2", n.Label(), n.Line)
		level = 2
	}
	return []BlueprintBlock{{Type: "heading", Level: level, Text: n.Label()}}
}

// alert maps to a callout: the alert's name is its title, its text the
// body. An alert with only a name shows the name as the body.
func (m *a11yMapper) alert(n *ariaNode) BlueprintBlock {
	body := n.Text
	if body == "" {
		var parts []string
		for _, c := range n.Children {
			if t := a11yTextOf(c); t != "" {
				parts = append(parts, t)
			}
		}
		body = strings.Join(parts, " ")
	}
	title := n.Name
	if body == "" {
		body, title = title, ""
	}
	return BlueprintBlock{Kind: "callout", Text: body, Props: map[string]any{"title": title}}
}

func (m *a11yMapper) link(n *ariaNode) BlueprintBlock {
	href := n.Props["url"]
	switch {
	case href == "":
		href = "#"
	case !urlsafe.OK(href, urlsafe.Anchor):
		m.warn("href:"+href, "link %q (line %d): dropped unsafe href %q", n.Label(), n.Line, href)
		href = "#"
	}
	text := n.Label()
	if text == "" {
		text = a11yTextOf(n)
	}
	if text == "" {
		text = href
	}
	return BlueprintBlock{Type: "link", Text: text, Href: href}
}

func (m *a11yMapper) button(n *ariaNode, typ string) BlueprintBlock {
	label := n.Label()
	if label == "" {
		label = a11yTextOf(n)
	}
	if label == "" {
		label = "Button"
		m.warn(fmt.Sprintf("unnamed:%d", n.Line), "button (line %d) has no accessible name: labelled %q", n.Line, label)
	}
	props := map[string]any{"label": label}
	if typ != "button" {
		props["type"] = typ
	}
	if a11yAttrOn(n, "disabled") {
		props["disabled"] = true
	}
	return BlueprintBlock{Kind: "action_button", Props: props}
}

// form groups controls into a ui.Form posting to the screen's route. The
// last button becomes the form's submit; earlier ones stay plain buttons.
func (m *a11yMapper) form(nodes []*ariaNode) BlueprintBlock {
	m.formDepth++
	children := m.siblings(nodes)
	m.formDepth--
	props := map[string]any{"action": m.action}
	for i := len(children) - 1; i >= 0; i-- {
		if children[i].Kind == "action_button" {
			props["submit"] = children[i].Props["label"]
			children = append(children[:i], children[i+1:]...)
			break
		}
	}
	if _, ok := props["submit"]; !ok {
		props["hide_submit"] = true
	}
	return BlueprintBlock{Kind: "custom_form", Props: props, Children: children}
}

func (m *a11yMapper) field(n *ariaNode) BlueprintBlock {
	label := m.fieldLabel(n)
	props := map[string]any{"label": label, "name": m.fieldName(label)}
	if a11yAttrOn(n, "disabled") {
		props["disabled"] = true
	}
	switch n.Role {
	case "checkbox", "switch":
		if a11yAttrOn(n, "checked") {
			props["checked"] = true
		}
		return BlueprintBlock{Kind: n.Role, Props: props}
	case "spinbutton":
		return BlueprintBlock{Kind: "number_field", Props: props}
	case "combobox", "listbox":
		var options []any
		for _, o := range a11yDescendants(n, "option") {
			if o.Label() == "" {
				continue
			}
			opt := map[string]any{"label": o.Label()}
			if a11yAttrOn(o, "selected") {
				opt["selected"] = true
			}
			options = append(options, opt)
		}
		if len(options) > 0 {
			m.warnDropped(n, "options", "option")
			props["options"] = options
			return BlueprintBlock{Kind: "select_field", Props: props}
		}
		// A combobox with no listed options is a typeahead input.
	case "textbox", "searchbox":
		if n.Text != "" {
			m.warn(fmt.Sprintf("value:%d", n.Line), "%s %q (line %d): dropped its current value %q; the generated field starts empty", n.Role, label, n.Line, n.Text)
		}
	}
	if p := n.Props["placeholder"]; p != "" {
		props["placeholder"] = p
	}
	return BlueprintBlock{Kind: "text_field", Props: props}
}

func (m *a11yMapper) fieldLabel(n *ariaNode) string {
	if n.Name != "" {
		return n.Name
	}
	if p := n.Props["placeholder"]; p != "" {
		return p
	}
	label := "Field"
	m.warn(fmt.Sprintf("unnamed:%d", n.Line), "%s (line %d) has no accessible name: labelled %q", n.Role, n.Line, label)
	return label
}

// fieldName derives a field name from a label, unique across the screen:
// the ui fields render the name as the element id too.
func (m *a11yMapper) fieldName(label string) string {
	base := a11ySlug(label, '_')
	switch {
	case base == "":
		base = "field"
	case base[0] >= '0' && base[0] <= '9':
		base = "field_" + base
	}
	name := base
	for k := 2; m.names[name]; k++ {
		name = fmt.Sprintf("%s_%d", base, k)
	}
	m.names[name] = true
	return name
}

// a11ySlug lowercases s to ASCII letters and digits joined by sep.
func a11ySlug(s string, sep byte) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case b.Len() > 0 && b.String()[b.Len()-1] != sep:
			b.WriteByte(sep)
		}
	}
	return strings.TrimRight(b.String(), string(sep))
}

// radioGroup maps radios to a ui.RadioGroup. Two radios with the same
// label get distinct values, which keeps their ids distinct.
func (m *a11yMapper) radioGroup(legend string, line int, radios []*ariaNode) []BlueprintBlock {
	var options []any
	seen := map[string]bool{}
	for _, r := range radios {
		if r.Label() == "" {
			m.warn(fmt.Sprintf("unnamed:%d", r.Line), "radio (line %d) has no accessible name: skipped", r.Line)
			continue
		}
		opt := map[string]any{"label": r.Label()}
		value := r.Label()
		for k := 2; seen[value]; k++ {
			value = fmt.Sprintf("%s %d", r.Label(), k)
		}
		seen[value] = true
		if value != r.Label() {
			opt["value"] = value
		}
		if a11yAttrOn(r, "checked") {
			opt["checked"] = true
		}
		options = append(options, opt)
	}
	if len(options) == 0 {
		m.warn(fmt.Sprintf("radios:%d", line), "skipped radio group (line %d): it has no named radios", line)
		return nil
	}
	if legend == "" {
		legend = "Choose one"
		m.warn(fmt.Sprintf("legend:%d", line), "radio group (line %d) has no accessible name: legend set to %q", line, legend)
	}
	return []BlueprintBlock{{Kind: "radio_group", Props: map[string]any{"legend": legend, "name": m.fieldName(legend), "options": options}}}
}

// navigation becomes a labelled nav of its links. Page-level navigation
// usually belongs in the app layout; an in-page one (a table of contents,
// a pager) keeps its links here.
func (m *a11yMapper) navigation(n *ariaNode) BlueprintBlock {
	var links []BlueprintBlock
	for _, l := range a11yDescendants(n, "link") {
		links = append(links, m.link(l))
	}
	m.warnDropped(n, "links", "link")
	label := n.Name
	if label == "" {
		label = "Page links"
	}
	return BlueprintBlock{Kind: "nav_links", Props: map[string]any{"label": label}, Children: links}
}

// warnDropped reports the text inside n that a mapping keeping only the
// kept roles leaves out.
func (m *a11yMapper) warnDropped(n *ariaNode, what string, kept ...string) {
	var lost []string
	var walk func(*ariaNode)
	walk = func(c *ariaNode) {
		if slices.Contains(kept, c.Role) {
			return
		}
		if len(c.Children) == 0 {
			if l := c.Label(); l != "" {
				lost = append(lost, strconv.Quote(l))
			}
			return
		}
		if c.Text != "" {
			lost = append(lost, strconv.Quote(c.Text))
		}
		for _, k := range c.Children {
			walk(k)
		}
	}
	for _, c := range n.Children {
		walk(c)
	}
	if len(lost) == 0 {
		return
	}
	if len(lost) > 3 {
		lost = append(lost[:3], "…")
	}
	m.warn(fmt.Sprintf("dropped:%d", n.Line), "%s (line %d) keeps only its %s: dropped %s", n.Role, n.Line, what, strings.Join(lost, ", "))
}

func (m *a11yMapper) list(n *ariaNode) BlueprintBlock {
	var items []BlueprintBlock
	for _, c := range n.Children {
		if c.Role != "listitem" {
			items = append(items, m.node(c)...)
			continue
		}
		parts := m.siblings(c.Children)
		if c.Text != "" {
			parts = append([]BlueprintBlock{{Type: "text", Text: c.Text}}, parts...)
		}
		if c.Name != "" && len(parts) == 0 {
			parts = []BlueprintBlock{{Type: "text", Text: c.Name}}
		}
		items = append(items, a11yGroup(parts)...)
	}
	return BlueprintBlock{Kind: "item_list", Children: items}
}

// table reads header and body rows (directly or inside rowgroups) into a
// static ui.DataTable. Cells become text.
func (m *a11yMapper) table(n *ariaNode) (BlueprintBlock, bool) {
	var header []string
	var rows []any
	for _, row := range a11yDescendants(n, "row") {
		var cells []string
		isHeader := true
		for _, c := range row.Children {
			switch c.Role {
			case "columnheader":
				cells = append(cells, a11yTextOf(c))
			case "cell", "gridcell", "rowheader":
				isHeader = false
				cells = append(cells, a11yTextOf(c))
			default:
				continue
			}
			if len(a11yDescendants(c, "link"))+len(a11yDescendants(c, "button")) > 0 {
				m.warn(fmt.Sprintf("cell:%d", n.Line), "table (line %d): cells holding links or buttons became plain text; add the controls by hand", n.Line)
			}
		}
		if len(cells) == 0 {
			continue
		}
		if isHeader && header == nil && len(rows) == 0 {
			header = cells
			continue
		}
		vals := make([]any, len(cells))
		for i, c := range cells {
			vals[i] = c
		}
		rows = append(rows, vals)
	}
	width := len(header)
	for _, r := range rows {
		width = max(width, len(r.([]any)))
	}
	if width == 0 {
		m.warn(fmt.Sprintf("table:%d", n.Line), "skipped table (line %d): no rows or cells", n.Line)
		return BlueprintBlock{}, false
	}
	columns := make([]any, width)
	for i := range columns {
		columns[i] = fmt.Sprintf("Column %d", i+1)
		if i < len(header) && header[i] != "" {
			columns[i] = header[i]
		}
	}
	props := map[string]any{"caption": n.Name, "columns": columns, "rows": rows}
	if n.Name == "" {
		// The caption names the table for assistive technology; a stand-in
		// one stays off screen.
		m.warn(fmt.Sprintf("caption:%d", n.Line), "table (line %d) has no accessible name: captioned \"Table\" for screen readers only", n.Line)
		props["caption"] = "Table"
		props["caption_hidden"] = true
	}
	// ui.DataTable derives its caption id from the caption, so a repeated
	// caption needs an id of its own.
	caption := props["caption"].(string)
	key := strings.ToLower(caption)
	m.tables[key]++
	if c := m.tables[key]; c > 1 {
		props["id"] = fmt.Sprintf("table-%s-%d", a11ySlug(caption, '-'), c)
	}
	return BlueprintBlock{Kind: "data_table", Props: props}, true
}

// section maps a named landmark (or a dialog's content) to a ui.Section,
// lifting a leading heading into the section heading. A leading level-1
// heading on a page with no title yet becomes the page header instead.
func (m *a11yMapper) section(n *ariaNode) []BlueprintBlock {
	children := n.Children
	props := map[string]any{"label": n.Name}
	var out []BlueprintBlock
	if len(children) > 0 && children[0].Role == "heading" && children[0].Label() != "" {
		if children[0].Attrs["level"] == "1" && !m.titled {
			out = m.heading(children[0])
		} else {
			props["heading"] = children[0].Label()
			props["label"] = ""
		}
		children = children[1:]
	}
	return append(out, BlueprintBlock{Kind: "section", Props: props, Children: m.siblings(children)})
}

// a11yDescendants collects every descendant with the given role, in order,
// without descending into a match.
func a11yDescendants(n *ariaNode, role string) []*ariaNode {
	var out []*ariaNode
	for _, c := range n.Children {
		if c.Role == role {
			out = append(out, c)
			continue
		}
		out = append(out, a11yDescendants(c, role)...)
	}
	return out
}

// a11yTextOf is a node's readable text: its name, its inline text, else
// its descendants' text joined by spaces.
func a11yTextOf(n *ariaNode) string {
	if l := n.Label(); l != "" {
		return l
	}
	var parts []string
	for _, c := range n.Children {
		if t := a11yTextOf(c); t != "" {
			parts = append(parts, t)
		}
	}
	return strings.Join(parts, " ")
}

// a11yAttrOn reports a boolean snapshot attribute: `[checked]` or
// `[checked=true]`. `[checked=mixed]` is not on.
func a11yAttrOn(n *ariaNode, attr string) bool {
	v, ok := n.Attrs[attr]
	return ok && (v == "true" || v == "")
}
