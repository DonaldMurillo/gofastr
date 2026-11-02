package headless

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

var instanceIDAttr = regexp.MustCompile(` id="([^"]+)"`)

// Rating built its choices' ids from Name even when ID was set, the
// one form control that did: two ratings sharing a field name (a
// filter form and an edit form) each given its own ID still repeated
// "stars-1".."stars-N", and every label's for= toggled the first
// group's radio. With an ID the choices are <ID>-1..<ID>-N, the way
// the other controls build from instanceID; with no ID they stay
// <Name>-N.
func TestRatingChoiceIDsDeriveFromID(t *testing.T) {
	ctx := render.WithIDScope(context.Background())
	h := string(Rating(RatingProps{Name: "stars", ID: "filter-stars", Label: "Stars", Max: 3, Ctx: ctx}, nil)) +
		string(Rating(RatingProps{Name: "stars", ID: "edit-stars", Label: "Stars", Max: 3, Ctx: ctx}, nil))
	assertUniqueIDs(t, "two IDed ratings sharing a Name", h)
	for _, id := range []string{"filter-stars-1", "filter-stars-3", "edit-stars-1", "edit-stars-3"} {
		if !strings.Contains(h, ` id="`+id+`"`) || !strings.Contains(h, ` for="`+id+`"`) {
			t.Errorf("choice id %q (and its label's for=) missing:\n%s", id, h)
		}
	}
	if strings.Contains(h, `id="stars-1"`) {
		t.Errorf("an IDed rating still builds its choice ids from Name:\n%s", h)
	}
	// No ID: the Name, as before.
	plain := string(Rating(RatingProps{Name: "stars", Label: "Stars", Max: 2}, nil))
	if !strings.Contains(plain, ` id="stars-2"`) {
		t.Errorf("an un-IDed rating no longer builds from Name:\n%s", plain)
	}
}

// assertUniqueIDs fails on an id rendered twice.
func assertUniqueIDs(t *testing.T, what, h string) {
	t.Helper()
	seen := map[string]bool{}
	for _, m := range instanceIDAttr.FindAllStringSubmatch(h, -1) {
		if seen[m[1]] {
			t.Errorf("%s: id %q renders twice on one page:\n%s", what, m[1], h)
			return
		}
		seen[m[1]] = true
	}
}

// The form controls derived their instance id from Name, so two
// controls sharing a field name on one page (a filter form and an edit
// form) repeated every id, and each label's for= toggled the first
// one. With the page's id scope on Ctx the second instance takes -2;
// alone, the output is what it always was.
func TestFormControlIDsAreUniqueAcrossInstances(t *testing.T) {
	one, ten := 1, 10
	render2 := func(f func(ctx context.Context) render.HTML) (alone, twice string) {
		alone = string(f(render.WithIDScope(context.Background())))
		ctx := render.WithIDScope(context.Background())
		twice = string(f(ctx)) + string(f(ctx))
		return alone, twice
	}
	cases := map[string]func(ctx context.Context) render.HTML{
		"MultiSelect": func(ctx context.Context) render.HTML {
			return MultiSelect(MultiSelectProps{Name: "tags", Label: "Tags", Ctx: ctx,
				Options: []MultiSelectOption{{Value: "a", Label: "A"}, {Value: "b", Label: "B"}}}, nil)
		},
		"Rating": func(ctx context.Context) render.HTML {
			return Rating(RatingProps{Name: "stars", Label: "Stars", Max: 3, Ctx: ctx}, nil)
		},
		"NumberInput": func(ctx context.Context) render.HTML {
			return NumberInput(NumberInputProps{Name: "qty", Label: "Quantity", Help: "Whole units", Error: "Too many", Min: &one, Max: &ten, Ctx: ctx}, nil)
		},
		"Slider": func(ctx context.Context) render.HTML {
			return Slider(SliderProps{Name: "vol", Label: "Volume", ShowValue: true, Ctx: ctx}, nil)
		},
		"RangeSlider": func(ctx context.Context) render.HTML {
			return RangeSlider(RangeSliderProps{Name: "price", Label: "Price", ShowValue: true, Ctx: ctx}, nil)
		},
		"TagInput": func(ctx context.Context) render.HTML {
			return TagInput(TagInputProps{Name: "topics", Label: "Topics", Help: "One word each", Ctx: ctx}, nil)
		},
	}
	for name, f := range cases {
		alone, twice := render2(f)
		assertUniqueIDs(t, name+" twice", twice)
		// One instance renders exactly what it rendered with no Ctx.
		if plain := string(f(nil)); plain != alone {
			t.Errorf("%s: one instance in a scope renders differently from no Ctx:\n%s\n%s", name, plain, alone)
		}
		// Every for= and aria-describedby still names an id that exists.
		for _, ref := range regexp.MustCompile(`(?:for|aria-describedby)="([^"]+)"`).FindAllStringSubmatch(twice, -1) {
			for _, id := range strings.Fields(ref[1]) {
				if !strings.Contains(twice, ` id="`+id+`"`) {
					t.Errorf("%s: a reference names %q, which no element carries:\n%s", name, id, twice)
				}
			}
		}
	}
}

// TagInput gave the root div id=p.ID AND the draft input
// id=orDefault(p.ID, p.Name): the same value whenever ID was set. The
// label's for= then resolved to the first element with that id (the
// root div, not labelable), so clicking the label focused nothing.
func TestTagInputIDNotRepeated(t *testing.T) {
	h := string(TagInput(TagInputProps{ID: "topics", Name: "topics", Label: "Topics"}, nil))
	if n := strings.Count(h, ` id="topics"`); n != 1 {
		t.Fatalf("id=\"topics\" renders %d times:\n%s", n, h)
	}
	field := regexp.MustCompile(`<input[^>]*data-hui-tag-input-field[^>]*>`).FindString(h)
	m := regexp.MustCompile(` id="([^"]+)"`).FindStringSubmatch(field)
	if m == nil || !regexp.MustCompile(`<label[^>]* for="`+regexp.QuoteMeta(m[1])+`"`).MatchString(h) {
		t.Fatalf("the label does not name the draft field: %s", h)
	}
}

// TagInput's Help was drawn as a <p> with no id, and the draft field
// carried no aria-describedby: the rule was on screen for sighted
// readers only.
func TestTagInputHelpDescribesTheField(t *testing.T) {
	for _, id := range []string{"", "labels"} {
		h := string(TagInput(TagInputProps{Name: "labels", ID: id, Label: "Labels",
			Help: "One word each, eight characters at most."}, nil))
		field := regexp.MustCompile(`<input[^>]*data-hui-tag-input-field[^>]*>`).FindString(h)
		m := regexp.MustCompile(`aria-describedby="([^"]+)"`).FindStringSubmatch(field)
		if m == nil {
			t.Fatalf("ID %q: the draft field is not described by its Help: %s", id, field)
		}
		if !strings.Contains(h, `id="`+m[1]+`">One word each`) {
			t.Fatalf("ID %q: aria-describedby=%q names no Help element:\n%s", id, m[1], h)
		}
	}
	if h := string(TagInput(TagInputProps{Name: "labels", Label: "Labels"}, nil)); strings.Contains(h, "aria-describedby") {
		t.Fatalf("a TagInput with no Help is described by nothing: %s", h)
	}
}
