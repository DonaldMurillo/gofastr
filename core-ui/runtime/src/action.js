// action.js: the optimistic mutation machine, written once.
//
// OptimisticAction and ToggleAction were one state machine implemented
// twice (src/optimisticaction.js, src/toggleaction.js, both now in
// framework/ui as adapters over this module). What lives here is the
// half they shared: a same-origin mutation request with the CSRF
// header and the invalidation hook, the idle → pending → committed →
// error state flip, hidden swapped between the two label parts,
// aria-busy and disabled while pending, and an event at each step.
// Attribute names are the owner's business: an adapter reads its own
// markup and calls bind with a spec, so this module reads no data-fui-*
// attribute at all and scans nothing.
//
// It has no marker, so nothing loads it on its own: a module that
// needs it declares registry.Requires("action") and the loader has it
// registered before the dependent evaluates. window.__gofastr.action
// is also a public API (request, bind) for surfaces with no adapter.
(() => {
  'use strict';
  const NS = window.__gofastr = window.__gofastr || {};

  // request performs the mutation. It resolves true on a 2xx, false on
  // a non-2xx, a network error, a refused origin or an empty URL, and
  // never throws, so the caller's settlement is always exactly one of
  // the two outcomes. A 2xx is handed to the kernel's invalidation
  // hook first (X-Gofastr-Invalidate), the way src/rpc.js does, so an
  // action refreshes the screens it changed.
  function request(url, method) {
    if (!url) return Promise.resolve(false);
    if (!NS._originOK || !NS._originOK(url)) return Promise.resolve(false);
    // CSRF: forward the page's <meta name="csrf-token"> as X-CSRF-Token
    // on every state-changing fetch, the contract hard rule 10 states.
    // Apps verify the token server-side; the runtime makes the value
    // reachable so each mutation surface does not re-implement the
    // lookup.
    const headers = { Accept: 'application/json' };
    const meta = document.querySelector('meta[name="csrf-token"]');
    const token = meta && meta.getAttribute('content');
    if (token) headers['X-CSRF-Token'] = token;
    return fetch(url, {
      method: (method || 'POST').toUpperCase(),
      credentials: 'same-origin',
      headers,
    }).then((res) => {
      if (!res.ok) return false;
      if (NS._inval) NS._inval(res);
      return true;
    }, () => false);
  }

  // Per-element state. The spec is kept in a WeakMap so a button that
  // leaves the DOM takes its state with it, and binding twice is a
  // no-op through the bound WeakSet: an adapter's scan and the kernel's
  // insertion pass both reach the same element.
  const specs = new WeakMap();
  const bound = new WeakSet();
  // Group members, keyed by group name. A Map of Sets rather than a
  // document scan on every commit: the registry is the truth about what
  // is bound. Members whose elements left the document are pruned at
  // iteration time (an island swap replaces a whole group at once; the
  // stale members must not be revoked-or-kept by accident) and on
  // gofastr:navigate (see the handler below), so a group navigated away
  // from is not held forever.
  const groups = new Map();

  // setState paints one state. idle and done are the two label parts
  // (elements, from the spec): done is shown for pending and committed
  // — the optimistic flip paints the success label the moment the
  // click lands — and idle otherwise. aria-busy lives on pending only:
  // the committed state is final but focusable, so an app can replace
  // the DOM with an undo affordance. The element's disabled attribute
  // is never touched: pending already ignores clicks (see bind), a
  // disabled button drops keyboard focus to the body the moment it is
  // disabled, and disabled has other owners (a hidden conditional
  // region disables its controls) whose decision a settlement must not
  // undo. When the spec asked for pressed, aria-pressed mirrors
  // committed, which is the pressed-toggle convention.
  function setState(el, spec, state) {
    el.setAttribute('data-state', state);
    if (spec.idle && spec.done) {
      if (state === 'pending' || state === 'committed') {
        spec.idle.setAttribute('hidden', '');
        spec.done.removeAttribute('hidden');
      } else {
        spec.idle.removeAttribute('hidden');
        spec.done.setAttribute('hidden', '');
      }
    }
    if (state === 'pending') {
      el.setAttribute('aria-busy', 'true');
    } else {
      el.removeAttribute('aria-busy');
    }
    if (spec.pressed) el.setAttribute('aria-pressed', state === 'committed' ? 'true' : 'false');
  }

  // revokeGroupSiblings flips every other committed member of the group
  // back to idle, with no second request: the server stays the source
  // of truth and a later navigation refreshes from it. It returns the
  // members it revoked so a failed commit can put them back: the
  // server never accepted the new member, so the old one is still the
  // committed one. Members whose elements left the document are pruned
  // here, on iteration, which is the only pass that would otherwise
  // hold them.
  function revokeGroupSiblings(group, except) {
    const revoked = [];
    const members = groups.get(group);
    if (!members) return revoked;
    for (const other of members) {
      if (!other.isConnected) {
        members.delete(other);
        continue;
      }
      if (other === except) continue;
      if (other.getAttribute('data-state') === 'committed') {
        setState(other, specs.get(other), 'idle');
        revoked.push(other);
      }
    }
    if (members.size === 0) groups.delete(group);
    return revoked;
  }

  // stillGrouped reports whether el is still a member of its group in
  // the document: a settlement that arrives after an island swap
  // replaced the group must not revoke the new page's committed member
  // on behalf of a button that is no longer there.
  function stillGrouped(spec, el) {
    if (!spec.group || !el.isConnected) return false;
    const members = groups.get(spec.group);
    return !!members && members.has(el);
  }

  // bind attaches the lifecycle, once per element.
  //
  // spec: endpoint (the commit URL), method (default POST), idle and
  // done (the two label parts, elements), and optionally group (a
  // mutex key), untoggle and pressed. untoggle arms the revert path:
  // a string is the untoggle endpoint, and an empty string means the
  // revert flips locally with no request (an allowed-untoggle button
  // with no endpoint of its own). Left undefined, a committed click is
  // ignored: the button is sticky, which is OptimisticAction's
  // behaviour and the default of ToggleAction's.
  function bind(el, spec) {
    if (bound.has(el)) return;
    bound.add(el);
    specs.set(el, spec);
    if (spec.group) {
      let members = groups.get(spec.group);
      if (!members) {
        members = new Set();
        groups.set(spec.group, members);
      }
      members.add(el);
    }
    el.addEventListener('click', (ev) => {
      ev.preventDefault();
      const state = el.getAttribute('data-state') || 'idle';
      if (state === 'pending') return;
      // A new click cancels any rollback timer a previous error
      // scheduled. Without this, error → click again → pending → the
      // 600ms timer fires and clobbers the state back to idle while
      // the new request is still in flight.
      if (el.__fuiRollbackTimer) {
        clearTimeout(el.__fuiRollbackTimer);
        el.__fuiRollbackTimer = null;
      }
      if (state === 'committed') {
        if (spec.untoggle === undefined) return;
        setState(el, spec, 'pending');
        el.dispatchEvent(new CustomEvent('action:untoggle', { bubbles: true }));
        // No rolled-back event on a failed untoggle: the state simply
        // stays where it was (committed), because the revert was
        // refused and there is nothing to roll back to.
        const settled = spec.untoggle ? request(spec.untoggle, spec.method) : Promise.resolve(true);
        settled.then((ok) => {
          setState(el, spec, ok ? 'idle' : 'committed');
          // A refused untoggle returns this member to committed, and a
          // sibling may have committed inside the same round trip
          // (its settlement revoke skipped this one while it was
          // pending). Returning to committed displaces like any other
          // commit, so the group still converges on one member.
          if (!ok && stillGrouped(spec, el)) revokeGroupSiblings(spec.group, el);
        });
        return;
      }
      // idle or error: commit.
      setState(el, spec, 'pending');
      const revoked = spec.group ? revokeGroupSiblings(spec.group, el) : [];
      el.dispatchEvent(new CustomEvent('action:start', { bubbles: true }));
      request(spec.endpoint, spec.method).then((ok) => {
        if (ok) {
          setState(el, spec, 'committed');
          el.dispatchEvent(new CustomEvent('action:committed', { bubbles: true }));
          // Two members of one group clicked inside one round trip both
          // get here: the re-entry guard is per element and the
          // click-time revoke only sees committed siblings, so without
          // this pass both would end committed. Re-running the revoke on
          // settlement makes the last completer win and the group
          // converge on one committed member. A failure of the later
          // one still restores the sibling it displaced (below).
          if (stillGrouped(spec, el)) revokeGroupSiblings(spec.group, el);
          return;
        }
        // The server refused the new member, so the sibling it
        // displaced is still the committed one: put it back, unless
        // something else moved it or it left the page meanwhile.
        for (const other of revoked) {
          if (other.isConnected && other.getAttribute('data-state') === 'idle') {
            setState(other, specs.get(other), 'committed');
          }
        }
        setState(el, spec, 'error');
        el.dispatchEvent(new CustomEvent('action:rolled-back', { bubbles: true }));
        // After the failure settles (~600ms, the length of the shake
        // the stylesheet plays on [data-state="error"]), return to
        // idle so the button can be tried again. Stored on the element
        // so a new click can cancel it (see above); the timer firing
        // after the element left the DOM writes to a detached node,
        // which touches nothing that is on the page.
        el.__fuiRollbackTimer = setTimeout(() => {
          el.__fuiRollbackTimer = null;
          setState(el, spec, 'idle');
        }, 600);
      });
    });
  }

  // A group whose members all left the document is dropped on the
  // kernel's client-navigation event. The revoke pass prunes too, but it
  // only runs when a sibling commits; a page navigated away from
  // wholesale would otherwise be referenced by the group registry
  // forever, the last holder of every detached button on that page.
  window.addEventListener('gofastr:navigate', () => {
    for (const [name, members] of groups) {
      for (const el of members) {
        if (!el.isConnected) members.delete(el);
      }
      if (members.size === 0) groups.delete(name);
    }
  });

  // _groupCount is test-only: how many group keys the registry still
  // holds. Nothing in the module or the kernel reads it; the retention
  // tests do, so a prune that stops running fails a test instead of
  // leaking quietly.
  NS.action = { request, bind, _groupCount: () => groups.size };
  (NS.loadedModules = NS.loadedModules || {}).action = true;
})();
