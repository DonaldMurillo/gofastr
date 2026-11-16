package ui

import (
	"context"
	"encoding/json"
	stdhtml "html"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

func bannerSystemID(t *testing.T, h render.HTML) string {
	t.Helper()
	s := string(h)
	const key = `data-hui-system-id="`
	i := strings.Index(s, key)
	if i < 0 {
		t.Fatalf("banner carries no data-hui-system-id:\n%s", s)
	}
	s = s[i+len(key):]
	return s[:strings.IndexByte(s, '"')]
}

func TestSweepFeedbackDismissCookieMatchesTheModule(t *testing.T) {
	js, err := os.ReadFile("../headless/behavior.js")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(js), `'`+bannerDismissCookiePrefix+`' + encodeURIComponent(id)`) {
		t.Fatalf("framework/headless/behavior.js no longer writes the %q cookie Banner reads", bannerDismissCookiePrefix)
	}
}

func TestSweepFeedbackBannerIdentityIsStable(t *testing.T) {
	base := BannerConfig{Title: "Maintenance", Body: "Tonight at nine"}
	first := bannerSystemID(t, Banner(base))
	if again := bannerSystemID(t, Banner(base)); again != first {
		t.Fatalf("the same banner rendered twice has two identities: %q, %q", first, again)
	}
	for name, other := range map[string]BannerConfig{
		"title":   {Title: "Outage", Body: base.Body},
		"body":    {Title: base.Title, Body: "Tomorrow"},
		"variant": {Title: base.Title, Body: base.Body, Variant: BannerWarn},
	} {
		if got := bannerSystemID(t, Banner(other)); got == first {
			t.Errorf("a banner differing in %s shares the identity %q", name, got)
		}
	}
	if a, b := bannerSystemID(t, Banner(BannerConfig{Title: "a:b", Body: "c"})), bannerSystemID(t, Banner(BannerConfig{Title: "a", Body: "b:c"})); a == b {
		t.Errorf("moving text between Title and Body kept the identity %q", a)
	}
	if got := bannerSystemID(t, Banner(BannerConfig{Title: "x", ID: "b1"})); got != "b1" {
		t.Errorf("ID did not name the banner: %q", got)
	}
	if got := bannerSystemID(t, Banner(BannerConfig{Title: "x", ID: "b1", DismissID: "d1"})); got != "d1" {
		t.Errorf("DismissID did not win over ID: %q", got)
	}
}

func TestSweepFeedbackNotificationIDKeepsItsClasses(t *testing.T) {
	h := string(Notification(NotificationConfig{Title: "x", ID: "n1", Class: "mine", Position: NotificationTopRight}))
	root := h[:strings.IndexByte(h, '>')+1]
	for _, want := range []string{`id="n1"`, "mine", "fui-notification--floating", "fui-notification--at-top-right"} {
		if !strings.Contains(root, want) {
			t.Errorf("a Notification with an ID lost %q:\n%s", want, root)
		}
	}
}

func TestSweepFeedbackNotificationRegisteredVariantKeepsTheInfoTone(t *testing.T) {
	h := string(Notification(NotificationConfig{Title: "Beta", Variant: testBetaStatus}))
	root := h[:strings.IndexByte(h, '>')+1]
	for _, want := range []string{"fui-notification--info", "fui-notification--beta", `role="status"`} {
		if !strings.Contains(root, want) {
			t.Errorf("a registered-variant Notification lacks %q:\n%s", want, root)
		}
	}
	if !strings.Contains(h, "Information: ") || !strings.Contains(h, "β") {
		t.Errorf("a registered-variant Notification lost the info tone word or its own glyph:\n%s", h)
	}
}

func TestSweepFeedbackNotificationWarningInterrupts(t *testing.T) {
	for v, role := range map[StatusVariant]string{
		StatusWarning: "alert", StatusDanger: "alert",
		StatusInfo: "status", StatusSuccess: "status", StatusNeutral: "status",
	} {
		h := string(Notification(NotificationConfig{Title: "x", Variant: v}))
		if !strings.Contains(h, `role="`+role+`"`) {
			t.Errorf("a %s Notification is not role=%s:\n%s", v, role, h)
		}
	}
}

func TestSweepFeedbackToastTriggerFieldsMatchTheModule(t *testing.T) {
	js, err := os.ReadFile("../headless/feedback.js")
	if err != nil {
		t.Fatal(err)
	}
	typ := reflect.TypeFor[ToastTrigger]()
	for i := range typ.NumField() {
		name, _, _ := strings.Cut(typ.Field(i).Tag.Get("json"), ",")
		if !strings.Contains(string(js), "cfg."+name) {
			t.Errorf("ToastTrigger.%s travels as %q, which framework/headless/feedback.js never reads", typ.Field(i).Name, name)
		}
	}
}

func toastHeader(t *testing.T, existing string, add ToastTrigger) []ToastTrigger {
	t.Helper()
	rec := httptest.NewRecorder()
	rec.Header().Set("X-Gofastr-Toast", existing)
	AddToast(rec, add)
	var got []ToastTrigger
	if err := json.Unmarshal([]byte(rec.Header().Get("X-Gofastr-Toast")), &got); err != nil {
		t.Fatalf("header is not a JSON array: %v", err)
	}
	return got
}

