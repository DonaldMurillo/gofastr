package packcmd

import (
	"os/exec"
	"reflect"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appgen"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
	"github.com/DonaldMurillo/gofastr/framework"
)

// displayStatesYAML is one entity whose display: and states: set every key
// the YAML can express, plus a second entity covering states.advisory with
// a minimal move. TestDisplayStatesRoundTripThroughGo decodes it and checks
// the emitted registration; packTree builds the same entities (packTreeYAML
// carries displayStatesEntities), registers them on a real App, and
// compares the registered Display and States to the decoded declaration:
// any field the emitter drops, renames or invents fails there, not in a
// screen.
const displayStatesYAML = `app:
  name: DS
  module: example.com/dstest
entities:
` + displayStatesEntities

// displayStatesEntities is displayStatesYAML's entity list.
const displayStatesEntities = `  - name: invoices
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
      title_fields: [number, issued_on]
      description: One billed order.
      columns: [number, amount, status, issued_on]
      nav:
        group: billing
        icon: receipt
        order: 2
        hide: true
        hide_count: true
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
	bp, err := decodeAppSpecString(displayStatesYAML)
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
	src, err := appgen.RenderEntityRegistration(*invoices)
	if err != nil {
		t.Fatalf("renderEntityRegistration: %v", err)
	}
	for _, want := range []string{
		"States: &framework.StatesConfig{",
		"Display: &framework.DisplayConfig{",
		`{Key: "mark_paid", Label: "Mark paid"`,
		"Hide: true, HideCount: true",
		"As: \"cards\"",
		"Default: true",
		"Collapsed: true",
		"NoDuplicate: true",
		"Advisory: true",
	} {
		// Advisory lives on the tags registration, not invoices.
		if want == "Advisory: true" {
			tagSrc, err := appgen.RenderEntityRegistration(*tags)
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

	// The compile-and-register leg: the emitted entity files must build
	// and, once registered, hold Display and States exactly equal to what
	// the YAML decoded to. packTree builds them, with the probe
	// (displayStatesProbe) beside them, from the same entity text.
	clitest.VariantBuild(t)
	shared, err := decodeAppSpecString(packTreeYAML)
	if err != nil {
		t.Fatalf("parse packTreeYAML: %v", err)
	}
	for _, want := range []*framework.EntityDeclaration{invoices, tags} {
		if got := packTreeEntity(shared.Entities, want.Name); got == nil || !reflect.DeepEqual(*got, *want) {
			t.Fatalf("packTreeYAML's %s is not displayStatesYAML's:\n got: %+v\nwant: %+v", want.Name, got, *want)
		}
	}
	dir, _ := packTree(t)
	cmd := exec.Command("go", "test", "-short", "-mod=mod", "-run", "^TestRegisteredDisplayStatesMatchDeclaration$", ".")
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated display/states round-trip test failed: %v\n%s", err, output)
	}
}
