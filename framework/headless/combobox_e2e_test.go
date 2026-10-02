package headless

// Browser coverage for headless-combobox and the shortcut half of
// headless-navigation: the input owns focus through every interaction,
// ArrowDown/ArrowUp move aria-activedescendant with wrap, Home/End
// jump, Enter commits the active option, Escape closes and a second
// Escape clears, Tab closes and lets focus move; the static list
// filters client-side and announces through the count sentence; a
// chord focuses or clicks its target with the composing guard. Same
// harness as behavior_e2e_test.go.

import (
	"encoding/json"
	"testing"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/core/render"
)

const comboboxLoaded = `!!(window.__gofastr && window.__gofastr.loadedModules && window.__gofastr.loadedModules['headless-combobox'])`

// The keyboard contract: active descendant movement, Enter commits,
// Escape twice.
func TestE2E_ComboboxKeyboardContract(t *testing.T) {
	box := Combobox(ComboboxProps{ID: "kb", Name: "q", Label: "Search", Options: []ComboboxOption{
		{Label: "Alpha"},
		{Label: "Beta"},
		{Label: "Gamma"},
	}}, nil)
	b := startBehaviorServer(t, string(box))
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, comboboxLoaded) {
		t.Fatal("the input marker never loaded headless-combobox")
	}
	var active string
	readActive := func() {
		t.Helper()
		if err := chromedp.Run(ctx, chromedp.Evaluate(
			`document.getElementById('kb').getAttribute('aria-activedescendant')`, &active)); err != nil {
			t.Fatal(err)
		}
	}
	key := func(k string) {
		t.Helper()
		if err := chromedp.Run(ctx,
			chromedp.Evaluate(`document.getElementById('kb').dispatchEvent(new KeyboardEvent('keydown',{key:'`+k+`',bubbles:true,cancelable:true}))`, nil),
		); err != nil {
			t.Fatal(err)
		}
	}
	// Focusing the input auto-opens the populated listbox and
	// highlights the first option; ArrowDown then moves to the second.
	if err := chromedp.Run(ctx, chromedp.Click(`#kb`, chromedp.ByQuery)); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `document.getElementById('kb').getAttribute('aria-expanded') === 'true'`) {
		t.Fatal("focus did not auto-open the populated listbox")
	}
	readActive()
	if active != "" {
		t.Fatalf("the auto-open pre-highlighted %q — the highlight is the reader's first move, not the focus", active)
	}
	key("ArrowDown")
	readActive()
	if active != "kb-listbox-opt-0" {
		t.Fatalf("ArrowDown highlighted %q, want the first option", active)
	}
	key("ArrowDown")
	readActive()
	if active != "kb-listbox-opt-1" {
		t.Fatalf("the second ArrowDown highlighted %q, want the second option", active)
	}
	// Wrap: ArrowUp from the second moves to the first; from the
	// first, to the last.
	key("ArrowUp")
	readActive()
	if active != "kb-listbox-opt-0" {
		t.Fatalf("ArrowUp moved to %q, want the first", active)
	}
	key("ArrowUp")
	readActive()
	if active != "kb-listbox-opt-2" {
		t.Fatalf("ArrowUp from the first highlighted %q, want the last (wrap)", active)
	}
	// Home jumps to the first, End to the last.
	key("Home")
	readActive()
	if active != "kb-listbox-opt-0" {
		t.Fatalf("Home highlighted %q, want the first", active)
	}
	key("End")
	readActive()
	if active != "kb-listbox-opt-2" {
		t.Fatalf("End highlighted %q, want the last", active)
	}
	// Enter commits: the input takes the option's value, the listbox
	// closes, and focus never left the input.
	key("Enter")
	var value, expanded, focused string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`JSON.stringify([
		document.getElementById('kb').value,
		document.getElementById('kb').getAttribute('aria-expanded'),
		document.activeElement.id])`, &focused)); err != nil {
		t.Fatal(err)
	}
	if err := jsonUnmarshalStrings(focused, &value, &expanded, &active); err != nil {
		t.Fatal(err)
	}
	if value != "Gamma" || expanded != "false" || active != "kb" {
		t.Fatalf("Enter committed value=%q expanded=%q focus=%q, want Gamma/false/kb", value, expanded, active)
	}
	// Escape closes an open listbox; a second Escape clears the query.
	key("ArrowDown")
	if !pollTrue(ctx, `document.getElementById('kb').getAttribute('aria-expanded') === 'true'`) {
		t.Fatal("ArrowDown did not reopen the listbox")
	}
	key("Escape")
	if !pollTrue(ctx, `document.getElementById('kb').getAttribute('aria-expanded') === 'false' &&
		document.getElementById('kb').value === 'Gamma'`) {
		t.Fatal("the first Escape did not close the listbox and keep the query")
	}
	key("Escape")
	if !pollTrue(ctx, `document.getElementById('kb').value === ''`) {
		t.Fatal("the second Escape did not clear the query")
	}
}

