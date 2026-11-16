package headless

import (
	"encoding/json"
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/urlsafe"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// Toast parts.
const (
	PartToastToneWord Part = "toast-tone-word"
	// PartToastItem is the stack row wrapping one runtime-built toast; only ToastTemplate draws it.
	PartToastItem Part = "toast-item"
	// PartToastAction is a runtime toast's action button; only ToastTemplate draws it.
	PartToastAction Part = "toast-action"
)

// ToastProps configures one toast.
type ToastProps struct {
	// Tone is info (the default), success, warning or danger; the tone word says it to a reader.
	Tone string
	// Icon is a decorative glyph, hidden from readers.
	Icon render.HTML
	// Title is the headline and Body the detail; one of them is required.
	Title string
	Body  string
	// DismissHref makes the toast dismissable through a link with a real href; it requires Island.
	DismissHref  string
	DismissLabel string
	// TTLMS is the runtime lifetime in milliseconds; zero stays until dismissed.
	TTLMS int
	// Live is polite by default; LiveAssertive renders role=alert.
	Live Live

	// Island is where the dismiss goes with script.
	Island Island

	ID         string
	ExtraAttrs html.Attrs

	Parts   Parts
	Strings *Strings
}

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
		panic("headless: Toast TTLMS " + strconv.Itoa(p.TTLMS) + " is past the timer's range — 0 is the lifetime without end")
	}

	title := scrubControlBytes(p.Title)
	body := scrubControlBytes(p.Body)
	w := p.Strings.Resolve()
	b := p.Parts.Box(s)
	own := Merge(Safe(p.ExtraAttrs, "role"), Attrs(map[string]string{"id": p.ID}))
	Mark(own, "data-hui-toast")
	if cls := s.Variant(PartRoot, p.Tone); cls != "" {
		own["class"] = cls
	}
	own["role"] = "status"
	if p.Live == LiveAssertive {
		own["role"] = "alert"
	}
	if p.TTLMS > 0 {
		own["data-hui-toast-ttl-ms"] = strconv.Itoa(p.TTLMS)
	}

	kids := []render.HTML{}
	if p.Tone != "" {
		kids = append(kids, b.El("span", PartToastToneWord, Internal(nil),
			render.Text(toneWord(p.Tone, w)+": ")))
	}
	if p.Icon != "" {
		kids = append(kids, b.El("span", PartIcon,
			internalIf(ownedSlot(p.Icon), Attrs(map[string]string{"aria-hidden": "true"})), p.Icon))
	}
	if title != "" {
		kids = append(kids, b.El("span", PartTitle, Internal(nil), render.Text(title)))
	}
	if body != "" {
		kids = append(kids, b.El("span", PartBody, Internal(nil), render.Text(body)))
	}
	if p.DismissHref != "" {
		requireIsland("Toast with DismissHref", p.Island)
		if urlsafe.CleanAnchor(p.DismissHref) == "" {
			panic("headless: Toast DismissHref " + strconv.Quote(p.DismissHref) + " is not a URL the anchor policy allows")
		}
		label := p.DismissLabel
		if label == "" {
			label = w.fill(w.DismissTitled, "title", orDefault(title, body))
		}
		kids = append(kids, b.El("a", PartDismiss, Internal(Merge(html.Attrs{
			"href":                   p.DismissHref,
			"aria-label":             label,
			"data-hui-toast-dismiss": "",
		}, p.Island.attrs(p.DismissHref, "GET"))), render.Text("×")))
	}
	return b.El("div", PartRoot, own, kids...)
}

// ToastTemplateProps configures the inert <template> the headless-feedback
// module clones for the toasts it builds at runtime.
type ToastTemplateProps struct {
	// Glyphs are the decorative icons per tone and per Variants name.
	Glyphs map[string]string
	// Variants are variant names beyond the four tones; each matches [a-z0-9-] and is not a tone.
	Variants []string
	// StyleName is the registered style the cloned row loads, rendered as data-cui-comp.
	StyleName string

	Parts   Parts
	Strings *Strings
}

type toastVariant struct {
	Class string `json:"class"`
	Glyph string `json:"glyph"`
}

var toastVariantRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

var styleNameRe = regexp.MustCompile(`^[a-zA-Z0-9_:.-]+$`)

