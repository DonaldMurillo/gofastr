package packcmd

import (
	"go/ast"
	"go/token"
	"net/url"
	"path"
	"strings"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appgen"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
)

// reverseRenderBody finds the screen's root call and reverses each child
// expression into a block. helpers maps package-local zero-arg function
// names to their single return expression, so a resource chain extracted
// into a shared helper (screen + island endpoint reusing one config) still
// reverses.
//
// Three root shapes are accepted. The generator emits the screen stack,
// `ui.Stack(ui.StackConfig{Gap: ui.GapXL}, children…)`, for a screen with
// blocks, wrapped in `html.Div(html.DivConfig{…data-component…}, stack)`
// when the screen has actions. Apps generated before that used
// `html.Div(html.DivConfig{…}, children…)`, and older ones (and
// hand-written screens that never moved) `render.Tag("div", attrs,
// children…)`. pack has to read all three or it silently returns no
// blocks for the others, which is how a round-trip loses screens.
func reverseRenderBody(fn *ast.FuncDecl, helpers map[string]ast.Expr) []appspec.AppSpecBlock {
	if fn.Body == nil {
		return nil
	}
	for _, stmt := range fn.Body.List {
		ret, ok := stmt.(*ast.ReturnStmt)
		if !ok || len(ret.Results) != 1 {
			continue
		}
		call, ok := ret.Results[0].(*ast.CallExpr)
		if !ok {
			continue
		}
		// Index of the first child argument: render.Tag takes (tag, attrs)
		// first, html.Div and ui.Stack take a single config.
		firstChild := -1
		switch callSel(call) {
		case "render.Tag":
			if len(call.Args) >= 2 {
				firstChild = 2
			}
		case "html.Div":
			if len(call.Args) >= 1 {
				firstChild = 1
			}
		case "ui.Stack":
			// Only the generator's own screen stack: a hand-written root
			// stack with its own Align or Justify would lose them.
			if isScreenStack(call) {
				firstChild = 1
			}
		}
		if firstChild < 0 {
			continue
		}
		// An html.Div whose one child is the screen stack is the actions
		// wrapper: the blocks are the stack's children.
		if callSel(call) == "html.Div" && len(call.Args) == 2 {
			if inner, ok := call.Args[1].(*ast.CallExpr); ok && isScreenStack(inner) {
				call, firstChild = inner, 1
			}
		}
		var out []appspec.AppSpecBlock
		children := call.Args[firstChild:]
		synthesized := len(children) > 0 && packIsSynthesizedHeader(children[0])
		if synthesized {
			children = children[1:]
		}
		for _, arg := range children {
			if b, ok := reverseBlock(arg, helpers); ok {
				out = append(out, b)
			}
		}
		if synthesized && appgen.AppSpecScreenDrawsH1(out) {
			// Something else draws the <h1>, so the generator would not
			// add this header: keep it as the author's.
			if b, ok := reverseBlock(call.Args[firstChild], helpers); ok {
				out = append([]appspec.AppSpecBlock{b}, out...)
			}
		}
		return out
	}
	return nil
}

// isScreenStack reports whether call is the generator's screen stack,
// `ui.Stack(ui.StackConfig{Gap: ui.GapXL}, …)` (appSpecScreenStackOpen),
// as opposed to a stack block an author placed on the screen.
func isScreenStack(call *ast.CallExpr) bool {
	if callSel(call) != "ui.Stack" || len(call.Args) < 1 {
		return false
	}
	lit, ok := call.Args[0].(*ast.CompositeLit)
	if !ok || len(lit.Elts) != 1 {
		return false
	}
	kv, ok := lit.Elts[0].(*ast.KeyValueExpr)
	if !ok {
		return false
	}
	key, ok := kv.Key.(*ast.Ident)
	if !ok || key.Name != "Gap" {
		return false
	}
	sel, ok := kv.Value.(*ast.SelectorExpr)
	return ok && sel.Sel.Name == "GapXL"
}

// packHelperReturns indexes top-level zero-arg functions with a single
// `return <expr>` across the parsed files, for reverseEntityResource to
// follow one hop when a screen body calls a local helper.
func packHelperReturns(files ...*ast.File) map[string]ast.Expr {
	out := map[string]ast.Expr{}
	for _, f := range files {
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || fn.Body == nil ||
				(fn.Type.Params != nil && len(fn.Type.Params.List) > 0) ||
				len(fn.Body.List) != 1 {
				continue
			}
			ret, ok := fn.Body.List[0].(*ast.ReturnStmt)
			if !ok || len(ret.Results) != 1 {
				continue
			}
			out[fn.Name.Name] = ret.Results[0]
		}
	}
	return out
}

