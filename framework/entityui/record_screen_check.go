package entityui

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
)

// ScreenHost is the optional Host half that answers whether the app's UI
// routes serve a path. known is false when the host cannot tell.
type ScreenHost interface {
	HasScreen(path string) (has, known bool)
}

const maxCheckedBases = 256

type recordScreens struct {
	host    ScreenHost
	checked sync.Map
	n       atomic.Int64
}

func (u *UI) checkRecordScreen(ctx context.Context, entityName, base, idPath string) {
	if u.screens == nil || u.screens.host == nil || idPath == "" {
		return
	}
	if _, seen := u.screens.checked.Load(base); seen {
		return
	}
	if u.screens.n.Add(1) > maxCheckedBases {
		return
	}
	has, known := u.screens.host.HasScreen(under(base, idPath))
	if _, seen := u.screens.checked.LoadOrStore(base, struct{}{}); seen || !known || has {
		return
	}
	slog.WarnContext(ctx, "entityui: list rows link to a path no screen serves",
		"entity", entityName,
		"link", base+"/<id>",
		"fix", "register a record screen at "+base+"/:id (UI.Record), or point ListBuilder.Base at the path the record screen is mounted under")
}
