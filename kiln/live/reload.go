package live

import (
	"fmt"
	"net/http"
)

// reloadJS is kiln's build-mode page-refresh client, the world-edit
// analog of framework/dev's livereload script, and like it a dev-mode
// exception to the "no bespoke EventSource" rule for app surfaces:
// an agent editing the world IS a genuine background push, and the
// affected surface is the rendered page itself, not a widget signal
// (the chat panel's signals poll /state instead, see kiln/chat).
//
// On any world edit, an undo, or a session reset, the current page's
// rendering may have changed under the visitor, so the script forces an
// SPA refresh through the framework runtime's navigate (cache-bypassing),
// never a hard reload while the runtime is present. It refreshes on
// every world edit rather than an op allow-list: the list held only
// add/delete page and route, so update_page_element on the page being
// viewed, a theme or app-config change, nav and seed edits all left a
// stale screen, and every new op was one more entry to forget. A forced
// SPA navigation of the current path is cheap next to a stale screen.
//
// The exception is an edit to the document shell: set_app_config (the
// theme stylesheet in the head) and set_scaffold (the layout's nav). A
// same-route SPA navigation keeps the head and the shared layout, so
// those, an undo of one, and a session reset load the document again,
// the way framework/dev's livereload does on a new build. So does a
// reconnect after a dropped connection: the bus has no replay, so the
// client cannot know whether it missed a shell edit.
const reloadJS = `// kiln build-mode reload (dev only)
(() => {
  'use strict';
  if (window.__kilnReloadWired) return;
  window.__kilnReloadWired = true;
  const SHELL_OPS = { set_app_config: 1, set_scaffold: 1 };
  const opOf = (ev) => { try { return (JSON.parse(ev.data) || {}).op || ''; } catch (_) { return ''; } };
  const reloadDocument = () => setTimeout(() => location.reload(), 200);
  const onEdit = (ev) => { if (SHELL_OPS[opOf(ev)]) reloadDocument(); else refresh(); };
  const refresh = () => setTimeout(() => {
    const path = location.pathname + location.search + location.hash;
    if (window.__gofastr && window.__gofastr.navigate) {
      window.__gofastr.navigate(path, { force: true });
    } else {
      location.reload();
    }
  }, 200);
  // Body classes mirror the link state for the panel's connection dot
  // (.kiln-panel-conn styles off body.cui-sse-up / body.cui-sse-down:
  // the same contract the old per-widget SSE block maintained).
  const mark = (up) => {
    document.body.classList.toggle('cui-sse-up', up);
    document.body.classList.toggle('cui-sse-down', !up);
  };
  // dirty: a prior connection dropped, so a world edit may have been
  // broadcast while we were gone (the broadcaster has no replay). The
  // client cannot tell whether a missed edit touched the document shell,
  // so the first 'ready' after a drop reloads the document: an SPA
  // refresh would keep a stale theme or nav.
  let dirty = false;
  const connect = () => {
    const es = new EventSource('/.kiln/events');
    es.onopen = () => mark(true);
    es.addEventListener('ready', () => { if (dirty) { dirty = false; reloadDocument(); } });
    es.addEventListener('world_edit', onEdit);
    es.addEventListener('` + EventUndo + `', onEdit);
    es.addEventListener('session_reset', reloadDocument);
    es.onerror = () => { mark(false); dirty = true; es.close(); setTimeout(connect, 3000); };
  };
  connect();
})();
`

// ServeReloadJS serves the build-mode reload client. Mount at
// "/.kiln/reload.js" and inject via uihost.WithExtraScripts.
func ServeReloadJS(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	fmt.Fprint(w, reloadJS)
}
