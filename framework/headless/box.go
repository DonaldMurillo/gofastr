package headless

// Slots and part attributes: the two ways a caller reaches inside a
// component without forking it.
//
// The problem this solves is the one every design system hits on its
// second year. A component is right for eighty pages and wrong for
// one, so someone copies it, and now there are two — one of which
// will not be fixed when the accessibility bug is found. Every escape
// hatch below exists so that page can stay on the real component.
//
// The vocabulary is deliberately the SAME vocabulary the class map uses.
// A Part is a name for an element inside a component, and it now
// answers three questions instead of one:
//
//	classes[part]       what class does it carry
//	slots[part]      what goes inside it
//	attrs[part]      what attributes does it also carry
//
// One list of names, three things you can do to each. A part a class map
// can style is a part a caller can fill and annotate, which means the
// component author declares its parts once and cannot accidentally
// offer a hook to one layer and not the others.
//
// What a caller may NOT do is the whole reason this is a type and not
// a map handed to render.Tag:
//
//   - it cannot set id. Ids are how a label finds its control and how
//     aria-describedby finds its hint; a caller that renames one
//     breaks a relationship it cannot see.
//   - it cannot set or forge a data-hui-* hook. Those are the contract
//     between the markup and the runtime; a forged one binds
//     behaviour to an element that was never built for it.
//   - it cannot set style. A host serving default-src 'self' with no
//     unsafe-inline (the framework's default posture) drops an inline
//     style, so it is not a style — it is a rule the browser ignores
//     and a component that renders wrong in production only.
//   - it cannot win against an attribute the component owns. A
//     component that sets role="dialog" means it; an override that
//     lands on top of it is how a dialog becomes a div.
//
// class is the exception, and it appends rather than replaces: adding
// a utility class is the common, harmless want, and dropping the
// Dropping the class map's own class is how a component arrives unstyled.

import (
	"net/url"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/interactive"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// Slots hold caller content for named parts. A slot REPLACES what the
// component would have put there, which is what makes it useful and
// what makes it dangerous: a component offers a slot only where its
// own content carries no guarantee, and the harness renders every
// declared slot filled with something hostile to prove it.
type Slots map[Part]render.HTML

// PartAttrs add attributes to named parts, subject to the rules above.
type PartAttrs map[Part]html.Attrs

// Bind keeps a part in step with one of the host framework's client
// signals: its runtime rewrites the part's text, its HTML, or one
// attribute whenever the signal changes. It is the third thing a
// caller may set on a part, beside slots and attrs, because a binding
// is neither: an attribute may
// not carry a data-fui-* key, on purpose, so the only way a part can
// follow a signal is to say so here, where it is typed, reviewable,
// lands on exactly one element, and cannot reach an attribute the
// runtime would execute.
type Bind struct {
	// Signal is the signal's name. Required.
	Signal string
	// Mode is "text" (the default: the part's text is the value),
	// "html" (the part's HTML is the value, the trusted path an island
	// fragment uses) or "attr" (one attribute is the value).
	Mode string
	// Attr is the attribute for mode "attr", and must be one the
	// framework lets a signal write: any aria-*, or its allow-list.
	// Empty otherwise.
	Attr string
}

// Binds maps parts to the signal each follows.
type Binds map[Part]Bind

// runtimeOwnedBindAttrs are the attribute names a signal may not
// write on a part, on top of the framework's own allow-list: the
// runtime rewrites them as state moves (the action lifecycle's
// aria-pressed and aria-busy, a control's aria-invalid,
// aria-expanded and aria-current, the platform states hidden and
// disabled), and every data-hui-* hook is this package's contract
// with its module. A bound value would fight whichever of them owns
// the part — winning some writes and losing others, with nothing
// anywhere reporting the race — so it is refused at render, where the
// mistake is a panic with a reason.
var runtimeOwnedBindAttrs = map[string]bool{
	"aria-pressed": true, "aria-busy": true, "aria-live": true, "aria-invalid": true,
	"aria-expanded": true, "aria-current": true,
	"hidden": true, "disabled": true, "data-state": true,
}

// attrs renders the binding triple, refusing what the runtime would
// refuse — at render, where it is a panic with a reason, rather than
// in the browser, where it is a part that never updates.
func (b Bind) attrs() html.Attrs {
	if b.Signal == "" {
		panic("headless: a Bind needs a Signal")
	}
	checkSignalName(b.Signal)
	mode := orDefault(b.Mode, "text")
	out := html.Attrs{"data-fui-signal": b.Signal, "data-fui-signal-mode": mode}
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
		out["data-fui-signal-attr"] = b.Attr
	default:
		panic("headless: Bind mode must be text, html or attr, not " + mode)
	}
	return out
}

