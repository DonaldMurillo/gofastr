package entityui

import (
	"context"
	"log/slog"
	"net/url"
	"sync"
)

// ScreenHost is the optional Host half that answers whether the app's UI
// routes serve a path. known is false when the host cannot tell (no UI
// host mounted), and the list then stays quiet. framework.App's host
// asks the mounted uihost's screen router.
type ScreenHost interface {
	HasScreen(path string) (has, known bool)
}

// recordScreens is the state the missing-record-screen check keeps: the
// host's ScreenHost half, captured at New because WithAPIPath wraps the
// host in a type that drops it, and the bases already warned about,
// shared by every copy of the UI.
type recordScreens struct {
	host   ScreenHost
	warned sync.Map
}

// checkRecordScreen warns once per base when a list's row links lead
// nowhere. A list draws <base>/<id> for every row, and until the app
// registers a record screen there, each link 404s with nothing in the
// log (#503).
func (u *UI) checkRecordScreen(ctx context.Context, entityName, base, id string) {
	if u.screens == nil || u.screens.host == nil || id == "" {
		return
	}
	has, known := u.screens.host.HasScreen(base + "/" + url.PathEscape(id))
	if !known || has {
		return
	}
	if _, seen := u.screens.warned.LoadOrStore(base, struct{}{}); seen {
		return
	}
	slog.WarnContext(ctx, "entityui: list rows link to a path no screen serves",
		"entity", entityName,
		"link", base+"/<id>",
		"fix", "register a record screen at "+base+"/:id (UI.Record), or point ListBuilder.Base at the path the record screen is mounted under")
}
