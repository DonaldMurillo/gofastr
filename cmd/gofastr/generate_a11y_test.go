package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestAriaParseEntryShapes(t *testing.T) {
	src := `
- heading "Create account" [level=1]
- link "Docs: API":
  - /url: /docs/api
- 'button "Save: now"'
- checkbox "Remember me" [checked]
- paragraph: "Text: with colon"
- heading /Wel+come/ [level=2]
- list:
  - listitem: Coffee
`
	nodes, err := parseAriaSnapshot(src)
	if err != nil {
		t.Fatal(err)
	}
	type row struct{ Role, Name, Text string }
	var got []row
	for _, n := range nodes {
		got = append(got, row{n.Role, n.Name, n.Text})
	}
	want := []row{
		{"heading", "Create account", ""},
		{"link", "Docs: API", ""},
		{"button", "Save: now", ""},
		{"checkbox", "Remember me", ""},
		{"paragraph", "", "Text: with colon"},
		{"heading", "Wel+come", ""},
		{"list", "", ""},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("entries:\n got %+v\nwant %+v", got, want)
	}
	if nodes[0].Attrs["level"] != "1" || nodes[3].Attrs["checked"] != "true" {
		t.Errorf("attrs: heading %v, checkbox %v", nodes[0].Attrs, nodes[3].Attrs)
	}
	if nodes[1].Props["url"] != "/docs/api" {
		t.Errorf("link /url property = %q", nodes[1].Props["url"])
	}
	if li := nodes[6].Children; len(li) != 1 || li[0].Text != "Coffee" {
		t.Errorf("list children = %+v", li)
	}
}

func TestAriaParseStripsControlBytes(t *testing.T) {
	nodes, err := parseAriaSnapshot("- button \"Pay\\u202enow\\u001b[2J\"")
	if err != nil {
		t.Fatal(err)
	}
	if got := nodes[0].Name; got != "Paynow[2J" {
		t.Fatalf("name = %q, want bidi and ESC stripped", got)
	}
}

func TestAriaParseRejectsBadInput(t *testing.T) {
	cases := map[string]string{
		"not a list":     "heading \"x\"",
		"child of value": "- paragraph: hi\n  - text: nested",
		"orphan prop":    "- /url: /x",
		"open quote":     "- button \"Save",
		"open attr":      "- heading \"x\" [level=1",
		"no role":        "- \"\": x",
	}
	for name, src := range cases {
		if _, err := parseAriaSnapshot(src); err == nil {
			t.Errorf("%s: parsed %q without error", name, src)
		}
	}
	deep := strings.Builder{}
	for i := 0; i <= ariaSnapshotMaxDepth+1; i++ {
		deep.WriteString(strings.Repeat(" ", i*2) + "- generic:\n")
	}
	if _, err := parseAriaSnapshot(deep.String()); err == nil || !strings.Contains(err.Error(), "depth") {
		t.Errorf("deep nesting: err = %v, want a depth error", err)
	}
}

func TestA11yMapsSignupToForm(t *testing.T) {
	nodes, err := parseAriaSnapshot(`
- banner:
  - link "Acme":
    - /url: /
- main:
  - heading "Create account" [level=1]
  - generic:
    - text: "Email:"
    - textbox "Email"
  - checkbox "Remember me" [checked]
  - button "Sign up"
`)
	if err != nil {
		t.Fatal(err)
	}
	blocks, warnings := mapA11yToBlocks(nodes, "/signup")
	if len(blocks) != 2 {
		t.Fatalf("want page_header + form, got %+v", blocks)
	}
	if blocks[0].Kind != "page_header" || blocks[0].Props["title"] != "Create account" {
		t.Errorf("first block = %+v", blocks[0])
	}
	form := blocks[1]
	if form.Kind != "custom_form" || form.Props["action"] != "/signup" || form.Props["submit"] != "Sign up" {
		t.Fatalf("form = %+v", form)
	}
	var kinds []string
	for _, c := range form.Children {
		kinds = append(kinds, c.Kind)
	}
	if !reflect.DeepEqual(kinds, []string{"text_field", "checkbox"}) {
		t.Errorf("form fields = %v (the label text beside the textbox is dropped)", kinds)
	}
	if form.Children[1].Props["checked"] != true || form.Children[0].Props["name"] != "email" {
		t.Errorf("field props: %+v", form.Children)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "banner") {
		t.Errorf("warnings = %q, want the skipped banner", warnings)
	}
}

