// The dev layout layer (framework/dev/layoutlayer.go): served only under
// `gofastr dev`. Alt+Shift+L draws the page's structure as labeled
// outlines: the layout layers and their slots, outlets and areas
// (data-cui-layout*, data-cui-outlet, data-cui-area), owned-style scopes,
// islands and widgets. It also marks every box that scrolls and every
// element that runs past the viewport, the phone-width bug class.
//
// The layer lives in a shadow root on a fixed host outside <body>, so app
// CSS cannot reach it and body swaps do not remove it. Every style is a
// CSSOM write, which the default CSP allows. Nothing is built until the
// layer is shown; the on/off choice is kept in localStorage so it
// survives livereload's reloads.
(() => {
  if (window.__gofastrDevLayout) return;
  const KEY = 'gofastr.dev.layout';
  const MARKERS = [
    { attr: 'data-cui-layout', name: 'layout', color: '#8b5cf6', line: 'solid',
      text: (el, v) => 'layout ' + v + ' · ' + (el.getAttribute('data-cui-layout-key') || '') },
    { attr: 'data-cui-layout-slot', name: 'slot', color: '#3b82f6', line: 'dashed' },
    { attr: 'data-cui-outlet', name: 'outlet', color: '#0d9488', line: 'solid' },
    { attr: 'data-cui-area', name: 'area', color: '#0d9488', line: 'dashed' },
    { attr: 'data-cui-scope', name: 'scope', color: '#d97706', line: 'dotted' },
    { attr: 'data-island', name: 'island', color: '#16a34a', line: 'solid' },
    { attr: 'data-cui-widget', name: 'widget', color: '#db2777', line: 'solid' },
  ];
  const SCROLL = { name: 'scrolls', color: '#64748b', line: 'dotted' };
  const OVERFLOW = { name: 'overflows', color: '#dc2626', line: 'solid' };
  const LABEL_H = 15;

  let host = null, root = null, items = [], pageOverflow = 0;
  let observer = null, timer = 0, collectTimer = 0, frame = 0;

  const read = () => { try { return localStorage.getItem(KEY) === '1'; } catch (_) { return false; } };
  const write = (on) => { try { on ? localStorage.setItem(KEY, '1') : localStorage.removeItem(KEY); } catch (_) {} };

  const css = (el, props) => { for (const k in props) el.style.setProperty(k, props[k]); return el; };
  const make = (tag, props, text) => {
    const el = css(document.createElement(tag), props);
    if (text != null) el.textContent = text;
    return el;
  };

  const scrolls = (v) => v === 'auto' || v === 'scroll';

  // collect walks the page once: the marker elements, then every element
  // for scroll boxes and viewport overflow. An element past the viewport
  // counts only when nothing above it scrolls, clips or fixes it (those
  // are deliberate: a scroll box, a carousel, an off-canvas drawer), and
  // only the outermost such element is reported.
  const collect = () => {
    collectTimer = 0;
    if (!host || !document.body) return;
    const next = [];
    for (const m of MARKERS) {
      for (const el of document.querySelectorAll('[' + m.attr + ']')) {
        const v = el.getAttribute(m.attr);
        next.push({ el, kind: m, text: m.text ? m.text(el, v) : m.name + ' ' + v });
      }
    }
    const vw = document.documentElement.clientWidth;
    const sx = window.scrollX;
    const down = new Map();
    const walker = document.createTreeWalker(document.body, NodeFilter.SHOW_ELEMENT);
    for (let el = walker.nextNode(); el; el = walker.nextNode()) {
      const above = down.get(el.parentElement) || '';
      if (above === 'hidden') { down.set(el, 'hidden'); continue; }
      const cs = getComputedStyle(el);
      if (cs.display === 'none') { down.set(el, 'hidden'); continue; }
      let mine = above || (cs.position === 'fixed' ? 'fixed' : '');
      if (!mine && cs.visibility !== 'hidden' && cs.display !== 'contents') {
        const r = el.getBoundingClientRect();
        const past = Math.max(r.right + sx - vw, -(r.left + sx));
        if (r.width > 0 && past > 1) {
          next.push({ el, kind: OVERFLOW, text: 'overflows +' + Math.round(past) + 'px' });
          mine = 'flagged';
        }
      }
      const sxOK = scrolls(cs.overflowX) && el.scrollWidth > el.clientWidth + 1;
      const syOK = scrolls(cs.overflowY) && el.scrollHeight > el.clientHeight + 1;
      if (sxOK || syOK) {
        next.push({ el, kind: SCROLL, text: 'scrolls ' + (sxOK ? 'x' : '') + (sxOK && syOK ? ' ' : '') + (syOK ? 'y' : '') });
      }
      down.set(el, mine || (cs.overflowX !== 'visible' ? 'clip' : ''));
    }
    items = next;
    pageOverflow = document.documentElement.scrollWidth - vw;
    draw();
  };

  const scheduleCollect = () => {
    if (!collectTimer) collectTimer = setTimeout(collect, 150);
  };
  const scheduleDraw = () => {
    if (!frame) frame = requestAnimationFrame(() => { frame = 0; draw(); });
  };

  const draw = () => {
    if (!root) return;
    root.replaceChildren();
    const vw = innerWidth, vh = innerHeight;
    const placed = [];
    const counts = new Map();
    for (const it of items) {
      if (!it.el.isConnected) continue;
      const r = it.el.getBoundingClientRect();
      if (r.width === 0 && r.height === 0) continue;
      counts.set(it.kind, (counts.get(it.kind) || 0) + 1);
      if (r.bottom < 0 || r.top > vh || r.right < 0 || r.left > vw) continue;
      const bad = it.kind === OVERFLOW;
      root.append(make('div', {
        position: 'absolute', left: r.left + 'px', top: r.top + 'px',
        width: r.width + 'px', height: r.height + 'px', 'box-sizing': 'border-box',
        border: (bad ? 2 : 1.5) + 'px ' + it.kind.line + ' ' + it.kind.color,
        background: bad ? 'rgba(220, 38, 38, 0.12)' : 'transparent',
      }));
      // Labels sit at the box's top-left, pushed down past any label
      // already there: a layer and its slot often share a corner.
      let x = Math.min(Math.max(r.left, 0), vw - 40), y = Math.max(r.top, 0);
      const w = it.text.length * 6.6 + 8;
      for (let i = 0; i < 8 && placed.some((p) => x < p.x + p.w && p.x < x + w && Math.abs(p.y - y) < LABEL_H); i++) y += LABEL_H;
      placed.push({ x, y, w });
      root.append(make('div', {
        position: 'absolute', left: x + 'px', top: y + 'px', height: LABEL_H + 'px',
        padding: '0 4px', 'border-radius': '0 0 3px 0', 'white-space': 'nowrap',
        font: '11px/15px ui-monospace, SFMono-Regular, Menlo, monospace',
        color: '#fff', background: it.kind.color,
      }, it.text));
    }
    legend(counts);
  };

  const legend = (counts) => {
    const box = make('div', {
      position: 'absolute', left: '8px', bottom: '8px', padding: '6px 8px',
      'border-radius': '6px', background: 'rgba(17, 17, 17, 0.88)', color: '#fff',
      font: '11px/16px ui-monospace, SFMono-Regular, Menlo, monospace',
    });
    box.append(make('div', { 'font-weight': '600' }, 'layout layer · Alt+Shift+L'));
    for (const k of [...MARKERS, SCROLL, OVERFLOW]) {
      const n = counts.get(k);
      if (!n) continue;
      const row = make('div', { display: 'flex', 'align-items': 'center', gap: '6px' });
      row.append(make('span', { width: '10px', height: '10px', border: '2px ' + k.line + ' ' + k.color, 'box-sizing': 'border-box' }));
      row.append(document.createTextNode(k.name + ' ' + n));
      box.append(row);
    }
    if (pageOverflow > 0) {
      box.append(make('div', { color: '#fca5a5' }, 'page scrolls sideways +' + pageOverflow + 'px'));
    }
    root.append(box);
  };

  const onMutate = (records) => {
    if (records.every((r) => r.target === host)) return;
    scheduleCollect();
  };

  const show = () => {
    if (host) return;
    host = css(document.createElement('fui-dev-layout'), {
      position: 'fixed', inset: '0', 'z-index': '2147483647',
      'pointer-events': 'none', overflow: 'hidden', contain: 'strict',
    });
    host.setAttribute('aria-hidden', 'true');
    root = host.attachShadow({ mode: 'open' });
    document.documentElement.append(host);
    observer = new MutationObserver(onMutate);
    observer.observe(document.documentElement, {
      subtree: true, childList: true, attributes: true,
      attributeFilter: ['class', 'hidden', 'open', ...MARKERS.map((m) => m.attr)],
    });
    addEventListener('scroll', scheduleDraw, { capture: true, passive: true });
    addEventListener('resize', scheduleCollect);
    document.addEventListener('transitionend', scheduleCollect, true);
    document.addEventListener('animationend', scheduleCollect, true);
    // Rects move without a mutation (a drag, a sticky header): redraw
    // from the last walk four times a second.
    timer = setInterval(draw, 250);
    write(true);
    collect();
  };

  const hide = () => {
    if (!host) return;
    observer.disconnect();
    removeEventListener('scroll', scheduleDraw, { capture: true });
    removeEventListener('resize', scheduleCollect);
    document.removeEventListener('transitionend', scheduleCollect, true);
    document.removeEventListener('animationend', scheduleCollect, true);
    clearInterval(timer);
    clearTimeout(collectTimer);
    cancelAnimationFrame(frame);
    host.remove();
    host = root = observer = null;
    items = [];
    timer = collectTimer = frame = 0;
    write(false);
  };

  const toggle = () => (host ? hide() : show());

  addEventListener('keydown', (e) => {
    if (e.altKey && e.shiftKey && !e.ctrlKey && !e.metaKey && !e.repeat && e.code === 'KeyL') {
      e.preventDefault();
      toggle();
    }
  });

  window.__gofastrDevLayout = {
    show, hide, toggle,
    get on() { return !!host; },
    get findings() { return items.filter((it) => it.kind === OVERFLOW).map((it) => it.text); },
  };
  if (read()) show();
  console.info('[gofastr dev] Alt+Shift+L toggles the layout layer');
})();
