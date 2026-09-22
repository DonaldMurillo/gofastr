package headless

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/urlsafe"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// The notification family: one toast, the stack it lives in, and the
// bell that announces an unread count. The stack is the live region —
// a toast announces by arriving in it, which is why the stack is
// mounted once per layout and the toasts carry no live attribute of
// their own — and the SSR toast is visible in it with no script at
// all. The module that binds the data-hui-toast* hooks
// (headless-feedback) owns the runtime path: toasts the response
// header injects, the TTL, the pause while focused, and the focus
// return after a dismissal.

// Toast parts.
const (
	PartToastToneWord Part = "toast-tone-word"
)

// ToastProps configures one toast.
type ToastProps struct {
	// Tone is one of info, success, warning, danger. The class map
	// colours the row from it; the tone word carries it to a reader.
	Tone string
	// Icon is the decorative glyph, hidden from readers: the tone
	// word is what tells them the kind of news. Empty renders no icon.
	Icon render.HTML
	// Title is the headline; Body is the detail. One of them is
	// required — a toast that says nothing is decoration.
	Title string
	Body  string
	// DismissHref makes the toast dismissable with a link that keeps
	// a real href, and requires a complete Island: dismissing an
	// in-page region is an in-page state change, never a route.
	DismissHref  string
	DismissLabel string
	// TTLMS is how long the module keeps the toast before it goes, on
	// the runtime path. Zero means it stays until dismissed. Negative
	// is refused.
	TTLMS int
	// Live is how the toast interrupts. The default is polite: a
	// toast is an arrival, and arrivals wait their turn; assertive is
	// for the failures a reader must hear before doing anything else.
	Live Live

	// Island is where the dismiss goes with script. Required when
	// DismissHref is set; ignored otherwise.
	Island Island

	ID         string
	ExtraAttrs html.Attrs

	// Parts: attrs and binds on every part. Strings carry the tone
	// word and the dismiss control's name.
	Parts Parts
	// Strings are the strings this component says. Nil means the English
	// defaults; a layer above sets them from the request's language.
	Strings *Strings
}

// toneWord resolves the tone's spoken word from Strings.
func toneWord(tone string, w *Strings) string {
	switch tone {
	case "success":
		return w.ToneSuccess
	case "warning":
		return w.ToneWarning
	case "danger":
		return w.ToneDanger
	default:
		return w.ToneInfo
	}
}

// Toast renders one notification row.
func Toast(p ToastProps, s Classes) render.HTML {
	if p.Title == "" && p.Body == "" {
		panic("headless: Toast requires Title or Body — a notification that says nothing is decoration pretending to be news")
	}
	switch p.Tone {
	case "", "info", "success", "warning", "danger":
	default:
		panic("headless: Toast Tone " + strconv.Quote(p.Tone) + ` is not one of: "" (info), info, success, warning, danger`)
	}
	switch p.Live {
	case LiveOff, LivePolite, LiveAssertive:
	default:
		panic("headless: Toast Live " + strconv.Quote(string(p.Live)) + ` is not one of: "" (polite), polite, assertive`)
	}
	if p.TTLMS < 0 {
		panic("headless: Toast TTLMS " + strconv.Itoa(p.TTLMS) + " is negative — a lifetime before the toast exists is no lifetime")
	}
	if p.TTLMS > math.MaxInt32 {
		// HTML's timer clamps a delay past 2^31-1 to zero: a toast
		// meant to stay forever would leave on the next task. Zero is
		// the forever.
		panic("headless: Toast TTLMS " + strconv.Itoa(p.TTLMS) + " is past the timer's range — 0 is the lifetime without end")
	}

	// The title and body are request-shaped: control bytes are
	// scrubbed, the way the table's carried query is.
	title := scrubControlBytes(p.Title)
	body := scrubControlBytes(p.Body)
	w := p.Strings.Resolve()
	b := p.Parts.Box(s)
	own := Merge(Safe(p.ExtraAttrs, "role"), Attrs(map[string]string{"id": p.ID}))
	Mark(own, "data-hui-toast")
	if cls := s.Variant(PartRoot, p.Tone); cls != "" {
		// The variant class only: El appends the part's own base class
		// to whatever class the component sets here.
		own["class"] = cls
	}
	// The toast's own live posture: the stack is the polite default,
	// and an assertive toast says so itself so the one row that must
	// interrupt can, inside a politely-announcing stack.
	if p.Live == LiveAssertive {
		own["role"] = "alert"
	}
	if p.TTLMS > 0 {
		own["data-hui-toast-ttl-ms"] = strconv.Itoa(p.TTLMS)
	}

	kids := []render.HTML{}
	if p.Tone != "" {
		// Read, not shown: the adapter hides it visually and shows the
		// icon instead, the way Alert and SystemBanner say their tone.
		kids = append(kids, b.El("span", PartToastToneWord, nil,
			render.Text(toneWord(p.Tone, w)+": ")))
	}
	if p.Icon != "" {
		kids = append(kids, b.El("span", PartIcon,
			Attrs(map[string]string{"aria-hidden": "true"}), p.Icon))
	}
	if title != "" {
		kids = append(kids, b.El("span", PartTitle, nil, render.Text(title)))
	}
	if body != "" {
		kids = append(kids, b.El("span", PartBody, nil, render.Text(body)))
	}
	if p.DismissHref != "" {
		requireIsland("Toast with DismissHref", p.Island)
		if urlsafe.CleanAnchor(p.DismissHref) == "" {
			panic("headless: Toast DismissHref " + strconv.Quote(p.DismissHref) + " is not a URL the anchor policy allows")
		}
		label := p.DismissLabel
		if label == "" {
			label = fmt.Sprintf(w.DismissTitled, orDefault(title, body))
		}
		kids = append(kids, b.El("a", PartDismiss, Merge(html.Attrs{
			"href":       p.DismissHref,
			"aria-label": label,
			// The module that owns the runtime dismissal reads the
			// hook; the island contract beside the href is the
			// in-page path.
			"data-hui-toast-dismiss": "",
		}, p.Island.attrs(p.DismissHref, "GET")), render.Text("×")))
	}
	return b.El("div", PartRoot, own, kids...)
}

