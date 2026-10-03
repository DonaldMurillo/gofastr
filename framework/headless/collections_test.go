package headless

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

// The collections' own contracts: the tag input's visible chips, the
// repeater's named submit controls, and the Islands each requires.

// Every committed value is a chip AND a hidden input: what a reader
// sees removed is what a submit stops carrying.
func TestTagInputValuesAreChipsAndInputs(t *testing.T) {
	got := TagInput(TagInputProps{Name: "tags", Label: "Tags", Values: []string{"go", "css"}}, nil)
	has(t, got, `aria-label="Remove go"`, "a chip's remove control does not name what it removes")
	has(t, got, `name="tags" type="hidden" value="go"`, "a committed value does not submit")
	has(t, got, `data-hui-tag-input-added="{name} added"`, "the added sentence did not travel from Strings")
	has(t, got, `data-hui-tag-input-removed="{name} removed"`, "the removed sentence did not travel from Strings")
	has(t, got, `aria-label="Add Tags"`, "the add control does not name its field")

	// The cap applies to rendered values too: a server value longer
	// than the cap cannot submit unchanged while a typed one is refused.
	capped := TagInput(TagInputProps{Name: "tags", Label: "Tags",
		Values: []string{"production"}, MaxLength: 4}, nil)
	has(t, capped, `value="prod"`, "the cap did not trim a server-rendered value")
}

func TestTagInputRefusesBrokenFields(t *testing.T) {
	refuse(t, "Name", func() { TagInput(TagInputProps{Label: "Tags"}, nil) })
	refuse(t, "Label", func() { TagInput(TagInputProps{Name: "tags"}, nil) })
	refuse(t, "negative", func() { TagInput(TagInputProps{Name: "tags", Label: "Tags", MaxLength: -1}, nil) })
}

// The repeater's controls are named submit buttons: the no-script page
// adds and removes rows through ordinary POSTs.
func TestRepeaterSubmitControlsAreNamed(t *testing.T) {
	got := Repeater(RepeaterProps{Name: "links", Label: "Links",
		Items:      []RepeaterItem{{Fields: []render.HTML{render.Text("")}}},
		AddName:    "links_add",
		RemoveName: "links_remove"}, nil)
	has(t, got, `name="links_add" type="submit"`, "the add control is not a named submit button")
	has(t, got, `name="links_remove" type="submit" value="0"`, "the remove control does not carry its row")
	has(t, got, `aria-label="Remove item 1"`, "a remove control does not name its row")
	has(t, got, `data-hui-repeater-index="0"`, "the row does not carry its index for the focus restore")
}

// The island path emits the framework's RPC contract on the same
// buttons, with the operation in the endpoint's query.
func TestRepeaterIslandEmitsTheRPCContract(t *testing.T) {
	got := Repeater(RepeaterProps{Name: "guests", Label: "Guests",
		Items:  []RepeaterItem{{Fields: []render.HTML{render.Text("")}}},
		Action: "/island/guests",
		Island: Island{Endpoint: "/island/guests", Signal: "guests"}}, nil)
	has(t, got, `data-fui-rpc="/island/guests?op=add"`, "the add operation is not in the endpoint's query")
	has(t, got, `data-fui-rpc="/island/guests?index=0&amp;op=remove"`, "the remove operation's row did not reach the endpoint's query")
	has(t, got, `data-fui-rpc-signal="guests"`, "the buttons do not name the region they update")

	// A plain repeater emits no RPC at all: the surrounding form is
	// the destination.
	plain := Repeater(RepeaterProps{Name: "links",
		Items: []RepeaterItem{{Fields: []render.HTML{render.Text("")}}}}, nil)
	hasNot(t, plain, "data-fui-rpc", "a plain repeater carries the RPC contract with no island")
}

func TestRepeaterRefusesBrokenConfigurations(t *testing.T) {
	refuse(t, "Name", func() { Repeater(RepeaterProps{}, nil) })
	refuse(t, "above MaxItems", func() {
		Repeater(RepeaterProps{Name: "r", MinItems: 3, MaxItems: 2}, nil)
	})
	refuse(t, "above MaxItems", func() {
		Repeater(RepeaterProps{Name: "r", MaxItems: 1,
			Items: []RepeaterItem{{}, {}}}, nil)
	})
	refuse(t, "no Action", func() {
		Repeater(RepeaterProps{Name: "r", Island: Island{Endpoint: "/i", Signal: "s"}}, nil)
	})
	refuse(t, "requires Island", func() {
		Repeater(RepeaterProps{Name: "r", Action: "/island/r"}, nil)
	})
	refuse(t, "needs a Signal", func() {
		Repeater(RepeaterProps{Name: "r", Action: "/island/r",
			Island: Island{Endpoint: "/island/r"}}, nil)
	})
	refuse(t, "negative", func() {
		Repeater(RepeaterProps{Name: "r", MinItems: -1}, nil)
	})
}

// The floor disables the remove control rather than hiding it, and
// the ceiling disables the add.
func TestRepeaterFloorsAndCeilingsDisable(t *testing.T) {
	atFloor := Repeater(RepeaterProps{Name: "owners",
		Items: []RepeaterItem{{}}, MinItems: 1,
		AddName: "owners_add", RemoveName: "owners_remove"}, nil)
	has(t, atFloor, `disabled="" name="owners_remove"`, "a row at the floor's remove control is not disabled")
	atCeiling := Repeater(RepeaterProps{Name: "owners",
		Items: []RepeaterItem{{}}, MaxItems: 1}, nil)
	has(t, atCeiling, `data-hui-repeater-action="add" disabled=""`, "the add control at the ceiling is not disabled")
}

// Control bytes never ride a value into the chip or its hidden input:
// a CR LF in a posted tag is scrubbed, the way the table's carried
// query is.
func TestTagInputScrubsControlBytesInValues(t *testing.T) {
	got := TagInput(TagInputProps{Name: "tags", Label: "Tags",
		Values: []string{"go\r\nrm"}}, nil)
	has(t, got, ">gorm<button", "a CR LF in a value reached the chip")
	has(t, got, `value="gorm"`, "a CR LF in a value reached the hidden input")
	hasNot(t, got, "\r", "a carriage return reached the markup")
}

// The value rides inside its chip, the shape the module builds: the
// module removes a list item and the value leaves the form with it,
// whether the server or the module rendered the chip.
func TestTagInputValueRidesInsideItsChip(t *testing.T) {
	got := string(TagInput(TagInputProps{Name: "tags", Label: "Tags", Values: []string{"go", "css"}}, nil))
	hidden := strings.Index(got, `type="hidden"`)
	chipEnd := strings.Index(got, "</li>")
	if hidden < 0 || chipEnd < 0 || hidden > chipEnd {
		t.Errorf("the first hidden input is not inside the first chip:\n%s", got)
	}
}