func TestA11yMapsTableListNav(t *testing.T) {
	nodes, err := parseAriaSnapshot(`
- navigation "Sections":
  - list:
    - listitem:
      - link "Intro":
        - /url: "#intro"
    - listitem:
      - link "Bad":
        - /url: javascript:alert(1)
- table "Users":
  - rowgroup:
    - row "Name Role":
      - columnheader "Name"
      - columnheader "Role"
  - rowgroup:
    - row "Ada Admin":
      - cell "Ada"
      - cell "Admin"
- list:
  - listitem: Coffee
  - listitem: Tea
- img "Logo"
- radiogroup "Plan":
  - radio "Free" [checked]
  - radio "Pro"
`)
	if err != nil {
		t.Fatal(err)
	}
	blocks, warnings := mapA11yToBlocks(nodes, "/x")
	var kinds []string
	for _, b := range blocks {
		kinds = append(kinds, b.Kind)
	}
	if !reflect.DeepEqual(kinds, []string{"nav_links", "data_table", "item_list", "custom_form"}) {
		t.Fatalf("kinds = %v", kinds)
	}
	if blocks[0].Props["label"] != "Sections" {
		t.Errorf("nav label = %v, want the landmark's name", blocks[0].Props["label"])
	}
	nav := blocks[0].Children
	if len(nav) != 2 || nav[0].Href != "#intro" || nav[1].Href != "#" {
		t.Errorf("nav links = %+v (the javascript: href must be dropped)", nav)
	}
	tbl := blocks[1].Props
	if !reflect.DeepEqual(tbl["columns"], []any{"Name", "Role"}) || !reflect.DeepEqual(tbl["rows"], []any{[]any{"Ada", "Admin"}}) {
		t.Errorf("table props = %+v", tbl)
	}
	if got := blocks[2].Children; len(got) != 2 || got[1].Text != "Tea" {
		t.Errorf("list items = %+v", got)
	}
	radio := blocks[3].Children[0]
	if radio.Kind != "radio_group" || radio.Props["legend"] != "Plan" {
		t.Errorf("radio group = %+v", radio)
	}
	joined := strings.Join(warnings, "\n")
	for _, want := range []string{"unsafe href", "img"} {
		if !strings.Contains(joined, want) {
			t.Errorf("warnings missing %q:\n%s", want, joined)
		}
	}
}

func TestA11yFieldNamesUnique(t *testing.T) {
	m := newA11yMapper("/x")
	got := []string{m.fieldName("Email"), m.fieldName("E-mail"), m.fieldName("Email"), m.fieldName("2FA code"), m.fieldName("☃"),
		m.fieldName("a"), m.fieldName("a"), m.fieldName("a 2")}
	want := []string{"email", "e_mail", "email_2", "field_2fa_code", "field", "a", "a_2", "a_2_2"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("names = %v, want %v", got, want)
	}
}

func TestGenerateScreenDispatches(t *testing.T) {
	dir := scaffoldEmptyDir(t)
	code := covT_capExit(t, func() {
		covT_capStdout(t, func() { runGenerate([]string{"screen", "contact"}) })
	})
	if code != -1 {
		t.Fatalf("`generate screen contact` exited %d", code)
	}
	if !fileExists(dir, "screen_contact.go") {
		t.Fatal("screen_contact.go not written")
	}
}

const a11ySignupSnapshot = `- main:
  - heading "Create account" [level=1]
  - paragraph: Start your free trial.
  - textbox "Email"
  - textbox "Password"
  - combobox "Country":
    - option "Canada" [selected]
    - option "Mexico"
  - radiogroup "Plan":
    - radio "Free" [checked]
    - radio "Pro"
  - spinbutton "Seats"
  - switch "Newsletter"
  - checkbox "I agree" [checked]
  - button "Sign up"
  - separator
  - heading "Recent signups" [level=2]
  - table "Recent signups":
    - row:
      - columnheader "Name"
      - columnheader "Plan"
    - row:
      - cell "Ada"
      - cell "Pro"
  - list:
    - listitem:
      - link "Terms":
        - /url: /terms
    - listitem: No card needed
  - alert: Trial ends in 14 days.
`

