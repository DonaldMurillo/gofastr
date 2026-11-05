package scan

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/upgrade"
)

func TestCSSClassAttributeSelectors(t *testing.T) {
	css := `a[class~=ui-button] {}
b[CLASS="ui-button ui-card"] {}
@media print { [cl\61 ss ~= 'ui\2d card'] {} }
.outer { [class = ui-card i] {} }
[class~=UI-CARD I] {}
[data-class=ui-button] {}
[class|=ui-button] {}
[class^=ui-card] {}
[class="ui-buttonish ui-cardinal"] {}
.value { content: "[class~=ui-button]"; }
[class~=UI-CARD s] {}
[class="ui-card other"] {}
`
	note := cssNote([]string{"ui-button", "ui-card"}, nil)
	result := mustRun(t, cssWorkspace(t, map[string]string{"app.css": css}), note)
	counts := map[string]int{}
	for _, hit := range result.Hits[note] {
		counts[hit.Why]++
		if hit.Line > 5 {
			t.Errorf("not a retired class selector: %+v", hit)
		}
	}
	if counts["css .ui-button"] != 2 || counts["css .ui-card"] != 4 {
		t.Fatalf("attribute class hits = %v, want button=2 card=4", counts)
	}
}

func TestCSSInvalidClassAttrsStaySilent(t *testing.T) {
	for _, source := range []string{
		"[", "[] {}", "[class] {}", "[class~] {}", "[class=] {}",
		"[class=123] {}", "[class='ui-card", "[class=ui-card {}",
		"[class=ui-card invalid] {}", "[class=ui-card i {}",
	} {
		note := cssNote([]string{"ui-card"}, nil)
		matcher := &engine{cssNotes: []*upgrade.Note{note}, hits: map[*upgrade.Note][]Hit{}}
		matcher.cssFile("app.css", source)
		if len(matcher.hits[note]) != 0 {
			t.Errorf("%q: invalid selector matched: %+v", source, matcher.hits[note])
		}
	}
}

func TestCSSClassStringContinuations(t *testing.T) {
	for _, ending := range []struct {
		name, text string
	}{
		{"LF", "\n"}, {"CR", "\r"}, {"CRLF", "\r\n"}, {"FF", "\f"},
	} {
		for _, operator := range []string{"=", "~="} {
			for _, quote := range []string{`"`, `'`} {
				t.Run(ending.name+operator+quote, func(t *testing.T) {
					value := "ui-\\" + ending.text + "ca\\" + ending.text + "rd"
					want := 1
					if operator == "=" {
						value += " ui\\" + ending.text + `\2d button`
						want++
					}
					css := "[class" + operator + quote + value + quote + "] {}"
					note := cssNote([]string{"ui-card", "ui-button"}, nil)
					result := mustRun(t, cssWorkspace(t, map[string]string{"app.css": css}), note)
					if hits := result.Hits[note]; len(hits) != want {
						t.Fatalf("continued class hits = %+v, want %d", hits, want)
					}
				})
			}
		}
	}
}

func TestCSSClassStringEscapesStayLiteral(t *testing.T) {
	for _, css := range []string{
		`[class~="ui-\ncard"] {}`,
		`[class~="ui-\a card"] {}`,
		"[class~=\"ui-\ncard\"] {}",
	} {
		note := cssNote([]string{"ui-card"}, nil)
		result := mustRun(t, cssWorkspace(t, map[string]string{"app.css": css}), note)
		if hits := result.Hits[note]; len(hits) != 0 {
			t.Errorf("non-continuation %q matched: %+v", css, hits)
		}
	}

	css := `[class~="ui-\\` + "\\\n" + `card"] {}`
	note := cssNote([]string{`ui-\card`}, nil)
	result := mustRun(t, cssWorkspace(t, map[string]string{"app.css": css}), note)
	if hits := result.Hits[note]; len(hits) != 1 {
		t.Fatalf("literal backslash with continuation hits = %+v, want one", hits)
	}
}
