package headless

import (
	"regexp"
	"strings"
	"testing"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/core/render"
)

func enterRepeater() render.HTML {
	return Repeater(RepeaterProps{Name: "links", Label: "Links", RemoveName: "remove", AddName: "add", Items: []RepeaterItem{
		{Fields: []render.HTML{render.HTML(`<input id="l0" name="url" value="a">`)}},
		{Fields: []render.HTML{render.HTML(`<input id="l1" name="url" value="b">`)}},
	}}, nil)
}

// Every row's Remove is a named submit and precedes the host form's
// Save, so the form's default button (first submit in tree order) was
// row 0's Remove: Enter in any row's field submitted remove=0. The
// repeater's first submit control is now a hidden, nameless default
// that carries no operation.
func TestRepeaterDefaultButtonCarriesNoOperation(t *testing.T) {
	h := string(enterRepeater())
	first := regexp.MustCompile(`<button[^>]*type="submit"[^>]*>`).FindString(h)
	if strings.Contains(first, `name=`) || strings.Contains(first, `data-hui-repeater-action`) {
		t.Fatalf("the repeater's first submit control carries an operation, so Enter in a row's field submits it: %s", first)
	}
	if !strings.Contains(first, ` hidden=""`) || !strings.Contains(first, `tabindex="-1"`) {
		t.Fatalf("the default is seen or reached by Tab: %s", first)
	}
}

// The browser proof: Enter in a row's field is the host form's own
// implicit submission. With the module loaded it submits through the
// host's Save (its first submit control outside the repeater), and in
// no case through a Remove.
func TestE2E_RepeaterEnterSubmitsTheHostDefault(t *testing.T) {
	body := `<form id="rf" method="post" action="/save">` + string(enterRepeater()) +
		`<button id="save" type="submit" name="intent" value="save">Save</button></form>`
	got := enterInRepeaterRow(t, body, "l1", `(e.submitter ? (e.submitter.id || 'nameless') : 'none') +
		'|remove=' + String(fd.getAll('remove')) + '|intent=' + String(fd.getAll('intent'))`)
	if got != "save|remove=|intent=save" {
		t.Fatalf("Enter in row 1's field submitted %q, want the form's Save with no remove", got)
	}
}

// With no submit control of its own the host form keeps the hidden
// default's nameless submission: still no Remove.
func TestE2E_RepeaterEnterWithNoHostButtonRemovesNothing(t *testing.T) {
	body := `<form id="rf" method="post" action="/save">` + string(enterRepeater()) + `</form>`
	got := enterInRepeaterRow(t, body, "l0", `'remove=' + String(fd.getAll('remove')) + '|add=' + String(fd.getAll('add'))`)
	if got != "remove=|add=" {
		t.Fatalf("Enter in row 0's field submitted %q, want no operation", got)
	}
}

// enterInRepeaterRow serves body, presses Enter in the field with id
// field, and returns what the #rf submit listener recorded: the JS
// expression record, evaluated with fd as the submission's FormData.
func enterInRepeaterRow(t *testing.T, body, field, record string) string {
	t.Helper()
	b := startBehaviorServer(t, body)
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, controlsLoadedExpr(CollectionsBehaviorName)) {
		t.Fatal("the repeater marker never loaded headless-collections")
	}
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.getElementById('rf').addEventListener('submit', (e) => {
			e.preventDefault();
			const fd = new FormData(e.target, e.submitter);
			window.__by = `+record+`;
		})`, nil),
		chromedp.Focus(`#`+field, chromedp.ByID),
		chromedp.KeyEvent("\r"),
	); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `window.__by !== undefined`) {
		t.Fatal("Enter submitted nothing")
	}
	var got string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.__by`, &got)); err != nil {
		t.Fatal(err)
	}
	return got
}