func TestGenerateScreenFromA11yBuilds(t *testing.T) {
	dir, _ := generateBaseProject(t, addScreenBaseBlueprint("example.com/addtest"))
	snap := filepath.Join(dir, "signup.aria.yml")
	if err := os.WriteFile(snap, []byte(a11ySignupSnapshot), 0o600); err != nil {
		t.Fatal(err)
	}
	var out string
	code := covT_capExit(t, func() {
		out = covT_capStdout(t, func() { runGenerate([]string{"screen", "signup", "--from-a11y=" + snap}) })
	})
	if code != -1 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	src := readFile(t, dir, "screen_signup.go")
	for _, want := range []string{
		`ui.PageHeader(ui.PageHeaderConfig{Title: "Create account", Subtitle: "Start your free trial."`,
		`ui.Form(ui.FormConfig{Action: "/signup"`,
		`SubmitLabel: "Sign up"`,
		`ui.TextField(ui.TextFieldConfig{Name: "password", Label: "Password"`,
		`ui.Select(ui.SelectConfig{Name: "country", Label: "Country"`,
		`ui.RadioGroup(ui.RadioGroupConfig{Name: "plan", Legend: "Plan"`,
		`ui.NumberField(`,
		`ui.Switch(`,
		`ui.Checkbox(ui.ToggleConfig{Name: "i_agree", Label: "I agree", Checked: true`,
		`ui.DataTable(ui.DataTableConfig{Caption: "Recent signups", CaptionHidden: true`,
		`ui.Link(ui.LinkConfig{Href: "/terms", Text: "Terms"`,
		`html.UnorderedList(`,
		`ui.Callout(`,
		`ui.Divider(`,
	} {
		if !strings.Contains(src, want) {
			t.Errorf("screen_signup.go missing %s", want)
		}
	}
	if t.Failed() {
		t.Logf("screen_signup.go:\n%s", src)
	}
	buildGenerated(t, dir)
}

func TestGenerateScreenFromA11yErrors(t *testing.T) {
	dir := scaffoldEmptyDir(t)
	bad := filepath.Join(dir, "bad.yml")
	if err := os.WriteFile(bad, []byte("- button \"Save"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"screen", "x", "--from-a11y=" + bad},
		{"screen", "x", "--from-a11y=" + filepath.Join(dir, "missing.yml")},
		{"screen", "x", "--from-a11y"},
		{"entity", "x", "--from-a11y=" + bad},
	} {
		code := covT_capExit(t, func() {
			covT_capStdout(t, func() { runGenerate(args) })
		})
		if code != 1 {
			t.Errorf("%v: exit %d, want 1", args, code)
		}
	}
	if fileExists(dir, "screen_x.go") {
		t.Error("a failed snapshot read still wrote screen_x.go")
	}
}