func TestSweepFeedbackAddToastKeepsAManualObject(t *testing.T) {
	got := toastHeader(t, `{"title":"manual"}`, ToastTrigger{Title: "mine"})
	if len(got) != 2 || got[0].Title != "manual" || got[1].Title != "mine" {
		t.Fatalf("a single-object header was not kept before the new toast: %+v", got)
	}
}

func TestSweepFeedbackAddToastDropsAMalformedArray(t *testing.T) {
	got := toastHeader(t, `[{"title":"half"},{"title":5}]`, ToastTrigger{Title: "mine"})
	if len(got) != 1 || got[0].Title != "mine" {
		t.Fatalf("a malformed array header was not dropped whole: %+v", got)
	}
}

func TestSweepFeedbackToastTemplateCarriesEveryVariant(t *testing.T) {
	tpl := string(toastTemplate(context.Background()))
	const key = `data-hui-toast-variants="`
	i := strings.Index(tpl, key)
	if i < 0 {
		t.Fatalf("toast template carries no variants:\n%s", tpl)
	}
	raw := tpl[i+len(key):]
	raw = stdhtml.UnescapeString(raw[:strings.IndexByte(raw, '"')])
	var variants map[string]struct{ Class, Glyph string }
	if err := json.Unmarshal([]byte(raw), &variants); err != nil {
		t.Fatalf("variants are not JSON: %v\n%s", err, raw)
	}
	for name, want := range map[string][2]string{
		"neutral": {"fui-notification--neutral", "•"},
		"beta":    {"fui-notification--beta", "β"},
	} {
		if got := variants[name]; got.Class != want[0] || got.Glyph != want[1] {
			t.Errorf("toast template variant %s = %+v, want class %q glyph %q", name, got, want[0], want[1])
		}
	}
}

func TestSweepFeedbackTooltipSkipsLeadingComments(t *testing.T) {
	h := string(Tooltip(TooltipConfig{Text: "x", Ctx: render.WithIDScope(context.Background())}, render.HTML(` <!-- a > b --><button>Hi</button>`)))
	if !strings.Contains(h, `<!-- a > b --><button aria-describedby="tip-x">`) {
		t.Fatalf("the splice did not skip the leading comment:\n%s", h)
	}
}

func TestSweepFeedbackTooltipSplicesBeforeASelfClose(t *testing.T) {
	h := string(Tooltip(TooltipConfig{Text: "x", Ctx: render.WithIDScope(context.Background())}, render.HTML(`<input type="text"/>`)))
	if !strings.Contains(h, `<input type="text" aria-describedby="tip-x"/>`) {
		t.Fatalf("the splice landed after the self-closing slash:\n%s", h)
	}
}

func TestSweepFeedbackPollingIndicatorOwnsItsLiveRegion(t *testing.T) {
	h := string(PollingIndicator(PollingIndicatorConfig{ExtraAttrs: map[string]string{
		"role": "alert", "aria-live": "assertive", "data-test": "hook",
	}}))
	root := h[:strings.IndexByte(h, '>')+1]
	for _, want := range []string{`role="status"`, `aria-live="polite"`, `data-test="hook"`} {
		if !strings.Contains(root, want) {
			t.Errorf("PollingIndicator root lacks %q:\n%s", want, root)
		}
	}
}

func TestSweepFeedbackBellSignalListSwapsTheList(t *testing.T) {
	h := string((&notificationBellSlot{name: "bell", label: "Inbox", emptyText: "none", signalList: "inbox.list"}).Render())
	if !strings.Contains(h, `class="fui-notification-bell__body" data-cui-signal="inbox.list" data-cui-signal-mode="html"`) {
		t.Fatalf("SignalList did not bind the list body as html:\n%s", h)
	}
	plain := string((&notificationBellSlot{name: "bell", label: "Inbox", emptyText: "none"}).Render())
	if strings.Contains(plain, "data-cui-signal") {
		t.Fatalf("a bell with no SignalList binds a signal:\n%s", plain)
	}
}

func TestSweepFeedbackSkeletonModifiersMarkTheShape(t *testing.T) {
	for _, c := range []struct {
		name, html, class string
		want              bool
	}{
		{"footed card", string(SkeletonCard(SkeletonCardConfig{ShowFooter: true})), "fui-skeleton-card--footed", true},
		{"plain card", string(SkeletonCard(SkeletonCardConfig{})), "fui-skeleton-card--footed", false},
		{"row", string(SkeletonRow(SkeletonRowConfig{})), "fui-skeleton-row--chevron", true},
		{"plain row", string(SkeletonRow(SkeletonRowConfig{HideChevron: true})), "fui-skeleton-row--chevron", false},
	} {
		root := c.html[:strings.IndexByte(c.html, '>')+1]
		if got := strings.Contains(root, c.class); got != c.want {
			t.Errorf("%s: %s on the root = %t, want %t:\n%s", c.name, c.class, got, c.want, root)
		}
	}
}

func TestSweepFeedbackSkeletonLineIsOneBar(t *testing.T) {
	h := string(SkeletonLine(SkeletonLineConfig{}))
	if n := strings.Count(h, `class="fui-skeleton__line"`); n != 1 {
		t.Fatalf("SkeletonLine draws %d bars, want 1:\n%s", n, h)
	}
}
