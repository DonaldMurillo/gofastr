package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
)

// displayStatesYAML is one entity whose display: and states: set every key
// the YAML can express, plus a second entity covering states.advisory with
// a minimal move. TestDisplayStatesRoundTripThroughGo decodes it, generates
// the entity files, compiles them in a temp module, registers them on a
// real App, and compares the registered Display and States to the decoded
// declaration: any field the emitter drops, renames or invents fails there,
// not in a screen.
const displayStatesYAML = `app:
  name: DS
  module: example.com/dstest
entities:
  - name: invoices
    crud: true
    search_fields: [number]
    fields:
      - name: number
        type: string
        required: true
      - name: amount
        type: decimal
      - name: status
        type: enum
        values: [draft, open, paid, void]
        default: draft
      - name: issued_on
        type: date
      - name: paid_on
        type: date
      - name: memo
        type: text
      - name: recurring
        type: bool
      - name: internal_code
        type: string
    states:
      field: status
      initial: [draft]
      transitions:
        - key: send
          label: Send
          from: [draft]
          to: open
          stamp: issued_on
        - key: mark_paid
          label: Mark paid
          from: [open]
          to: paid
          stamp: paid_on
          variant: primary
          permission: invoices:pay
        - key: void
          from: [draft, open]
          to: void
          variant: danger
        - key: archive
          from: [paid]
          to: void
          system: true
    display:
      singular: Invoice
      plural: Invoices
      title_field: number
      description: One billed order.
      columns: [number, amount, status, issued_on]
      nav:
        group: billing
        icon: receipt
        order: 2
        hide: true
      views:
        - key: open
          label: Open
          where: 'status = "open"'
          sort: issued_on ASC
          default: true
        - key: big
          where: 'amount >= 1000'
          sort: amount DESC
          as: cards
      facets: [status, recurring]
      form:
        main:
          - row: [number, amount]
          - section: dates
            help: The moves set these.
            items:
              - row: [issued_on, paid_on]
          - section: notes
            collapsed: true
            items: [memo]
        side: [status, recurring]
      card:
        title: number
        subtitle: memo
        badge: status
        meta: [amount, issued_on]
      fields:
        number:
          label: Number
          help: The invoice number
          placeholder: INV-0001
        amount:
          input: money
        memo:
          locked: true
        paid_on:
          show_when: 'status = "paid"'
        internal_code:
          omit: true
      page_sizes: [10, 25, 100]
      no_duplicate: true
      no_bulk: true
  - name: tags
    crud: true
    fields:
      - name: label
        type: string
        required: true
      - name: stage
        type: enum
        values: [new, gone]
        default: new
    states:
      field: stage
      advisory: true
      transitions:
        - key: retire
          from: [new]
          to: gone
    display:
      singular: Tag
`

func TestDisplayStatesRoundTripThroughGo(t *testing.T) {
	bp, err := decodeBlueprintString(displayStatesYAML)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	var invoices, tags *framework.EntityDeclaration
	for i := range bp.Entities {
		switch bp.Entities[i].Name {
		case "invoices":
			invoices = &bp.Entities[i]
		case "tags":
			tags = &bp.Entities[i]
		}
	}
	if invoices == nil || invoices.Display == nil || invoices.States == nil {
		t.Fatalf("fixture must decode display and states on invoices, got %+v", invoices)
	}
	if tags == nil || tags.States == nil || !tags.States.Advisory {
		t.Fatalf("fixture must decode an advisory states on tags, got %+v", tags)
	}

	// Emission drops every zero field: an invented or emptied one shows up
	// as a quoted empty string or a bare false/0 in the emitted source.
	src, err := renderEntityRegistration(*invoices)
	if err != nil {
		t.Fatalf("renderEntityRegistration: %v", err)
	}
	for _, want := range []string{
		"States: &framework.StatesConfig{",
		"Display: &framework.DisplayConfig{",
		`{Key: "mark_paid", Label: "Mark paid"`,
		"Hide: true",
		"As: \"cards\"",
		"Default: true",
		"Collapsed: true",
		"NoDuplicate: true",
		"Advisory: true",
	} {
		// Advisory lives on the tags registration, not invoices.
		if want == "Advisory: true" {
			tagSrc, err := renderEntityRegistration(*tags)
			if err != nil {
				t.Fatalf("renderEntityRegistration(tags): %v", err)
			}
			if !strings.Contains(tagSrc, want) {
				t.Fatalf("tags registration missing %s:\n%s", want, tagSrc)
			}
			continue
		}
		if !strings.Contains(src, want) {
			t.Fatalf("invoices registration missing %s:\n%s", want, src)
		}
	}
	for _, refuse := range []string{`: ""`, ": false", ": 0,"} {
		if strings.Contains(src, refuse) {
			t.Fatalf("emitted registration carries a zero field (%q):\n%s", refuse, src)
		}
	}

	// The compile-and-register leg: the emitted entity files must build in a
	// temp module and, once registered, hold Display and States exactly
	// equal to what the YAML decoded to.
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	goVersion, err := repoGoVersion(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	goMod := "module example.com/dstest\n\ngo " + goVersion +
		"\n\nrequire github.com/DonaldMurillo/gofastr v0.0.0\n\nreplace github.com/DonaldMurillo/gofastr => " + repoRoot + "\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := copyGoSum(repoRoot, dir); err != nil {
		t.Fatal(err)
	}
	bp.App.OutputDir = "gen"
	for _, file := range mustRenderBlueprintFiles(t, bp) {
		full := filepath.Join(dir, "gen", file.name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(file.content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	wantDisplay, err := json.Marshal(invoices.Display)
	if err != nil {
		t.Fatal(err)
	}
	wantStates, err := json.Marshal(invoices.States)
	if err != nil {
		t.Fatal(err)
	}
	gen := `package main

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/entity"

	"` + bp.App.Module + `/gen/entities"
)

func TestRegisteredDisplayStatesMatchDeclaration(t *testing.T) {
	app := framework.NewApp()
	entities.RegisterAll(app)

	e, err := app.Registry.Get("invoices")
	if err != nil {
		t.Fatalf("get invoices: %v", err)
	}
	var wantDisplay entity.DisplayConfig
	if err := json.Unmarshal([]byte(` + "`" + string(wantDisplay) + "`" + `), &wantDisplay); err != nil {
		t.Fatalf("decode expected display: %v", err)
	}
	if !reflect.DeepEqual(e.Config.Display, &wantDisplay) {
		t.Fatalf("Display mismatch.\n got: %#v\nwant: %#v", e.Config.Display, &wantDisplay)
	}
	var wantStates entity.StatesConfig
	if err := json.Unmarshal([]byte(` + "`" + string(wantStates) + "`" + `), &wantStates); err != nil {
		t.Fatalf("decode expected states: %v", err)
	}
	if !reflect.DeepEqual(e.Config.States, &wantStates) {
		t.Fatalf("States mismatch.\n got: %#v\nwant: %#v", e.Config.States, &wantStates)
	}

	tag, err := app.Registry.Get("tags")
	if err != nil {
		t.Fatalf("get tags: %v", err)
	}
	if tag.Config.States == nil || !tag.Config.States.Advisory {
		t.Fatalf("tags states advisory lost: %#v", tag.Config.States)
	}
}
`
	if err := os.WriteFile(filepath.Join(dir, "gen", "displaystates_test.go"), []byte(gen), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "test", "-short", "-mod=mod", "-run", "TestRegisteredDisplayStatesMatchDeclaration", "./gen")
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated display/states round-trip test failed: %v\n%s", err, output)
	}
}