func TestBlueprintControlBlocksNeedLabels(t *testing.T) {
	cases := []BlueprintBlock{
		{Kind: "action_button"},
		{Kind: "text_field", Props: map[string]any{"label": "Email"}},
		{Kind: "checkbox", Props: map[string]any{"name": "x"}},
		{Kind: "select_field", Props: map[string]any{"label": "A", "name": "a"}},
		{Kind: "radio_group", Props: map[string]any{"name": "a", "options": []any{"x"}}},
		{Kind: "custom_form"},
		{Kind: "custom_form", Props: map[string]any{"action": "javascript:alert(1)"}},
		{Kind: "data_table", Props: map[string]any{"caption": "T"}},
		{Kind: "data_table", Props: map[string]any{"columns": []any{"A"}, "caption_hidden": true}},
		{Kind: "data_table", Props: map[string]any{"columns": []any{"A"}, "rows": []any{[]any{"1", "2"}}}},
		{Kind: "action_button", Props: map[string]any{"label": "x", "variant": "loud"}},
		{Kind: "action_button", Props: map[string]any{"label": "x", "type": "image"}},
		{Kind: "custom_form", Props: map[string]any{"action": "/s", "method": "put"}},
		{Kind: "select_field", Props: map[string]any{"label": "A", "name": "a", "options": []any{map[string]any{"label": "A", "value": 1}}}},
		{Kind: "select_field", Props: map[string]any{"label": "A", "name": "a", "options": []any{map[string]any{"label": "A", "selected": "yes"}}}},
		{Kind: "nav_links"},
		{Kind: "custom_form", Props: map[string]any{"action": "/a"}, Children: []BlueprintBlock{{Kind: "custom_form", Props: map[string]any{"action": "/b"}}}},
		{Type: "div", Children: []BlueprintBlock{{Kind: "custom_form", Props: map[string]any{"action": "/b"}}}},
		{Type: "div", Children: []BlueprintBlock{{Kind: "section", Children: []BlueprintBlock{{Kind: "card"}}}}},
		{Type: "link", Href: "javascript:alert(1)", Text: "x"},
		{Type: "link", Href: "//evil.example", Text: "x"},
	}
	for _, b := range cases {
		if err := validateBlueprintBlock("s", nil, b); err == nil {
			t.Errorf("%s %v validated", b.Kind, b.Props)
		}
	}
	ok := BlueprintBlock{Kind: "custom_form", Props: map[string]any{"action": "/s"}, Children: []BlueprintBlock{
		{Kind: "text_field", Props: map[string]any{"label": "Email", "name": "email"}},
		{Kind: "select_field", Props: map[string]any{"label": "A", "name": "a", "options": []any{"x", map[string]any{"label": "Y", "value": "y"}}}},
	}}
	if err := validateBlueprintBlock("s", nil, ok); err != nil {
		t.Errorf("valid form rejected: %v", err)
	}
	// section is a node kind too: the node renderer draws it.
	nodeSection := BlueprintBlock{Type: "div", Children: []BlueprintBlock{{Kind: "section", Children: []BlueprintBlock{{Type: "text", Text: "x"}}}}}
	if err := validateBlueprintBlock("s", nil, nodeSection); err != nil {
		t.Errorf("section under a div rejected: %v", err)
	}
}

func TestBlueprintYAMLControlBlocksBuild(t *testing.T) {
	yml := addScreenBaseBlueprint("example.com/addtest") + `    body:
      - kind: custom_form
        props:
          action: /contact
          submit: Send
        children:
          - kind: text_field
            props:
              label: Email
              name: email
              required: true
          - kind: select_field
            props:
              label: Topic
              name: topic
              options: [Sales, Support]
          - kind: radio_group
            props:
              legend: Plan
              name: plan
              options:
                - label: Free
                  value: free
                  checked: true
                - label: Pro
                  value: pro
      - kind: data_table
        props:
          caption: People
          columns: [Name]
          rows:
            - [Ada]
      - kind: item_list
        props:
          ordered: true
        children:
          - type: link
            href: /terms
            text: Terms
      - kind: action_button
        props:
          label: Cancel
          variant: Ghost
      - kind: nav_links
        props:
          label: More
        children:
          - type: link
            href: /help
            text: Help
      - kind: data_table
        props:
          caption: People
          id: people-2
          columns: [Name]
      - kind: custom_form
        props:
          action: /search
          method: GET
          hide_submit: true
`
	dir, _ := generateBaseProject(t, yml)
	src := readFile(t, dir, "screen_about.go")
	for _, want := range []string{
		`ui.Form(ui.FormConfig{Action: "/contact", SubmitLabel: "Send", Ctx: ctx}`,
		`ui.TextField(ui.TextFieldConfig{Name: "email", Label: "Email", Required: true})`,
		`[]ui.SelectOption{{Value: "Sales", Text: "Sales"}, {Value: "Support", Text: "Support"}}`,
		`{Value: "free", Label: "Free", Checked: true}`,
		`html.OrderedList(`,
		`ui.Button(ui.ButtonConfig{Label: "Cancel", Variant: ui.ButtonGhost, Type: "button"})`,
		`html.Nav(html.NavConfig{Label: "More"}, ui.Cluster(ui.ClusterConfig{Gap: ui.GapMD}`,
		`ID: "people-2"`,
		`Method: "GET"`,
	} {
		if !strings.Contains(src, want) {
			t.Errorf("screen_about.go missing %s", want)
		}
	}
	if t.Failed() {
		t.Logf("screen_about.go:\n%s", src)
	}
	buildGenerated(t, dir)
}

