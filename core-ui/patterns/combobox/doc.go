// Package combobox implements the WAI-ARIA Combobox 1.2 pattern as a
// server-rendered input that's bound to a debounced RPC dropdown.
//
// The Go-side renders the label, input, and an empty listbox. The
// runtime (core-ui/runtime/runtime.js) handles:
//
//   - Debounced input events (via existing data-fui-rpc-trigger="input"
//   - data-fui-rpc-debounce-ms="N") firing the search RPC.
//   - Keyboard navigation: ArrowUp/Down/Home/End move highlight,
//     Enter selects, Esc closes (twice clears input), Tab closes.
//   - aria-expanded + aria-activedescendant updates on every move.
//   - Click-to-pick on options.
//   - Outside-click closes.
//   - Auto-open on focus when the listbox already has options.
//
// Server contract for the search RPC handler:
//
//   - Request body: form-encoded with `query=<text>` (whatever the
//     input's `name` attribute is — default "q").
//   - Response: `<li role="option" id="...">label</li>` fragments,
//     swapped into the listbox via data-fui-rpc-signal. Each option
//     SHOULD carry a `data-value` attribute that becomes the input's
//     selected value on pick — if absent, the option's textContent
//     is used.
//   - To indicate "no results", emit a single `<li role="option"
//     aria-disabled="true">No results</li>` — the keyboard nav skips
//     disabled options.
package combobox
