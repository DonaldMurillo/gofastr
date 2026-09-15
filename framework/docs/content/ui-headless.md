# Headless components

`framework/headless` is the structure half of a design system. A
component there renders tags, roles, labelling relationships, state
attributes and the hooks a runtime binds to, and nothing else: no
classes at a nil skin, no CSS, no script. A skin dresses it; a runtime
module binds it; a harness proves every registered component against
one contract.

It exists because structure and styling have different lifetimes.
Whether a field's error is tied to its input by `aria-describedby` does
not change when the palette does, and it is testable without rendering
a pixel. Once the two are separate, a skin can be redrawn or replaced
without putting a single accessibility guarantee back at risk.

This is the same SSR-first, incrementally hydrated model as the rest of
the framework ([core-ui/ARCHITECTURE.md](../../../core-ui/ARCHITECTURE.md)):
first paint is the full markup, behaviour arms by hook on arrival, and
an in-page state change is an island RPC, never a route.

**Import:** `github.com/DonaldMurillo/gofastr/framework/headless`

---

## The vocabulary

A component is a pure function from its props and a `Skin` to HTML.
Seven things are named in its contract, and the harness checks each in
both directions.

- A **part** (`headless.Part`) names an element the component draws.
  The skin styles those and only those; a part declared and never
  rendered fails a test, and so does a rendered part nobody declared.
- A **skin** (`headless.Skin`) is a flat map from part to class. Nil is
  valid and renders unstyled. Variants are looked up as
  `<part>--<variant>`, so the headless layer passes a variant name
  through without knowing what any of them mean.
- A **hook** is a `data-hui-*` attribute a runtime binds to. A component
  declares the hooks it publishes in its `Spec`; a runtime that binds
  to an undeclared hook is bound to nothing.
- **Slots** replace the content of the parts a component lists as
  fillable. Most list none: a slot exists only where the component
  composes something no prop can express.
- **Overrides** add attributes to a part, through a sanitiser that
  refuses `id`, `style`, every `data-hui-*` hook, every `data-fui-*`
  key and the runtime's privileged unprefixed keys (`data-behavior`,
  `data-island`, `data-action` and their family), stores names folded
  the way the browser reads them, refuses one name under two
  spellings, and never beats an attribute the component owns.
  `ExtraAttrs` goes through the same refusal.
- **Binds** keep a part in step with a client signal (text, html or one
  attribute), refusing reserved signal names and attributes outside the
  runtime's own allow-list.
- **Words** are the strings a component says: one typed struct, nil for
  English, for a layer above to resolve once per request from the
  framework's `i18nui` keys. A missing field is a compile error, not a
  stray English word on a French page, and a partial value keeps the
  English default for every field it leaves empty.

Two more are not per part. A `Button` takes an **Action**, the
framework's request contract, on itself; `ExtraAttrs` cannot carry one.
A component that changes in-page state takes an **Island**: the
endpoint that renders the region again and the signal the region is
bound to.

```go
skin := headless.Skin{
    headless.PartRoot:  "field",
    headless.PartLabel: "field__label",
    headless.PartHint:  "field__hint",
    headless.PartError: "field__error",
}

headless.Field(headless.FieldProps{
    Label: "Port", For: "port", Hint: "1024 to 65535", Required: true,
}, skin, func(c headless.FieldControl) render.HTML {
    return headless.Input(headless.InputProps{
        Name: "port", ID: c.ID, Required: c.Required,
        DescribedBy: c.DescribedBy, Invalid: c.Invalid,
    }, inputSkin)
})
```

The control is built from the wiring the field passes down, so its id,
its `aria-describedby` and its invalid state cannot disagree with the
label, the hint and the error beside it.

## Islands: hard rule 1, at render time

The framework's first hard rule is that an in-page state change is
never a route: no `<a href="?page=2">` that the router treats as a
navigation. A headless component that turns a page, sorts a column or
applies a filter takes an `Island` and renders the framework's own RPC
contract (`data-fui-rpc`, `data-fui-rpc-method`, `data-fui-rpc-signal`,
`data-fui-push-state`) on the same element that keeps its href or
action for a reader with no script. First paint is the page; hydration
makes it the island; the URL is written by the runtime.

```go
headless.Pagination(headless.PaginationProps{
    Page: 2, Pages: 9, HrefPattern: "/apps?page=%d",
    Island: headless.Island{Endpoint: "/island/apps", Signal: "apps"},
}, skin)
```

