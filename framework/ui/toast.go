package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/interactive"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/widget/preset"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
	"github.com/DonaldMurillo/gofastr/framework/headless"
)

// ToastTrigger is one toast, as the X-Gofastr-Toast header carries it
// and as window.__gofastr.toast(cfg) takes it on the client.
type ToastTrigger struct {
	Variant StatusVariant `json:"variant,omitempty"` // info | success | warning | danger | neutral
	Title   string        `json:"title"`             // required
	Body    string        `json:"body,omitempty"`
	TTL     int           `json:"ttl,omitempty"` // milliseconds; 0 = persistent
	// Stack names the toast stack to push into; empty is the page's first.
	Stack string `json:"stack,omitempty"`
	// Action is the toast's one RPC button; it keeps the toast up at least ten seconds.
	Action *interactive.ToastAction `json:"action,omitempty"`
}

func toastPoisoned(t ToastTrigger) bool {
	if textsafe.HasControlBytes(t.Title) || textsafe.HasControlBytes(t.Body) || textsafe.HasControlBytes(t.Stack) {
		return true
	}
	if t.Action == nil {
		return false
	}
	if textsafe.HasControlBytes(t.Action.Label) {
		return true
	}
	for k, v := range t.Action.Attrs {
		if textsafe.HasControlBytes(k) || textsafe.HasControlBytes(v) {
			return true
		}
	}
	return false
}

// AddToast appends t to the response's X-Gofastr-Toast JSON array, shown
// when the data-cui-rpc fetch resolves with a 2xx. A toast with no
// Title, or with a control byte in any of its text, is dropped.
func AddToast(w http.ResponseWriter, t ToastTrigger) {
	if t.Title == "" {
		return
	}
	if toastPoisoned(t) {
		return
	}
	if t.Variant == "" {
		t.Variant = StatusInfo
	}
	existing := w.Header().Get("X-Gofastr-Toast")
	var list []ToastTrigger
	if existing != "" {
		if existing[0] == '[' {
			if err := json.Unmarshal([]byte(existing), &list); err != nil {
				list = nil
			}
		} else {
			var single ToastTrigger
			if json.Unmarshal([]byte(existing), &single) == nil {
				list = append(list, single)
			}
		}
		kept := list[:0]
		for _, e := range list {
			if !toastPoisoned(e) {
				kept = append(kept, e)
			}
		}
		list = kept
	}
	list = append(list, t)
	enc, err := json.Marshal(list)
	if err != nil {
		return
	}
	w.Header().Set("X-Gofastr-Toast", asciiJSON(enc))
}

func asciiJSON(enc []byte) string {
	var b strings.Builder
	b.Grow(len(enc))
	for _, r := range string(enc) {
		switch {
		case r < utf8.RuneSelf:
			b.WriteRune(r)
		case r > 0xFFFF:
			r1, r2 := utf16.EncodeRune(r)
			fmt.Fprintf(&b, `\u%04x\u%04x`, r1, r2)
		default:
			fmt.Fprintf(&b, `\u%04x`, r)
		}
	}
	return b.String()
}

// AddToastSuccess adds a success toast; a ttlMs of 0 keeps it up until dismissed.
func AddToastSuccess(w http.ResponseWriter, title, body string, ttlMs int) {
	AddToast(w, ToastTrigger{Variant: StatusSuccess, Title: title, Body: body, TTL: ttlMs})
}

// AddToastError adds a danger toast that stays up until dismissed.
func AddToastError(w http.ResponseWriter, title, body string) {
	AddToast(w, ToastTrigger{Variant: StatusDanger, Title: title, Body: body, TTL: 0})
}

// AddToastWarning adds a warning toast; a ttlMs of 0 keeps it up until dismissed.
func AddToastWarning(w http.ResponseWriter, title, body string, ttlMs int) {
	AddToast(w, ToastTrigger{Variant: StatusWarning, Title: title, Body: body, TTL: ttlMs})
}

var toastTemplateClasses = func() headless.Classes {
	c := maps.Clone(notificationClasses)
	c[headless.PartToastItem] = "fui-toast-stack__item"
	return c
}()

func toastTemplate(ctx context.Context) render.HTML {
	glyphs := map[string]string{
		"info":    notificationGlyph(StatusInfo),
		"success": notificationGlyph(StatusSuccess),
		"warning": notificationGlyph(StatusWarning),
		"danger":  notificationGlyph(StatusDanger),
	}
	variants := append([]string{string(StatusNeutral)}, registeredStatusNames()...)
	classes := maps.Clone(toastTemplateClasses)
	for _, v := range variants {
		glyphs[v] = notificationGlyph(StatusVariant(v))
		classes[headless.Part("root--"+v)] = "fui-notification--" + v
	}
	return headless.ToastTemplate(headless.ToastTemplateProps{
		Glyphs:    glyphs,
		Variants:  variants,
		StyleName: notificationStyle.Name(),
		Strings:   StringsFor(ctx),
	}, classes)
}

func registeredStatusNames() []string {
	statusMods.mu.RLock()
	defer statusMods.mu.RUnlock()
	return slices.Sorted(maps.Keys(statusMods.status))
}

var _ = registry.RegisterTemplate(preset.ToastTemplate, toastTemplate)

type toastSlot struct{ name string }

func (t toastSlot) Render() render.HTML {
	return preset.ToastSlotHTML(context.Background(), t.name)
}

func (t toastSlot) RenderCtx(ctx context.Context) render.HTML {
	return preset.ToastSlotHTML(ctx, t.name)
}

var (
	_ component.Component        = toastSlot{}
	_ component.ContextComponent = toastSlot{}
)

// ToastSlot returns the empty toast stack named name, for a host that
// composes preset.ToastStack(name, ToastSlot(name)) itself.
func ToastSlot(name string) component.Component { return toastSlot{name: name} }
