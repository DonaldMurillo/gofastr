// headless: the behaviour module for this package's data-hui-* hooks.
// The package that renders the markup owns the JavaScript that binds
// it, the way it owns the stylesheet that styles it: the host serves
// this file as the runtime module "headless" at
// /__gofastr/runtime/headless.js, and the kernel loads it when one of
// its markers is on the page, at boot, on DOM insertion, or after a
// client navigation, handing every inserted subtree and every
// post-navigation document to scan() below. Nothing here observes the
// DOM on its own: a MutationObserver or a navigate listener of our
// own would arm everything a second time on top of the kernel's pass.
//
// Every sentence this module writes arrived as a data-hui-* attribute
// the component rendered from its Strings, so a translated page
// announces in its own language. The only attributes it writes back
// are its own runtime-owned hooks, data-hui-when-off and
// data-hui-drop-over, which no component renders.
(function () {
  'use strict';
  const NAME = 'headless';
  const NS = window.__gofastr = window.__gofastr || {};
  // The kernel fetches a module once per page, but anything that
  // evaluates this file a second time must bind nothing twice.
  if (NS.loadedModules && Object.prototype.hasOwnProperty.call(NS.loadedModules, NAME)) return;

  // The loaded flag is set before anything installs, the contract
  // every registered module keeps: a script that failed halfway has
  // its cached promise dropped, so a retry re-executes this file and
  // would install every listener of the first pass a second time.
  // Everything below the flag is once()-guarded, delegated from the
  // document, or owned by a WeakMap, so running past this point twice
  // binds nothing twice — but only the flag first keeps a half-failed
  // file from being retried into a double install.
  NS.loadedModules = NS.loadedModules || {};
  NS.loadedModules[NAME] = true;

  const HEX = /^#([0-9a-f]{3}|[0-9a-f]{6})$/i;
  const DISMISSED_KEY = 'gofastr.headless.system.dismissed';

  // within(root, sel): root itself when it matches, plus everything
  // matching inside it. The kernel hands scan() one inserted subtree,
  // and a subtree whose root IS the marker is missed by
  // querySelectorAll alone.
  function within(root, sel) {
    const out = [];
    if (root.matches && root.matches(sel)) out.push(root);
    if (root.querySelectorAll) out.push(...root.querySelectorAll(sel));
    return out;
  }

  // once(el, kind) is the arrival pass's idempotence guard. The same
  // element reaches scan() more than once (an island swap inside an
  // already-scanned subtree, the post-navigation document pass), and
  // whatever binds a listener on arrival must not bind it twice.
  const armedFor = new WeakMap();
  function once(el, kind) {
    let kinds = armedFor.get(el);
    if (!kinds) {
      kinds = new Set();
      armedFor.set(el, kinds);
    }
    if (kinds.has(kind)) return false;
    kinds.add(kind);
    return true;
  }

  // ─── reveal (Password) ──────────────────────────────────────────

  // reveal retypes the input and swaps both the button's visible text
  // and its accessible name, so both say what the button will do NEXT.
  // All four strings came from the component's words as data-*, which
  // is why none is said here. The caret is kept where the reader left
  // it: a reveal that costs the typing position fights the person
  // using it.
  function reveal(btn) {
    const shell = btn.closest('[data-hui-affix]');
    const input = shell && shell.querySelector('[data-hui-affix-input]');
    if (!input) return;
    const shown = input.type === 'text';
    input.type = shown ? 'password' : 'text';
    btn.setAttribute('aria-pressed', String(!shown));
    btn.setAttribute('aria-label', shown ? btn.dataset.huiShowLabel : btn.dataset.huiHideLabel);
    btn.textContent = shown ? btn.dataset.huiShowText : btn.dataset.huiHideText;
    const at = input.value.length;
    input.focus();
    try { input.setSelectionRange(at, at); } catch (e) { /* type=password forbids it in Safari */ }
  }

  // ─── colour (Color) ─────────────────────────────────────────────

  // syncColour keeps the swatch and the hex text one value. From the
  // swatch the text takes the uppercase hex; from the text the swatch
  // takes the expanded #rgb or #rrggbb. A non-empty value that is
  // neither marks the shell and stays verbatim in the text: the field
  // edits config, and rewriting an unpickable value to black would
  // destroy it.
  function syncColour(el) {
    const shell = el.closest('[data-hui-affix]');
    if (!shell) return;
    const swatch = shell.querySelector('[data-hui-affix-swatch]');
    const text = shell.querySelector('[data-hui-affix-input]');
    if (!swatch || !text) return;
    if (el === swatch) {
      text.value = swatch.value.toUpperCase();
      shell.removeAttribute('data-invalid');
      return;
    }
    const v = text.value.trim();
    if (HEX.test(v)) {
      swatch.value = v.length === 4 ? '#' + v[1] + v[1] + v[2] + v[2] + v[3] + v[3] : v;
      shell.removeAttribute('data-invalid');
    } else if (v !== '') {
      shell.setAttribute('data-invalid', '');
    }
  }

  // ─── when (ConditionalField) ────────────────────────────────────

  // watchedControls finds the controls a region's condition reads.
  // The region's own form comes first: two forms on one page can each
  // carry a "plan" control, and a region inside one form must follow
  // that form's plan, not whichever control the document happens to
  // offer first. A region with no form of its own — or one whose form
  // holds no control of that name — reads the document, preferring
  // controls no form owns: a form-less control is a page-level switch
  // a region outside the forms can belong to, where the first form's
  // control of the same name is that form's business. Among several
  // candidates the first in document order wins, so the rule is
  // deterministic.
  function watchedControls(region, name) {
    const sel = '[name="' + CSS.escape(name) + '"]';
    const all = document.querySelectorAll(sel);
    const form = region.closest('form');
    if (form) {
      // The form's controls are the ones it owns, not the ones inside
      // it: a control outside the element with form="id" belongs to
      // it, and one inside with form= pointing elsewhere does not.
      const own = [];
      for (const a of all) {
        if (a.form === form) own.push(a);
      }
      if (own.length) return own;
    }
    const loose = [];
    for (const a of all) {
      if (!a.form) loose.push(a);
    }
    return loose.length ? loose : all;
  }

  // whenValue reads the watched field's value the way the form would
  // submit it: the checked radio's value, a checkbox's value when
  // checked and the empty string when not, and any other control's
  // value.
  function whenValue(fields) {
    for (let i = 0; i < fields.length; i++) {
      const el = fields[i];
      if (el.type === 'radio') {
        if (el.checked) return el.value;
        continue;
      }
      if (el.type === 'checkbox') return el.checked ? el.value : '';
      return el.value;
    }
    return '';
  }

  // insideHiddenWhen reports whether el sits inside a [data-hui-when]
  // region that is hidden. Regions nest, and each hides on its own
  // condition; a region inside a hidden region is out whatever its
  // own condition says, because showing it would reach controls the
  // outer region's condition meant to keep out of the page and out of
  // the submit.
  function insideHiddenWhen(el) {
    for (let anc = el.parentElement; anc; anc = anc.parentElement) {
      if (anc.matches && anc.matches('[data-hui-when]') && anc.hidden) return true;
    }
    return false;
  }

  // syncWhenRegions is the whole when behaviour in two passes. The
  // first sets every region's effective visibility — its own
  // condition AND no hidden ancestor region — in document order, so
  // an outer region's fresh state is already on it when its
  // descendants look up. The second disables exactly the controls
  // inside any hidden region and re-enables only the controls hiding
  // disabled, told apart by the runtime-owned data-hui-when-off mark:
  // a control the page disabled itself is never touched. One mark
  // serves the whole nest because the second pass asks where the
  // control sits NOW, not which region disabled it — an inner region
  // showing inside a hidden outer one re-enables nothing.
  function syncWhenRegions(regions) {
    for (const region of regions) {
      const own = whenValue(watchedControls(region, region.dataset.huiWhen)) === region.dataset.huiWhenValue;
      region.hidden = !(own && !insideHiddenWhen(region));
    }
    for (const region of regions) {
      const controls = region.querySelectorAll('input, select, textarea, button');
      for (const c of controls) {
        if (insideHiddenWhen(c)) {
          if (!c.disabled) {
            c.disabled = true;
            c.dataset.huiWhenOff = '';
          }
        } else if (c.dataset.huiWhenOff !== undefined) {
          c.disabled = false;
          delete c.dataset.huiWhenOff;
        }
      }
    }
  }

  // ─── form errors (Form with a ValidationSummary) ────────────────

  // A failed submit is announced by moving focus to the summary, once
  // per form element. A swap that brings the SAME form back (an island
  // re-render of the region around it) must not steal focus again; a
  // NEW form element with the same errors is focused, because a reader
  // who submitted again and failed again has to be told.
  function armFormErrors(root) {
    const forms = within(root, '[data-hui-form-errors]');
    // Every form in the pass is marked, and only the first summary is
    // focused: a form left unmarked because an earlier one took the
    // focus would take it itself on the next pass, from wherever the
    // reader had moved to by then.
    let announced = false;
    for (let i = 0; i < forms.length; i++) {
      const form = forms[i];
      const summary = form.querySelector('[role="alert"][tabindex="-1"]');
      // The once-mark is spent only when there is a summary to focus.
      // A form whose Errors is not a summary yet — a plain message
      // while the server still renders one — must keep its mark, or
      // the summary that arrives on a later render of the same form
      // would find the mark spent and the failed submit would stay
      // unannounced.
      if (!summary) continue;
      if (!once(form, 'errors')) continue;
      if (!announced) {
        summary.focus();
        announced = true;
      }
    }
  }

  // ─── action (OptimisticAction / ToggleAction) ───────────────────

  // The buttons bind through the kernel's action primitive, which the
  // loader has registered before this module evaluates (the
  // registration Requires it): the primitive owns the request, the
  // state machine and the label flip; this module owns the hooks the
  // components render. pressed is read off the aria-pressed the
  // component ships, which is present exactly when the button is a
  // ToggleAction (OptimisticAction commits once and claims no pressed
  // state, and a caller cannot inject the attribute: ExtraAttrs are
  // refused by safeActionExtras and Parts.Attrs on the root by the
  // render, one shared owned list), so the rule needs no second
  // attribute to say what the markup already says.

  function armActions(root) {
    const btns = within(root, '[data-hui-action]');
    for (let i = 0; i < btns.length; i++) {
      const btn = btns[i];
      const idle = btn.querySelector('[data-hui-action-idle]');
      const done = btn.querySelector('[data-hui-action-done]');
      const endpoint = btn.getAttribute('data-hui-action-endpoint');
      if (!idle || !done || !endpoint) continue;
      const spec = {
        endpoint,
        method: btn.getAttribute('data-hui-action-method') || 'POST',
        idle,
        done,
        pressed: btn.getAttribute('aria-pressed') !== null,
      };
      const group = btn.getAttribute('data-hui-action-group');
      if (group) spec.group = group;
      // The untoggle hook is present whenever the button may revert:
      // its value is the revert endpoint, empty for the local flip
      // with no request of its own.
      if (btn.hasAttribute('data-hui-action-untoggle')) {
        spec.untoggle = btn.getAttribute('data-hui-action-untoggle') || '';
      }
      window.__gofastr.action.bind(btn, spec);
    }
  }

  // A rolled-back mutation announces its failure sentence in the
  // polite status span, the words coming from the root's
  // data-hui-action-failed. The primitive dispatches action:rolled-back
  // for a failed commit on either button, so a failed toggle speaks
  // too; it stays silent on a failed untoggle, where the state simply
  // stayed and nothing was rolled back.
  function announceFailure(btn) {
    const status = btn.querySelector('[data-hui-action-status]');
    if (!status) return;
    const text = btn.dataset.huiActionFailed || '';
    status.textContent = '';
    requestAnimationFrame(function () { status.textContent = text; });
  }

  // ─── drop (FileUpload) ──────────────────────────────────────────

  // showFiles lists the chosen names and says the sentence, both built
  // from the words the component rendered on the root:
  // data-hui-drop-one for a single file with {name}, data-hui-drop-many
  // for several with {n} and {names}. An empty selection clears both,
  // so changing one's mind leaves nothing behind.
  function showFiles(root) {
    const input = document.getElementById(root.dataset.huiDropInput);
    const list = root.querySelector('[data-hui-drop-list]');
    const status = root.querySelector('[data-hui-drop-status]');
    if (!input || !list) return;
    list.textContent = '';
    const files = input.files || [];
    const names = [];
    for (let i = 0; i < files.length; i++) {
      const li = document.createElement('li');
      li.textContent = files[i].name;
      list.appendChild(li);
      names.push(files[i].name);
    }
    if (!status) return;
    if (files.length === 1) {
      status.textContent = (root.dataset.huiDropOne || '').replace('{name}', files[0].name);
    } else if (files.length > 1) {
      status.textContent = (root.dataset.huiDropMany || '')
        .replace('{n}', String(files.length))
        .replace('{names}', names.join(', '));
    } else {
      status.textContent = '';
    }
  }

  function armDrop(root) {
    if (!once(root, 'drop')) return;
    function stop(e) { e.preventDefault(); e.stopPropagation(); }
    root.addEventListener('dragenter', function (e) { stop(e); root.dataset.huiDropOver = ''; });
    root.addEventListener('dragover', function (e) { stop(e); root.dataset.huiDropOver = ''; });
    root.addEventListener('dragleave', function (e) { stop(e); delete root.dataset.huiDropOver; });
    root.addEventListener('drop', function (e) {
      stop(e);
      delete root.dataset.huiDropOver;
      // The input is resolved on every event from the hook on the
      // root, never captured at arm time: a swap that replaced the
      // input while the zone survived would leave a captured one
      // pointing at a detached element, and the drop would set files
      // on an input nothing submits.
      const input = document.getElementById(root.dataset.huiDropInput);
      if (!input || input.disabled || !e.dataTransfer) return;
      // A disabled input keeps its files: the drop is refused rather
      // than silently queued for a control that cannot submit.
      // The picker lets one file through an input without multiple;
      // a drop keeps the same rule rather than smuggling several past
      // it. The first file is the one taken, as the picker would take
      // the one chosen.
      let dropped = e.dataTransfer.files;
      if (!input.multiple && dropped.length > 1) {
        const one = new DataTransfer();
        one.items.add(dropped[0]);
        dropped = one.files;
      }
      input.files = dropped;
      showFiles(root);
      input.dispatchEvent(new Event('change', { bubbles: true }));
    });
  }


  function armDrops(root) {
    for (const r of within(root, '[data-hui-drop]')) armDrop(r);
  }

  // ─── system banners (SystemBanner) ──────────────────────────────

  // A dismissed system message is remembered for the session, so an
  // island swap that re-renders it does not say it twice. The store
  // can be refused (private mode, policy): every access is guarded and
  // the worst case is a message shown again.
  const systemDismissed = new Set();
  try {
    const stored = JSON.parse(sessionStorage.getItem(DISMISSED_KEY) || '[]');
    for (const s of stored) systemDismissed.add(s);
  } catch (e) { /* no storage: dismissals last until the page does */ }

  function rememberDismissal(id) {
    systemDismissed.add(id);
    try {
      const ids = [];
      systemDismissed.forEach(function (v) { ids.push(v); });
      sessionStorage.setItem(DISMISSED_KEY, JSON.stringify(ids));
    } catch (e) { /* no storage: nothing to remember with */ }
  }

  function armSystem(root) {
    for (const el of within(root, '[data-hui-system]')) {
      if (el.hasAttribute('data-hui-system-offline')) {
        // The runtime owns this one. The dismissed set never applies:
        // the banner has no dismiss memory of its own, because losing
        // the connection again must show it again. And a banner that
        // arrived after the connection was already lost waits for no
        // event — the state sse.js mirrors is read here, on arm.
        el.hidden = !sseLost(NS.sseStatus);
        continue;
      }
      if (!el.hidden && systemDismissed.has(el.dataset.huiSystemId)) el.hidden = true;
    }
  }

  // sseLost reads the connection state sse.js mirrors onto
  // window.__gofastr.sseStatus — one object mutated in place, and the
  // same shape on every gofastr:sse-status detail: lost once a retry
  // is actually scheduled, because a blip during the first connect is
  // not an outage. The field names read here are pinned to the ones
  // sse.js assigns by a source gate in behavior_test.go, so a rename
  // there fails in this package's tests, not in production.
  function sseLost(st) {
    return !!st && st.connected === false && st.retryCount > 0;
  }

  // ─── table (Table) ──────────────────────────────────────────────

  // An island sort or page turn is a swap: the runtime writes the
  // region's HTML, the anchor the reader clicked is destroyed with
  // it, and focus falls to <body> with nothing saying what changed.
  // The click listener accepts a signal-bound table and records the
  // clicked control's identity and replacement region; when a fresh
  // answer inserts a table in that region, armTables returns focus
  // to the same control in that region — the same column's anchor
  // for a sort, the same page's anchor for a page turn, the current
  // page's anchor when the answer has fewer pages, and the scroll
  // region when the answer dropped the control — and copies the
  // sentence the server rendered into data-hui-table-announcement
  // into the status. A failed answer inserts no table and leaves the
  // existing focus and status alone.
  function armTables(root) {
    const p = NS._huiTableSwap;
    if (!p) return;
    // Thirty seconds covers a cold module fetch and a slow answer; it
    // still prevents an old click from owning a later passive swap.
    if (p[3] + 3e4 < performance.now()) return NS._huiTableSwap = null;
    for (const x of within(root, '[data-hui-table]')) {
      if (x.parentNode !== p[2]) continue;
      NS._huiTableSwap = null;
      // a column key and a page number are both anything a query can
      // encode, and one with a quote or a bracket in it must find its
      // control like any other. The control the answer dropped leaves
      // el null — for a page that is gone, the current page's anchor
      // is where the reader lands — and the scroll region, the
      // table's own focusable surface, takes the focus after that.
      let el = null;
      for (const a of x.querySelectorAll('[' + CSS.escape(p[0]) + ']')) {
        if (a.getAttribute(p[0]) === p[1]) { el = a; break; }
      }
      if (!el && p[0] === 'data-hui-page') el = x.querySelector('[aria-current="page"]');
      if (!el) el = x.querySelector('[data-hui-table-scroll]');
      el.focus({preventScroll:!0});
      // The sentence is the server's, composed from the component's
      // Strings into data-hui-table-announcement: the module copies
      // it, clear then frame, so a repeated identical sentence is
      // announced again the way the action module's failure is.
      const status = x.querySelector('[data-hui-table-status]');
      if (status) {
        const text = x.getAttribute('data-hui-table-announcement') || '';
        status.textContent = '';
        requestAnimationFrame(function () { status.textContent = text; });
      }
      return;
    }
  }

  // ─── delegated listeners ────────────────────────────────────────

  // Clicks, typing and the two custom events are delegated from the
  // document and bound once at load, which is why markup that arrives
  // later (an island swap, a client navigation) needs no re-binding
  // for them: the listener was never on the element.
  document.addEventListener('click', function (e) {
    const t = e.target;
    if (!t || !t.closest) return;
    // One record: whichever control the reader clicked — a sort
    // anchor, or the pager's page anchor, which the table treats
    // exactly as a sort click — owns the next swap's restore. A
    // click on anything else clears it, so an old click cannot own a
    // later passive swap.
    const c = t.closest('[data-hui-table-sort],[data-hui-page]'),
      table = c?.closest('[data-hui-table-signal]'),
      page = c?.hasAttribute('data-hui-page');
    NS._huiTableSwap = table && [page ? 'data-hui-page' : 'data-hui-table-sort',
      c.getAttribute(page ? 'data-hui-page' : 'data-hui-table-sort'),
      table.parentNode, performance.now()];
    const btn = t.closest('[data-hui-reveal]');
    if (btn) {
      e.preventDefault();
      reveal(btn);
      return;
    }
    const dismiss = t.closest('[data-hui-system-dismiss]');
    if (dismiss) {
      const el = dismiss.closest('[data-hui-system]');
      if (!el) return;
      e.preventDefault();
      el.hidden = true;
      if (el.dataset.huiSystemId) rememberDismissal(el.dataset.huiSystemId);
    }
  });

  document.addEventListener('input', function (e) {
    const t = e.target;
    if (!t || !t.closest) return;
    if (t.matches('[data-hui-affix-swatch], [data-hui-color] [data-hui-affix-input]')) syncColour(t);
    // The document, not the control's form: a region may watch a
    // control outside its own form, and one outside every form may
    // watch a control inside one, so no smaller scope holds.
    syncWhenRegions(document.querySelectorAll('[data-hui-when]'));
  });

  document.addEventListener('change', function (e) {
    const t = e.target;
    if (!t || !t.closest) return;
    const root = t.closest('[data-hui-drop]');
    if (root && t.type === 'file') showFiles(root);
    syncWhenRegions(document.querySelectorAll('[data-hui-when]'));
  });

  document.addEventListener('action:rolled-back', function (e) {
    const btn = e.target && e.target.closest && e.target.closest('[data-hui-action]');
    if (btn) announceFailure(btn);
  });

  // The offline banner follows the connection the framework reports:
  // shown once a retry is actually scheduled (a blip during the first
  // connect is not an outage), hidden when the link is back. The
  // dismissed set does not apply here, because the banner has no
  // dismiss memory of its own: losing the connection again must show
  // it again.
  document.addEventListener('gofastr:sse-status', function (e) {
    const lost = sseLost(e && e.detail);
    for (const b of document.querySelectorAll('[data-hui-system-offline]')) b.hidden = !lost;
  });

  // ─── the arrival pass ───────────────────────────────────────────

  // scan arms what arrival alone cannot: the summary focus, the drag
  // listeners, the when-regions' first sync, the dismissed banners,
  // the action buttons' bind, the sort or page the reader just
  // clicked made whole again. It is what the kernel calls on every inserted
  // subtree and over the document after a client navigation, and it
  // is idempotent: once() guards what binds a listener, and the
  // primitive's own WeakSet guards the buttons.
  function scan(root) {
    if (root === document) NS._huiTableSwap = null;
    const scope = root && root.querySelectorAll ? root : document;
    armFormErrors(scope);
    armActions(scope);
    armDrops(scope);
    armTables(scope);
    let regions = within(scope, '[data-hui-when]');
    // A subtree inserted inside a region arrives with no region of its
    // own above it: sync every enclosing region as well, so a control
    // inserted alone inside a hidden region is disabled like the
    // siblings it joined. The enclosing regions go FIRST, outermost
    // first, because the first pass reads an ancestor's hidden state
    // as it goes: an inserted swap that restores the gating value of
    // the region around it must un-hide that region before the regions
    // inside the swap look up, or they read the stale hidden and stay
    // buried until the next input.
    if (scope !== document && scope.closest) {
      const enclosing = [];
      for (let r = scope.closest('[data-hui-when]'); r; r = r.parentElement && r.parentElement.closest('[data-hui-when]')) {
        if (regions.indexOf(r) === -1) enclosing.unshift(r);
      }
      regions = enclosing.concat(regions);
    }
    if (regions.length) syncWhenRegions(regions);
    armSystem(scope);
  }

  scan(document);
  NS._moduleScanners = NS._moduleScanners || {};
  NS._moduleScanners[NAME] = scan;
})();