// callSel returns "pkg.Method" for a simple selector call, else "".
func callSel(call *ast.CallExpr) string {
	if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
		if x := appgen.IdentName(sel.X); x != "" {
			return x + "." + sel.Sel.Name
		}
	}
	return ""
}

// reverseBlock turns one emitted UI expression back into an AppSpecBlock.
func reverseBlock(e ast.Expr, helpers map[string]ast.Expr) (appspec.AppSpecBlock, bool) {
	call, ok := e.(*ast.CallExpr)
	if !ok {
		return appspec.AppSpecBlock{}, false
	}
	// Entity builder chains (appUI.List("x")…): the entityui spelling the
	// generator emits now. The older appResources chains are read by
	// reverseEntityResource for apps generated before entityui.
	if b, ok := reverseEntityUI(e, helpers); ok {
		return b, true
	}
	// Entity resource chains (appResources["x"]…List/Detail/Form(ctx)):
	// the pre-entityui spelling, still read for apps generated then.
	if b, ok := reverseEntityResource(call, helpers); ok {
		return b, true
	}
	if control, ok := reverseControlBlock(call, helpers); ok {
		return control, true
	}
	if node, ok := reverseNodeBlock(call); ok {
		return node, true
	}
	if chart, ok := reverseChartCall(call); ok {
		return chart, true
	}
	switch callSel(call) {
	case "ui.Hero":
		return reverseHero(call), true
	case "ui.Section":
		return reverseSection(call, helpers), true
	case "ui.Card":
		// A titled chart is emitted as ui.Card(CardConfig{Heading}, <chart>):
		// reverse it back to the chart block with the heading as its title.
		// A card block always writes Description, so a card whose first
		// child is a chart (CardConfig{Heading, Description}, chart,
		// siblings…) is a card: read as a chart it lost its text and every
		// other child.
		c := cfgOf(call, 0)
		if _, isCard := c["Description"]; !isCard && len(call.Args) == 2 {
			if child, ok := call.Args[1].(*ast.CallExpr); ok {
				if b, ok := reverseChartCall(child); ok {
					putStr(b.Props, "title", appspec.AstString(c["Heading"]))
					return b, true
				}
			}
		}
		b := block("card", props2("heading", appspec.AstString(c["Heading"]), "text", appspec.AstString(c["Description"])))
		// The generator passes a card's children after the config:
		// ui.Card(cfg, children...).
		for _, arg := range call.Args[1:] {
			if child, ok := reverseBlock(arg, helpers); ok {
				b.Children = append(b.Children, child)
			}
		}
		return b, true
	case "ui.PageHeader":
		c := cfgOf(call, 0)
		return block("page_header", props2("title", appspec.AstString(c["Title"]), "subtitle", appspec.AstString(c["Subtitle"]), "eyebrow", appspec.AstString(c["Eyebrow"]))), true
	case "ui.LinkButton":
		c := cfgOf(call, 0)
		return block("link_button", props2("label", appspec.AstString(c["Label"]), "href", appspec.AstString(c["Href"]), "variant", buttonVariant(c["Variant"]))), true
	case "ui.Callout":
		// ui.Callout(CalloutConfig{Title}, render.Text(text)).
		b := block("callout", props2("title", appspec.AstString(cfgOf(call, 0)["Title"])))
		if len(b.Props) == 0 {
			b.Props = nil
		}
		if len(call.Args) > 1 {
			b.Text = renderTextArg(call.Args[1])
		}
		return b, true
	case "ui.Divider":
		return appspec.AppSpecBlock{Kind: "divider"}, true
	case "ui.Link":
		config := cfgOf(call, 0)
		return appspec.AppSpecBlock{Type: "link", Href: appspec.AstString(config["Href"]), Text: appspec.AstString(config["Text"]), Class: appspec.AstString(config["Class"])}, true
	case "ui.Stack":
		return reverseLayoutBlock("stack", call, helpers), true
	case "ui.Cluster":
		return reverseLayoutBlock("cluster", call, helpers), true
	case "ui.Grid":
		// A grid of pricing cards is a `pricing` block. stat_row is the
		// one grid the generator emits with no Gap (Min "12rem", whatever
		// its children); grid and stat_grid always carry Min and Gap, and
		// one holding only stat cards reads back as a stat_grid with its
		// layout. Every other grid preserves the explicit layout block.
		if len(call.Args) > 1 {
			if first, ok := call.Args[1].(*ast.CallExpr); ok && callSel(first) == "ui.PricingCard" {
				return reversePricing(call), true
			}
		}
		cfg := cfgOf(call, 0)
		_, hasGap := cfg["Gap"]
		allStats := len(call.Args) > 1
		for _, arg := range call.Args[1:] {
			child, ok := arg.(*ast.CallExpr)
			if !ok || callSel(child) != "ui.StatCard" {
				allStats = false
				break
			}
		}
		if !hasGap && (allStats || appspec.AstString(cfg["Min"]) == "12rem") {
			b := reverseLayoutBlock("stat_row", call, helpers)
			b.Props = nil
			return b, true
		}
		if allStats {
			return reverseLayoutBlock("stat_grid", call, helpers), true
		}
		return reverseLayoutBlock("grid", call, helpers), true
	case "ui.StatCard":
		return reverseStatCard(call), true
	case "ui.Markdown":
		return appspec.AppSpecBlock{Kind: "markdown", Text: appspec.AstString(cfgOf(call, 0)["Source"])}, true
	case "ui.AuthCard":
		return reverseAuthCard(call), true
	case "html.Heading":
		c := cfgOf(call, 0)
		lvl := 1
		if n, ok := appspec.AstInt(c["Level"]); ok {
			lvl = n
		}
		return appspec.AppSpecBlock{Type: "heading", Level: lvl, Class: appspec.AstString(c["Class"]), Text: renderTextArg(call.Args[len(call.Args)-1])}, true
	case "render.Join":
		if b, ok := reverseEntityForm(call); ok {
			return b, true
		}
	case "html.Div":
		// A bare-tag text block (type: div, header, article, nav, …; every
		// tag but the ones above draws as a div): html.Div(DivConfig{Class},
		// render.Text(text)). The authored tag name is not in the output,
		// so it packs as type: div, which regenerates the same code.
		if len(call.Args) == 2 {
			if text, ok := call.Args[1].(*ast.CallExpr); ok && callSel(text) == "render.Text" {
				return appspec.AppSpecBlock{Type: "div", Class: appspec.AstString(cfgOf(call, 0)["Class"]), Text: renderTextArg(text)}, true
			}
		}
	case "html.Paragraph":
		return appspec.AppSpecBlock{Type: "paragraph", Class: appspec.AstString(cfgOf(call, 0)["Class"]), Text: renderTextArg(call.Args[len(call.Args)-1])}, true
	case "render.Tag":
		return reverseRenderTag(call)
	}
	return appspec.AppSpecBlock{}, false
}

