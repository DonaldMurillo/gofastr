package main

// Draft notes: the framework/local demo. A draft the browser keeps
// (IndexedDB through the kernel's local primitive), a signal seeded
// from a record, a preference mirrored into a cookie so the server can
// render it at first paint, an "upload draft" action whose Go handler
// reads the declared record, and a receipt the response writes back
// into the browser. No inline script: the page's own JavaScript is
// served at /__site/local-notes.js on the extra-script rail, beside
// the store's manifest.

import (
	"context"
	"fmt"
	"net/http"
	"time"
	"unicode/utf8"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/store"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/local"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

const (
	localNotesPath       = "/forms/draft-notes"
	localNotesUploadPath = "/__site/local/upload"
	localNotesScriptPath = "/__site/local-notes.js"
	localNotesLogoutPath = "/__site/local/logout"
)

// siteDraft is the record the browser keeps. The key field is id, so
// the browser API's put(value) reads it from the record.
type siteDraft struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Text  string `json:"text"`
}

// siteNotesView is a tiny preference mirrored into a cookie: the server
// reads it at first paint.
type siteNotesView struct {
	Compact bool `json:"compact"`
}

// siteReceipt is what the upload handler writes back into the browser.
type siteReceipt struct {
	Title string `json:"title"`
	Chars int    `json:"chars"`
	At    string `json:"at"`
}

var (
	// One store per app, declared once.
	siteLocal = local.New("site")
	// The draft: up to 16 KiB per record, fifty records.
	siteDrafts = local.Define[siteDraft](siteLocal, "drafts", local.CollectionConfig{
		Version: 1, KeyField: "id", MaxRecordBytes: 16 << 10, MaxRecords: 50,
	})
	// The view preference: mirrored, so RenderCtx sees it on the request.
	siteNotesPrefs = local.Define[siteNotesView](siteLocal, "notes-prefs", local.CollectionConfig{Version: 1, Mirror: true})
	// Receipts the server pushes back after an upload.
	siteReceipts = local.Define[siteReceipt](siteLocal, "receipts", local.CollectionConfig{Version: 1, MaxRecords: 5})
	// A plain string collection behind a seeded signal.
	siteScratch = local.Define[string](siteLocal, "scratch", local.CollectionConfig{Version: 1})

	notesStore   = store.New("notes")
	notesScratch = notesStore.String("scratch", "nothing yet")
	notesUpload  = notesStore.String("upload", "Not uploaded yet.")
	// The seed bridge: notes.scratch is filled from scratch:last after
	// hydration and written back when a data-fui-signal-set button
	// changes it.
	notesSeed = local.SeedSignal(siteScratch, "last", notesScratch)
	// The upload bridge: only drafts:current rides the upload request.
	notesUploadSend = local.Send(siteDrafts.Key("current"))
)

// LocalNotesScreen is the demo page at /forms/draft-notes.
type LocalNotesScreen struct{}

func (s *LocalNotesScreen) ScreenTitle() string { return "Draft notes: local-first state" }
func (s *LocalNotesScreen) ScreenDescription() string {
	return "A draft the browser keeps, a signal seeded from a record, a preference the server reads at first paint, and an upload action that reads the draft in Go. framework/local end to end."
}
func (s *LocalNotesScreen) ScreenType() app.ScreenType { return app.ScreenPage }

// Render satisfies component.Component; the host calls RenderCtx with
// the request context, which is where the mirrored cookie is read.
func (s *LocalNotesScreen) Render() render.HTML { return s.RenderCtx(context.Background()) }