// The static list filters client-side, skips hidden rows in the
// rotation, and announces through the count sentence.
func TestE2E_ComboboxStaticFilterAndAnnouncement(t *testing.T) {
	box := Combobox(ComboboxProps{ID: "st", Name: "q", Label: "Search", Options: []ComboboxOption{
		{Label: "Deployment"},
		{Label: "Docs"},
		{Label: "Domain"},
	}}, nil)
	b := startBehaviorServer(t, string(box))
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, comboboxLoaded) {
		t.Fatal("the module never loaded")
	}
	if err := chromedp.Run(ctx, chromedp.Click(`#st`, chromedp.ByQuery)); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `document.getElementById('st').getAttribute('aria-expanded') === 'true'`) {
		t.Fatal("focus did not auto-open the populated listbox")
	}
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`(() => {
			const i = document.getElementById('st');
			i.value = 'do';
			i.dispatchEvent(new Event('input', {bubbles: true}));
		})()`, nil),
		chromedp.Sleep(100*1e6),
	); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `document.getElementById('st-listbox-opt-0').hidden === true &&
		document.getElementById('st-listbox-opt-1').hidden === false &&
		document.getElementById('st-listbox-opt-2').hidden === false`) {
		t.Fatal("the static filter did not hide the non-matching row (Deployment must hide; Docs and Domain stay)")
	}
	// The rotation skips the hidden row: End highlights the last
	// VISIBLE option (Domain), never the hidden Deployment.
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.activeElement.dispatchEvent(new KeyboardEvent('keydown',{key:'End',bubbles:true,cancelable:true}))`, nil),
	); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `document.getElementById('st').getAttribute('aria-activedescendant') === 'st-listbox-opt-2'`) {
		t.Fatal("End highlighted a filtered-out row")
	}
	// Clearing the query shows all and announces the count through the
	// server's sentence.
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`const i=document.getElementById('st'); i.value=''; i.dispatchEvent(new Event('input',{bubbles:true}))`, nil),
	); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `document.querySelector('[data-hui-combobox-status]').textContent === '3 results'`) {
		t.Fatal("the count sentence never arrived in the status region")
	}
}

// A shortcut chord focuses its target: the wrapper carries the chord
// and the selector, the module resolves the first connected match.
func TestE2E_ShortcutChordFocusesTarget(t *testing.T) {
	box := Combobox(ComboboxProps{ID: "sc", Name: "q", Label: "Search", Options: []ComboboxOption{
		{Label: "Docs"},
	}}, nil)
	page := `<div data-hui-shortcut-focus="/" data-hui-shortcut-target="#sc">` + string(box) + `</div>`
	b := startBehaviorServer(t, page)
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, `!!(window.__gofastr && window.__gofastr.loadedModules && window.__gofastr.loadedModules['headless-navigation'])`) {
		t.Fatal("the chord marker never loaded headless-navigation")
	}
	// Focus something else, press the chord: the input receives focus.
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.body.focus ? document.body.focus() : true`, nil),
		chromedp.Evaluate(`document.dispatchEvent(new KeyboardEvent('keydown',{key:'/',bubbles:true,cancelable:true}))`, nil),
	); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `document.activeElement === document.getElementById('sc')`) {
		t.Fatal("the chord did not focus its target")
	}
}