// reverseEntityForm recovers an entity_form from its header, form markers,
// and controls in their rendered order.
func reverseEntityForm(call *ast.CallExpr) (appspec.AppSpecBlock, bool) {
	if len(call.Args) != 2 {
		return appspec.AppSpecBlock{}, false
	}
	header, ok := call.Args[0].(*ast.CallExpr)
	if !ok || callSel(header) != "ui.PageHeader" {
		return appspec.AppSpecBlock{}, false
	}
	form, ok := call.Args[1].(*ast.CallExpr)
	if !ok || callSel(form) != "ui.Form" || len(form.Args) == 0 {
		return appspec.AppSpecBlock{}, false
	}
	merge, ok := cfgOf(form, 0)["ExtraAttrs"].(*ast.CallExpr)
	if !ok || callSel(merge) != "html.MergeAttrs" || len(merge.Args) == 0 {
		return appspec.AppSpecBlock{}, false
	}
	markers := map[string]string{}
	if lit, ok := merge.Args[0].(*ast.CompositeLit); ok {
		for _, el := range lit.Elts {
			if kv, ok := el.(*ast.KeyValueExpr); ok {
				markers[appspec.AstString(kv.Key)] = appspec.AstString(kv.Value)
			}
		}
	}
	if markers["data-entity-form"] == "" {
		return appspec.AppSpecBlock{}, false
	}
	b := appspec.AppSpecBlock{
		Kind:   "entity_form",
		Entity: markers["data-entity-form"],
		Mode:   markers["data-entity-mode"],
		Text:   appspec.AstString(cfgOf(header, 0)["Title"]),
	}
	for _, arg := range form.Args[1:] {
		name := entityFormControlField(arg)
		if name == "" {
			return appspec.AppSpecBlock{}, false
		}
		b.Fields = append(b.Fields, name)
	}
	return b, true
}