// checkSignalName refuses the three names the runtime kernel never
// writes, because as dynamic property names on the store they
// re-parent its prototype chain. It mirrors core-ui/interactive's
// refuseReservedSignalName, and it is one function because a Bind, an
// Island and a Button's Action all name a signal, and a region bound
// to a name the kernel drops is a region that never updates with no
// error anywhere.
func checkSignalName(name string) {
	if name == "__proto__" || name == "constructor" || name == "prototype" {
		panic("headless: signal name " + strconv.Quote(name) + " is reserved and the runtime never writes it")
	}
}

// Box carries the three layers through a component's render. A zero
// Box with only a Classes behaves exactly as El always did, which is why
// adopting it is a per-component change and not a rewrite.
type Box struct {
	Classes Classes
	Slots   Slots
	Over    PartAttrs
	Binds   Binds

	// fillable is the set of parts whose content a caller may replace:
	// a Slot fills one, and a text or html Bind rewrites one. It is
	// set from the same list the component's spec declares as
	// Fillable, and the harness holds the two lists to the same answer
	// from both sides — a text Bind on any other part refuses, and one
	// on each of these arrives. Nil, a component offering no slots,
	// text-binds nothing at all.
	fillable map[Part]bool
}

// FillableOn names the parts whose content a caller may replace — the
// same parts the spec lists as Fillable — so a text or html Bind can
// be refused on every other part at render, where the mistake is a
// panic with a reason. A Bind that rewrites content is a Slot with a
// timer: allowed where Slots are, and nowhere else.
func (b Box) FillableOn(parts ...Part) Box {
	if b.fillable == nil {
		b.fillable = make(map[Part]bool, len(parts))
	}
	for _, p := range parts {
		b.fillable[p] = true
	}
	return b
}

// Boxed is the constructor a component calls with whatever its props
// carry.
func Boxed(s Classes, slots Slots, over PartAttrs) Box {
	return Box{Classes: s, Slots: slots, Over: over}
}