func (s *LocalNotesScreen) RenderCtx(ctx context.Context) render.HTML {
	// The mirrored preference: present on the request as a cookie, so
	// this render already knows it. No flash of the wrong state.
	// A mirror read is a CLIENT HINT: the browser wrote the cookie, so
	// this is evidence about what to paint and nothing more.
	view, viewSrc, _ := local.Get(ctx, siteNotesPrefs, "view")
	viewLabel := "Comfortable view"
	if view.Compact {
		viewLabel = "Compact view"
	}
	viewSource := "server default: no preference stored yet"
	if viewSrc == local.SourceMirror {
		viewSource = "rendered by the server from the mirrored cookie — a client hint, validated like any input"
	}

	draftForm := ui.Form(ui.FormConfig{
		Action:      localNotesUploadPath,
		SubmitLabel: "Upload draft",
		ID:          "draft-form",
		// The RPC attributes make the form an island; Merge adds the
		// upload declaration (data-local-send) and the module the
		// runtime loads before dispatching (data-fui-rpc-with).
		ExtraAttrs: notesUploadSend.Merge(html.Attrs{
			"data-fui-rpc":        localNotesUploadPath,
			"data-fui-rpc-signal": notesUpload.Name(),
		}),
	},
		ui.FormField(ui.FormFieldConfig{
			Label: "Title", For: "draft-title",
			Input: html.Input(html.InputConfig{Type: "text", Name: "title", ID: "draft-title", Placeholder: "A title for the draft"}),
		}),
		ui.FormField(ui.FormFieldConfig{
			Label: "Draft", For: "draft-text",
			Help:  "Typed here, kept in this browser's IndexedDB as drafts:current. Reload the page: it is still here. Nothing reaches the server until you upload.",
			Input: html.TextArea(html.TextAreaConfig{Name: "text", ID: "draft-text", Rows: 6, Placeholder: "Start writing…"}),
		}),
	)

	// A full-navigation logout: a plain form POST, answered with a redirect.
	// No RPC attributes, so rpc.js never sees the response and the clear
	// rides ClearOnNextLoad instead of the response header.
	logoutForm := ui.Form(ui.FormConfig{
		Action:      localNotesLogoutPath,
		SubmitLabel: "Sign out and clear this browser's store",
		ID:          "local-logout",
	})

	return container(
		ui.PageHeader(ui.PageHeaderConfig{
			Eyebrow:  "Forms",
			Title:    "Draft notes",
			Subtitle: "Local-first state with framework/local: declared in Go, persisted in the browser, uploaded only when you say so.",
		}),
		ui.Stack(ui.StackConfig{Gap: ui.GapLG},
			ui.Section(ui.SectionConfig{
				Heading:     "Your draft, kept in this browser",
				Description: "The form is an RPC island. Its trigger declares that drafts:current accompanies the request; the Go handler reads it with local.Get and answers with a receipt, which it also writes back into the browser through the response.",
				Ctx:         ctx,
			},
				ui.Card(ui.CardConfig{}, draftForm),
				notesUpload.Bind(ctx, "p", map[string]string{"id": "upload-result"}),
				ui.Card(ui.CardConfig{Heading: "Receipts the server pushed back", Description: "Written into the browser by the upload response, newest first."},
					html.UnorderedList(html.ListConfig{ID: "receipts"}),
				),
			),
			ui.Section(ui.SectionConfig{
				Heading:     "A signal seeded from a record",
				Description: "The server renders the default. After hydration the runtime fills the signal from scratch:last and writes every change back, so the last button you pressed survives a reload and a client-side navigation.",
				Ctx:         ctx,
			},
				notesSeed.Bind(ctx, "p", map[string]string{"id": "scratch-value"}),
				ui.Cluster(ui.ClusterConfig{Gap: ui.GapSM},
					ui.Button(ui.ButtonConfig{Label: "Say hello", Variant: ui.ButtonSecondary, ID: "say-hello",
						ExtraAttrs: html.Attrs{"data-fui-signal-set": notesScratch.Name() + ":hello"}}),
					ui.Button(ui.ButtonConfig{Label: "Say goodbye", Variant: ui.ButtonSecondary, ID: "say-goodbye",
						ExtraAttrs: html.Attrs{"data-fui-signal-set": notesScratch.Name() + ":goodbye"}}),
				),
			),
			ui.Section(ui.SectionConfig{
				Heading:     "A preference the server reads at first paint",
				Description: "notes-prefs is a Mirror collection: each record also lives in a cookie, so this render read it with local.Get before a single byte of JavaScript ran. The toggle writes the record, then navigates client-side so the server renders the new value.",
				Ctx:         ctx,
			},
				ui.Cluster(ui.ClusterConfig{Gap: ui.GapSM, Align: ui.AlignCenter},
					ui.Tag(ui.TagConfig{Label: viewLabel, ID: "view-tag", Ctx: ctx}),
					html.Span(html.TextConfig{ID: "view-source"}, render.Text(viewSource)),
					ui.Button(ui.ButtonConfig{Label: "Toggle view", Variant: ui.ButtonSecondary, ID: "toggle-view"}),
				),
			),
			ui.Section(ui.SectionConfig{
				Heading:     "Logout clears the store",
				Description: "battery/auth ends the session and knows nothing about this store, so the app clears it: local.ClearOnNextLoad before the redirect of a full-navigation logout, and every collection and mirror cookie is gone on the page you land on.",
				Ctx:         ctx,
			},
				logoutForm,
			),
		),
	)
}