// entityFormControlField recovers the declared field name from ID or For.
// The wire name tells a form-scoped ID from an unscoped field whose name holds a dash.
func entityFormControlField(control ast.Expr) string {
	var id, wire string
	ast.Inspect(control, func(n ast.Node) bool {
		if id != "" && wire != "" {
			return false
		}
		kv, ok := n.(*ast.KeyValueExpr)
		if !ok {
			return true
		}
		key, ok := kv.Key.(*ast.Ident)
		if !ok {
			return true
		}
		switch key.Name {
		case "ID", "For":
			if v, found := strings.CutPrefix(appspec.AstString(kv.Value), "field-"); found && id == "" {
				id = v
			}
		case "Name":
			if wire == "" {
				wire = appspec.AstString(kv.Value)
			}
		}
		return true
	})
	if id == "" {
		return wire
	}
	formID, field, scoped := strings.Cut(id, "-")
	if scoped && strings.HasPrefix(formID, "entity_form_") && field != "" && appspec.WireKey(field) == wire {
		return field
	}
	return id
}

func reverseLayoutBlock(kind string, call *ast.CallExpr, helpers map[string]ast.Expr) appspec.AppSpecBlock {
	b := appspec.AppSpecBlock{Kind: kind, Props: map[string]any{}}
	cfg := cfgOf(call, 0)
	if kind == "grid" {
		putStr(b.Props, "min", appspec.AstString(cfg["Min"]))
	}
	if min := appspec.AstString(cfg["Min"]); kind == "stat_grid" && min != "12rem" {
		// "12rem" is stat_grid's default minimum, which the generator
		// writes out when the block set none.
		putStr(b.Props, "min", min)
	}
	if gap := reverseGap(appspec.AstSelName(cfg["Gap"])); gap != "" && gap != "md" {
		b.Props["gap"] = gap
	}
	if kind == "stack" || kind == "cluster" {
		// The generator emits Align/Justify only when the block set them,
		// so a present field is a prop, start included.
		if align := reverseAlign(appspec.AstSelName(cfg["Align"])); align != "" {
			b.Props["align"] = align
		}
		if justify := reverseJustify(appspec.AstSelName(cfg["Justify"])); justify != "" {
			b.Props["justify"] = justify
		}
	}
	if kind == "cluster" && appspec.AstBool(cfg["NoWrap"]) {
		b.Props["no_wrap"] = true
	}
	for _, arg := range call.Args[1:] {
		if child, ok := reverseBlock(arg, helpers); ok {
			b.Children = append(b.Children, child)
		}
	}
	if len(b.Props) == 0 {
		b.Props = nil
	}
	return b
}

func reverseGap(name string) string {
	return map[string]string{
		"GapNone": "none", "GapXS": "xs", "GapSM": "sm", "GapMD": "md",
		"GapLG": "lg", "GapXL": "xl", "Gap2XL": "2xl",
	}[name]
}

func reverseAlign(name string) string {
	return map[string]string{
		"AlignStart": "start", "AlignCenter": "center", "AlignEnd": "end",
		"AlignBaseline": "baseline", "AlignStretch": "stretch",
	}[name]
}

func reverseJustify(name string) string {
	return map[string]string{
		"JustifyStart": "start", "JustifyCenter": "center", "JustifyEnd": "end",
		"JustifyBetween": "between", "JustifyAround": "around",
	}[name]
}

func reverseEntityResource(call *ast.CallExpr, helpers map[string]ast.Expr) (appspec.AppSpecBlock, bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return appspec.AppSpecBlock{}, false
	}
	method := sel.Sel.Name
	if method != "List" && method != "Detail" && method != "Form" {
		return appspec.AppSpecBlock{}, false
	}
	b := appspec.AppSpecBlock{}
	node := sel.X
	hops := 0
	for {
		// Bound the helper-hop walk so a self-referential or mutually
		// recursive zero-arg helper (a() returns a()) can't loop
		// forever. 32 is far beyond any real helper chain depth;
		// returning not-reversible is the safe fallback (the screen
		// just isn't packed; it stays as authored Go).
		if hops > 32 {
			return appspec.AppSpecBlock{}, false
		}
		if idx, ok := node.(*ast.IndexExpr); ok {
			if appgen.IdentName(idx.X) == "appResources" {
				b.Entity = appspec.AstString(idx.Index)
				break
			}
			return appspec.AppSpecBlock{}, false
		}
		c, ok := node.(*ast.CallExpr)
		if !ok {
			return appspec.AppSpecBlock{}, false
		}
		s, ok := c.Fun.(*ast.SelectorExpr)
		if !ok {
			// customersList().List(ctx): the chain was extracted into a
			// package-local zero-arg helper (shared by the screen and the
			// island endpoint). Follow the hop and keep walking.
			if id, isIdent := c.Fun.(*ast.Ident); isIdent && len(c.Args) == 0 {
				if body, found := helpers[id.Name]; found {
					hops++
					node = body
					continue
				}
			}
			return appspec.AppSpecBlock{}, false
		}
		switch s.Sel.Name {
		case "WithColumns":
			for _, a := range c.Args {
				b.Fields = append(b.Fields, appspec.AstString(a))
			}
		case "WithLimit":
			if len(c.Args) == 1 {
				if n, ok := appspec.AstInt(c.Args[0]); ok {
					b.Limit = n
				}
			}
		case "WithCreate":
			b.Create = true
		case "WithHeading":
			if len(c.Args) == 1 {
				b.Text = appspec.AstString(c.Args[0])
			}
		case "WithEmpty":
			if len(c.Args) == 1 {
				b.EmptyText = appspec.AstString(c.Args[0])
			}
		case "WithEdit":
			// detail-only affordance; not a list flag
		}
		node = s.X
	}
	switch method {
	case "List":
		b.Kind = "entity_list"
	case "Detail":
		b.Kind = "entity_detail"
	case "Form":
		// synthesized create/edit form screen: mark so it can be dropped.
		if len(call.Args) == 2 && appspec.AstString(call.Args[1]) == "" {
			b.Kind = "entity_create"
		} else {
			b.Kind = "entity_edit"
		}
	}
	return b, true
}

