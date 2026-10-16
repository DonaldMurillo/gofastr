package entityui

import (
	"context"
	"log/slog"
	"net/url"
	"sync"
	"sync/atomic"
)

// ScreenHost is the optional Host half that answers whether the app's UI
// routes serve a path. known is false when the host cannot tell (no UI
// host mounted), and the list then stays quiet. framework.App's host
// asks the mounted uihost's screen router.
type ScreenHost interface {
	HasScreen(path string) (has, known bool)
}

// maxCheckedBases caps the bases the check remembers. A list with no
// Base takes the request path as its base, so on a dynamic route
// (/u/:name/orders) callers choose new bases; past the cap the check
// stops instead of growing the set and the log for them.
const maxCheckedBases = 256

// recordScreens is the state the missing-record-screen check keeps: the
// host's ScreenHost half, captured at New because WithAPIPath wraps the
// host in a type that drops it, and the bases already checked, shared by
// every copy of the UI. Screens register at boot, so one answer per base
// holds for the process and later renders skip the router match.
type recordScreens struct {
	host    ScreenHost
	checked sync.Map
	n       atomic.Int64
}

// checkRecordScreen warns once per base when a list's row links lead
// nowhere. A list draws <base>/<id> for every row, and until the app
// registers a record screen there, each link 404s with nothing in the
// log (#503).
func (u *UI) checkRecordScreen(ctx context.Context, entityName, base, id string) {
	if u.screens == nil || u.screens.host == nil || id == "" {
		return
	}
	if _, seen := u.screens.checked.Load(base); seen {
		return
	}
	if u.screens.n.Add(1) > maxCheckedBases {
		return
	}
	has, known := u.screens.host.HasScreen(base + "/" + url.PathEscape(id))
	if _, seen := u.screens.checked.LoadOrStore(base, struct{}{}); seen || !known || has {
		return
	}
	slog.WarnContext(ctx, "entityui: list rows link to a path no screen serves",
		"entity", entityName,
		"link", base+"/<id>",
		"fix", "register a record screen at "+base+"/:id (UI.Record), or point ListBuilder.Base at the path the record screen is mounted under")
}