// ToastTemplate renders the row template, every part the module fills present and empty.
func ToastTemplate(p ToastTemplateProps, s Classes) render.HTML {
	w := p.Strings.Resolve()
	b := p.Parts.Box(s)
	rootOwn := Mark(html.Attrs{}, "data-hui-toast")
	if p.StyleName != "" {
		if !styleNameRe.MatchString(p.StyleName) {
			panic("headless: ToastTemplate StyleName " + strconv.Quote(p.StyleName) + " is not a sheet name")
		}
		rootOwn["data-cui-comp"] = p.StyleName
	}
	row := b.El("div", PartRoot, rootOwn,
		b.El("span", PartToastToneWord, Mark(Internal(nil), "data-hui-toast-tone")),
		b.El("span", PartIcon, Mark(Internal(Attrs(map[string]string{"aria-hidden": "true"})), "data-hui-toast-icon")),
		b.El("span", PartTitle, Mark(Internal(nil), "data-hui-toast-title")),
		b.El("span", PartBody, Mark(Internal(nil), "data-hui-toast-body")),
		b.El("button", PartToastAction, Mark(Internal(Attrs(map[string]string{"type": "button"})), "data-hui-toast-action", "hidden")),
		b.El("button", PartDismiss, Mark(Internal(Attrs(map[string]string{"type": "button"})), "data-hui-toast-dismiss"),
			render.Text("×")),
	)
	item := b.El("div", PartToastItem, Mark(Internal(nil), "data-hui-toast-item"), row)
	attrs := html.Attrs{
		"data-hui-toast-template":        "",
		"data-hui-toast-glyph-info":      p.Glyphs["info"],
		"data-hui-toast-glyph-success":   p.Glyphs["success"],
		"data-hui-toast-glyph-warning":   p.Glyphs["warning"],
		"data-hui-toast-glyph-danger":    p.Glyphs["danger"],
		"data-hui-toast-variant-info":    s.Variant(PartRoot, "info"),
		"data-hui-toast-variant-success": s.Variant(PartRoot, "success"),
		"data-hui-toast-variant-warning": s.Variant(PartRoot, "warning"),
		"data-hui-toast-variant-danger":  s.Variant(PartRoot, "danger"),
		"data-hui-toast-dismiss-label":   w.DismissTitled,
		"data-hui-toast-tone-info":       w.ToneInfo,
		"data-hui-toast-tone-success":    w.ToneSuccess,
		"data-hui-toast-tone-warning":    w.ToneWarning,
		"data-hui-toast-tone-danger":     w.ToneDanger,
	}
	if len(p.Variants) > 0 {
		extra := map[string]toastVariant{}
		for _, v := range p.Variants {
			if !toastVariantRe.MatchString(v) {
				panic("headless: ToastTemplate Variants " + strconv.Quote(v) + " is not a variant name ([a-z0-9-])")
			}
			switch v {
			case "info", "success", "warning", "danger":
				panic("headless: ToastTemplate Variants " + strconv.Quote(v) + " is a tone: the template lists the tones already")
			}
			extra[v] = toastVariant{Class: s.Variant(PartRoot, v), Glyph: p.Glyphs[v]}
		}
		if b, err := json.Marshal(extra); err == nil {
			attrs["data-hui-toast-variants"] = string(b)
		}
	}
	return render.Tag("template", attrs, item)
}

// ToastStackProps configures the one stack a layout mounts.
type ToastStackProps struct {
	// Label names the stack region. Required.
	Label string
	// Toasts are the server-rendered rows.
	Toasts []render.HTML
	// Max is how many rows the module keeps (default 4); negative is refused.
	Max int

	ID         string
	ExtraAttrs html.Attrs
	Parts      Parts
	Strings    *Strings
}

// ToastStack renders the region the toasts live in, named for both the kernel and the module.
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
		"data-cui-toast-stack": p.Label,
	}))
	Mark(own, "data-hui-toast-stack")
	if p.Max > 0 {
		own["data-hui-toast-max"] = strconv.Itoa(p.Max)
	}
	w := p.Strings.Resolve()
	own["data-hui-toast-dismiss-label"] = w.DismissTitled
	own["data-hui-toast-tone-info"] = w.ToneInfo
	own["data-hui-toast-tone-success"] = w.ToneSuccess
	own["data-hui-toast-tone-warning"] = w.ToneWarning
	own["data-hui-toast-tone-danger"] = w.ToneDanger
	kids := append([]render.HTML{}, p.Toasts...)
	return b.El("div", PartRoot, own, kids...)
}

// NotificationBellProps configures the unread-count trigger.
type NotificationBellProps struct {
	// Href is the notifications page the link goes to without script. Required.
	Href string
	// Label is the trigger's accessible name, followed by its unread count. Required.
	Label string
	// UnreadCount is the badge's count; negative is refused, and zero hides the
	// badge unless UnreadBind is set.
	UnreadCount int
	// UnreadBind keeps the count following a client signal.
	UnreadBind *Bind
	// Opens is the widget the anchor opens with script; whitespace and control bytes are refused.
	Opens string
	// Icon replaces the default bell glyph.
	Icon render.HTML

	ID         string
	ExtraAttrs html.Attrs

	Parts   Parts
	Strings *Strings
}