// ToastStackProps configures the one stack a layout mounts.
type ToastStackProps struct {
	// Label names the stack for assistive technology. Required: the
	// stack is a landmark a reader can jump to, and an unnamed one is
	// a hole.
	Label string
	// Toasts are the rendered rows. The runtime path inserts into the
	// stack; the server-rendered rows are already home.
	Toasts []render.HTML
	// Max is how many rows the module keeps before dropping the
	// oldest. Default 4; zero takes the default; negative is refused.
	Max int

	ID         string
	ExtraAttrs html.Attrs
	Parts      Parts
	// Strings carry the dismiss label template a runtime-inserted
	// toast names itself by ("%s" where the toast's title goes).
	Strings *Strings
}

// ToastStack renders the region the toasts live in.
//
// The stack carries the framework's data-fui-toast-stack name beside
// its own: the kernel's response-header toast path and the runtime's
// auto-mount look for the framework's name, and the module that owns
// the component lifecycle looks for this package's. One region, two
// contracts, because the kernel's half is infrastructure this package
// does not own.
func ToastStack(p ToastStackProps, s Classes) render.HTML {
	if strings.TrimSpace(p.Label) == "" {
		panic("headless: ToastStack requires Label — the stack is a landmark, and an unnamed landmark is a hole")
	}
	if p.Max < 0 {
		panic("headless: ToastStack Max " + strconv.Itoa(p.Max) + " is negative — a capacity below one holds nothing")
	}
	b := p.Parts.Box(s)
	own := Merge(Safe(p.ExtraAttrs, "role", "aria-label"), Attrs(map[string]string{
		"role":                 "region",
		"aria-label":           p.Label,
		"id":                   p.ID,
		"data-fui-toast-stack": p.Label,
	}))
	Mark(own, "data-hui-toast-stack")
	if p.Max > 0 {
		own["data-hui-toast-max"] = strconv.Itoa(p.Max)
	}
	// The dismiss label the feedback module writes onto a toast it
	// builds travels here: the module says no sentence of its own.
	w := p.Strings.Resolve()
	own["data-hui-toast-dismiss-label"] = w.DismissTitled
	// The tone words too: a toast the module builds from a response
	// header says its kind to a reader the way a server-rendered one
	// does, in the page's language.
	own["data-hui-toast-tone-info"] = w.ToneInfo
	own["data-hui-toast-tone-success"] = w.ToneSuccess
	own["data-hui-toast-tone-warning"] = w.ToneWarning
	own["data-hui-toast-tone-danger"] = w.ToneDanger
	kids := append([]render.HTML{}, p.Toasts...)
	return b.El("div", PartRoot, own, kids...)
}

