package headless

import (
	"net/url"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/interactive"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// Slots hold replacement content for named parts.
type Slots map[Part]render.HTML

// PartAttrs add attributes to named parts.
type PartAttrs map[Part]html.Attrs

// Bind keeps a part's text, HTML or one attribute in step with a client
// signal.
type Bind struct {
	Signal string
	// Mode is "text" (the default), "html" or "attr".
	Mode string
	// Attr is the attribute mode "attr" writes.
	Attr string
}

// Binds maps parts to the signal each follows.
type Binds map[Part]Bind

var runtimeOwnedBindAttrs = map[string]bool{
	"aria-pressed": true, "aria-busy": true, "aria-live": true, "aria-invalid": true,
	"aria-expanded": true, "aria-current": true,
	"hidden": true, "disabled": true, "data-state": true,
}

func (b Bind) attrs() html.Attrs {
	if b.Signal == "" {
		panic("headless: a Bind needs a Signal")
	}
	checkSignalName(b.Signal)
	mode := orDefault(b.Mode, "text")
	out := html.Attrs{"data-cui-signal": b.Signal, "data-cui-signal-mode": mode}
	switch mode {
	case "text", "html":
		if b.Attr != "" {
			panic("headless: Bind mode " + mode + " takes no Attr")
		}
	case "attr":
		if b.Attr == "" {
			panic("headless: Bind mode attr needs the Attr to write")
		}
		folded := strings.ToLower(b.Attr)
		if runtimeOwnedBindAttrs[folded] || strings.HasPrefix(folded, "data-hui-") {
			panic("headless: a signal may not write " + b.Attr + " — the runtime owns it (a lifecycle state, a platform state, or a data-hui-* hook), and a bound value would fight it")
		}
		if !interactive.SignalAttrAllowed(b.Attr) {
			panic("headless: a signal may not write " + b.Attr + " — an attribute outside the framework's allow-list executes regardless of the value bound to it")
		}
		out["data-cui-signal-attr"] = b.Attr
	default:
		panic("headless: Bind mode must be text, html or attr, not " + mode)
	}
	return out
}

func checkSignalName(name string) {
	if name == "__proto__" || name == "constructor" || name == "prototype" {
		panic("headless: signal name " + strconv.Quote(name) + " is reserved and the runtime never writes it")
	}
}

// Box carries a component's Classes and a caller's Parts through its render.
type Box struct {
	Classes Classes
	Slots   Slots
	Over    PartAttrs
	Binds   Binds

	fillable map[Part]bool
}

// FillableOn names the parts whose content a caller may replace.
func (b Box) FillableOn(parts ...Part) Box {
	if b.fillable == nil {
		b.fillable = make(map[Part]bool, len(parts))
	}
	for _, p := range parts {
		b.fillable[p] = true
	}
	return b
}

// Boxed builds a Box from a component's props.
func Boxed(s Classes, slots Slots, over PartAttrs) Box {
	return Box{Classes: s, Slots: slots, Over: over}
}

// El renders one element: the class map's class, the caller's part attrs,
// then the component's own attrs, which win.
func (b Box) El(tag string, p Part, own html.Attrs, children ...render.HTML) render.HTML {
	if len(b.Over) == 0 && len(b.Binds) == 0 {
		return El(tag, b.Classes, p, own, children...)
	}
	merged := html.Attrs{}
	extraClass := ""
	ownedKeys := make(map[string]bool, len(own))
	for k := range own {
		ownedKeys[strings.ToLower(k)] = true
	}
	for k, v := range allowedPartAttrs(b.Over[p]) {
		if strings.ToLower(k) == "class" {
			extraClass = v
			continue
		}
		if ownedKeys[strings.ToLower(k)] {
			continue
		}
		merged[k] = v
	}
	for k, v := range own {
		merged[k] = v
	}
	if extraClass != "" {
		merged["class"] = strings.TrimSpace(merged["class"] + " " + extraClass)
	}
	if bind, ok := b.Binds[p]; ok {
		triple := bind.attrs()
		if mode := orDefault(bind.Mode, "text"); mode == "text" || mode == "html" {
			if !b.fillable[p] {
				panic("headless: a " + mode + " Bind rewrites the content of " + string(p) +
					", which is not a fillable part — a Slot may not fill it either; bind an attribute, or a part the spec lists as fillable")
			}
		}
		for k, v := range triple {
			merged[k] = v
		}
	}
	return El(tag, b.Classes, p, merged, children...)
}

// Fill returns the caller's slot content for a part, or def.
func (b Box) Fill(p Part, def render.HTML) render.HTML {
	if v, ok := b.Slots[p]; ok {
		return v
	}
	return def
}

// Filled reports whether a slot holds content.
func (b Box) Filled(p Part) bool {
	v, ok := b.Slots[p]
	return ok && v != ""
}

func allowedPartAttrs(a html.Attrs) html.Attrs {
	out := html.Attrs{}
	for k, v := range a {
		lk := strings.ToLower(k)
		if lk == "id" || refused(lk) {
			continue
		}
		if _, twice := out[lk]; twice {
			panic("headless: part attrs repeat " + lk + " under two spellings")
		}
		out[lk] = v
	}
	return out
}

// Parts is what a caller may set on a component's named parts: attributes,
// content for the fillable parts, and signal bindings.
type Parts struct {
	Attrs PartAttrs
	Slots Slots
	Binds Binds
}

// Box builds the render Box that applies s; fillable lists the parts a
// text or html Bind may rewrite.
func (s Parts) Box(classes Classes, fillable ...Part) Box {
	b := Boxed(classes, s.Slots, s.Attrs)
	b.Binds = s.Binds
	return b.FillableOn(fillable...)
}

func (i Island) attrs(href, method string) html.Attrs {
	i.check()
	m := strings.ToUpper(method)
	if m == "" {
		panic("headless: an Island trigger needs a method — GET for a read, the form's method for a mutation")
	}
	out := html.Attrs{
		"data-cui-rpc":        mergeQuery(i.Endpoint, href),
		"data-cui-rpc-method": m,
		"data-cui-rpc-signal": i.Signal,
	}
	if href != "" && m == "GET" {
		out["data-cui-push-state"] = href
	}
	return out
}

func mergeQuery(endpoint, href string) string {
	u, err := url.Parse(endpoint)
	if err != nil {
		return endpoint
	}
	u.Fragment = ""
	u.RawFragment = ""
	h, err := url.Parse(href)
	if err != nil {
		return u.String()
	}
	q := u.Query()
	for k, vs := range h.Query() {
		q[k] = append(q[k], vs...)
	}
	u.RawQuery = q.Encode()
	return u.String()
}
func group(kids ...render.HTML) render.HTML {
	var b strings.Builder
	for _, k := range kids {
		b.WriteString(string(k))
	}
	return render.HTML(b.String())
}
