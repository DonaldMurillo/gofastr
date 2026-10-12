package headless

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/chromedp/chromedp"
)

// A select-all header checks and clears its own table's row boxes,
// never a box of the same name in another table, and shows checked,
// clear or mixed from the rows' states.
func TestE2E_SelectAllGovernsItsRows(t *testing.T) {
	box := func(v string) render.HTML {
		return Choice(ChoiceProps{Type: "checkbox", Name: "ids", Value: v, Label: "Select " + v}, nil)
	}
	table := func(id string, n int) string {
		rows := make([]Row, n)
		for i := range rows {
			v := id + string(rune('a'+i))
			rows[i] = Row{ID: v, Cells: map[string]render.HTML{"pick": box(v), "name": render.Text(v)}}
		}
		return string(Table(TableProps{ID: id, Columns: []Column{
			{Key: "pick", SelectAll: "ids"}, {Key: "name", Header: "Name"},
		}, Rows: rows}, nil))
	}
	b := startBehaviorServer(t, table("one", 3)+table("two", 1))
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, moduleLoadedExpr) {
		t.Fatal("the module never loaded")
	}
	state := func() map[string]any {
		var got map[string]any
		if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
			const all = document.querySelector('#one [data-hui-table-select-all]');
			const rows = [...document.querySelectorAll('#one input[name=ids]')].map(b => b.checked);
			return {checked: all.checked, mixed: all.indeterminate, rows: rows.join(','),
				other: document.querySelector('#two input[name=ids]').checked};
		})()`, &got)); err != nil {
			t.Fatalf("reading the boxes: %v", err)
		}
		return got
	}
	click := func(sel string) {
		if err := chromedp.Run(ctx, chromedp.Click(sel, chromedp.ByQuery)); err != nil {
			t.Fatalf("clicking %s: %v", sel, err)
		}
	}

	click(`#one [data-hui-table-select-all]`)
	if got := state(); got["rows"] != "true,true,true" || got["checked"] != true || got["other"] != false {
		t.Fatalf("select-all did not check exactly its own rows: %v", got)
	}
	click(`#one input[value=oneb]`)
	if got := state(); got["checked"] != false || got["mixed"] != true {
		t.Fatalf("a partial selection does not show mixed: %v", got)
	}
	click(`#one input[value=oneb]`)
	if got := state(); got["checked"] != true || got["mixed"] != false {
		t.Fatalf("rechecking the last row does not show checked: %v", got)
	}
	click(`#one [data-hui-table-select-all]`)
	if got := state(); got["rows"] != "false,false,false" || got["checked"] != false || got["mixed"] != false {
		t.Fatalf("select-all did not clear its rows: %v", got)
	}
}