// A chord declared on a wrapper clicks the element its selector names
// (ShortcutHint's BindTarget contract), and a composing keydown is
// ignored — an IME confirmation is not a hotkey.
func TestE2E_ShortcutChordClicksTargetAndSkipsComposing(t *testing.T) {
	page := `<span data-hui-shortcut-hint="#go" data-hui-shortcut-click="?" data-hui-shortcut-target="#go">?</span>` +
		`<button type="button" id="go" onclick="window.__went=true">Go</button>`
	b := startBehaviorServer(t, page)
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, `!!(window.__gofastr && window.__gofastr.loadedModules && window.__gofastr.loadedModules['headless-navigation'])`) {
		t.Fatal("the module never loaded")
	}
	// A composing keydown must not fire the chord.
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.dispatchEvent(new KeyboardEvent('keydown',{key:'?',bubbles:true,cancelable:true,isComposing:true}))`, nil),
		chromedp.Sleep(100*1e6),
	); err != nil {
		t.Fatal(err)
	}
	var went bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.__went === true`, &went)); err != nil {
		t.Fatal(err)
	}
	if went {
		t.Fatal("a composing keydown fired the chord — an IME confirmation is not a hotkey")
	}
	// The real chord clicks the selector's match.
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.dispatchEvent(new KeyboardEvent('keydown',{key:'?',bubbles:true,cancelable:true}))`, nil),
	); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `window.__went === true`) {
		t.Fatal("the chord did not click its target")
	}
}

// jsonUnmarshalStrings decodes a JSON array of strings into vars.
func jsonUnmarshalStrings(src string, out ...*string) error {
	var arr []string
	if err := json.Unmarshal([]byte(src), &arr); err != nil {
		return err
	}
	for i, v := range arr {
		if i < len(out) {
			*out[i] = v
		}
	}
	return nil
}

var _ = render.Text

// Keyboard navigation only walks options the static filter left
// visible (#302): with one option on screen, ArrowDown twice wraps
// back to it, never lands on a hidden row, and Enter picks the visible
// option — not one the user filtered away. Moved from the retired
// core-ui combobox module's filter-nav e2e, rendered here from the real
// primitive.
func TestE2E_ComboboxKeyboardSkipsFilteredOutRows(t *testing.T) {
	box := Combobox(ComboboxProps{ID: "pick", Name: "pick", Label: "Pick a word", Options: []ComboboxOption{
		{Label: "Alpha", Value: "alpha"},
		{Label: "Bravo", Value: "bravo"},
		{Label: "Charlie", Value: "charlie"},
		{Label: "Delta", Value: "delta"},
	}}, nil)
	b := startBehaviorServer(t, string(box))
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, comboboxLoaded) {
		t.Fatal("the module never loaded")
	}
	var afterFilterActive, afterDownActive, afterDownHidden, afterEnterValue, afterEnterExpanded string
	if err := chromedp.Run(ctx,
		// Type "cha": Alpha/Bravo/Delta hide, only Charlie matches.
		chromedp.Evaluate(`(() => {
			const el = document.getElementById('pick');
			el.focus();
			el.value = 'cha';
			el.dispatchEvent(new Event('input', { bubbles: true }));
		})()`, nil),
	); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `!document.getElementById('pick-listbox-opt-2').hidden &&
		document.getElementById('pick-listbox-opt-0').hidden`) {
		t.Fatal("the filter did not reduce the list to Charlie")
	}
	press := func(k string) {
		t.Helper()
		if err := chromedp.Run(ctx, chromedp.Evaluate(
			`document.activeElement.dispatchEvent(new KeyboardEvent('keydown',{key:'`+k+`',bubbles:true,cancelable:true}))`, nil)); err != nil {
			t.Fatal(err)
		}
	}
	read := func(expr string, dst *string) {
		t.Helper()
		if err := chromedp.Run(ctx, chromedp.Evaluate(expr, dst)); err != nil {
			t.Fatal(err)
		}
	}
	read(`document.getElementById('pick').getAttribute('aria-activedescendant')`, &afterFilterActive)
	// ArrowDown twice with one visible option: both presses wrap back
	// to Charlie.
	press("ArrowDown")
	press("ArrowDown")
	read(`document.getElementById('pick').getAttribute('aria-activedescendant')`, &afterDownActive)
	read(`String(document.getElementById('pick-listbox-opt-2').hidden)`, &afterDownHidden)
	// Enter picks Charlie.
	press("Enter")
	read(`document.getElementById('pick').value`, &afterEnterValue)
	read(`document.getElementById('pick').getAttribute('aria-expanded')`, &afterEnterExpanded)
	if afterFilterActive != "pick-listbox-opt-2" {
		t.Fatalf("filter: aria-activedescendant = %q, want Charlie", afterFilterActive)
	}
	if afterDownActive != "pick-listbox-opt-2" {
		t.Fatalf("ArrowDown x2: aria-activedescendant = %q, want Charlie (keyboard nav walked a hidden filtered-out row)", afterDownActive)
	}
	if afterDownHidden != "false" {
		t.Fatalf("ArrowDown x2: highlighted option hidden = %s, want false", afterDownHidden)
	}
	if afterEnterValue != "charlie" {
		t.Fatalf("Enter: input value = %q, want charlie (Enter selected a filtered-away option)", afterEnterValue)
	}
	if afterEnterExpanded != "false" {
		t.Fatalf("Enter: aria-expanded = %q, want false", afterEnterExpanded)
	}
}
