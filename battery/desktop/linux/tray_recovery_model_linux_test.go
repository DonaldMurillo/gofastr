//go:build linux && (amd64 || arm64)

package linux

import (
	"context"
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
)

func TestTrayModelHasOneRecoveryShow(t *testing.T) {
	for _, closeHides := range []bool{false, true} {
		for _, custom := range []bool{false, true} {
			tray := &desktop.Tray{Title: "Notes", CloseHidesWindow: closeHides}
			if custom {
				tray.Menu = &desktop.Menu{Items: []desktop.MenuItem{{ID: "blocked", Navigate: "/"}}}
			}
			model, err := newTrayModel(context.Background(), desktop.WindowConfig{Title: "Notes", Tray: tray})
			if err != nil {
				t.Fatal(err)
			}
			shows := 0
			for _, node := range model.root.children {
				if node.plan.Role == desktop.RoleShow {
					shows++
				}
			}
			if shows != 1 {
				t.Errorf("closeHides=%v custom=%v: %d top-level Show rows, want one", closeHides, custom, shows)
			}
			showID, quitID := int32(1), int32(2)
			if custom {
				showID, quitID = 3, 4
				if node := model.nodes[1]; node == nil || node.actionID != "blocked" || node.plan.Role != "" {
					t.Errorf("closeHides=%v: row 1 must remain the custom callback: %+v", closeHides, node)
				}
				if node := model.nodes[2]; node == nil || !node.plan.Sep {
					t.Errorf("closeHides=%v: row 2 must remain the separator: %+v", closeHides, node)
				}
			}
			for id, role := range map[int32]string{showID: desktop.RoleShow, quitID: desktop.RoleQuit} {
				if node := model.nodes[id]; node == nil || node.plan.Role != role {
					t.Errorf("closeHides=%v custom=%v: row %d must be %s: %+v", closeHides, custom, id, role, node)
				}
			}
			if tray.CloseHidesWindow != closeHides {
				t.Error("tray planning changed the caller's CloseHidesWindow setting")
			}
		}
	}
}
