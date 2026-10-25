package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/generatecmd"
)

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
	dir, _ := generateBaseProject(t, clitest.AddScreenBaseAppSpec("example.com/addtest"))
	snap := filepath.Join(dir, "signup.aria.yml")
	if err := os.WriteFile(snap, []byte(a11ySignupSnapshot), 0o600); err != nil {
		t.Fatal(err)
	}
	var out string
	code := clitest.CovT_capExit(t, func() {
		out = clitest.CovT_capStdout(t, func() { generatecmd.RunGenerate([]string{"screen", "signup", "--from-a11y=" + snap}) })
	})
	if code != -1 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	src := inlinePhrases(clitest.ReadFile(t, dir, "screen_signup.go"))
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
	clitest.BuildGenerated(t, dir)
}
