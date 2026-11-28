package appgen

import (
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/cli"
	"github.com/DonaldMurillo/gofastr/core-ui/urlsafe"
)

const a11ySnapshotMaxBytes = 4 << 20

func a11yScreenBody(path, route string, jsonMode bool) ([]appspec.AppSpecBlock, error) {
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
			cli.Fprintln(os.Stderr, "warning: "+w)
			continue
		}
		cli.Warn("%s", w)
	}
	if len(blocks) == 0 {
		return nil, fmt.Errorf("the snapshot has nothing to render: no headings, text, links, or controls")
	}
	return blocks, nil
}

type a11yMapper struct {
	action    string
	warnings  []string
	warned    map[string]bool
	names     map[string]bool
	tables    map[string]int
	formDepth int
	titled    bool
}

func newA11yMapper(action string) *a11yMapper {
	return &a11yMapper{action: action, warned: map[string]bool{}, names: map[string]bool{}, tables: map[string]int{}}
}

func mapA11yToBlocks(nodes []*ariaNode, action string) ([]appspec.AppSpecBlock, []string) {
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

var a11yTransparentRoles = map[string]bool{
	"generic": true, "none": true, "presentation": true, "main": true,
	"document": true, "application": true, "tabpanel": true, "labeltext": true, "superscript": true, "subscript": true,
}

var a11yLandmarkRoles = map[string]bool{
	"region": true, "article": true, "complementary": true, "group": true,
	"search": true, "figure": true,
}

var a11yTextRoles = map[string]bool{
	"text": true, "paragraph": true, "statictext": true, "blockquote": true,
	"code": true, "strong": true, "emphasis": true, "caption": true,
	"term": true, "definition": true, "note": true, "time": true,
	"status": true, "log": true, "mark": true, "insertion": true, "deletion": true,
}

var a11yFieldRoles = map[string]bool{
	"textbox": true, "searchbox": true, "spinbutton": true, "checkbox": true,
	"switch": true, "combobox": true, "listbox": true, "radiogroup": true, "radio": true,
}

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

func (m *a11yMapper) siblings(nodes []*ariaNode) []appspec.AppSpecBlock {
	nodes = dropRedundantLabels(m.flatten(nodes))
	var out []appspec.AppSpecBlock
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
		if last := len(out) - 1; last >= 1 && out[last].Kind == "data_table" && out[last-1].Type == "heading" &&
			strings.EqualFold(out[last-1].Text, appspec.AppSpecProp(out[last], "caption")) {
			out[last].Props["caption_hidden"] = true
		}
		if last := len(out) - 1; last >= 0 && out[last].Kind == "page_header" && out[last].Props["subtitle"] == nil &&
			i < len(nodes) && (nodes[i].Role == "paragraph" || nodes[i].Role == "text") &&
			len(nodes[i].Children) == 0 && nodes[i].Label() != "" {
			out[last].Props["subtitle"] = nodes[i].Label()
			i++
		}
	}
	return out
}

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

func (m *a11yMapper) node(n *ariaNode) []appspec.AppSpecBlock {
	switch {
	case n.Role == "banner" || n.Role == "contentinfo":
		m.warn("role:"+n.Role, "skipped %s (line %d): the app layout renders the site header and footer", n.Role, n.Line)
		return nil
	case n.Role == "heading":
		return m.heading(n)
	case a11yTextRoles[n.Role]:
		if len(n.Children) > 0 {
			parts := m.siblings(n.Children)
			if n.Text != "" {
				parts = append([]appspec.AppSpecBlock{{Type: "text", Text: n.Text}}, parts...)
			}
			return a11yGroup(parts)
		}
		if n.Label() == "" {
			return nil
		}
		return []appspec.AppSpecBlock{{Type: "text", Text: n.Label()}}
	case n.Role == "alert":
		return []appspec.AppSpecBlock{m.alert(n)}
	case n.Role == "link":
		return []appspec.AppSpecBlock{m.link(n)}
	case n.Role == "button":
		return []appspec.AppSpecBlock{m.button(n, "button")}
	case n.Role == "form":
		if m.formDepth > 0 {
			m.warn(fmt.Sprintf("form:%d", n.Line), "form (line %d) sits inside another form: its fields joined the outer one, since HTML forms do not nest", n.Line)
			return m.siblings(n.Children)
		}
		return []appspec.AppSpecBlock{m.form(n.Children)}
	case n.Role == "radiogroup":
		m.warnDropped(n, "radios", "radio")
		return m.radioGroup(n.Name, n.Line, a11yDescendants(n, "radio"))
	case a11yFieldRoles[n.Role]:
		return []appspec.AppSpecBlock{m.field(n)}
	case n.Role == "navigation":
		return []appspec.AppSpecBlock{m.navigation(n)}
	case n.Role == "list" || n.Role == "directory":
		return []appspec.AppSpecBlock{m.list(n)}
	case n.Role == "table" || n.Role == "grid" || n.Role == "treegrid":
		if b, ok := m.table(n); ok {
			return []appspec.AppSpecBlock{b}
		}
		return nil
	case n.Role == "separator":
		return []appspec.AppSpecBlock{{Kind: "divider"}}
	case a11yLandmarkRoles[n.Role] || n.Role == "dialog" || n.Role == "alertdialog":
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
		return []appspec.AppSpecBlock{{Type: "text", Text: n.Label()}}
	}
	m.warn("role:"+n.Role, "skipped %s (line %d): no catalog component and no text", n.Role, n.Line)
	return nil
}

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