// ─── NotificationBell ───────────────────────────────────────────────

// NotificationBellProps configures the unread-count trigger.
type NotificationBellProps struct {
	// Href is where the link goes: the notifications page,
	// same-origin. Required — a bell that rings to nowhere is a dead
	// link on the no-script page, and "#" is not a destination.
	Href string
	// Label is the trigger's visible name. Required.
	Label string
	// UnreadCount is the count the badge shows and the accessible
	// name says. Negative is refused; zero hides the badge (nothing
	// unread is no news) unless a Bind raises it.
	UnreadCount int
	// UnreadBind, when set, keeps the count following a client
	// signal: the badge the server rendered at zero appears when the
	// signal changes it.
	UnreadBind *Bind
	// Opens, when set, is the widget name the anchor opens with
	// script: the same element is the no-script link (Href) and the
	// widget's trigger, rendered as the kernel's data-fui-open and a
	// bottom-anchored popover. A name carrying control bytes or
	// whitespace is refused — it is a widget key, not free text.
	Opens string
	// Icon is the glyph. The default bell is the structure's own; a
	// caller's SVG replaces it.
	Icon render.HTML

	ID         string
	ExtraAttrs html.Attrs

	// Parts: attrs and binds on the root, the icon, the badge and the
	// count. Strings carry the spoken count.
	Parts Parts
	// Strings are the strings this component says. Nil means the English
	// defaults; a layer above sets them from the request's language.
	Strings *Strings
}

// orDefaultHTML returns fallback when v is empty.
func orDefaultHTML(v, fallback render.HTML) render.HTML {
	if v == "" {
		return fallback
	}
	return v
}

// NotificationBell renders the trigger.
//
// The trigger is an anchor with a real destination — the no-script
// page goes to the notifications page, the script-enhanced one opens
// the popover, and the same element is both. The count the anchor's
// name says is the count the badge shows, so the news and the number
// cannot disagree.
func NotificationBell(p NotificationBellProps, s Classes) render.HTML {
	if strings.TrimSpace(p.Label) == "" {
		panic("headless: NotificationBell requires Label — a bell glyph names nothing")
	}
	if p.Href == "" {
		panic("headless: NotificationBell requires Href — the no-script page needs the notifications page, and a bell that rings to nowhere is a dead link")
	}
	if urlsafe.CleanAnchor(p.Href) == "" {
		panic("headless: NotificationBell Href " + strconv.Quote(p.Href) + " is not a URL the anchor policy allows")
	}
	if p.UnreadCount < 0 {
		panic("headless: NotificationBell UnreadCount " + strconv.Itoa(p.UnreadCount) + " is negative — unread news cannot be un-happened")
	}

	w := p.Strings.Resolve()
	b := p.Parts.Box(s, PartText)
	own := Merge(Safe(p.ExtraAttrs, "href", "aria-label"), Attrs(map[string]string{
		"href":       urlsafe.CleanAnchor(p.Href),
		"aria-label": fmt.Sprintf(w.NotificationCount, p.UnreadCount),
		"id":         p.ID,
		// The sentence shape travels so the module can re-say the
		// count when the signal changes it.
		"data-hui-notification-count-fmt": w.NotificationCount,
	}))
	Mark(own, "data-hui-notification-bell")
	if p.Opens != "" {
		if strings.ContainsAny(p.Opens, " \t\n\r\x00") {
			panic("headless: NotificationBell Opens " + strconv.Quote(p.Opens) + " is not a widget name — whitespace and control bytes are not keys")
		}
		own["data-fui-open"] = p.Opens
		own["data-fui-popover-anchor"] = "bottom"
	}

	kids := []render.HTML{
		b.El("span", PartIcon, Attrs(map[string]string{"aria-hidden": "true"}), orDefaultHTML(p.Icon, render.Text("🔔"))),
	}
	if p.UnreadCount > 0 || p.UnreadBind != nil {
		countAttrs := html.Attrs{"data-hui-notification-count": strconv.Itoa(p.UnreadCount)}
		if p.UnreadBind != nil {
			for k, v := range p.UnreadBind.attrs() {
				countAttrs[k] = v
			}
		}
		kids = append(kids, b.El("span", PartMarker, countAttrs,
			b.El("span", PartText, nil, render.Text(strconv.Itoa(p.UnreadCount)))))
	}
	return b.El("a", PartRoot, own, kids...)
}