// localNotesLogout is the full-navigation logout: the auth battery
// would end the session here, and the app clears its own store, which
// the battery cannot know about. ClearOnNextLoad goes before the
// redirect so the landing page's module finds the bit.
func localNotesLogout(w http.ResponseWriter, r *http.Request) {
	local.ClearOnNextLoad(w, r, siteLocal)
	http.Redirect(w, r, "/forms/draft-notes", http.StatusSeeOther)
}

// actOnDraft is the habit every handler in this tree keeps: a record is
// acted on only when it came through Upload.Wrap, on a request whose
// trigger declared it. src.Found() would also accept a mirror cookie,
// a value any script on the origin writes and any client forges, which
// is fine for deciding what to paint (the view preference above says so
// on the page) and never for acting on. The team-builder example
// (github.com/AlexCiccolella125/gofastr-team-builder) requires the same
// thing; two examples teaching two habits is how the weaker one gets
// copied.
func actOnDraft(src local.Source) bool { return src == local.SourceUpload }

// localNotesUpload is the wrapped handler: the upload bridge has
// already read drafts:current off the body, refused anything
// undeclared, and put the record on the context.
func localNotesUpload(w http.ResponseWriter, r *http.Request) {
	draft, src, err := local.Get(r.Context(), siteDrafts, "current")
	if err != nil {
		http.Error(w, "the draft does not decode: "+err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if !actOnDraft(src) {
		fmt.Fprint(w, "No draft arrived: write something first.")
		return
	}
	title := draft.Title
	if title == "" {
		title = "untitled"
	}
	chars := utf8.RuneCountInString(draft.Text)
	// The download bridge: a receipt the browser keeps, keyed by the
	// second so a run of uploads leaves a short history (MaxRecords 5
	// caps it; the page's script shows the newest first).
	_ = local.Put(w, siteReceipts, time.Now().UTC().Format("20060102T150405.000"), siteReceipt{
		Title: title, Chars: chars, At: time.Now().UTC().Format(time.RFC3339),
	})
	fmt.Fprintf(w, "The server received %q (%d characters).", title, chars)
}

// localNotesJS is the page's own script, served on the extra-script
// rail after runtime.js and the store manifest. It uses the browser API
// the declaration generated: __gofastr.localStore('site').
const localNotesJS = `(() => {
  'use strict';
  const G = window.__gofastr;
  let wired = null;
  const wire = () => {
    const text = document.getElementById('draft-text');
    const title = document.getElementById('draft-title');
    if (!text || !title || wired === text) return;
    wired = text;
    G.loadModule('local-store').then(() => {
      const site = G.localStore('site');
      const drafts = site.collection('drafts');
      const receipts = site.collection('receipts');
      const prefs = site.collection('notes-prefs');
      drafts.get('current').then((d) => {
        if (!d) return;
        title.value = d.title || '';
        text.value = d.text || '';
      });
      let timer = 0;
      const save = () => {
        clearTimeout(timer);
        timer = setTimeout(() => {
          drafts.put({ id: 'current', title: title.value, text: text.value }).then((r) => {
            if (!r.ok) console.warn('[draft-notes] not saved:', r.reason);
          });
        }, 150);
      };
      title.addEventListener('input', save);
      text.addEventListener('input', save);
      const list = document.getElementById('receipts');
      const renderReceipts = () => receipts.list({ orderBy: 'at', desc: true }).then((rows) => {
        if (!list) return;
        list.textContent = '';
        for (const row of rows) {
          const li = document.createElement('li');
          li.textContent = row.value.title + ' — ' + row.value.chars + ' characters at ' + row.value.at;
          list.appendChild(li);
        }
      });
      renderReceipts();
      receipts.subscribe(renderReceipts);
      const toggle = document.getElementById('toggle-view');
      if (toggle) toggle.addEventListener('click', () => {
        prefs.get('view').then((v) => prefs.put('view', { compact: !(v && v.compact) })).then(() => {
          G.navigate(location.pathname, { force: true });
        });
      });
    });
  };
  wire();
  window.addEventListener('gofastr:navigate', () => { wired = null; wire(); });
})();
`

func serveLocalNotesJS(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write([]byte(localNotesJS))
}