// a11yRun runs `gofastr generate` with stdout and stderr captured, both
// kept when the command exits.
func a11yRun(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	outF, err := os.CreateTemp(t.TempDir(), "out-*")
	if err != nil {
		t.Fatal(err)
	}
	errF, err := os.CreateTemp(t.TempDir(), "err-*")
	if err != nil {
		t.Fatal(err)
	}
	oldOut, oldErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = outF, errF
	code = covT_capExit(t, func() { runGenerate(args) })
	os.Stdout, os.Stderr = oldOut, oldErr
	read := func(f *os.File) string {
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			t.Fatal(err)
		}
		b, err := io.ReadAll(f)
		if err != nil {
			t.Fatal(err)
		}
		f.Close()
		return string(b)
	}
	return code, read(outF), read(errF)
}

func a11yMap(t *testing.T, src string) ([]BlueprintBlock, string) {
	t.Helper()
	nodes, err := parseAriaSnapshot(src)
	if err != nil {
		t.Fatal(err)
	}
	blocks, warnings := mapA11yToBlocks(nodes, "/x")
	return blocks, strings.Join(warnings, "\n")
}

// a11yFind collects every block of a kind, at any depth.
func a11yFind(blocks []BlueprintBlock, kind string) []BlueprintBlock {
	var out []BlueprintBlock
	for _, b := range blocks {
		if b.Kind == kind {
			out = append(out, b)
		}
		out = append(out, a11yFind(b.Children, kind)...)
	}
	return out
}

func TestA11yHelpTextKeepsOneForm(t *testing.T) {
	blocks, _ := a11yMap(t, `- main:
  - heading "Sign up" [level=1]
  - generic:
    - generic:
      - text: Email
      - textbox "Email"
      - paragraph: We never share your email.
    - generic:
      - text: Password
      - textbox "Password"
      - text: At least 8 characters.
    - button "Create account"
  - paragraph:
    - text: Already a member?
    - link "Sign in":
      - /url: /login
`)
	forms := a11yFind(blocks, "custom_form")
	if len(forms) != 1 {
		t.Fatalf("want one form, got %d: %+v", len(forms), blocks)
	}
	if forms[0].Props["submit"] != "Create account" || forms[0].Props["hide_submit"] != nil {
		t.Errorf("form props = %+v, want the button as its submit", forms[0].Props)
	}
	if n := len(a11yFind(blocks, "action_button")); n != 0 {
		t.Errorf("%d stray buttons outside the form", n)
	}
	var texts []string
	for _, c := range forms[0].Children {
		if c.Type == "text" {
			texts = append(texts, c.Text)
		}
	}
	if !reflect.DeepEqual(texts, []string{"We never share your email.", "At least 8 characters."}) {
		t.Errorf("help text in the form = %q", texts)
	}
}

func TestA11yFormsNeverNest(t *testing.T) {
	blocks, warnings := a11yMap(t, `- form "Checkout":
  - textbox "Full name"
  - group "Shipping":
    - radio "Fast"
    - radio "Slow" [checked]
  - group "Billing address":
    - textbox "Street"
    - form "Inner":
      - textbox "City"
  - button "Pay"
`)
	if n := len(a11yFind(blocks, "custom_form")); n != 1 {
		t.Fatalf("want 1 form, got %d: %+v", n, blocks)
	}
	radios := a11yFind(blocks, "radio_group")
	if len(radios) != 1 || radios[0].Props["legend"] != "Shipping" {
		t.Errorf("radio group = %+v, want the fieldset's name as legend", radios)
	}
	if strings.Contains(warnings, "Choose one") {
		t.Errorf("legend fallback used: %s", warnings)
	}
	if !strings.Contains(warnings, "inside another form") {
		t.Errorf("nested form not reported: %s", warnings)
	}
}

