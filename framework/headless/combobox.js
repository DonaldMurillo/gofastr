// headless-combobox: the behaviour module for this package's
// comboboxes — a [role=combobox] input paired with its
// [role=listbox] through aria-controls. The input owns focus through
// every interaction; the module owns the keyboard contract
// (ArrowDown/ArrowUp open and move aria-activedescendant with wrap,
// Home/End jump, Enter commits the active option, Escape closes and a
// second Escape clears the query, Tab closes and lets focus move),
// pointer picks, focus auto-open, outside-click close, and the static
// list's client-side filtering. The RPC debounce and the signal swap
// are the kernel's data-fui-rpc contract, not reimplemented here.
(function () {
  'use strict';
  const NAME = 'headless-combobox';
  const NS = window.__gofastr = window.__gofastr || {};
  if (NS.loadedModules && Object.prototype.hasOwnProperty.call(NS.loadedModules, NAME)) return;
  NS.loadedModules = NS.loadedModules || {};
  NS.loadedModules[NAME] = true;

  function within(root, sel) {
    const out = [];
    if (root.matches && root.matches(sel)) out.push(root);
    if (root.querySelectorAll) out.push.apply(out, root.querySelectorAll(sel));
    return out;
  }

  function inputOf(lb) {
    if (!lb.id) return null;
    return document.querySelector('[data-hui-combobox-input][aria-controls="' + cssEscape(lb.id) + '"]');
  }

  function cssEscape(s) {
    if (window.CSS && CSS.escape) return CSS.escape(s);
    let out = '';
    if (s.length > 0 && s.charCodeAt(0) >= 48 && s.charCodeAt(0) <= 57) {
      out = '\\3' + s.charAt(0) + ' ';
      s = s.slice(1);
    }
    return out + s.replace(/([!"#$%&'()*+,./:;<=>?@[\]^`{|}~])/g, '\\$1');
  }

  function highlight(lb, opt) {
    for (const o of lb.querySelectorAll('[role="option"].is-active')) o.classList.remove('is-active');
    if (opt) {
      opt.classList.add('is-active');
      const input = inputOf(lb);
      if (input) input.setAttribute('aria-activedescendant', opt.id || '');
    }
  }

  function say(lb, what) {
    const input = inputOf(lb);
    const root = input && input.closest('[data-hui-combobox]');
    const status = root && root.querySelector('[data-hui-combobox-status]');
    if (status) status.textContent = what || '';
  }

  function announce(lb) {
    // The count sentence travels on the listbox; {n} is written in
    // when the count is known. No sentence, no announcement.
    const fmt = lb.getAttribute('data-hui-combobox-count');
    if (!fmt) return;
    const n = lb.querySelectorAll('[role="option"]:not([hidden])').length;
    if (n === 0) {
      say(lb, lb.getAttribute('data-hui-combobox-no-results') || '');
      return;
    }
    say(lb, fmt.replace('{n}', String(n)));
  }

  function closeListbox(input, lb) {
    input.setAttribute('aria-expanded', 'false');
    input.setAttribute('aria-activedescendant', '');
    if (lb) {
      lb.setAttribute('hidden', '');
      for (const o of lb.querySelectorAll('[role="option"].is-active')) o.classList.remove('is-active');
    }
  }

  function openListbox(input, lb) {
    if (!lb) return;
    input.setAttribute('aria-expanded', 'true');
    lb.removeAttribute('hidden');
  }

  // sameOriginDest for the pre-boot pick path: a javascript: or data:
  // URL resolves to the opaque "null" origin and is refused.
  function sameOriginDest(u) {
    try { return new URL(String(u ?? ''), location.href).origin === location.origin; }
    catch (_) { return false; }
  }

  function pickOption(input, lb, opt) {
    if (!opt) return;
    const val = opt.getAttribute('data-value') || (opt.textContent || '').trim();
    input.value = val;
    input.dispatchEvent(new Event('change', { bubbles: true }));
    closeListbox(input, lb);
    // A picked option's destination rides data-fui-push-state (or the
    // anchor's own href): a same-origin pick navigates through the SPA
    // navigator, closing any enclosing widget first so the navigation
    // is not behind a backdrop.
    const dest = opt.getAttribute('data-fui-push-state') ||
      (opt.tagName === 'A' ? opt.getAttribute('href') : null);
    if (dest && (NS._originOK ? NS._originOK(dest) : sameOriginDest(dest))) {
      const widget = opt.closest('[data-fui-widget]');
      if (widget && NS.closeWidget) {
        try { NS.closeWidget(widget.getAttribute('data-fui-widget')); } catch (_) {}
      }
      if (NS.navigate) NS.navigate(dest);
      else location.href = dest;
    }
  }

  function options(lb) {
    return Array.from(lb.querySelectorAll(
      '[role="option"]:not([aria-disabled="true"]):not([hidden])'
    ));
  }

  document.addEventListener('keydown', function (e) {
    const input = e.target && e.target.closest && e.target.closest('[data-hui-combobox-input]');
    if (!input) return;
    const lbId = input.getAttribute('aria-controls');
    if (!lbId) return;
    const lb = document.getElementById(lbId);
    if (!lb) return;
    const opts = options(lb);
    const activeId = input.getAttribute('aria-activedescendant');
    const activeIdx = opts.findIndex(function (o) { return o.id === activeId; });
    const isOpen = input.getAttribute('aria-expanded') === 'true';

    switch (e.key) {
      case 'ArrowDown': {
        if (opts.length === 0) return;
        e.preventDefault();
        if (!isOpen) { openListbox(input, lb); highlight(lb, opts[0]); return; }
        highlight(lb, opts[(activeIdx + 1 + opts.length) % opts.length]);
        return;
      }
      case 'ArrowUp': {
        if (opts.length === 0) return;
        e.preventDefault();
        if (!isOpen) { openListbox(input, lb); highlight(lb, opts[opts.length - 1]); return; }
        highlight(lb, opts[(activeIdx - 1 + opts.length) % opts.length]);
        return;
      }
      case 'Home': {
        if (!isOpen || opts.length === 0) return;
        e.preventDefault();
        highlight(lb, opts[0]);
        return;
      }
      case 'End': {
        if (!isOpen || opts.length === 0) return;
        e.preventDefault();
        highlight(lb, opts[opts.length - 1]);
        return;
      }
      case 'Enter': {
        if (!isOpen || activeIdx < 0) return;
        e.preventDefault();
        pickOption(input, lb, opts[activeIdx]);
        return;
      }
      case 'Escape': {
        if (isOpen) { e.preventDefault(); closeListbox(input, lb); return; }
        if (input.value) {
          e.preventDefault();
          input.value = '';
          input.dispatchEvent(new Event('input', { bubbles: true }));
        }
        return;
      }
      case 'Tab': {
        if (isOpen) closeListbox(input, lb);
        return;
      }
    }
  });

  // Click-to-pick, delegated so island-swapped options are covered.
  document.addEventListener('click', function (e) {
    const opt = e.target && e.target.closest && e.target.closest('[role="option"]');
    if (!opt || opt.getAttribute('aria-disabled') === 'true') return;
    const lb = opt.closest('[data-hui-combobox-listbox]');
    if (!lb || !lb.id) return;
    const input = inputOf(lb);
    if (!input) return;
    e.preventDefault();
    pickOption(input, lb, opt);
  });

  // Focus auto-open: a refocused input whose listbox already holds
  // options reopens so the reader can continue.
  document.addEventListener('focusin', function (e) {
    const input = e.target && e.target.closest && e.target.closest('[data-hui-combobox-input]');
    if (!input) return;
    const lbId = input.getAttribute('aria-controls');
    const lb = lbId ? document.getElementById(lbId) : null;
    if (lb && lb.querySelector('[role="option"]')) openListbox(input, lb);
  });

  // Outside-click closes any open combobox.
  document.addEventListener('click', function (e) {
    for (const input of document.querySelectorAll('[data-hui-combobox-input][aria-expanded="true"]')) {
      const lbId = input.getAttribute('aria-controls');
      const lb = lbId ? document.getElementById(lbId) : null;
      if (input.contains(e.target) || (lb && lb.contains(e.target))) continue;
      closeListbox(input, lb);
    }
  });

  // The island dialect's loading word: the input event fired the RPC
  // (the kernel owns the debounce and the swap); the status region
  // says the word until the swap lands and the arrival pass announces
  // the count. The word rides the loader carrier's hook.
  document.addEventListener('input', function (e) {
    const loader = e.target && e.target.closest && e.target.closest('[data-hui-combobox-loader]');
    if (loader) {
      const root = loader.closest('[data-hui-combobox]');
      const status = root && root.querySelector('[data-hui-combobox-status]');
      const word = loader.getAttribute('data-hui-combobox-loading') || '';
      if (status && word) status.textContent = word;
    }
  }, true);

  // Static-option filtering: hide non-matches, show all when the query
  // clears, highlight the first match, and announce through the count
  // sentence.
  document.addEventListener('input', function (e) {
    const input = e.target && e.target.closest && e.target.closest('[data-hui-combobox-input]');
    if (!input) return;
    const lbId = input.getAttribute('aria-controls');
    const lb = lbId ? document.getElementById(lbId) : null;
    if (!lb || !lb.hasAttribute('data-hui-combobox-static')) return;
    const q = (input.value || '').toLowerCase().trim();
    let firstVisible = null;
    let anyVisible = false;
    lb.querySelectorAll('[role="option"]').forEach(function (opt) {
      const match = !q || (opt.textContent || '').toLowerCase().indexOf(q) !== -1;
      opt.hidden = !match;
      if (match) { anyVisible = true; if (!firstVisible) firstVisible = opt; }
    });
    if (anyVisible) { openListbox(input, lb); highlight(lb, firstVisible); }
    else input.setAttribute('aria-activedescendant', '');
    announce(lb);
  });

  // The arrival pass: announce the static list's count, and close any
  // listbox whose island region was swapped empty.
  function scan(root) {
    const scope = root && root.querySelectorAll ? root : document;
    for (const lb of within(scope, '[data-hui-combobox-listbox]')) {
      if (lb.hasAttribute('data-hui-combobox-static')) announce(lb);
    }
  }

  scan(document);
  NS._moduleScanners = NS._moduleScanners || {};
  NS._moduleScanners[NAME] = scan;
})();
