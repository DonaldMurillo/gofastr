package headless

import (
	"regexp"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
)

// refuse asserts that fn panics and that the panic names the missing
// prop, so the reader of the stack trace is told what to fix rather
// than only where it broke.
func refuse(t *testing.T, prop string, fn func()) {
	t.Helper()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatalf("rendering without %s did not panic", prop)
		}
		msg, ok := r.(string)
		if !ok {
			t.Fatalf("panic is a %T, not the string the components speak: %v", r, r)
		}
		if !strings.Contains(msg, prop) {
			t.Errorf("the panic does not name %s: %s", prop, msg)
		}
		if !strings.HasPrefix(msg, "headless: ") {
			t.Errorf("the panic is not in this package's voice: %s", msg)
		}
	}()
	fn()
}

// A control with no name submits nothing: the browser drops it from
// the form data entirely, so the value a person typed arrives nowhere.
func TestInputRefusesANamelessControl(t *testing.T) {
	refuse(t, "Name", func() { Input(InputProps{}, nil) })
}

func TestTextareaRefusesANamelessControl(t *testing.T) {
	refuse(t, "Name", func() { Textarea(TextareaProps{}, nil) })
}

func TestSelectRefusesANamelessControl(t *testing.T) {
	refuse(t, "Name", func() { Select(SelectProps{}, nil) })
}

func TestPasswordRefusesANamelessControl(t *testing.T) {
	refuse(t, "Name", func() { Password(PasswordProps{}, nil) })
}

func TestColorRefusesANamelessControl(t *testing.T) {
	refuse(t, "Name", func() { Color(ColorProps{}, nil) })
}

// A choice with no label is a small box nobody can name: the label
// wrapping the control IS its accessible name and its hit area.
func TestChoiceRefusesAnUnlabelledControl(t *testing.T) {
	refuse(t, "Label", func() { Choice(ChoiceProps{Type: "checkbox"}, nil) })
}

func TestSwitchRefusesAnUnlabelledControl(t *testing.T) {
	refuse(t, "Label", func() { Switch(SwitchProps{}, nil) })
}

// A form with no action posts to the current URL: the framework's
// silent default, and a failed submit then quietly re-renders the
// same page with no error anyone can see.
func TestFormRefusesAnActionlessSubmit(t *testing.T) {
	refuse(t, "Action", func() { Form(FormProps{}, nil) })
}

// A choice that is not a checkbox or a radio is a text input wearing
// a label wrap: it submits, it never groups, and nothing looks wrong.
func TestChoiceRefusesAnUnknownType(t *testing.T) {
	refuse(t, "Type", func() {
		Choice(ChoiceProps{Type: "bananna", Name: "n", Label: "L"}, nil)
	})
}

// The navigation components refuse in the package's own voice, so a
// caller reading a panic knows which layer spoke. Pinned because these
// refusals were the ones a port left in another package's name.
func TestPaginationRefusesAnUnnamedNav(t *testing.T) {
	refuse(t, "AriaLabel", func() {
		Pagination(PaginationProps{Page: 1, Pages: 2, HrefPattern: "/x?p=%d", Island: fixtureIsland}, nil)
	})
}

// The choice family's other required props: a control with no name
// submits nothing; radios without distinct values all submit "on";
// a fieldset with no legend names nothing inside it.
func TestChoiceFamilyRefusesWhatCannotSubmitOrBeNamed(t *testing.T) {
	refuse(t, "Name", func() { Choice(ChoiceProps{Type: "checkbox", Label: "L"}, nil) })
	refuse(t, "Value", func() { Choice(ChoiceProps{Type: "radio", Name: "n", Label: "L"}, nil) })
	refuse(t, "Name", func() { Switch(SwitchProps{Label: "L"}, nil) })
	refuse(t, "Legend", func() { Group(GroupProps{}, nil) })
}

// A heading tag that is not h1 to h6 renders an unknown element, or a
// void one that drops the title. Refused with the reason.
func TestCardRefusesAnUnknownTitleTag(t *testing.T) {
	refuse(t, "TitleTag", func() { Card(CardProps{Title: "T", TitleTag: "h33"}, nil) })
	refuse(t, "TitleTag", func() { Card(CardProps{Title: "T", TitleTag: "input"}, nil) })
}

// Owned is a seam for numeric bounds and nothing else; a key that is
// not min, max or step is refused rather than let past Safe.
func TestInputOwnedIsForBoundsOnly(t *testing.T) {
	refuse(t, "Owned", func() {
		Input(InputProps{Name: "n", AriaLabel: "n", Owned: html.Attrs{"type": "text"}}, nil)
	})
	got := Input(InputProps{Name: "n", AriaLabel: "n", Owned: html.Attrs{"min": "1", "MAX": "9"}}, nil)
	has(t, got, `min="1"`, "a bound was dropped")
	has(t, got, `max="9"`, "a folded bound was dropped")
}

