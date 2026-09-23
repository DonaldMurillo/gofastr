package gallery

import (
	"maps"
	"reflect"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// The ui- prefix belongs to the registered sheet names and their
// data-fui-comp markers, and to nothing else: every class framework/ui
// emits is fui-*. These two gates hold that line. The first renders the
// whole catalog and refuses a ui-* class TOKEN anywhere in the output;
// the second walks every sheet framework/ui registers and refuses a
// .ui-* class SELECTOR, so a component whose demo is note-only or whose
// variant the catalog never exercises is still caught at its sheet.

// classAttrRe matches the class attributes render.Tag emits. Single
// quotes and unquoted values do not occur in framework output.
var classAttrRe = regexp.MustCompile(`class="([^"]*)"`)

// patternSlugs are the catalog entries that render a core-ui pattern
// package. Those packages own their own class vocabulary — multiselect
// and sortablelist still emit ui-* classes — so the framework gate
// exempts them by name until each pattern migrates in a change of its
// own. One line per entry, so a name here is a decision, not drift.
var patternSlugs = map[string]string{
	"accordion":      "core-ui/patterns/accordion owns its class vocabulary until it migrates",
	"breadcrumbs":    "core-ui/patterns/breadcrumbs owns its class vocabulary until it migrates",
	"infinitescroll": "core-ui/patterns/infinitescroll owns its class vocabulary until it migrates",
	"multiselect":    "core-ui/patterns/multiselect still emits ui-multiselect* classes",
	"nestedlist":     "core-ui/patterns/nestedlist owns its class vocabulary until it migrates",
	"progress":       "core-ui/patterns/progress owns its class vocabulary until it migrates",
	"sortablelist":   "core-ui/patterns/sortablelist still emits ui-sortable-list* classes",
	"tree":           "core-ui/patterns/tree owns its class vocabulary until it migrates",
}

// TestCatalogRendersNoUIClassTokens renders every catalog entry and
// refuses any class token starting with ui-. A token, not a substring:
// fui-hero must not be matched by a ui-hero needle, and the check is
// the whole class value split on spaces.
func TestCatalogRendersNoUIClassTokens(t *testing.T) {
	for _, e := range Catalog {
		reason, exempt := patternSlugs[e.Slug]
		if exempt {
			t.Logf("slug %q exempt: %s", e.Slug, reason)
			continue
		}
		html := string(e.Demo())
		bad := map[string]bool{}
		for _, m := range classAttrRe.FindAllStringSubmatch(html, -1) {
			for _, tok := range strings.Fields(m[1]) {
				if strings.HasPrefix(tok, "ui-") {
					bad[tok] = true
				}
			}
		}
		for _, tok := range slices.Sorted(maps.Keys(bad)) {
			t.Errorf("catalog entry %q renders class %q: framework/ui classes are fui-*, and ui-* belongs to the sheet names and markers alone", e.Slug, tok)
		}
	}
}

// uiSelectorRe matches a .ui-* class selector in CSS. The dot is what
// separates a selector from every other place the ui- prefix legitimately
// appears: the [data-fui-comp="ui-…"] markers, the --ui-* custom
// properties, and @keyframes names.
var uiSelectorRe = regexp.MustCompile(`\.ui-[A-Za-z0-9_-]+`)

// TestFrameworkUISheetsSelectNoUIClassTokens walks every sheet whose
// builder is defined in framework/ui — the package is read off the
// registered StyleFn at runtime, so a sheet cannot escape the walk by
// its registered name — and refuses a .ui-* class selector in its CSS.
func TestFrameworkUISheetsSelectNoUIClassTokens(t *testing.T) {
	uiPkg := funcPkg(t, ui.Button)
	th := style.DefaultTheme()
	checked := 0
	for _, e := range registry.All() {
		if e.StyleFn == nil {
			continue
		}
		if funcPkg(t, e.StyleFn) != uiPkg {
			continue
		}
		checked++
		if sel := uiSelectorRe.FindString(e.CSSFor(th)); sel != "" {
			t.Errorf("sheet %q selects %q: framework/ui classes are fui-*; the ui- prefix stays on the registered name and marker", e.Name, sel)
		}
	}
	if checked == 0 {
		t.Fatal("no framework/ui sheet was found — the walk is broken, not the tree clean")
	}
	t.Logf("walked %d framework/ui sheets", checked)
}

// funcPkg returns the package path a function is defined in, the way
// the registry's own duplicate-name panic does: off the function
// pointer, not the caller's guess.
func funcPkg(t *testing.T, fn any) string {
	t.Helper()
	v := reflect.ValueOf(fn)
	if v.Kind() != reflect.Func {
		t.Fatalf("%v is not a function", fn)
	}
	name := runtime.FuncForPC(v.Pointer()).Name()
	// Trim the function symbol: pkg.Path.Func or pkg.Path.glob..func1.
	if i := strings.LastIndexByte(name, '/'); i >= 0 {
		name = name[:i] + "/" + strings.SplitN(name[i+1:], ".", 2)[0]
	} else {
		name = strings.SplitN(name, ".", 2)[0]
	}
	return name
}
