# framework/headless — structure without styling

The headless layer renders a component's tags, roles, labelling
relationships, state attributes and runtime hooks, and nothing else: no
classes at a nil skin, no CSS, no script. A skin (a flat map from part
to class) dresses it; a runtime module binds its `data-hui-*` hooks. The
harness proves every registered component against the same contract, so
a skin can be replaced without a single accessibility guarantee moving.
No skin or runtime module ships in this repository yet; `framework/ui`
is today's styled layer and does not render through this package.

**Use this when** the prompt mentions: headless, unstyled, reskin, a
second design system, parts, slots, overrides, binds, words, translated
component strings, an island on a table or pager, or a component whose
accessibility must be pinned by a golden.

**Import:** `github.com/DonaldMurillo/gofastr/framework/headless`

## Shape

```go
// A component: props and a skin in, HTML out. Nil skin = no classes.
headless.Button(headless.ButtonProps{Label: "Save", Type: "submit"}, skin)

// A skin: part → class. The only coupling between the two layers.
skin := headless.Skin{headless.PartRoot: "btn", headless.PartIcon: "btn__icon"}

// Seams: how a page reaches inside without forking.
headless.Card(headless.CardProps{Title: "Apps", Seams: headless.Seams{
    Slots:     headless.Slots{headless.PartCardHeader: header},
    Overrides: headless.Overrides{headless.PartFooter: {"data-testid": "f"}},
    Binds:     headless.Binds{headless.PartTitle: {Signal: "count"}},
    Words:     words, // nil means English
}}, skin, body)

// An in-page state change is an island, never a route (hard rule 1).
headless.Pagination(headless.PaginationProps{
    Page: 2, Pages: 9, HrefPattern: "/apps?page=%d",
    Island: headless.Island{Endpoint: "/island/apps", Signal: "apps"},
}, skin)
```

Every component registers a `Spec`: its name, the parts it draws, the
parts a slot may fill, the hooks it publishes, and the cases worth
rendering with a reason each. One fixture drives the nil-skin sweep,
the seam tests and the two goldens (`testdata/spec_golden.txt` at the
English words, `spec_golden_words.txt` at probe words).

## Don't reinvent

- **A class to find an element from script.** The runtime binds to
  `data-hui-*` hooks only; a skin may rename every class.
- **A link that changes in-page state.** `Pagination`, `ToolbarSearch`,
  a `Tag` with a dismiss and an `Alert` with a dismiss require an
  `Island` and refuse to render without one; the same element keeps
  its href for no-script. Every href goes through the framework's
  anchor policy (`urlsafe.CleanAnchor`).
- **A request through `ExtraAttrs`.** `Safe` drops every `data-fui-*`
  key. A request is `ButtonProps.Action`; a signal is a `Bind`; a
  region's refresh is an `Island`.
- **One attribute under two spellings.** `Safe` and the override
  sanitiser store names folded, as the browser reads them, so a
  caller's `NAME` cannot land beside the component's `name`; a key
  given twice is refused.
- **An English string in a component.** Words live on `Words`, one typed
  field each, so a missing translation is a compile error rather than a
  stray word on a French page; `words_test.go` refuses English outside
  the seam. A partial `Words` is safe: every empty field falls back to
  its English default.
- **A golden update without reading it.** `GOFASTR_UPDATE_GOLDEN=1 go test`
  regenerates; every changed line is a change to what assistive
  technology is told.

Full contract: `gofastr docs ui-headless`.
