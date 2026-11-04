package desktop

import (
	"context"
	"testing"
)

func TestHiddenWindowHasShowMenu(t *testing.T) {
	for _, menu := range []*Menu{nil, {Items: []MenuItem{{Role: RoleQuit}}}, {Items: []MenuItem{{Role: RoleShow}}}} {
		var actions []string
		rows := PlanTrayMenu(context.Background(), &Tray{Menu: menu, CloseHidesWindow: true}, "Notes", &actions)
		shows := 0
		for _, row := range rows {
			if row.Role == RoleShow {
				shows++
			}
		}
		if shows != 1 {
			t.Errorf("menu %+v has %d recovery Show rows, want one", menu, shows)
		}
	}
}