// A pager whose pattern has no %d renders every page at one URL.
func TestPaginationRefusesAPatternWithoutThePage(t *testing.T) {
	refuse(t, "%d", func() {
		Pagination(PaginationProps{Page: 1, Pages: 2, HrefPattern: "/apps", AriaLabel: "Pages", Island: fixtureIsland}, nil)
	})
}

// Every href a component writes goes through the framework's anchor
// policy. A Button's rejected href renders the disabled-link posture;
// a Form's action, a Tag's or an Alert's dismiss, and a pager's
// pattern are refused at render.
func TestHrefsGoThroughTheAnchorPolicy(t *testing.T) {
	got := Button(ButtonProps{Label: "Go", Href: "javascript:alert(1)"}, nil)
	hasNot(t, got, "href=", "a javascript: href reached the anchor")
	has(t, got, `aria-disabled="true"`, "a rejected href did not render as a dead link")
	refuse(t, "Action", func() { Form(FormProps{Action: "javascript:alert(1)"}, nil) })
	refuse(t, "DismissHref", func() {
		Tag(TagProps{Label: "x", DismissHref: "data:text/html,x", Island: fixtureIsland}, nil)
	})
	refuse(t, "DismissHref", func() {
		Alert(AlertProps{Title: "T", DismissHref: "//evil/x", Island: fixtureIsland}, nil)
	})
	refuse(t, "HrefPattern", func() {
		Pagination(PaginationProps{Page: 1, Pages: 2, HrefPattern: "javascript:%d", AriaLabel: "Pages", Island: fixtureIsland}, nil)
	})
	refuse(t, "data-fui-rpc", func() {
		Button(ButtonProps{Label: "Go", Type: "button", Action: html.Attrs{"data-fui-rpc": "//evil/x"}}, nil)
	})
	refuse(t, "data-fui-rpc", func() {
		Button(ButtonProps{Label: "Go", Type: "button", Action: html.Attrs{"data-fui-rpc": ""}}, nil)
	})
}

// Owned's keys are folded, so min and MIN are one attribute; two
// spellings with values would leave the winner to map order.
func TestInputOwnedRefusesOneKeyUnderTwoSpellings(t *testing.T) {
	refuse(t, "repeats", func() {
		Input(InputProps{Name: "n", AriaLabel: "n", Owned: html.Attrs{"min": "1", "MIN": "2"}}, nil)
	})
}

// A <time datetime> promises a machine-readable value; "tomorrow" is
// not one, and rendering it would be invalid markup nothing notices.
func TestTimelineRefusesAMachineValueThatIsNotATimestamp(t *testing.T) {
	refuse(t, "RFC 3339", func() {
		Timeline(TimelineProps{Events: []Event{{Title: "Deployed", When: "soon", Machine: "tomorrow"}}}, nil)
	})
}

// NAME and name are one attribute to the browser, which keeps the
// first it reads. Stored as written, a caller's NAME sorted ahead of
// the component's name and the submitted field carried the caller's
// value; Safe and the override sanitiser store keys folded, and one
// key under two spellings is refused rather than left to map order.
func TestExtrasAndOverridesStoreKeysFolded(t *testing.T) {
	got := Input(InputProps{Name: "owner", AriaLabel: "n", Extra: html.Attrs{"NAME": "caller", "Data-Testid": "kept"}}, nil)
	if n := len(nameAttr.FindAllString(string(got), -1)); n != 1 {
		t.Errorf("the input carries %d name attributes, not one:\n%s", n, got)
	}
	has(t, got, `name="owner"`, "the component's own name lost to a caller's spelling")
	has(t, got, `data-testid="kept"`, "an ordinary attribute was not stored folded")
	refuse(t, "two spellings", func() {
		Badge(BadgeProps{Label: "x", ExtraAttrs: html.Attrs{"data-testid": "a", "DATA-TESTID": "b"}}, nil)
	})
	for _, sp := range Specs() {
		if sp.WithSeams == nil {
			continue
		}
		refuse(t, "two spellings", func() {
			sp.WithSeams(Skin{PartRoot: "real"}, Seams{Overrides: Overrides{PartRoot: html.Attrs{"role": "a", "ROLE": "b"}}})
		})
		break
	}
}

var nameAttr = regexp.MustCompile(`(?i)\sname="`)