// reverseEntityUI turns one appUI.List/Record/Create builder chain back
// into its AppSpecBlock: Columns→fields, PageSize→limit, As→mode,
// Heading→text, Empty→empty_text, Bulk→bulk, and a list without NoCreate
// is create:true (the builder's default). Key, Base, Delete and Duplicate
// are structural and do not round-trip. Returns false for anything that is
// not an appUI chain.
func reverseEntityUI(e ast.Expr, helpers map[string]ast.Expr) (appspec.AppSpecBlock, bool) {
	call, ok := e.(*ast.CallExpr)
	if !ok {
		return appspec.AppSpecBlock{}, false
	}
	b := appspec.AppSpecBlock{}
	noCreate, deletes, wheres := false, false, 0
	var omit []string
	node := ast.Expr(call)
	hops := 0
	for {
		// Same hop bound as reverseEntityResource: a self-referential or
		// mutually recursive helper must break the walk, not loop forever.
		if hops > 32 {
			return appspec.AppSpecBlock{}, false
		}
		c, ok := node.(*ast.CallExpr)
		if !ok {
			return appspec.AppSpecBlock{}, false
		}
		s, ok := c.Fun.(*ast.SelectorExpr)
		if !ok {
			if id, isIdent := c.Fun.(*ast.Ident); isIdent && len(c.Args) == 0 {
				if body, found := helpers[id.Name]; found {
					hops++
					node = body
					continue
				}
			}
			return appspec.AppSpecBlock{}, false
		}
		if appgen.IdentName(s.X) == "appUI" {
			switch s.Sel.Name {
			case "List":
				b.Kind = "entity_list"
				b.Create = !noCreate
			case "Record":
				// A detail offers Delete; an edit form is the record page
				// without it, its fields allowlist spelled as Omit.
				b.Kind = "entity_detail"
				if !deletes {
					b.Kind, b.Mode = "entity_form", "edit"
					if len(omit) > 0 {
						b.Props = map[string]any{packOmitProp: omit}
					}
				}
			case "Create":
				// synthesized create screen: marked so pack can drop it.
				b.Kind = "entity_create"
			default:
				return appspec.AppSpecBlock{}, false
			}
			if len(c.Args) < 1 {
				return appspec.AppSpecBlock{}, false
			}
			b.Entity = appspec.AstString(c.Args[0])
			return b, true
		}
		switch s.Sel.Name {
		case "Columns":
			for _, a := range c.Args {
				b.Fields = append(b.Fields, appspec.AstString(a))
			}
		case "As":
			if len(c.Args) == 1 {
				b.Mode = appspec.AstString(c.Args[0])
			}
		case "PageSize":
			if len(c.Args) == 1 {
				if n, ok := appspec.AstInt(c.Args[0]); ok {
					b.Limit = n
				}
			}
		case "NoCreate":
			noCreate = true
		case "Bulk":
			b.Bulk = true
		case "Where":
			// The pinned filter: one equality is the app spec's whole
			// filter, so a second pin makes the chain irreversible (pack
			// reports the screen instead of dropping a pin silently). Both
			// arguments must be string literals — the spec's filter is an
			// authored literal, and a pin by variable has no spelling.
			wheres++
			if wheres > 1 || len(c.Args) != 2 {
				return appspec.AppSpecBlock{}, false
			}
			field, fieldOK := c.Args[0].(*ast.BasicLit)
			value, valueOK := c.Args[1].(*ast.BasicLit)
			if !fieldOK || !valueOK || field.Kind != token.STRING || value.Kind != token.STRING {
				return appspec.AppSpecBlock{}, false
			}
			b.Filter = appspec.AstString(field) + "=" + appspec.AstString(value)
		case "Heading":
			if len(c.Args) >= 1 {
				b.Text = appspec.AstString(c.Args[0])
			}
		case "Empty":
			if len(c.Args) == 1 {
				b.EmptyText = appspec.AstString(c.Args[0])
			}
		case "Delete":
			deletes = true
		case "Omit":
			for _, a := range c.Args {
				omit = append(omit, appspec.AstString(a))
			}
		case "Key", "Base", "NoLinks", "Duplicate", "RenderCtx", "Actions", "Related", "RelatedAt":
			// structural: no app spec key (RenderCtx turns the builder
			// into the HTML the screen's stack takes; Actions, Related and
			// RelatedAt are app-side extensions pack does not declare).
			// NoLinks follows from the spec: the generator emits it on a
			// list whose entity has no record screen,
			// so the packed spec regenerates it; a NoLinks written by hand
			// on a list that has one is a difference pack check reports.
		default:
			return appspec.AppSpecBlock{}, false
		}
		node = s.X
		hops++
	}
}

