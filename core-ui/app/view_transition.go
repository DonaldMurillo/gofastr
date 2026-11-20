package app

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
)

// Side is the edge a slide enters from.
type Side int

const (
	// Left slides in from the left edge (translateX(-24px) → 0).
	Left Side = iota
	// Right slides in from the right edge (translateX(24px) → 0).
	Right
)

// Anim is one leg of a transition.
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

// Transition is a placed cell's view transition ().
type Transition struct {
	// Name overrides the generated view-transition-name.
	Name string
	// Enter animates the incoming (::view-transition-new) snapshot; Exit the outgoing
	// (::view-transition-old) one.
	Enter Anim
	Exit  Anim
	// Narrow, when set, is a CSS width ("920px") that makes the name BREAKPOINT-CONDITIONAL: the placed
	// cell carries it at (width >= Narrow) and, below, the REGION the layout build marks with
	// LayoutTree.VTRegion() carries it instead.
	Narrow  string
	instant bool
}

// Slide moves the new content in from side as the old fades out, one after the other.
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

// FadeThrough is Crossfade run one leg after the other.
func FadeThrough(d time.Duration) Transition {
	return Transition{Enter: Anim{kind: 'f', dur: d, seq: true}, Exit: Anim{kind: 'f', dur: d, seq: true}}
}

// Instant swaps the region in one frame while the rest of the page transitions around it: no fade,
// no group morph, the old snapshot hidden from the start.
func Instant() Transition { return Transition{instant: true} }

func (tr Transition) isZero() bool {
	return tr.Name == "" && !tr.instant && tr.Enter.kind == 0 && tr.Exit.kind == 0
}

func (tr Transition) vtName(layout, slot string) string {
	if tr.Name != "" {
		return tr.Name
	}
	return "vt-" + layout + "-" + slot
}

func cssDur(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	if d == 0 {
		d = 200 * time.Millisecond
	}
	return strconv.FormatFloat(float64(d)/float64(time.Millisecond), 'f', -1, 64) + "ms"
}

