# framework/local: local-first state, declared in Go

A `local.Store` declares, once per app, the collections a browser keeps
for that app: a Go record type, a schema version with migrations, a key
field, a size cap per record and per collection. The browser API is
generated from that declaration and served as three runtime modules:
`local-store` (the store, the caps, the collection API), `local-bridge`
(every way the store reaches a Go handler: seed, mirror cookie, upload,
download) and `local-migrate` (`LoadIdle`: the version steps). All on
the kernel's `local` primitive (IndexedDB, tiny-value localStorage
fallback), under `gofastr.state.local.<app>.<collection>:<key>`.

The bridges are **HTTP-shaped**, the upload a request-body field and
the download a response header, so a WebSocket app uploads through an
action. The mirror is for a few small preferences (4 KiB of Cookie
header for the mirrored collections of every store the process
declares, a panic at `Define` past it); a large record is read at
action time through the upload, or painted after hydration by a seed.
A mirror read is a **client hint** (check the `local.Source`), and the
upload **fails closed**: a request whose declared records could not be
attached is not sent.

**Use this when** the prompt mentions: local-first, browser state,
persisted draft, "keep it on the device", read a browser value on the
server, push a value into the browser after an action, clear on logout.
NOT for offline sync, and NEVER for a secret or a session token.

**Import:** `github.com/DonaldMurillo/gofastr/framework/local`

## Shape

This block compiles: `go test ./framework/docs -run TestDocExamplesCompile`
builds it along with the guides' snippets, so it cannot rot.

<!-- gofastr:compile
import "context"
import "net/http"
import "github.com/DonaldMurillo/gofastr/core-ui/app"
import "github.com/DonaldMurillo/gofastr/core-ui/store"
import "github.com/DonaldMurillo/gofastr/core/render"
import "github.com/DonaldMurillo/gofastr/core/router"
import "github.com/DonaldMurillo/gofastr/framework/local"
import "github.com/DonaldMurillo/gofastr/framework/uihost"

type Draft struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}
type View struct {
	Compact bool `json:"compact"`
}

var site = app.NewApp("site")
var rt = router.New()
var S = store.New("editor")
-->
```go
var Site = local.New("site")
var Drafts = local.Define[Draft](Site, "drafts", local.CollectionConfig{
	Version: 2, KeyField: "id", MaxRecordBytes: 32 << 10, MaxRecords: 200,
	Migrations: []local.Migration{{Version: 2, Steps: []local.Step{local.Rename("body", "text")}}},
})
var Prefs = local.Define[View](Site, "prefs", local.CollectionConfig{Version: 1, Mirror: true})

// Serve the declaration on the extra-script rail (once, in main). Both
// halves are required: the URL on the rail AND mount(app.Router()),
// which usually runs later; one alone is a silent 404 and a null store.
var scriptURL, mount = Site.Script()
var host = uihost.New(site, uihost.WithExtraScripts(scriptURL))

// Seed a signal from a record; the runtime patches it in after hydration.
// A page script writes it with setSignal(<data-fui-signal>, value).
var title = local.SeedSignal(Drafts, "current", store.JSON[Draft](S, "draft", Draft{}))

// Upload: the trigger declares what rides along; the handler reads it.
var up = local.Send(Drafts.Key("current")) // or local.Send(Drafts)

func screen(ctx context.Context) render.HTML {
	return title.Bind(ctx, "p", nil) // + up.Merge(...) on the RPC trigger
}

func routes() {
	mount(rt)
	rt.Post("/drafts/upload", up.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// src is SourceUpload, SourceMirror or SourceNone; never authorise
		// on a mirror read, any client forges that cookie.
		d, src, err := local.Get(r.Context(), Drafts, "current")
		if err != nil || src != local.SourceUpload {
			http.Error(w, "no draft arrived", http.StatusBadRequest)
			return
		}
		p, _, _ := local.Get(r.Context(), Prefs, "view") // mirrored: also at first paint
		_ = p
		local.Put(w, Drafts, "current", d) // download: write back
		local.Clear(w, Site)               // logout over RPC
		local.ClearOnNextLoad(w, r, Site)  // logout by full navigation
	}))
}
```

Browser side (after `__gofastr.loadModule('local-store')`):
`__gofastr.localStore('site').collection('drafts')` has `get`, `put`,
`delete`, `list({orderBy, desc})`, `count`, `subscribe`, `clear`. Every
call settles `{ok, reason}`; refusals raise `gofastr:local-error`, and a
collection whose migration did not complete refuses every call with
reason `migration`.

Docs: `gofastr docs local-state`.
