package app

// view transitions.
//
// The transition demand module (runtime src/transition.js, loaded when
// the document declares a transition: a [data-cui-vt] cell or a
// data-cui-vt-kinds vocabulary) wraps a client navigation's DOM swap
// in document.startViewTransition({update, types}) with types
// 'forward' | 'back' | 'reload'; a document that declares none swaps
// directly, as before the layout work. What the transition LOOKS
// like is CSS on the root — component sheets refuse
// ::view-transition-* (core-ui/style/component_sheet.go), so the
// framework's blocks are returned as plain CSS, and the host collects
// every registered layout's blocks into app.css as root-level rules.
//
// The typed variant: the author configures a region's transition
// with TYPED Go values (Transition / Anim) on the layout spec; the
// framework generates the CSS (Layout.TransitionCSS) including the
// generated view-transition-name and the back-direction variants, so
// the author writes no CSS for the standard moves. The escape hatch is
// Transition{Name: "detail"}: a raw name with no anims, author CSS
// deciding the move, for the platform's full power. Root-wide presets by name stay available
// (ViewTransitionPresetCSS).
//
// prefers-reduced-motion is enforced by the RUNTIME (it never starts a
// transition under 'reduce'), so no generated rule can reintroduce
// motion for those users either.

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Side is the edge a slide enters from.
type Side int

const (
	// Left slides in from the left edge (translateX(-24px) → 0).
	Left Side = iota
	// Right slides in from the right edge (translateX(24px) → 0).
	Right
)

// Anim is one leg of a transition. The zero Anim animates nothing.
//
// seq (set by Slide on both legs) keys the leg's keyframes
// SEQUENTIALLY: the old snapshot is fully faded by the 50% mark and
// the new one holds opacity 0 until then, so two text snapshots are
// never stacked at near-full opacity mid-transition (two paragraphs
// crossed at half opacity each read as one garbled block; on a phone
// the new detail drew over the list and its search box). Plain
// Fade/SlideFrom legs keep the classic simultaneous shape.
type Anim struct {
	kind byte // 0 none, 'f' fade, 's' slide
	side Side
	dur  time.Duration
	seq  bool
}

// Fade is a plain opacity animation.
func Fade(d time.Duration) Anim { return Anim{kind: 'f', dur: d} }

// SlideFrom slides the leg in from side.
func SlideFrom(side Side, d time.Duration) Anim { return Anim{kind: 's', side: side, dur: d} }

// Transition is a placed cell's view transition (). The zero
// Transition transitions nothing. Name is the raw-name escape hatch:
// when set (and both anims zero) the framework only assigns the name
// and the author's own CSS decides what happens — the model.
type Transition struct {
	// Name overrides the generated view-transition-name. A CSS
	// custom-ident (letters, digits, '-', '_', not starting with a
	// digit, no CSS-wide keyword); NewLayout panics on a bad one.
	Name string
	// Enter animates the incoming (::view-transition-new) snapshot;
	// Exit the outgoing (::view-transition-old) one.
	Enter Anim
	Exit  Anim
	// Narrow, when set, is a CSS width ("920px") that makes the name
	// BREAKPOINT-CONDITIONAL: the placed cell carries it at
	// (width >= Narrow) and, below, the REGION the layout build marks
	// with LayoutTree.VTRegion() carries it instead. The master-detail
	// collapse: below the breakpoint list and detail are one pane, and
	// the move must read as that whole pane transitioning — a detail
	// snapshot there would morph its group geometry across the list.
	// NewLayout panics on a value that is not a plain length.
	Narrow string
}

// Slide is the canonical master-detail move: the new content enters
// from side by a short nudge while it fades in, the old fades out
// with a nudge the other way — SEQUENTIALLY: the old snapshot is
// fully gone by the midpoint and the new one only starts to show
// after it, so the two never stack at near-full opacity
// mid-transition. Snapshots draw in the top layer unclipped, so the
// offsets stay at the spacing-xl token — the move reads as direction
// without ever crossing into a neighbouring region. The generated
// CSS flips the side automatically for Back navigations (type
// 'back').
func Slide(side Side, d time.Duration) Transition {
	return Transition{
		Enter: Anim{kind: 's', side: side, dur: d, seq: true},
		Exit:  Anim{kind: 'f', dur: d, seq: true},
	}
}

// Crossfade fades both legs.
func Crossfade(d time.Duration) Transition {
	return Transition{Enter: Fade(d), Exit: Fade(d)}
}

// FadeThrough is Crossfade run SEQUENTIALLY, Slide's legs with the
// nudge taken out: the old snapshot is fully faded by the midpoint and
// the new one holds opacity 0 until then, so two snapshots of the same
// region are never legible at once. For regions whose two states carry
// text that must not ghost over itself (breadcrumbs: "Billing / BIL-31"
// and "Projects / Billing" crossed at half opacity each read as one
// trail) where the root crossfade must stay simultaneous — unchanged
// pixels behind it would blink if the ROOT went sequential.
func FadeThrough(d time.Duration) Transition {
	return Transition{Enter: Anim{kind: 'f', dur: d, seq: true}, Exit: Anim{kind: 'f', dur: d, seq: true}}
}