func reverseHero(call *ast.CallExpr) appspec.AppSpecBlock {
	c := cfgOf(call, 0)
	p := map[string]any{}
	putStr(p, "eyebrow", appspec.AstString(c["Eyebrow"]))
	putStr(p, "title", appspec.AstString(c["Title"]))
	putStr(p, "subtitle", appspec.AstString(c["Subtitle"]))
	// Actions: []render.HTML{LinkButton(cta), LinkButton(secondary)}.
	// The variant says which CTA a button is: a hero with only a
	// secondary CTA emits it alone, first in the list.
	if actions, ok := c["Actions"].(*ast.CompositeLit); ok {
		for _, el := range actions.Elts {
			ac, ok := el.(*ast.CallExpr)
			if !ok || callSel(ac) != "ui.LinkButton" {
				continue
			}
			lc := cfgOf(ac, 0)
			label, href := appspec.AstString(lc["Label"]), appspec.AstString(lc["Href"])
			if buttonVariant(lc["Variant"]) != "secondary" {
				putStr(p, "cta_text", label)
				putStr(p, "cta_href", href)
			} else {
				putStr(p, "secondary_text", label)
				putStr(p, "secondary_href", href)
			}
		}
	}
	// Media: html.Image(html.ImageConfig{Src: image, Alt: title}), from
	// props.image or its media alias; the alt is always the title.
	if img, ok := c["Media"].(*ast.CallExpr); ok && callSel(img) == "html.Image" {
		putStr(p, "image", appspec.AstString(cfgOf(img, 0)["Src"]))
	}
	return appspec.AppSpecBlock{Kind: "hero", Props: p}
}

func reverseSection(call *ast.CallExpr, helpers map[string]ast.Expr) appspec.AppSpecBlock {
	c := cfgOf(call, 0)
	b := appspec.AppSpecBlock{Kind: "section", Props: props2(
		"heading", appspec.AstString(c["Heading"]), "eyebrow", appspec.AstString(c["Eyebrow"]), "description", appspec.AstString(c["Description"]),
		"label", appspec.AstString(c["Label"]), "class", appspec.AstString(c["Class"]), "id", appspec.AstString(c["ID"]))}
	// The section's children are wrapped in ui.Grid/ui.Stack(cfg, children…).
	for _, arg := range call.Args[1:] {
		wrap, ok := arg.(*ast.CallExpr)
		if !ok {
			continue
		}
		if cs := callSel(wrap); cs == "ui.Grid" || cs == "ui.Stack" {
			for _, child := range wrap.Args[1:] {
				if cb, ok := reverseBlock(child, helpers); ok {
					b.Children = append(b.Children, cb)
				}
			}
		}
	}
	return b
}

func reverseStatCard(call *ast.CallExpr) appspec.AppSpecBlock {
	c := cfgOf(call, 0)
	p := map[string]any{}
	putStr(p, "label", appspec.AstString(c["Label"]))
	// Value: statValue(ctx, entity, agg, field, filter, format) — the
	// app-local helper a hand-maintained app kept — or the generator's
	// appUI.StatValue spelling; both take the same six args.
	if vc, ok := c["Value"].(*ast.CallExpr); ok && len(vc.Args) == 6 {
		bare, _ := vc.Fun.(*ast.Ident)
		if (bare != nil && bare.Name == "statValue" && callSel(vc) == "") || callSel(vc) == "appUI.StatValue" {
			src := map[string]any{}
			putStr(src, "entity", appspec.AstString(vc.Args[1]))
			putStr(src, "agg", appspec.AstString(vc.Args[2]))
			putStr(src, "field", appspec.AstString(vc.Args[3]))
			putStr(src, "filter", appspec.AstString(vc.Args[4]))
			p["source"] = src
			putStr(p, "format", appspec.AstString(vc.Args[5]))
		}
	}
	if _, sourced := p["source"]; !sourced {
		// A static card: the generator writes props.value as a literal.
		putStr(p, "value", appspec.AstString(c["Value"]))
	}
	return appspec.AppSpecBlock{Kind: "stat_card", Props: p}
}

