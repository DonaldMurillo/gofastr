package headless

import (
	"regexp"
	"strings"
	"testing"
)

// menuItemIDs `continue`d on a row with no ID before recursing, so a
// duplicate id inside an id-less submenu parent was never refused.
func TestMenuRefusesDuplicateIDInsideIDlessSubmenu(t *testing.T) {
	mustRefuse(t, "a menu rendering the row id x twice", func() {
		Menu(MenuProps{Label: "M", Items: []MenuItem{
			{Label: "A", ID: "x"},
			{Label: "Sub", Children: []MenuItem{{Label: "B", ID: "x"}}},
		}}, nil)
	})
	mustRefuse(t, "a menu repeating an id two id-less levels down", func() {
		Menu(MenuProps{Label: "M", Items: []MenuItem{
			{Label: "Sub", Children: []MenuItem{{Label: "B", ID: "y"},
				{Label: "Deeper", Children: []MenuItem{{Label: "C", ID: "y"}}}}},
		}}, nil)
	})
}

// A Disabled Action row's submit button carried aria-disabled but not
// the disabled attribute (the button path Marks it; the Action path
// returned first), so the form still submitted on activation.
func TestMenuDisabledActionRowCannotSubmit(t *testing.T) {
	h := string(Menu(MenuProps{Label: "M", Items: []MenuItem{{
		Label: "Delete", Disabled: true,
		Action: &MenuAction{Path: "/del", Fields: map[string]string{"csrf": "t"}},
	}}}, nil))
	btn := regexp.MustCompile(`<button[^>]*type="submit"[^>]*>`).FindString(h)
	if !strings.Contains(btn, ` disabled=""`) {
		t.Fatalf("a Disabled form row's submit button is not disabled: %s", btn)
	}
	on := string(Menu(MenuProps{Label: "M", Items: []MenuItem{{
		Label: "Delete", Action: &MenuAction{Path: "/del", Fields: map[string]string{"csrf": "t"}},
	}}}, nil))
	if strings.Contains(on, ` disabled=""`) {
		t.Fatalf("an enabled form row renders disabled: %s", on)
	}
}
