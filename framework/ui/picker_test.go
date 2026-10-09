package ui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

func TestPickerRendersAFieldAroundAPickCombobox(t *testing.T) {
	h := string(Picker(PickerConfig{
		Name: "customer_id", Label: "Customer", Help: "Who pays.", Required: true,
		Value: "c1", ValueLabel: "Ada", Endpoint: "/api/invoices/_options/customer_id",
		Options: []PickerOption{{Value: "c1", Label: "Ada"}, {Value: "c2", Label: "Grace", Meta: "Globex"}},
		More:    "Showing 2 of 40. Type to find others.",
		Action:  render.HTML(`<a href="/new">New</a>`),
	}))
	for _, want := range []string{
		`for="pick-customer_id"`,
		`name="customer_id" type="hidden" value="c1"`,
		`aria-required="true"`,
		`aria-describedby="`,
		`value="Ada"`,
		`data-cui-rpc="/api/invoices/_options/customer_id"`,
		`data-cui-signal="pick-customer_id"`,
		`aria-disabled="true"`,
		`Showing 2 of 40. Type to find others.`,
		`<a href="/new">New</a>`,
	} {
		if !strings.Contains(h, want) {
			t.Errorf("picker missing %q:\n%s", want, h)
		}
	}
	// One label: the field's.
	if n := strings.Count(h, "<label"); n != 1 {
		t.Errorf("want one label, got %d:\n%s", n, h)
	}
}

func TestPickerRowsUseThePickerIDs(t *testing.T) {
	h := string(PickerRows("pick-customer_id", []PickerOption{{Value: "c2", Label: "Grace"}}, ""))
	if !strings.Contains(h, `id="pick-customer_id-listbox-opt-0"`) || !strings.Contains(h, `data-value="c2"`) {
		t.Errorf("rows: %s", h)
	}
}