func reversePricing(call *ast.CallExpr) appspec.AppSpecBlock {
	plans := []any{}
	for _, arg := range call.Args[1:] {
		pc, ok := arg.(*ast.CallExpr)
		if !ok || callSel(pc) != "ui.PricingCard" {
			continue
		}
		c := cfgOf(pc, 0)
		plan := map[string]any{}
		putStr(plan, "name", appspec.AstString(c["Name"]))
		putStr(plan, "price", appspec.AstString(c["Price"]))
		putStr(plan, "period", appspec.AstString(c["Period"]))
		putStr(plan, "description", appspec.AstString(c["Description"]))
		if feats := appspec.AstStringSlice(c["Features"]); len(feats) > 0 {
			fa := make([]any, len(feats))
			for i, f := range feats {
				fa[i] = f
			}
			plan["features"] = fa
		}
		putStr(plan, "cta_text", appspec.AstString(c["CTALabel"]))
		putStr(plan, "cta_href", appspec.AstString(c["CTAHref"]))
		if appspec.AstBool(c["Featured"]) {
			plan["featured"] = true
		}
		plans = append(plans, plan)
	}
	return appspec.AppSpecBlock{Kind: "pricing", Props: map[string]any{"plans": plans}}
}

func reverseAuthCard(call *ast.CallExpr) appspec.AppSpecBlock {
	c := cfgOf(call, 0)
	// Body: ui.Form(ui.FormConfig{Action: …}, hidden-next, fields…).
	action, next, forgot := "", "", ""
	if form, ok := c["Body"].(*ast.CallExpr); ok && callSel(form) == "ui.Form" {
		action = appspec.AstString(cfgOf(form, 0)["Action"])
		for _, arg := range form.Args[1:] {
			// The forgot-password link: a ui.Link among the form's fields.
			if link, ok := arg.(*ast.CallExpr); ok && callSel(link) == "ui.Link" {
				forgot = appspec.AstString(cfgOf(link, 0)["Href"])
			}
			// The hidden next input: the generator emits it as
			// html.Input(html.InputConfig{Type: "hidden", Name: "next", …}).
			if in, ok := arg.(*ast.CallExpr); ok && callSel(in) == "html.Input" {
				ic := cfgOf(in, 0)
				if appspec.AstString(ic["Name"]) == "next" {
					value := ic["Value"]
					if pathOr, ok := value.(*ast.CallExpr); ok && callSel(pathOr) == "handler.SafeRelativePathOr" && len(pathOr.Args) == 2 {
						value = pathOr.Args[1]
					}
					next = appspec.AstString(value)
				}
			}
		}
	}
	footerHref := htmlAttr(rawString(c["Footer"]), "href")
	if footerHref == "" {
		// Since v0.61 the emitter renders the auth footer as the URL-checked
		// ui.Link component instead of a raw anchor; read Href from its config.
		if link, ok := c["Footer"].(*ast.CallExpr); ok && callSel(link) == "ui.Link" {
			footerHref = appspec.AstString(cfgOf(link, 0)["Href"])
		}
	}
	kind, hrefKey := "login_form", "register_href"
	if u, err := url.Parse(action); err == nil && path.Base(u.Path) == appspec.AuthRegisterSegment {
		kind, hrefKey = "signup_form", "login_href"
	}
	p := map[string]any{}
	putStr(p, "action", action)
	putStr(p, "next", next)
	putStr(p, hrefKey, footerHref)
	putStr(p, "forgot_href", forgot)
	return appspec.AppSpecBlock{Kind: kind, Text: appspec.AstString(c["Title"]), Props: p}
}

// rawString unwraps render.Raw("…") → its string content.
func rawString(e ast.Expr) string {
	if call, ok := e.(*ast.CallExpr); ok && callSel(call) == "render.Raw" && len(call.Args) == 1 {
		return appspec.AstString(call.Args[0])
	}
	return ""
}