func (tr Transition) animCSS(name string) string {
	var b strings.Builder
	if tr.Narrow == "" {
		fmt.Fprintf(&b, "[data-cui-vt=%q] { view-transition-name: %s; }\n", name, name)
	} else {
		fmt.Fprintf(&b, "@media (width >= %s) { [data-cui-vt=%q][data-cui-vt-when=\"(width >= %s)\"] { view-transition-name: %s; } }\n", tr.Narrow, name, tr.Narrow, name)
		fmt.Fprintf(&b, "@media (width < %s) { [data-cui-vt=%q][data-cui-vt-when=\"(width < %s)\"] { view-transition-name: %s; } }\n", tr.Narrow, name, tr.Narrow, name)
	}
	if tr.instant {
		fmt.Fprintf(&b, "::view-transition-group(%s), ::view-transition-new(%s) { animation: none; }\n", name, name)
		fmt.Fprintf(&b, "::view-transition-old(%s) { animation: none; opacity: 0; }\n", name)
		fmt.Fprintf(&b, "[data-cui-vt=%q][data-cui-vt][aria-busy=\"true\"] { opacity: 1; transition: none; }\n", name)
		return b.String()
	}
	leg := func(a Anim, newSnap bool) {
		if a.kind == 0 {
			return
		}
		suffix := map[bool]string{true: "-in", false: "-out"}[newSnap]
		pseudo := map[bool]string{true: "::view-transition-new", false: "::view-transition-old"}[newSnap]
		if a.kind == 'f' {
			fmt.Fprintf(&b, "%s(%s) { animation: %s%s %s var(--easing-ease-in-out, ease) both; }\n", pseudo, name, name, suffix, cssDur(a.dur))
			if newSnap {
				if a.seq {
					fmt.Fprintf(&b, "@keyframes %s%s { 0%%, 50%% { opacity: 0; } }\n", name, suffix)
				} else {
					fmt.Fprintf(&b, "@keyframes %s%s { from { opacity: 0; } }\n", name, suffix)
				}
			} else {
				if a.seq {
					fmt.Fprintf(&b, "@keyframes %s%s { 50%%, to { opacity: 0; } }\n", name, suffix)
				} else {
					fmt.Fprintf(&b, "@keyframes %s%s { to { opacity: 0; } }\n", name, suffix)
				}
			}
			return
		}
		off := map[Side]string{Left: "calc(-1 * var(--spacing-xl, 24px))", Right: "var(--spacing-xl, 24px)"}[a.side]
		if newSnap {
			fmt.Fprintf(&b, "%s(%s) { animation: %s%s %s var(--easing-ease-in-out, ease) both; }\n", pseudo, name, name, suffix, cssDur(a.dur))
			if a.seq {
				fmt.Fprintf(&b, "@keyframes %s%s { 0%%, 50%% { transform: translateX(%s); opacity: 0; } }\n", name, suffix, off)
			} else {
				fmt.Fprintf(&b, "@keyframes %s%s { from { transform: translateX(%s); opacity: 0; } }\n", name, suffix, off)
			}
			backOff := map[Side]string{Left: "var(--spacing-xl, 24px)", Right: "calc(-1 * var(--spacing-xl, 24px))"}[a.side]
			fmt.Fprintf(&b, ":root:active-view-transition-type(back) %s(%s) { animation-name: %s%s-back; }\n", pseudo, name, name, suffix)
			if a.seq {
				fmt.Fprintf(&b, "@keyframes %s%s-back { 0%%, 50%% { transform: translateX(%s); opacity: 0; } }\n", name, suffix, backOff)
			} else {
				fmt.Fprintf(&b, "@keyframes %s%s-back { from { transform: translateX(%s); opacity: 0; } }\n", name, suffix, backOff)
			}
			return
		}
		fmt.Fprintf(&b, "%s(%s) { animation: %s%s %s var(--easing-ease-in-out, ease) both; }\n", pseudo, name, name, suffix, cssDur(a.dur))
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

// TransitionCSS returns the root CSS for every transition this layout declares (spec primary,
// outlets, and areas).
func (l *Layout) TransitionCSS() string {
	if l == nil || l.spec == nil {
		return ""
	}
	var b strings.Builder
	if !l.spec.Primary.Transition.isZero() {
		name := l.spec.Primary.Transition.vtName(l.Name, "primary")
		b.WriteString(l.spec.Primary.Transition.animCSS(name))
	}
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

// ViewTransitionPresetCSS returns the root CSS of a named ROOT-WIDE view transition preset (kept
// from; orthogonal to the typed per-region API).
func ViewTransitionPresetCSS(preset string) (string, error) {
	switch preset {
	case "fade":
		return `/* gofastr view-transition preset "fade" (root) */
::view-transition-old(root) { animation: cui-vt-fade-out var(--duration-fast, 150ms) var(--easing-ease-in-out, ease) both; }
::view-transition-new(root) { animation: cui-vt-fade-in var(--duration-fast, 150ms) var(--easing-ease-in-out, ease) both; }
@keyframes cui-vt-fade-out { to { opacity: 0; } }
@keyframes cui-vt-fade-in { from { opacity: 0; } }
`, nil
	case "slide":
		return `/* gofastr view-transition preset "slide" (root); forward = new page
   from the right, back = new page from the left. */
::view-transition-old(root) { animation: cui-vt-slide-out var(--duration-normal, 250ms) var(--easing-ease-in-out, ease) both; }
::view-transition-new(root) { animation: cui-vt-slide-in var(--duration-normal, 250ms) var(--easing-ease-in-out, ease) both; }
:root:active-view-transition-type(back) ::view-transition-old(root) { animation-name: cui-vt-slide-out-b; }
:root:active-view-transition-type(back) ::view-transition-new(root) { animation-name: cui-vt-slide-in-b; }
@keyframes cui-vt-slide-in { from { transform: translateX(100%); } }
@keyframes cui-vt-slide-out { to { transform: translateX(-24%); opacity: var(--opacity-muted, 0.6); } }
@keyframes cui-vt-slide-in-b { from { transform: translateX(-100%); } }
@keyframes cui-vt-slide-out-b { to { transform: translateX(24%); opacity: var(--opacity-muted, 0.6); } }
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

// TransitionSheet names the component sheet holding the layout's TransitionCSS, the data-cui-comp
// marker its layer root carries; empty when the layout declares no transition.
func (l *Layout) TransitionSheet() string {
	if l == nil {
		return ""
	}
	return l.vtSheet
}

func registerTransitionSheet(css string) string {
	if css == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(css))
	name := "cui-vt-" + hex.EncodeToString(sum[:8])
	registry.RegisterRuntimeStyle(name, func(style.Theme) string { return css })
	return name
}