// isZero reports whether the transition configures nothing.
func (tr Transition) isZero() bool { return tr.Name == "" && tr.Enter.kind == 0 && tr.Exit.kind == 0 }

// vtName resolves the name the placed cell carries: the author's, or a
// generated one derived from the layout and slot (both already
// custom-ident-safe by construction).
func (tr Transition) vtName(layout, slot string) string {
	if tr.Name != "" {
		return tr.Name
	}
	return "vt-" + layout + "-" + slot
}

// cssDur renders a duration as a CSS time. Zero (unset) takes the
// default; a NEGATIVE duration is nonsense, not the strongest setting:
// clamp it to zero before the default arm (the negdur contract).
func cssDur(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	if d == 0 {
		d = 200 * time.Millisecond
	}
	return strconv.FormatFloat(float64(d)/float64(time.Millisecond), 'f', -1, 64) + "ms"
}

// animCSS emits the rules and keyframes for one transition. slot keys
// the generated names; enter/exit legs with kind 0 emit nothing.
func (tr Transition) animCSS(name string) string {
	var b strings.Builder
	if tr.Narrow == "" {
		fmt.Fprintf(&b, "[data-cui-vt=%q] { view-transition-name: %s; }\n", name, name)
	} else {
		// Narrow: the name is breakpoint-conditional — the placed cell
		// above it, the build's VTRegion element below (one pane on a
		// phone). Both rules key on the when-attribute so exactly one
		// element matches at any width; a bare [data-cui-vt] selector
		// would name both and the browser would skip the transition.
		fmt.Fprintf(&b, "@media (width >= %s) { [data-cui-vt=%q][data-cui-vt-when=\"(width >= %s)\"] { view-transition-name: %s; } }\n", tr.Narrow, name, tr.Narrow, name)
		fmt.Fprintf(&b, "@media (width < %s) { [data-cui-vt=%q][data-cui-vt-when=\"(width < %s)\"] { view-transition-name: %s; } }\n", tr.Narrow, name, tr.Narrow, name)
	}
	leg := func(a Anim, newSnap bool) {
		if a.kind == 0 {
			return
		}
		suffix := map[bool]string{true: "-in", false: "-out"}[newSnap]
		pseudo := map[bool]string{true: "::view-transition-new", false: "::view-transition-old"}[newSnap]
		if a.kind == 'f' {
			fmt.Fprintf(&b, "%s(%s) { animation: %s%s %s ease both; }\n", pseudo, name, name, suffix, cssDur(a.dur))
			if newSnap {
				if a.seq {
					// Hold 0 through the midpoint: the old snapshot is
					// fully gone before this one starts to show.
					fmt.Fprintf(&b, "@keyframes %s%s { 0%%, 50%% { opacity: 0; } }\n", name, suffix)
				} else {
					fmt.Fprintf(&b, "@keyframes %s%s { from { opacity: 0; } }\n", name, suffix)
				}
			} else {
				if a.seq {
					// Reach 0 BY the midpoint, then stay there.
					fmt.Fprintf(&b, "@keyframes %s%s { 50%%, to { opacity: 0; } }\n", name, suffix)
				} else {
					fmt.Fprintf(&b, "@keyframes %s%s { to { opacity: 0; } }\n", name, suffix)
				}
			}
			return
		}
		// Slide: a SHORT offset plus the fade, never a full-width
		// crossing. View-transition snapshots draw in the top layer
		// and are not clipped by the region's slot, so a ±100%
		// translate drags the new content across its neighbours
		// mid-transition (the master-detail pane sliding over the
		// aside). The offset is the spacing-xl token: a nudge the
		// eye reads as direction without leaving the region.
		off := map[Side]string{Left: "calc(-1 * var(--spacing-xl, 24px))", Right: "var(--spacing-xl, 24px)"}[a.side]
		if newSnap {
			fmt.Fprintf(&b, "%s(%s) { animation: %s%s %s ease both; }\n", pseudo, name, name, suffix, cssDur(a.dur))
			if a.seq {
				fmt.Fprintf(&b, "@keyframes %s%s { 0%%, 50%% { transform: translateX(%s); opacity: 0; } }\n", name, suffix, off)
			} else {
				fmt.Fprintf(&b, "@keyframes %s%s { from { transform: translateX(%s); opacity: 0; } }\n", name, suffix, off)
			}
			// Back navigation: mirrored side.
			backOff := map[Side]string{Left: "var(--spacing-xl, 24px)", Right: "calc(-1 * var(--spacing-xl, 24px))"}[a.side]
			fmt.Fprintf(&b, ":root:active-view-transition-type(back) %s(%s) { animation-name: %s%s-back; }\n", pseudo, name, name, suffix)
			if a.seq {
				fmt.Fprintf(&b, "@keyframes %s%s-back { 0%%, 50%% { transform: translateX(%s); opacity: 0; } }\n", name, suffix, backOff)
			} else {
				fmt.Fprintf(&b, "@keyframes %s%s-back { from { transform: translateX(%s); opacity: 0; } }\n", name, suffix, backOff)
			}
			return
		}
		fmt.Fprintf(&b, "%s(%s) { animation: %s%s %s ease both; }\n", pseudo, name, name, suffix, cssDur(a.dur))
		outOff := map[Side]string{Left: "var(--spacing-xl, 24px)", Right: "calc(-1 * var(--spacing-xl, 24px))"}[a.side]
		if a.seq {
			fmt.Fprintf(&b, "@keyframes %s%s { 50%%, to { transform: translateX(%s); opacity: 0; } }\n", name, suffix, outOff)
		} else {
			fmt.Fprintf(&b, "@keyframes %s%s { to { transform: translateX(%s); opacity: 0; } }\n", name, suffix, outOff)
		}
	}
	leg(tr.Enter, true)
	leg(tr.Exit, false)
	return b.String()
}

