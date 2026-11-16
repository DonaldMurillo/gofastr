package ui

import (
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui/theme"
)

func TestSweepCoreScrubAttrsDropsHandlersAndControlBytes(t *testing.T) {
	in := html.Attrs{
		"onclick": "x()", "OnLoad": "x()", "title": "a\x01b", "data-\x7fx": "v",
		"data-x": "ok", "aria-label": "fine",
	}
	out := scrubAttrs(in)
	if len(out) != 2 || out["data-x"] != "ok" || out["aria-label"] != "fine" {
		t.Fatalf("scrubAttrs kept %v, want only data-x and aria-label", out)
	}
	if len(in) != 6 {
		t.Fatalf("scrubAttrs mutated its input: %v", in)
	}
	if scrubAttrs(nil) != nil {
		t.Fatal("scrubAttrs(nil) is not nil")
	}
}

func TestSweepCoreShortIDTargetIsTheSameOnEveryRender(t *testing.T) {
	target := regexp.MustCompile(`data-hui-copy-target="([^"]+)"`)
	first := target.FindStringSubmatch(string(ShortID(ShortIDConfig{Value: longID})))
	second := target.FindStringSubmatch(string(ShortID(ShortIDConfig{Value: longID})))
	other := target.FindStringSubmatch(string(ShortID(ShortIDConfig{Value: longID + "0"})))
	if first == nil || second == nil || other == nil {
		t.Fatal("ShortID rendered no copy target")
	}
	if first[1] != second[1] {
		t.Errorf("one value got two copy targets: %q and %q", first[1], second[1])
	}
	if first[1] == other[1] {
		t.Errorf("two values share the copy target %q", first[1])
	}
}

func TestSweepCoreInvalidateScreensDropsAHalfDecodedHeader(t *testing.T) {
	w := httptest.NewRecorder()
	w.Header().Set("X-Gofastr-Invalidate", `["/kept-by-mistake", 5]`)
	InvalidateScreens(w, "/orders")
	if got := w.Header().Get("X-Gofastr-Invalidate"); got != `["/orders"]` {
		t.Fatalf("header = %s, want only the new path", got)
	}
}

func TestSweepCoreThemedWrapsInTheOverrideClass(t *testing.T) {
	ref := style.RegisterThemeOverride(theme.Default(theme.Overrides{Primary: "#123456", Dark: &theme.Overrides{}}))
	got := string(Themed(ref, render.Text("x")))
	if want := `<div class="` + ref.Class() + `">x</div>`; got != want {
		t.Fatalf("Themed = %s, want %s", got, want)
	}
}

func TestSweepCoreStatusIconRefusesCSSBreakers(t *testing.T) {
	for i, icon := range []string{`"`, `\`, "a\nb", "a\rb"} {
		wantPanic(t, "status icon "+icon, func() {
			RegisterStatusVariant("sweep-icon-"+string(rune('a'+i)), StatusVariantCSS{Color: "#000", Icon: icon})
		})
	}
}

func TestSweepCoreButtonClassTokensDefaultToPrimary(t *testing.T) {
	if got := buttonClassTokens("", ButtonSizeDefault); got != "fui-button fui-button--primary" {
		t.Fatalf("buttonClassTokens(\"\") = %q, want the primary variant", got)
	}
}

func TestSweepCoreComponentCSSGoesInsideHead(t *testing.T) {
	button := Button(ButtonConfig{Label: "Save"})
	doc := render.Join(render.Raw("<html><head></head><body>"), button, render.Raw("</body></html>"))
	if out := string(WithComponentCSS(doc, style.DefaultTheme())); !strings.HasPrefix(out, "<html><head><style>") {
		t.Fatalf("styles are not the head's first child:\n%.80s", out)
	}
	if out := string(WithComponentCSS(button, style.DefaultTheme())); !strings.HasPrefix(out, "<style>") {
		t.Fatalf("a headless document does not start with its styles:\n%.80s", out)
	}
}

func TestSweepCoreButtonVariantRuleIsAPlainClassRule(t *testing.T) {
	css := buttonCSS(style.DefaultTheme())
	if !regexp.MustCompile(`(?m)^\.fui-button--brand \{`).MatchString(css) {
		t.Fatalf("the registered variant is not a plain .fui-button--brand rule:\n%s", css)
	}
}

func TestSweepCoreExtraAttrsSerializeSorted(t *testing.T) {
	for range 20 {
		if got := serializeExtraAttrs(html.Attrs{"c": "3", "a": "1", "b": "2", "d": "4"}); got != ` a="1" b="2" c="3" d="4"` {
			t.Fatalf("serializeExtraAttrs = %q, want keys in order", got)
		}
	}
}

func TestSweepCoreVariantRegistrationNamesItsFault(t *testing.T) {
	ok := VariantCSS{Props: []string{"color", "red"}}
	for _, tc := range []struct {
		name string
		css  VariantCSS
		seal bool
		want string
	}{
		{"odd", VariantCSS{Props: []string{"color"}}, false, "odd count"},
		{"hover-odd", VariantCSS{Props: []string{"color", "red"}, Hover: []string{"x"}}, false, "odd count"},
		{"empty", VariantCSS{}, false, "Props must be non-empty"},
		{"Bad Name", ok, false, "name must be"},
		{"-lead", ok, false, "name must be"},
		{"taken", ok, false, "collides"},
		{"late", ok, true, "called after"},
	} {
		set := &variantSet{sheet: "ui-sweep", reserved: map[string]bool{"taken": true}}
		if tc.seal {
			set.sealAndSnapshot()
		}
		func() {
			defer func() {
				if r, _ := recover().(string); !strings.Contains(r, tc.want) {
					t.Errorf("%s: panic %q, want one naming %q", tc.name, r, tc.want)
				}
			}()
			set.register("RegisterButtonVariant", tc.name, kindVariant, tc.css)
		}()
	}
}

func TestSweepCoreStatusVariantRefusesConsumerModifierNames(t *testing.T) {
	for _, name := range []string{"interactive", "floating", "at-top-right", "at-bottom-left"} {
		wantPanic(t, "status variant "+name, func() {
			RegisterStatusVariant(name, StatusVariantCSS{Color: "#000"})
		})
	}
}

func TestSweepCoreVariantSnapshotIsSorted(t *testing.T) {
	set := &variantSet{sheet: "ui-sweep"}
	for _, n := range []string{"delta", "alpha", "charlie", "bravo", "echo"} {
		set.register("RegisterButtonVariant", n, kindVariant, VariantCSS{Props: []string{"color", "red"}})
	}
	for range 10 {
		if got := strings.Join(set.sealAndSnapshot(), ","); got != "alpha,bravo,charlie,delta,echo" {
			t.Fatalf("snapshot order %s, want sorted", got)
		}
	}
}
