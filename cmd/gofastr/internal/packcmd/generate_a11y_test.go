package packcmd

import (
	"reflect"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

// controlBlocksBody is the about screen's body: one of each control
// block the YAML can declare. packTreeYAML carries it too.
const controlBlocksBody = `    body:
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

func TestAppSpecYAMLControlBlocksBuild(t *testing.T) {
	yml := clitest.AddScreenBaseAppSpec("example.com/addtest") + controlBlocksBody
	dir, _ := generateBaseProject(t, yml)
	src := inlinePhrases(clitest.ReadFile(t, dir, "screen_about.go"))
	for _, want := range []string{
		`ui.Form(ui.FormConfig{Action: "/contact", SubmitLabel: "Send", Ctx: ctx}`,
		`ui.TextField(ui.TextFieldConfig{Name: "email", Label: "Email", Required: true})`,
		`[]ui.SelectOption{{Value: "Sales", Text: "Sales"}, {Value: "Support", Text: "Support"}}`,
		`{Value: "free", Label: "Free", Checked: true}`,
		`html.OrderedList(`,
		`ui.Button(ui.ButtonConfig{Label: "Cancel", Variant: ui.ButtonGhost, Type: "button", Ctx: ctx})`,
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
	// The build: packTree compiles every package of an app whose about
	// screen is this one.
	clitest.VariantBuild(t)
	mine, err := decodeAppSpecString(yml)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	shared, err := decodeAppSpecString(packTreeYAML)
	if err != nil {
		t.Fatalf("parse packTreeYAML: %v", err)
	}
	if got, want := packTreeScreen(shared, "about"), packTreeScreen(mine, "about"); want == nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("packTreeYAML's about screen is not this test's:\n got: %+v\nwant: %+v", got, want)
	}
	packTree(t)
}