func a11yGroup(parts []appspec.AppSpecBlock) []appspec.AppSpecBlock {
	if len(parts) <= 1 {
		return parts
	}
	for _, p := range parts {
		inline := p.Kind == "action_button" || (p.Kind == "" && (p.Type == "text" || p.Type == "link"))
		if !inline {
			return []appspec.AppSpecBlock{{Kind: "stack", Props: map[string]any{"gap": "xs"}, Children: parts}}
		}
	}
	return []appspec.AppSpecBlock{{Kind: "cluster", Props: map[string]any{"gap": "xs", "align": "baseline"}, Children: parts}}
}

func (m *a11yMapper) heading(n *ariaNode) []appspec.AppSpecBlock {
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
			return []appspec.AppSpecBlock{{Kind: "page_header", Props: map[string]any{"title": n.Label()}}}
		}
		m.warn(fmt.Sprintf("h1:%d", n.Line), "heading %q (line %d) is a second level-1 heading: made level 2", n.Label(), n.Line)
		level = 2
	}
	return []appspec.AppSpecBlock{{Type: "heading", Level: level, Text: n.Label()}}
}

func (m *a11yMapper) alert(n *ariaNode) appspec.AppSpecBlock {
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
	return appspec.AppSpecBlock{Kind: "callout", Text: body, Props: map[string]any{"title": title}}
}

func (m *a11yMapper) link(n *ariaNode) appspec.AppSpecBlock {
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
	return appspec.AppSpecBlock{Type: "link", Text: text, Href: href}
}

func (m *a11yMapper) button(n *ariaNode, typ string) appspec.AppSpecBlock {
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
	return appspec.AppSpecBlock{Kind: "action_button", Props: props}
}

func (m *a11yMapper) form(nodes []*ariaNode) appspec.AppSpecBlock {
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
	return appspec.AppSpecBlock{Kind: "custom_form", Props: props, Children: children}
}

func (m *a11yMapper) field(n *ariaNode) appspec.AppSpecBlock {
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
		return appspec.AppSpecBlock{Kind: n.Role, Props: props}
	case "spinbutton":
		return appspec.AppSpecBlock{Kind: "number_field", Props: props}
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
			return appspec.AppSpecBlock{Kind: "select_field", Props: props}
		}
	case "textbox", "searchbox":
		if n.Text != "" {
			m.warn(fmt.Sprintf("value:%d", n.Line), "%s %q (line %d): dropped its current value %q; the generated field starts empty", n.Role, label, n.Line, n.Text)
		}
	}
	if p := n.Props["placeholder"]; p != "" {
		props["placeholder"] = p
	}
	return appspec.AppSpecBlock{Kind: "text_field", Props: props}
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

func (m *a11yMapper) radioGroup(legend string, line int, radios []*ariaNode) []appspec.AppSpecBlock {
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
	return []appspec.AppSpecBlock{{Kind: "radio_group", Props: map[string]any{"legend": legend, "name": m.fieldName(legend), "options": options}}}
}

func (m *a11yMapper) navigation(n *ariaNode) appspec.AppSpecBlock {
	var links []appspec.AppSpecBlock
	for _, l := range a11yDescendants(n, "link") {
		links = append(links, m.link(l))
	}
	m.warnDropped(n, "links", "link")
	label := n.Name
	if label == "" {
		label = "Page links"
	}
	return appspec.AppSpecBlock{Kind: "nav_links", Props: map[string]any{"label": label}, Children: links}
}

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

func (m *a11yMapper) list(n *ariaNode) appspec.AppSpecBlock {
	var items []appspec.AppSpecBlock
	for _, c := range n.Children {
		if c.Role != "listitem" {
			items = append(items, m.node(c)...)
			continue
		}
		parts := m.siblings(c.Children)
		if c.Text != "" {
			parts = append([]appspec.AppSpecBlock{{Type: "text", Text: c.Text}}, parts...)
		}
		if c.Name != "" && len(parts) == 0 {
			parts = []appspec.AppSpecBlock{{Type: "text", Text: c.Name}}
		}
		items = append(items, a11yGroup(parts)...)
	}
	return appspec.AppSpecBlock{Kind: "item_list", Children: items}
}

func (m *a11yMapper) table(n *ariaNode) (appspec.AppSpecBlock, bool) {
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
		return appspec.AppSpecBlock{}, false
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
		m.warn(fmt.Sprintf("caption:%d", n.Line), "table (line %d) has no accessible name: captioned \"Table\" for screen readers only", n.Line)
		props["caption"] = "Table"
		props["caption_hidden"] = true
	}
	caption := props["caption"].(string)
	key := strings.ToLower(caption)
	m.tables[key]++
	if c := m.tables[key]; c > 1 {
		props["id"] = fmt.Sprintf("table-%s-%d", a11ySlug(caption, '-'), c)
	}
	return appspec.AppSpecBlock{Kind: "data_table", Props: props}, true
}

func (m *a11yMapper) section(n *ariaNode) []appspec.AppSpecBlock {
	children := n.Children
	props := map[string]any{"label": n.Name}
	var out []appspec.AppSpecBlock
	if len(children) > 0 && children[0].Role == "heading" && children[0].Label() != "" {
		if children[0].Attrs["level"] == "1" && !m.titled {
			out = m.heading(children[0])
		} else {
			props["heading"] = children[0].Label()
			props["label"] = ""
		}
		children = children[1:]
	}
	return append(out, appspec.AppSpecBlock{Kind: "section", Props: props, Children: m.siblings(children)})
}

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

func a11yAttrOn(n *ariaNode, attr string) bool {
	v, ok := n.Attrs[attr]
	return ok && (v == "true" || v == "")
}