func TestA11yFieldsetJoinsImplicitForm(t *testing.T) {
	blocks, _ := a11yMap(t, `- textbox "Name"
- group "Size":
  - text: Size
  - radio "S"
  - radio "M"
- button "Order"
`)
	if len(blocks) != 1 || blocks[0].Kind != "custom_form" || blocks[0].Props["submit"] != "Order" {
		t.Fatalf("blocks = %+v, want one form holding the fieldset", blocks)
	}
}

func TestA11yUnnamedRadiosSkipGroup(t *testing.T) {
	blocks, warnings := a11yMap(t, `- heading "Pick" [level=1]
- radiogroup "Size":
  - radio
  - radio
- radiogroup
`)
	if n := len(a11yFind(blocks, "radio_group")); n != 0 {
		t.Errorf("emitted %d radio groups with no options", n)
	}
	if !strings.Contains(warnings, "no named radios") {
		t.Errorf("warnings = %s", warnings)
	}
}

func TestA11yDuplicateRadioValues(t *testing.T) {
	blocks, _ := a11yMap(t, `- radiogroup "Agree":
  - radio "Yes"
  - radio "Yes"
`)
	rg := a11yFind(blocks, "radio_group")
	if len(rg) != 1 {
		t.Fatalf("blocks = %+v", blocks)
	}
	choices, err := blueprintChoices(rg[0])
	if err != nil || len(choices) != 2 || choices[0].Value == choices[1].Value {
		t.Errorf("choices = %+v, %v; want distinct values", choices, err)
	}
}

func TestA11yAlertKeepsBody(t *testing.T) {
	blocks, _ := a11yMap(t, `- alert "Note": Trial ends in 14 days.
- alert: Saved.
`)
	if blocks[0].Text != "Trial ends in 14 days." || blocks[0].Props["title"] != "Note" {
		t.Errorf("named alert = %+v", blocks[0])
	}
	if blocks[1].Text != "Saved." || blocks[1].Props["title"] != "" {
		t.Errorf("plain alert = %+v", blocks[1])
	}
}

func TestA11yTablesGetDistinctIDs(t *testing.T) {
	blocks, warnings := a11yMap(t, `- table:
  - row:
    - cell "a"
- table:
  - row:
    - cell "b"
- table "Users":
  - row:
    - cell "c"
- table "Users":
  - row:
    - cell "d"
`)
	tables := a11yFind(blocks, "data_table")
	if len(tables) != 4 {
		t.Fatalf("tables = %+v", tables)
	}
	if tables[0].Props["caption_hidden"] != true {
		t.Errorf("stand-in caption shown on screen: %+v", tables[0].Props)
	}
	ids := map[string]bool{}
	for _, tb := range tables {
		id := blueprintProp(tb, "id")
		if id == "" {
			id = "caption:" + strings.ToLower(blueprintProp(tb, "caption"))
		}
		if ids[id] {
			t.Errorf("two tables share %q", id)
		}
		ids[id] = true
	}
	if !strings.Contains(warnings, "no accessible name") {
		t.Errorf("unnamed table not reported: %s", warnings)
	}
}

func TestA11yLossyMappingsWarn(t *testing.T) {
	_, warnings := a11yMap(t, `- navigation "Pager":
  - button "Previous page"
  - text: Page 2 of 9
  - link "Next":
    - /url: /p/3
- table "Users":
  - row:
    - cell:
      - link "Ada":
        - /url: /users/1
- textbox "Email": ada@example.com
- radiogroup "Size":
  - radio "S"
  - text: Pick one
`)
	for _, want := range []string{"Previous page", "Page 2 of 9", "links or buttons", "ada@example.com", "Pick one"} {
		if !strings.Contains(warnings, want) {
			t.Errorf("warnings miss %q:\n%s", want, warnings)
		}
	}
}