func orDefaultHTML(v, fallback render.HTML) render.HTML {
	if v == "" {
		return fallback
	}
	return v
}

// NotificationBell renders the trigger: an anchor whose name says the count its badge shows.
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
		"href":                            urlsafe.CleanAnchor(p.Href),
		"aria-label":                      p.Label + ": " + w.fill(w.NotificationCount, "n", p.UnreadCount),
		"id":                              p.ID,
		"data-hui-notification-count-fmt": w.NotificationCount,
		"data-hui-notification-label":     p.Label,
	}))
	Mark(own, "data-hui-notification-bell")
	if p.Opens != "" {
		if strings.ContainsAny(p.Opens, " \t\n\r\x00") {
			panic("headless: NotificationBell Opens " + strconv.Quote(p.Opens) + " is not a widget name — whitespace and control bytes are not keys")
		}
		own["data-cui-open"] = p.Opens
		own["data-cui-popover-anchor"] = "bottom"
	}

	iconOwn, markerOwn := Internal(nil), html.Attrs(nil)
	if p.Icon != "" && !ownedSlot(p.Icon) {
		iconOwn, markerOwn = nil, Internal(nil)
	}
	iconKids := []render.HTML{orDefaultHTML(p.Icon, render.Text("🔔"))}
	if p.UnreadCount > 0 || p.UnreadBind != nil {
		countAttrs := Merge(html.Attrs{"data-hui-notification-count": strconv.Itoa(p.UnreadCount)}, markerOwn)
		if p.UnreadBind != nil {
			for k, v := range p.UnreadBind.attrs() {
				countAttrs[k] = v
			}
		}
		iconKids = append(iconKids,
			b.El("span", PartMarker, countAttrs,
				b.El("span", PartText, nil, render.Text(w.Number(p.UnreadCount)))))
	}
	kids := []render.HTML{
		b.El("span", PartIcon, Merge(Attrs(map[string]string{"aria-hidden": "true"}), iconOwn), iconKids...),
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
		Name:    "ToastTemplate",
		Anatomy: []Part{PartToastItem, PartRoot, PartToastToneWord, PartIcon, PartTitle, PartBody, PartToastAction, PartDismiss},
		Hooks: []string{"data-hui-toast-template", "data-hui-toast-item", "data-hui-toast",
			"data-hui-toast-tone", "data-hui-toast-icon", "data-hui-toast-title", "data-hui-toast-body", "data-hui-toast-action", "data-hui-toast-dismiss",
			"data-hui-toast-glyph-info", "data-hui-toast-glyph-success", "data-hui-toast-glyph-warning", "data-hui-toast-glyph-danger",
			"data-hui-toast-variant-info", "data-hui-toast-variant-success", "data-hui-toast-variant-warning", "data-hui-toast-variant-danger",
			"data-hui-toast-dismiss-label", "data-hui-toast-variants",
			"data-hui-toast-tone-info", "data-hui-toast-tone-success", "data-hui-toast-tone-warning", "data-hui-toast-tone-danger"},
		WithParts: func(s Classes, parts Parts) render.HTML {
			return ToastTemplate(ToastTemplateProps{Parts: parts}, s)
		},
		Cases: func(k Kit) []Case {
			s := k.Classes
			return []Case{{
				Name: "the row the module clones",
				Why:  "an inert template inside the stack carries every class, glyph and word a runtime toast wears, so the module that builds a row from a response header names hooks only and the kit keeps its skin to itself",
				HTML: ToastTemplate(ToastTemplateProps{
					Glyphs: map[string]string{"info": "i", "success": "✓", "warning": "!", "danger": "✕"}}, s),
			}, {
				Name: "a variant beyond the tones",
				Why:  "neutral and an app's registered status variants are not tones: the template lists each with its class and glyph so a runtime row of that variant wears them and says no tone word instead of passing for info",
				HTML: ToastTemplate(ToastTemplateProps{Variants: []string{"neutral"},
					Glyphs: map[string]string{"neutral": "•"}}, s),
			}, {
				Name: "no glyphs",
				Why:  "a kit without icons leaves the glyph attributes empty and the module drops the icon part at clone time; the tone word still tells a reader the kind of news",
				HTML: ToastTemplate(ToastTemplateProps{}, s),
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
		Hooks:   []string{"data-hui-notification-bell", "data-hui-notification-count", "data-hui-notification-count-fmt", "data-hui-notification-label"},
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
				Why:  "Opens names the widget the same anchor opens with script: one element is the no-script link and the trigger, through the kernel's own data-cui-open spelling typed from a prop",
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