func init() {
	Register(Spec{
		Name:    "Toast",
		Anatomy: []Part{PartRoot, PartToastToneWord, PartIcon, PartTitle, PartBody, PartDismiss},
		Hooks:   []string{"data-hui-toast", "data-hui-toast-dismiss", "data-hui-toast-ttl-ms"},
		WithParts: func(s Classes, parts Parts) render.HTML {
			return Toast(ToastProps{Title: "Saved", Parts: parts}, s)
		},
		Cases: func(k Kit) []Case {
			s := k.Classes
			return []Case{{
				Name: "a success, visible with no script",
				Why:  "the toast row is on the first paint — a notification the server rendered is news already delivered, and no module is needed to read it",
				HTML: Toast(ToastProps{Tone: "success", Icon: render.Text("✓"), Title: "Deploy finished",
					Body: "api is serving build 482"}, s),
			}, {
				Name: "assertive, with a dismiss that keeps its href",
				Why:  "the failure a reader must hear carries role=alert on its own row inside the politely-announcing stack, and the dismiss link needs its Island at render because dropping a notification is an in-page state change, never a route",
				HTML: Toast(ToastProps{Tone: "danger", Title: "Connection lost", Live: LiveAssertive,
					TTLMS: 8000, DismissHref: "/dismiss/conn",
					Island: Island{Endpoint: "/island/toasts", Signal: "toasts"}}, s),
			}}
		},
	})

	Register(Spec{
		Name:    "ToastStack",
		Anatomy: []Part{PartRoot},
		Hooks: []string{"data-hui-toast-stack", "data-hui-toast-max", "data-hui-toast-dismiss-label",
			"data-hui-toast-tone-info", "data-hui-toast-tone-success", "data-hui-toast-tone-warning", "data-hui-toast-tone-danger"},
		WithParts: func(s Classes, parts Parts) render.HTML {
			return ToastStack(ToastStackProps{Label: "Notifications", Parts: parts}, s)
		},
		Cases: func(k Kit) []Case {
			s := k.Classes
			return []Case{{
				Name: "the stack with its rows",
				Why:  "one region a layout mounts once: the runtime's header path inserts into it, the server's rows are already in it, and its name is what a reader jumps by",
				HTML: ToastStack(ToastStackProps{Label: "Notifications", Max: 4,
					Toasts: []render.HTML{
						Toast(ToastProps{Tone: "success", Title: "Saved"}, k.For("Toast")),
					}}, s),
			}, {
				Name: "empty, the wiring still there",
				Why:  "an empty stack still renders, because the module that injects runtime toasts needs the region to exist — a stack that vanishes when empty is a stack the first toast has to rebuild",
				HTML: ToastStack(ToastStackProps{Label: "Notifications"}, s),
			}}
		},
	})

	Register(Spec{
		Name:    "NotificationBell",
		Anatomy: []Part{PartRoot, PartIcon, PartMarker, PartText},
		Hooks:   []string{"data-hui-notification-bell", "data-hui-notification-count", "data-hui-notification-count-fmt"},
		WithParts: func(s Classes, parts Parts) render.HTML {
			return NotificationBell(NotificationBellProps{Href: "/notifications", Label: "Notifications", Parts: parts}, s)
		},
		Cases: func(k Kit) []Case {
			s := k.Classes
			return []Case{{
				Name: "unread",
				Why:  "the anchor's accessible name says the count in words while the badge shows the number, and the href is the notifications page — the no-script page goes there",
				HTML: NotificationBell(NotificationBellProps{Href: "/notifications",
					Label: "Notifications", UnreadCount: 3}, s),
			}, {
				Name: "a widget trigger",
				Why:  "Opens names the widget the same anchor opens with script: one element is the no-script link and the trigger, through the kernel's own data-fui-open spelling typed from a prop",
				HTML: NotificationBell(NotificationBellProps{Href: "/notifications",
					Label: "Notifications", UnreadCount: 1, Opens: "bell-panel"}, s),
			}, {
				Name: "nothing unread, the count live",
				Why:  "zero renders no badge (no news is no news) and the signal-bound count raises it the moment news arrives, without a re-render",
				HTML: NotificationBell(NotificationBellProps{Href: "/notifications",
					Label: "Notifications", UnreadBind: &Bind{Signal: "unread"}}, s),
			}}
		},
	})
}