// TransitionCSS returns the root CSS for every transition this layout
// declares (spec primary, outlets, and areas). The host appends it to
// app.css for every layout App.Layouts reports, so apps never wire it
// by hand.
func (l *Layout) TransitionCSS() string {
	if l == nil || l.spec == nil {
		return ""
	}
	var b strings.Builder
	if !l.spec.Primary.Transition.isZero() {
		name := l.spec.Primary.Transition.vtName(l.Name, "primary")
		b.WriteString(l.spec.Primary.Transition.animCSS(name))
	}
	// The keyed set's entries in SORTED key order,
	// static and CSP-safe — the sheet's size follows the declared
	// options, never the records a render picks.
	for _, k := range sortedVTKeys(l.spec.Primary.Transitions) {
		tr := l.spec.Primary.Transitions[k]
		b.WriteString(tr.animCSS(tr.vtName(l.Name, keyedSlotName("primary", k))))
	}
	for _, o := range l.spec.Outlets {
		if !o.Transition.isZero() {
			name := o.Transition.vtName(l.Name, o.Name())
			b.WriteString(o.Transition.animCSS(name))
		}
		for _, k := range sortedVTKeys(o.Transitions) {
			tr := o.Transitions[k]
			b.WriteString(tr.animCSS(tr.vtName(l.Name, keyedSlotName(o.Name(), k))))
		}
	}
	for _, a := range l.spec.Areas {
		if a.Transition.isZero() {
			continue
		}
		name := a.Transition.vtName(l.Name, a.Name)
		b.WriteString(a.Transition.animCSS(name))
	}
	return b.String()
}

// ViewTransitionPresetCSS returns the root CSS of a named ROOT-WIDE
// view transition preset (kept from; orthogonal to the typed
// per-region API). Presets:
//
//   - "fade":  an explicit crossfade of the root snapshot (≈ the
//     browser default, but with a duration the author chose);
//   - "slide": the new page slides in from the right on a forward
//     navigation and from the left on Back;
//   - "none":  no animation at all (the swap still rides a transition,
//     of zero duration, so nothing else changes).
//
// An unknown name is an error, not a silent no-op.
func ViewTransitionPresetCSS(preset string) (string, error) {
	switch preset {
	case "fade":
		return `/* gofastr view-transition preset "fade" (root) */
::view-transition-old(root) { animation: cui-vt-fade-out var(--duration-fast, 150ms) ease both; }
::view-transition-new(root) { animation: cui-vt-fade-in var(--duration-fast, 150ms) ease both; }
@keyframes cui-vt-fade-out { to { opacity: 0; } }
@keyframes cui-vt-fade-in { from { opacity: 0; } }
`, nil
	case "slide":
		return `/* gofastr view-transition preset "slide" (root); forward = new page
   from the right, back = new page from the left. */
::view-transition-old(root) { animation: cui-vt-slide-out var(--duration-normal, 250ms) ease both; }
::view-transition-new(root) { animation: cui-vt-slide-in var(--duration-normal, 250ms) ease both; }
:root:active-view-transition-type(back) ::view-transition-old(root) { animation-name: cui-vt-slide-out-b; }
:root:active-view-transition-type(back) ::view-transition-new(root) { animation-name: cui-vt-slide-in-b; }
@keyframes cui-vt-slide-in { from { transform: translateX(100%); } }
@keyframes cui-vt-slide-out { to { transform: translateX(-24%); opacity: .6; } }
@keyframes cui-vt-slide-in-b { from { transform: translateX(-100%); } }
@keyframes cui-vt-slide-out-b { to { transform: translateX(24%); opacity: .6; } }
`, nil
	case "none":
		return `/* gofastr view-transition preset "none" (root) */
::view-transition-group(*), ::view-transition-image-pair(*),
::view-transition-old(*), ::view-transition-new(*) { animation: none !important; }
`, nil
	default:
		return "", fmt.Errorf("app: unknown view transition preset %q (want fade, slide, or none)", preset)
	}
}

// MustViewTransitionPresetCSS is ViewTransitionPresetCSS for
// package-level and option-call positions; an unknown name panics at
// the same stage a bad layout name would.
func MustViewTransitionPresetCSS(preset string) string {
	css, err := ViewTransitionPresetCSS(preset)
	if err != nil {
		panic(err)
	}
	return css
}