Where the change would otherwise be a route the Island is required:
`Pagination`, `ToolbarSearch`, a `Tag` with a dismiss and an `Alert`
with a dismiss panic without one, so the link-only render cannot be
built. On a `Form` it is optional, for a page that is the form.

Every href a component writes goes through the framework's anchor
policy, `urlsafe.CleanAnchor`: a `Button` whose href is rejected
renders the disabled-link posture, and a form action, a dismiss href
or a pager pattern that is rejected is refused at render.

The endpoint keeps the href's query, so the page and the island answer
the same question. `data-fui-push-state` is rendered only for a GET
with a href to write; a mutation's URL is the server's to set through
`X-Gofastr-Push-State`.

## The spec and the harness

Every component registers a `Spec` beside its code: its name, the
parts it draws, the parts a slot may fill, the hooks it publishes, and
its cases, each with a `Why`. One fixture drives everything that must
hold for every component:

- every case says why it exists and renders something;
- at the nil skin no case renders a `class`, an empty `aria-*`, a
  `style`, a `<button>` with no type, or a duplicate id;
- rendering is deterministic;
- every declared part is drawn and every drawn part is declared;
- every declared hook survives rendering;
- every `aria-*` reference resolves inside the fixture, and every
  control has a name;
- a filled slot and a hostile override cannot break the contract;
- every exported component has a spec.

Two goldens pin the corpus: `testdata/spec_golden.txt` at the English
words and `spec_golden_words.txt` with every word replaced by its own
probe token, which proves that every string on the page came through
the `Words` seam. `words_test.go` also walks the source and refuses an
English phrase in the sinks it knows: text and HTML literals, format
strings, defaults, and the attribute values a reader is told.

```sh
go test ./framework/headless/                    # every gate
GOFASTR_UPDATE_GOLDEN=1 go test ./framework/headless/ # after an intended markup change
```

Read the golden diff. Every line of it is a change to what assistive
technology will be told.

## What ships in this package

Button, Field, FieldRow, ConditionalField, Input, Textarea, Select,
Password, Color, Choice, Switch, Group, Form, InputGroup, FileUpload,
ValidationSummary, Card, Stack, Cluster, Grid, Container, Section,
Divider, Spacer, Spinner, Skeleton, Alert, SystemBanner, Badge, Tag,
Toolbar, ToolbarGroup, ToolbarSpacer, ToolbarSearch, Pagination, Steps,
Timeline, OptimisticAction and ToggleAction.

No skin, stylesheet or runtime module for the `data-hui-*` hooks ships
in this repository yet: the hooks are the contract that module will be
written to, and every component renders markup that is correct and
usable without it. `framework/ui` is today's styled layer and does not
render through this package; the skin, the stylesheet, the runtime
module and that adoption follow in their own changes.

## Common mistakes

- **Finding an element from script by its class.** The runtime binds to
  `data-hui-*` hooks only. A skin may rename every class, and a class
  used as a hook is the one thing it cannot rename.
- **Rendering a pager or a search form without an Island.** That is a
  route for an in-page state change, and the component refuses it at
  render time with the rule's reason.
- **Smuggling a request through `ExtraAttrs` or an Override.** Both
  drop every `data-fui-*` key. A request is `ButtonProps.Action`, a
  signal is a `Bind`, a region's refresh is an `Island`.
- **One attribute under two spellings.** `NAME` and `name` are one
  attribute to the browser, which keeps the first it reads. Both seams
  store names folded, so the component's own spelling wins, and a key
  given twice is refused at render.
- **Building the hint's id by hand.** `Field` passes a `FieldControl`
  with the id, the `aria-describedby` and the invalid state already
  agreed; a control built from anything else drifts.
- **Setting an attribute whose presence is the value through `Attrs`.**
  `Attrs` drops empty values on purpose; `hidden`, `popover`, `open`
  and every `data-hui-*` hook go through `Mark`, and boolean HTML
  attributes through `Flag`.
- **Adding an English string to a component.** Give it a `Words` field
  with a doc comment saying its shape. The source walk in
  `words_test.go` fails on a phrase said outside the seam.
- **Regenerating a golden to make a test pass.** Regenerate only after
  every other test in the package is green, then read the diff.
