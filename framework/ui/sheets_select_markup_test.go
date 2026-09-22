package ui

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
)

// A member whose markup moved to fui-* classes must carry its sheet
// with it: a sheet that still selects .ui-<name>__part or
// .ui-<name>--variant matches nothing, and every gate but a pair of
// screenshots passes. This walks every registered sheet against the
// package's own source: where the source says "fui-<name>", the sheet
// may not select ".ui-<name>", and the other way round.
func TestSheetsSelectTheClassesTheirMarkupEmits(t *testing.T) {
	src := packageSourceWithoutComments(t)
	th := style.DefaultTheme()
	for _, e := range registry.All() {
		if e.StyleFn == nil {
			continue
		}
		name := strings.TrimPrefix(strings.TrimPrefix(e.Name, "fui-"), "ui-")
		css := e.StyleFn(th)
		oldSel := regexp.MustCompile(`\.ui-` + regexp.QuoteMeta(name) + `(__|--|[^A-Za-z0-9_-])`)
		newSel := regexp.MustCompile(`\.fui-` + regexp.QuoteMeta(name) + `(__|--|[^A-Za-z0-9_-])`)
		// A class string in markup: quoted or space-joined, never the
		// selector's leading dot and never the data-fui-comp marker.
		oldCls := regexp.MustCompile(`["' ]ui-` + regexp.QuoteMeta(name) + `(__|--)`)
		newCls := regexp.MustCompile(`["' ]fui-` + regexp.QuoteMeta(name) + `(__|--|["' ])`)
		markupNew := newCls.MatchString(src)
		markupOld := oldCls.MatchString(src)
		if markupNew && !markupOld && oldSel.MatchString(css) {
			t.Errorf("sheet %q still selects %s while the markup says fui-%s: an orphaned rule", e.Name, oldSel.FindString(css), name)
		}
		if markupOld && !markupNew && newSel.MatchString(css) {
			t.Errorf("sheet %q selects %s while the markup still says ui-%s: an orphaned rule", e.Name, newSel.FindString(css), name)
		}
	}
}

// The registered sheet names and their data-fui-comp markers stay
// ui-*: hosts select on them, and the prefix rule gives fui- to the
// classes alone. A sheet registered as fui-* would also have escaped
// the walk above while it filtered on the ui- prefix.
func TestRegisteredSheetNamesKeepTheirPrefix(t *testing.T) {
	// Registered under fui- before the prefix decision, in the
	// framework/ui rewrite this migration stack builds on (the seven
	// at 149bd1f1). They stay until a change of their own renames
	// them; nothing joins the list.
	before := map[string]bool{"fui-collapsible": true, "fui-counter": true, "fui-dropdown": true,
		"fui-reveal": true, "fui-section-menu": true, "fui-tabs": true, "fui-toggle": true}
	for _, e := range registry.All() {
		if e.StyleFn != nil && strings.HasPrefix(e.Name, "fui-") && !before[e.Name] {
			t.Errorf("sheet %q is registered under the class prefix; registered names and markers stay ui-*", e.Name)
		}
	}
}

func packageSourceWithoutComments(t *testing.T) string {
	t.Helper()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "//") {
				continue
			}
			b.WriteString(line)
			b.WriteByte('\n')
		}
	}
	return b.String()
}
