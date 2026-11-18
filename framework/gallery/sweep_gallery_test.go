package gallery

import (
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"slices"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core/router"
)

func TestSweepCategorySlugIsLowerDashed(t *testing.T) {
	for in, want := range map[string]string{
		"Buttons & links":     "buttons-links",
		"  Data (additions) ": "data-additions",
		"A--B__C":             "a-b-c",
		"&&":                  "",
		"Charts2":             "charts2",
	} {
		if got := CategorySlug(in); got != want {
			t.Errorf("CategorySlug(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSweepGroupedKeepsCatalogOrder(t *testing.T) {
	var firstSeen []string
	for _, e := range Catalog {
		if !slices.Contains(firstSeen, e.Category) {
			firstSeen = append(firstSeen, e.Category)
		}
	}
	if got := Categories(); !slices.Equal(got, firstSeen) {
		t.Fatalf("Categories() = %v, want first-appearance order %v", got, firstSeen)
	}
	for _, g := range Grouped() {
		want := ByCategory(g.Name)
		if len(want) != len(g.Entries) {
			t.Fatalf("group %q has %d entries, ByCategory %d", g.Name, len(g.Entries), len(want))
		}
		for i := range want {
			if want[i].Slug != g.Entries[i].Slug {
				t.Errorf("group %q entry %d = %q, want catalog order %q", g.Name, i, g.Entries[i].Slug, want[i].Slug)
			}
		}
	}
	if DemoFor("no-such-slug") != nil {
		t.Error("DemoFor of an unknown slug is not nil")
	}
	if ByCategory("no such category") != nil {
		t.Error("ByCategory of an unknown category is not nil")
	}
}

var openRe = regexp.MustCompile(`data-cui-open="([^"]*)"`)

func TestSweepEveryDeleteTriggerOpensAMountedModal(t *testing.T) {
	mounted := map[string]bool{}
	for _, b := range OptimisticDeleteModals() {
		mounted[b.Build().Name] = true
	}
	opened := 0
	for _, m := range openRe.FindAllStringSubmatch(string(RenderOptimisticDeleteDemoFor(InitialOptimisticNotes)), -1) {
		opened++
		if !mounted[m[1]] {
			t.Errorf("trigger opens %q, which OptimisticDeleteModals does not mount", m[1])
		}
	}
	if opened != len(InitialOptimisticNotes)+1 {
		t.Fatalf("found %d delete triggers, want %d", opened, len(InitialOptimisticNotes)+1)
	}
}

func TestSweepEmptyOptimisticListsStillRender(t *testing.T) {
	if RenderOptimisticCreateList(nil) == "" || RenderOptimisticDeleteList(nil) == "" {
		t.Fatal("an empty optimistic list renders nothing, leaving its region without content to swap")
	}
}

type readTrap struct{ read bool }

func (r *readTrap) Read([]byte) (int, error) { r.read = true; return 0, io.EOF }

func TestSweepDemoFormKeepsNothing(t *testing.T) {
	routes := router.New()
	MountDemoFormRoute(routes, func(slug string) string { return "/components/" + slug })
	body := &readTrap{}
	req := httptest.NewRequest(http.MethodPost, DemoFormPath+"?demo=form", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	routes.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("demo form answered %d, want 303", rec.Code)
	}
	if body.read {
		t.Error("the demo form route read the posted body")
	}
	for _, page := range []string{"/x\ny", "/x\x7f", "/x\ty"} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("demo page %q with a control byte was accepted", page)
				}
			}()
			MountDemoFormRoute(router.New(), func(string) string { return page })
		}()
	}
}

func TestSweepGalleryRegistersNoStylesheet(t *testing.T) {
	own := funcPkg(t, CategorySlug)
	for _, e := range registry.All() {
		if e.StyleFn != nil && funcPkg(t, e.StyleFn) == own {
			t.Errorf("sheet %q is defined in framework/gallery; demos compose framework/ui and ship no CSS", e.Name)
		}
	}
}

func TestDemoFormRouteRefusesControlBytesInTheFragment(t *testing.T) {
	for _, page := range []string{"/components/form#x\ny", "/components/form#x\ty", "/components/form#x\x7fy"} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("demo page %q with a control byte in its fragment was accepted", page)
				}
			}()
			MountDemoFormRoute(router.New(), func(string) string { return page })
		}()
	}
	MountDemoFormRoute(router.New(), func(string) string { return "/components/form#details" })
}
