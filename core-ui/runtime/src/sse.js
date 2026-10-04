// GoFastr runtime module, SSE island stream
//
// Connects to the framework's server-side SSE bus declared on the
// page via <meta name="gofastr-sse" content="<url>"> and reflects
// "island" events into matching [data-island] regions. The transport
// is one long-lived EventSource per session; the bus multiplexes
// updates by event type so multiple islands share one connection.
//
// ON DEMAND, not always: the meta means "SSE is available", never
// "open it". The stream opens only while the live document holds a
// PUSH TARGET, and closes when the last one leaves — an open stream
// holds one of the browser's six HTTP/1.1 connections per host, so a
// page that takes no pushes must not hold one. A push target is:
//   - any [data-island] region: UIHost.PushUpdate can target any
//     island id, so every island counts. Presence rosters are islands
//     (the roster swap slot is [data-island]); there is deliberately
//     no separate opt-in marker for islands, that would silently
//     break hosts that push today.
//   - the offline banner ([data-hui-system-offline], the connection
//     banner built from NetworkRetryBanner): it reads the sseStatus
//     this module mirrors, so it needs the stream to have a state.
// The scan (demand below) runs at module load, after every navigation
// apply (the kernel's scanner hook fires on gofastr:navigate — cached
// replays included), when a deferred part lands (gofastr:fill — a
// part can carry the page's only island), and on DOM insertion (the
// kernel's MutationObserver runs the same scanner hook).
//
// Connection state is mirrored onto window.__gofastr.sseStatus
// ({ connected, lastEventAt, retryCount }), one object mutated in
// place so holders (NetworkRetryBanner, app code) keep a live
// reference. Transitions (connect / disconnect) announce a
// `gofastr:sse-status` CustomEvent on document; per-frame updates
// only touch lastEventAt silently. The banner reads lastEventAt to
// spot a silent link and listens for the event to re-probe health
// on reconnect.
//
// Loads on demand:
//   - core looks for a push target (any [data-island] region, the
//     offline banner) at boot and after every apply; if one is
//     present, idle-loads this module.
//   - reconnects on transport error (3s back-off).
//   - closes the transport on pagehide and re-establishes it on a
//     bfcache restore (pageshow.persisted). See the lifecycle block.
(() => {
  'use strict';
  window.__gofastr = window.__gofastr || {};
  const NS = window.__gofastr;

  // The live transport and any pending reconnect timer, held at module
  // scope so the pagehide handler below can tear them down.
  let source = null;
  let retryTimer = 0;

  // One live status object. Mutated in place, never reassigned, so
  // every reference (banner poll, app listener) sees updates.
  const status = NS.sseStatus = { connected: false, lastEventAt: 0, retryCount: 0 };
  const emit = () => document.dispatchEvent(new CustomEvent('gofastr:sse-status', { detail: status }));

  // remintSession recovers an idle page whose session token died under
  // it (server restart, key rotation, or 30-day expiry). An EventSource
  // onerror can't see the 401 that handleSSE returns, so a purely idle
  // page, one that never navigates, would otherwise reconnect-loop
  // forever on the dead stream id. POST /__gofastr/session mints a fresh
  // token (Set-Cookie) and returns its bare id; we rewrite the stream
  // meta so the next connect() uses an id that matches the new cookie.
  // Same-origin + credentials so the cookie round-trips. Best-effort.
  function remintSession() {
    return fetch('/__gofastr/session', { method: 'POST', credentials: 'same-origin' })
      .then((r) => (r.ok ? r.json() : null))
      .then((j) => {
        if (!j || !j.sessionId) return;
        const m = document.querySelector('meta[name="gofastr-sse"]');
        if (m) m.setAttribute('content', m.getAttribute('content').replace(/([?&]session=)[^&]*/, '$1' + j.sessionId));
      })
      .catch(() => {});
  }

  function connect() {
    const sseUrl = document.querySelector('meta[name="gofastr-sse"]')?.getAttribute('content');
    if (!sseUrl) return;

    source = new EventSource(sseUrl);

    source.onopen = () => {
      status.connected = true;
      status.retryCount = 0;
      status.lastEventAt = Date.now();
      emit();
    };

    source.addEventListener('island', (event) => {
      // Refresh on every frame; no event dispatch (too chatty).
      status.lastEventAt = Date.now();
      try {
        const { island, html } = JSON.parse(event.data);
        if (island === undefined || html === undefined) return;
        // Escape the server-supplied island name before it enters the
        // CSS attribute selector, a crafted name (e.g. `x"], [data-…`)
        // would otherwise re-target the write to an unintended element,
        // or throw an invalid-selector error that silently drops the
        // legitimate island's update. Matches the CSS.escape pattern in
        // widgets.js / headless-feedback.
        const el = document.querySelector('[data-island="' + CSS.escape(String(island)) + '"]');
        if (!el) return;
        el.innerHTML = html;
        // A server-pushed island can introduce a [data-cui-comp] the
        // page hadn't carried, load its component CSS, same as every
        // other innerHTML swap path (nav/signals/poll/widgets).
        // This used to be the only swap that skipped
        // scanAndLoadCSS, so pushed islands rendered unstyled.
        window.__gofastr.scanAndLoadCSS?.(el);
        el.classList.add('island-updated');
        setTimeout(() => el.classList.remove('island-updated'), 1000);
      } catch { /* ignore malformed SSE frames */ }
    });

    source.onerror = () => {
      status.connected = false;
      status.retryCount++;
      emit();
      source.close();
      source = null;
      // After repeated failures the cause is more likely a dead token
      // than a flapping network, attempt a re-mint (throttled to every
      // other failure past the 2nd, so a genuine outage doesn't hammer
      // /session). The rewrite lands on the meta; the reconnect below
      // picks it up on this or the next cycle. Reconnect timing is NOT
      // blocked on the re-mint (a hung fetch must not stall recovery).
      if (status.retryCount >= 2 && status.retryCount % 2 === 0) remintSession();
      retryTimer = setTimeout(connect, 3000);
    };
  }

  // The push-target selector, kept in one place with the kernel's
  // marker row (frag/boot.js) and the preload mirror (preload.go):
  // every island plus the offline banner that reads sseStatus. A new
  // stream-status reader is a new push target and belongs here AND
  // there — a target the selector misses is a page whose stream never
  // opens.
  const PUSH_TARGETS = '[data-island],[data-hui-system-offline]';

  // demand is the open/close decision: the stream exists only while
  // the live document holds a push target. Called at module load,
  // after every navigation apply (through the scanner hook the kernel
  // runs on gofastr:navigate — cached replays included), when a
  // deferred part lands (gofastr:fill), and on DOM insertion (the
  // kernel's MutationObserver runs the same hook). Always scans the
  // DOCUMENT, never the passed root: a navigation can REMOVE the last
  // target anywhere in the tree, so no region-scoped answer is sound.
  // A deliberate close cancels the pending reconnect and clears
  // retryCount, so a later reopen starts from a clean slate (a stale
  // retry count plus connected:false would read as an outage to a
  // banner arriving on the reopening page).
  function demand() {
    if (document.querySelector(PUSH_TARGETS)) {
      if (!source) connect();
      return;
    }
    clearTimeout(retryTimer);
    retryTimer = 0;
    if (source) {
      source.close();
      source = null;
    }
    if (status.connected || status.retryCount) {
      status.connected = false;
      status.retryCount = 0;
      emit();
    }
  }

  // The kernel runs every LOADED module's scanner after each apply and
  // on DOM insertion; gofastr:fill is the parts module's landing event
  // and fires for no scanner, so it gets its own listener here (a
  // deferred part can carry the page's only island).
  NS._moduleScanners = NS._moduleScanners || {};
  NS._moduleScanners.sse = demand;
  window.addEventListener('gofastr:fill', demand);

  // bfcache lifecycle. On a hard navigation Chrome puts the outgoing
  // page into the back/forward cache WITHOUT closing its EventSource:
  // the dead page's stream keeps hoarding one of the tab's ~6 per-host
  // HTTP/1.1 connections until the cache entry is evicted, so six
  // SSE-bearing navigations starve the tab and the seventh page never
  // loads. Close the transport when the page is hidden (pagehide fires
  // on both bfcache entry and real unload; closing is correct for
  // either), and re-establish it when the page comes back out of the
  // cache (pageshow.persisted). demand() re-reads the stream meta, so
  // a session re-mint that landed before hiding is honored, and its
  // onopen resets retryCount/lastEventAt and re-announces the status.
  // The retry timer is cleared so a bfcached page's pending reconnect
  // can't fire on restore alongside pageshow's own demand.
  addEventListener('pagehide', () => {
    clearTimeout(retryTimer);
    if (source) {
      source.close();
      source = null;
    }
    // Silent: the page is going away, holders re-learn the state from
    // onopen's emit after the pageshow reconnect.
    status.connected = false;
  });
  addEventListener('pageshow', (e) => {
    if (e.persisted && !source) demand();
  });

  // remintSession runs only while the stream is open (it is the
  // onerror recovery path), so a page with NO push target — no stream,
  // no idle re-mint — recovers a dead session through the next
  // navigation instead: the navigator's session rollover rewrites the
  // stream meta from the X-Gofastr-Session header on every navigation
  // answer (and a whole-document fetch copies the fresh head's meta),
  // so the id is already current when a later page reopens the
  // stream. No timer is added for this: a page that never navigates
  // and never takes pushes has nothing to recover for.
  //
  // Connect through demand(), never blindly: the kernel loaded this
  // module because a push target was on the page, but an idle-slot
  // delay or a fast navigation may have removed it by now.
  NS.connectSSE = connect;
  demand();

  (NS.loadedModules ||= {}).sse = true;
})();