// htmlAttr extracts the value of attr from an HTML snippet (attr="value").
func htmlAttr(html, attr string) string {
	needle := attr + `="`
	i := strings.Index(html, needle)
	if i < 0 {
		return ""
	}
	rest := html[i+len(needle):]
	if j := strings.IndexByte(rest, '"'); j >= 0 {
		return rest[:j]
	}
	return ""
}

func reverseRenderTag(call *ast.CallExpr) (appspec.AppSpecBlock, bool) {
	tag := appspec.AstString(call.Args[0])
	switch tag {
	case "p":
		return appspec.AppSpecBlock{Type: "paragraph", Text: renderTextArg(call.Args[len(call.Args)-1])}, true
	case "section":
		// A `type: section` text block: render.Tag("section", attrs,
		// render.Text(text)), its attrs nil or {"class": …}.
		if len(call.Args) != 3 {
			return appspec.AppSpecBlock{}, false
		}
		return appspec.AppSpecBlock{Type: "section", Class: tagAttrClass(call.Args[1]), Text: renderTextArg(call.Args[2])}, true
	case "div":
		var chart appspec.AppSpecBlock
		var title string
		for _, arg := range call.Args[2:] {
			child, ok := arg.(*ast.CallExpr)
			if !ok {
				continue
			}
			if callSel(child) == "html.Heading" && len(child.Args) > 0 {
				title = renderTextArg(child.Args[len(child.Args)-1])
			} else if chart.Kind == "" {
				chart, _ = reverseChartCall(child)
			}
		}
		if chart.Kind != "" {
			putStr(chart.Props, "title", title)
			return chart, true
		}
	}
	return appspec.AppSpecBlock{}, false
}

// tagAttrClass reads the class out of the attrs map a render.Tag call
// takes: map[string]string{"class": "x"} or nil.
func tagAttrClass(e ast.Expr) string {
	lit, ok := e.(*ast.CompositeLit)
	if !ok {
		return ""
	}
	for _, el := range lit.Elts {
		if kv, ok := el.(*ast.KeyValueExpr); ok && appspec.AstString(kv.Key) == "class" {
			return appspec.AstString(kv.Value)
		}
	}
	return ""
}

// reverseChartCall decodes a chart; wrappers recover any title.
func reverseChartCall(call *ast.CallExpr) (appspec.AppSpecBlock, bool) {
	if appgen.IdentName(call.Fun) == "lineChart" || callSel(call) == "appUI.LineChart" {
		return block("line_chart", map[string]any{"source": chartSource(call)}), true
	}
	var kind string
	switch callSel(call) {
	case "ui.BarChart":
		kind = "bar_chart"
	case "ui.PieChart":
		kind = "pie_chart"
	case "ui.LineChart":
		kind = "line_chart"
	default:
		return appspec.AppSpecBlock{}, false
	}
	props := map[string]any{}
	cfg := cfgOf(call, 0)
	if dataCall, ok := cfg["Bars"].(*ast.CallExpr); ok {
		props["source"] = chartSource(dataCall)
	} else if dataCall, ok := cfg["Slices"].(*ast.CallExpr); ok {
		props["source"] = chartSource(dataCall)
	}
	return block(kind, props), true
}

func chartSource(dataCall *ast.CallExpr) map[string]any {
	src := map[string]any{}
	if len(dataCall.Args) == 3 {
		putStr(src, "entity", appspec.AstString(dataCall.Args[1]))
		putStr(src, "group_by", appspec.AstString(dataCall.Args[2]))
	}
	return src
}

// ----- small reverse helpers -------------------------------------------------

func cfgOf(call *ast.CallExpr, i int) map[string]ast.Expr {
	if i < len(call.Args) {
		return appspec.FieldVals(call.Args[i])
	}
	return map[string]ast.Expr{}
}

func block(kind string, props map[string]any) appspec.AppSpecBlock {
	return appspec.AppSpecBlock{Kind: kind, Props: props}
}

func props2(kv ...string) map[string]any {
	m := map[string]any{}
	for i := 0; i+1 < len(kv); i += 2 {
		if kv[i+1] != "" {
			m[kv[i]] = kv[i+1]
		}
	}
	return m
}

func renderTextArg(e ast.Expr) string {
	if call, ok := e.(*ast.CallExpr); ok && callSel(call) == "render.Text" && len(call.Args) == 1 {
		return appspec.AstString(call.Args[0])
	}
	return ""
}

func buttonVariant(e ast.Expr) string {
	switch appspec.AstSelName(e) {
	case "ButtonPrimary":
		return "primary"
	case "ButtonSecondary":
		return "secondary"
	case "ButtonDanger":
		return "danger"
	case "ButtonGhost":
		return "ghost"
	}
	return ""
}