// El renders one element with the part's class, the caller's
// attrs for that part, then the component's own attrs — in that
// order, so the component wins.
func (b Box) El(tag string, p Part, own html.Attrs, children ...render.HTML) render.HTML {
	if len(b.Over) == 0 && len(b.Binds) == 0 {
		return El(tag, b.Classes, p, own, children...)
	}
	merged := html.Attrs{}
	extraClass := ""
	// Ownership is compared folded: ROLE and role are one attribute to
	// the browser, so a spelling the component does not use must not
	// slip past the one it does.
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
	// The binding last: its three keys are the runtime's, and no
	// component owns them, so there is nothing for it to beat.
	if bind, ok := b.Binds[p]; ok {
		triple := bind.attrs()
		// A text or html Bind replaces the part's content, so it is
		// allowed exactly where a Slot is. Anywhere else it would gut
		// whatever the part guarantees — a Password's input and reveal
		// button, an action's two labels — and the component would
		// render as its own skeleton with nothing failing.
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

// Fill returns the caller's content for a part, or the component's own
// when there is none.
func (b Box) Fill(p Part, def render.HTML) render.HTML {
	if v, ok := b.Slots[p]; ok {
		return v
	}
	return def
}

// Filled reports whether a slot was supplied. A component uses it when
// the presence of content changes the structure — a footer that is not
// rendered at all rather than rendered empty.
func (b Box) Filled(p Part) bool {
	v, ok := b.Slots[p]
	return ok && v != ""
}

// Child returns a Box for a nested component: the same class map, and none
// of the slots or attrs. A slot named "footer" means this
// component's footer, not the footer of everything it happens to
// contain — passing them down would make one name reach an unbounded
// set of elements.
func (b Box) Child(s Classes) Box { return Box{Classes: s} }

// allowedPartAttrs drops what a caller may not set on a part. It is a function
// rather than a method so the test can state the rule directly. Keys
// are stored folded, as Safe stores them, and one key under two
// spellings is refused for the same reason.
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

// Parts is what a caller may set on a component's named parts: extra
// attributes, replacement content for the parts the spec lists as
// fillable, and a binding to a client signal. It is one value rather
// than three loose fields so the harness can find it: a component
// either offers its parts or does not, and which parts a caller can
// reach is a question with one answer per component.
type Parts struct {
	// Attrs add attributes to named parts. An attribute the component
	// owns is never beaten, and the keys refused on every way in are
	// refused here too (see refused).
	Attrs PartAttrs
	// Slots replace named parts' content. Only the parts a component
	// lists as fillable do anything; the rest are ignored rather than
	// silently half-applied.
	Slots Slots
	// Binds keep named parts in step with client signals: an
	// attribute anywhere the allow-lists allow, and text or html only
	// on a fillable part, where a Bind may replace what a Slot may.
	Binds Binds
}

// Box builds the render box that applies a caller's Parts. The
// fillable names are the parts whose content a caller may replace —
// the spec's Fillable list — and a text or html Bind on any other
// part is refused at render: it would replace content the component
// guarantees, which is the same reason a Slot is offered only there.
func (s Parts) Box(classes Classes, fillable ...Part) Box {
	b := Boxed(classes, s.Slots, s.Attrs)
	b.Binds = s.Binds
	return b.FillableOn(fillable...)
}

// attrs renders an Island's RPC contract for one trigger. The Island
// itself is declared in island.go; the method lives here because it
// speaks html.Attrs, the vocabulary of this file.
//
// href is the no-script destination the same element already carries
// ("" for a form whose action is the page); method is GET for a read
// and the form's method for a mutation.
//
// The endpoint keeps the href's query, merged pair by pair onto its
// own. State keys (the page, the sort, the filter) belong in the href
// and nowhere else: when the endpoint carries a key the href also
// carries, both values survive, the endpoint's first, and a handler
// that reads Query().Get sees the endpoint's stale one. The merge is
// pair by pair because the page and the island answer the same question: a
// link to "/apps?page=3&sort=name" fetches the third page sorted by
// name as a document, and the island fetches exactly that as a
// region. Two URLs for one click is how the two drift apart and the
// island starts showing a different page than the address bar says.
//
// data-fui-push-state is rendered only for a GET with a href to write:
// a read knows the canonical URL ahead of time, while a mutation's URL
// is the server's to set — it answers with X-Gofastr-Push-State once
// it knows whether the change succeeded and where the thing it changed
// now lives.
func (i Island) attrs(href, method string) html.Attrs {
	i.check()
	m := strings.ToUpper(method)
	if m == "" {
		panic("headless: an Island trigger needs a method — GET for a read, the form's method for a mutation")
	}
	out := html.Attrs{
		"data-fui-rpc":        mergeQuery(i.Endpoint, href),
		"data-fui-rpc-method": m,
		"data-fui-rpc-signal": i.Signal,
	}
	if href != "" && m == "GET" {
		out["data-fui-push-state"] = href
	}
	return out
}

// mergeQuery joins the href's query onto the endpoint's, parsed and
// re-encoded rather than concatenated: the href's pairs are added
// after the endpoint's own, a key present in both keeps both values in
// order, and a fragment is dropped — the fragment names a place
// inside the document the href renders, never a place inside the
// region the island fetches. Concatenation would let a query whose
// pairs arrive percent-encoded differently smuggle a second "?" past
// the join; parsing makes the merge structural.
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