func TestA11yHeadingLevels(t *testing.T) {
	blocks, warnings := a11yMap(t, `- region "Intro":
  - heading "Welcome" [level=1]
  - paragraph: Hi.
- heading [level=2]
- heading "Again" [level=1]
`)
	if blocks[0].Kind != "page_header" || blocks[0].Props["title"] != "Welcome" {
		t.Fatalf("first block = %+v, want the region's h1 as page header", blocks[0])
	}
	if blocks[1].Kind != "section" || blocks[1].Props["label"] != "Intro" {
		t.Errorf("section = %+v", blocks[1])
	}
	last := blocks[len(blocks)-1]
	if last.Type != "heading" || last.Level != 2 || last.Text != "Again" {
		t.Errorf("second h1 = %+v, want level 2", last)
	}
	if len(blocks) != 3 || !strings.Contains(warnings, "no text") {
		t.Errorf("empty heading kept: %+v\n%s", blocks, warnings)
	}
}

func TestA11yNestedListStacks(t *testing.T) {
	blocks, _ := a11yMap(t, `- list:
  - listitem:
    - text: Fruit
    - list:
      - listitem: Apple
`)
	item := blocks[0].Children[0]
	if item.Kind != "stack" {
		t.Errorf("list item with a sub-list = %+v, want a stack", item)
	}
}

func TestAriaErrorsQuoteInput(t *testing.T) {
	for _, src := range []string{
		"- button \"\x1b]0;pwned\x07 \u202e x",
		"- button \"a\\q\"",
		"- heading /" + strings.Repeat("x", 1<<20),
	} {
		_, err := parseAriaSnapshot(src)
		if err == nil {
			t.Fatalf("parsed %q", src[:20])
		}
		msg := err.Error()
		if strings.ContainsAny(msg, "\x1b\x07\u202e") || len(msg) > 300 {
			t.Errorf("error carries raw input (%d bytes): %q", len(msg), msg[:min(len(msg), 120)])
		}
	}
}

func TestAriaParseBOMAndBareCR(t *testing.T) {
	nodes, err := parseAriaSnapshot("\uFEFF- heading \"A\" [level=1]\r- paragraph: b\r")
	if err != nil || len(nodes) != 2 {
		t.Fatalf("nodes = %+v, err = %v", nodes, err)
	}
}

func TestAriaParseNodeCap(t *testing.T) {
	src := strings.Repeat("- text: x\n", ariaSnapshotMaxNodes+1)
	if _, err := parseAriaSnapshot(src); err == nil || !strings.Contains(err.Error(), "entries") {
		t.Errorf("err = %v, want the node cap", err)
	}
}

func TestA11ySnapshotSizeCap(t *testing.T) {
	path := filepath.Join(t.TempDir(), "big.yml")
	big := strings.Repeat("- text: x\n", a11ySnapshotMaxBytes/10+1)
	if err := os.WriteFile(path, []byte(big), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := a11yScreenBody(path, "/x", true); err == nil || !strings.Contains(err.Error(), "larger") {
		t.Errorf("err = %v, want the size cap", err)
	}
}

func TestFromA11yJSONStaysParseable(t *testing.T) {
	dir := scaffoldEmptyDir(t)
	bad := filepath.Join(dir, "bad.yml")
	if err := os.WriteFile(bad, []byte("- button \"Save"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, out, _ := a11yRun(t, "screen", "x", "--from-a11y="+bad, "--dry-run", "--json")
	var doc struct{ Errors []any }
	if code != 1 || json.Unmarshal([]byte(out), &doc) != nil || len(doc.Errors) == 0 {
		t.Errorf("exit %d, stdout not the JSON error shape:\n%s", code, out)
	}
	good := filepath.Join(dir, "good.yml")
	if err := os.WriteFile(good, []byte("- heading \"Hi\" [level=1]\n- img \"Logo\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, out, errOut := a11yRun(t, "screen", "x", "--from-a11y="+good, "--dry-run", "--json")
	if code != -1 || !json.Valid([]byte(out)) || !strings.Contains(errOut, "warning: skipped img") {
		t.Errorf("exit %d; stdout %q; stderr %q; want JSON out and the warning on stderr", code, out, errOut)
	}
}
