# What changed in v0.86.0

v0.86.0 rebuilds the UI layer. It breaks a lot, on purpose, and
deprecates nothing: a removed API is gone, and `gofastr upgrade` points
at every line of an app that still uses one.

Four changes make up the release, and each one leans on the others:

1. **Headless.** Every `framework/ui` component renders through
   `framework/headless`, which owns structure, accessibility and the
   hooks behaviour binds to. The styled layer only dresses it.
2. **`fui-*` classes.** Every class the kit emits is renamed from `ui-*`
   to `fui-*`.
3. **One layout primitive.** `app.NewLayout(name, spec, build)` replaces
   the fixed header/sidebar/footer template, and the preset site header,
   footer and docs page are deleted. A site's chrome is its own code.
4. **Owned styles.** An app writes CSS in one place: a `<name>.style.css`
   sheet beside the Go that owns it, scoped to that owner, generated into
   typed Go and checked against the theme's tokens.

This page is the full account. The changelog entry is the summary.

## Moving an app

Install the v0.86.0 CLI and run the upgrade report before you touch
`go.mod`:

```bash
go install github.com/DonaldMurillo/gofastr/cmd/gofastr@v0.86.0
gofastr upgrade --to v0.86.0
```

If `go.mod` already names v0.86.0, pass `--from` with the release the
code was written for. The report has three parts:

- **Edit these.** Lines that spell something v0.86.0 no longer accepts,
  each with its file, position and the symbol, class or token it matched.
- **Check these.** Lines that still build but whose behaviour changed.
- **Nothing found.** Notes no scan matched. Read them anyway: some
  changes have no spelling a scan can find.

The scan reads Go through the type checker, CSS through a CSS tokenizer
and markup in Go strings through an HTML tokenizer. An aliased import,
an escaped selector (`.ui\2d button`) and an upper-case `CLASS=`
attribute all match. When the project no longer compiles, the report
reads the compile errors instead and lists every error no note explains.
[Upgrading](upgrading.md) covers the flags.

A class built at run time (`fmt.Sprintf("ui-%s", kind)`, a name read from
the database) never appears in source. Your tests catch those. In Go test
binaries and under `gofastr dev`, the framework reads every page its
router serves for the classes and attributes v0.86.0 retired.
`framework.TestHarness` fails the test on each one, and `gofastr dev`
warns once per path and name. Production binaries never scan. See
[Testkit](testkit.md).

An order that works for most apps:

1. Port each layout to `app.NewLayout(name, spec, build)` (see
   [Layouts](#layouts-one-primitive-no-preset-frames)).
2. Replace `ui.SiteHeader`, `ui.SiteFooter` and `ui.DocLayout` with
   `gofastr generate package siteheader`, `sitefooter` and `docpage`.
3. Rewrite each `FormFieldConfig.Input` as a builder closure.
4. Move app CSS into owned sheets, reading tokens.
5. Run the tests. A config the kit now refuses panics at render, and in
   a test a render panic fails the request, so the test run lists them.

The docpage copy keeps the old narrow mode: a page that leaves `Nav`
and `Toc` unset (the `ui.DocLayout(ui.DocLayoutConfig{}, body)` shape)
renders one centred reading column at the prose measure, with both
rail columns collapsed.

## Headless: structure split from styling

`framework/headless` renders tags, ARIA roles, labelling relationships,
state attributes and `data-hui-*` hooks. It renders no classes and ships
no CSS. `framework/ui` dresses each primitive with a class map and a
style sheet. Every primitive registers a spec, and one harness holds all
of them to the same contract: goldens, refusal tests, and sweeps over
the attributes and slots a caller can set. [Headless
components](ui-headless.md) is the reference.

What this changes for an app:

- **Accessibility comes from the primitive.** Labels, descriptions,
  live regions and focus are structure, so the spec tests them. A sorted
  island table returns focus to the sort link and announces the new
  order. A failed form submit moves focus to the error summary. A field
  shows its hint and its error together.
- **Behaviour lives beside its component.** A component's script sits
  next to its Go and registers with `registry.RegisterBehavior`, naming
  its markers, the modules it needs first (`registry.Requires`) and the
  clicks or keys to hold while it loads (`registry.Interactions`). The
  kernel loads it when a marker appears. The old kernel modules are
  retired (listed below), and their `data-fui-*` hooks became
  `data-hui-*`.
- **Wiring is typed.** `ui.Button`'s `Action` and `ui.Form`'s `Request`
  admit a fixed set of `data-fui-*` keys and check each value. Any other
  `data-fui-*` key in `ExtraAttrs` panics at render and names the key.
  Build wiring with `interactive.Post(...).OnSuccess(...).Attrs()`.
- **Words are typed.** `headless.Strings` holds one field per sentence a
  component says. `ui.StringsFor(ctx)` fills it from the `i18nui` catalog
  in the request's locale, with English as the default.
  `ui.CheckStrings(ctx)` lists translations that would be refused, such
  as one that drops a `%s`.
- **Islands are checked at render.** A toolbar search, a repeater with an
  action, and a dismissible tag, alert or notification change state
  inside the page, so each refuses to render without an `Island` (hard
  rule 1). A pager and a sortable table take an `Island` when the sort or
  page must not change the URL, and are plain links otherwise.

### `core-ui/patterns` is deleted

| Pattern | Use instead | Renames |
| --- | --- | --- |
| `breadcrumbs` | `ui.Breadcrumbs` | `Crumb` keeps its fields; separators are `aria-hidden` spans |
| `progress` | `ui.Progress` | `LabelVisible` is `ShowLabel`; a value past `Max` clamps |
| `multiselect` | `ui.MultiSelect` | `Option` keeps its fields |
| `sortablelist` | `ui.SortableList`, `ui.SortableListItems` | `Item` is `SortableItem` |
| `tree` | `ui.Tree` | `Node` is `TreeItem`; `SignalPrefix` is `LazySignalPrefix` |
| `pagination` | `ui.Pagination` | `Total`/`Current`/`HrefPattern` are `Pages`/`Page` plus `Path` and `Query` |
| `skeleton` | the `ui` skeleton presets | none |
| `accordion` | `ui.Collapsible` | an exclusive group shares `CollapsibleConfig.Name` |
| `nestedlist` | `ui.Tree` or `ui.Collapsible` | none |
| `infinitescroll` | no replacement | append with a poll or an island |

### Retired runtime modules

These modules no longer exist. A page that loads one by hand gets a 404.

| Retired | Now handled by |
| --- | --- |
| `menu`, `tabs`, `carousel`, `disclosure`, `scrollspy`, `toc`, `combobox`, `panehost`, `sidebar`, `shortcut` | `headless-menu`, `-tabs`, `-carousel`, `-disclosure`, `-rail`, `-toc`, `-combobox`, `-panehost`, `-sidebar`, `-navigation` |
| `numberinput`, `slider`, `rangeslider`, `taginput`, `formrepeater` | `headless-controls`, `headless-collections` |
| `banner`, `copy`, `toasts`, `networkretrybanner` | `headless-feedback` |
| `animatedcounter`, `backtotop`, `themeswitch` | the `data-hui-counter-*`, `data-hui-back-to-top*` and `data-hui-theme-toggle` hooks |
| `passwordinput`, `conditionalfield`, `fileupload`, `dropzone` | the `headless` module (`data-hui-reveal`, `-when`, `-drop`); thumbnails in `framework/ui`'s `filedropzone` |
| `tree`, `multiselect`, `sortablelist`, `infinitescroll` | `headless-tree`, `-multiselect`, `-sortablelist`; infinite scroll has none |
| `optimisticaction`, `toggleaction` | the headless action contract (`data-hui-action*`) on the kernel's `action` primitive |

The lightbox module moved out of the kernel into `framework/ui`. It
loads with `framework/ui`'s registration, so a page that hand-rolls
lightbox markup without importing `framework/ui` loses navigation and
zoom.

The hooks moved with the modules. A few that apps wrote by hand:

| Old | New |
| --- | --- |
| `data-fui-menu-*`, `data-fui-tabs`, `data-fui-tab` | `data-hui-menu-*`, `data-hui-tabs`, `data-hui-tab` |
| `data-fui-disclosure`, `data-fui-disclosure-persist` | `data-hui-disclosure`, `data-hui-disclosure-persist` |
| `data-fui-pane-open`, `-close`, `-key`, `-swap` | `data-hui-pane-open-control`, `-close`, `-key`, `-swap` |
| `data-fui-pane-deeplink`, `data-fui-scrollspy` | `data-hui-pane-deeplink`, `data-hui-rail` |
| `data-fui-sidebar*`, `data-fui-combobox*` | `data-hui-sidebar*`, `data-hui-combobox*` |
| `data-fui-tree-toggle`, `data-fui-multiselect*`, `data-fui-sortable*` | `data-hui-tree-toggle`, `data-hui-multiselect*`, `data-hui-sortable*` |
| `data-when-name`, `data-when-value` | `data-hui-when`, `data-hui-when-value` |

### Components whose markup or config changed

| Component | What changed |
| --- | --- |
| `ui.Button`, `ui.LinkButton` | `disabled` is `ButtonConfig.Disabled`; an unknown `data-fui-*` key panics. A link takes only `-push-state`, `-prefetch`, `-open` and `-deeplink` |
| `ui.Form` | `ExtraAttrs` wiring goes through `Request`; an action the anchor policy refuses panics (it used to become `#`); with `Errors` set it needs `ID` |
| `FormConfig.Summary` | A row inside `ui.ValidationSummary`, rendered even when `Errors` is empty (it was a Callout's whole body) |
| `ui.ValidationSummary` | `ID` is required |
| `ui.StepWizard` | Takes `Errors`, `Summary` and `ID` like `ui.Form`, and focuses the summary on a failed step |
| `FormFieldConfig.Input` | A builder, `func(headless.FieldControl) render.HTML`. Build the control with `ui.Control`, a typed field or `ui.PasswordInput`'s `Field`. A closure that ignores its `FieldControl` compiles and loses the field's wiring |
| `ui.PasswordInput` | `Error` is gone (put it on the enclosing field); `ID` is optional when `Field` carries one |
| `ui.ColorField` | `SwatchValue` is gone and `Name` is required; an invalid value marks the shell `data-invalid` |
| `ui.ConditionalField` | Renders visible and hides once its module arms; `ConditionalFieldVisible` and `EvaluateInitialState` are gone |
| `ui.FileUpload`, `ui.TextArea` | Hint inside the zone, error below; `TextArea` is a field shell around its control |
| Checkbox, Radio, Switch and their groups | The label wraps the control; groups are plain fieldsets; a required group marks every input `required` |
| `ui.DataTable` | `SortHrefPattern`, `IslandSignal` and `IslandEndpoint` are gone. Pass `Query url.Values` (plus optional `Path`, `SortParam`, `DirParam`) and `Island headless.Island`. `Pagination` is a `*ui.PaginationConfig`. The sort control is an anchor |
| `ui.Callout` | Renders `headless.Alert`; `Landmark` is gone; only danger and warning interrupt with `role=alert` |
| `ui.Card` | A titled card is no longer a labelled `<section>`; a `HeadingLevel` outside 1 to 6 panics |
| `ui.Section` | No heading and no label renders a `div`; `SectionConfig.Ctx` is gone; heading ids are `<slug>-title` |
| `ui.EmptyState` | A named `role=region` |
| `ui.PageHeader`, `ui.Spinner` | No `role="banner"` on the header; no `aria-live` or `aria-busy` on the spinner |
| `ui.Divider`, `ui.Spacer` | Vertical divider is `<hr aria-orientation="vertical">`; spacer is a `span` |
| `SkeletonAvatarConfig.Size` | Removed (it wrote an inline style a strict CSP drops) |
| `ui.Gallery` | A captioned item is `li > figure > (a > img, figcaption)` |
| `ui.Sidebar` | Renders `headless.Sidebar`. An item with both `Href` and `Children`, a blank `Label`, or control bytes in `DrawerName` or `CollapseStorageKey` panics |
| `ui.Banner`, `ui.NetworkRetryBanner` | Polite announcements; `FailureThreshold` and `SSESilenceMs` are gone |
| `ui.Notification`, `ui.Tag`, `ui.FilterChipBar` | A dismissal needs an `Island` |
| `ui.NotificationBell`, `ui.BackToTop` | `Href` is required |
| `ui.GlobalSearch`, `ui.CommandPalette` | `NoScriptAction` and `FallbackHref` are required and same-origin |
| `ui.Carousel` | The `VirtualScroll` fields are gone |
| `ui.Lightbox` | Classes are `fui-lightbox*`; the image carries `data-fui-lightbox-image` |
| `html.DetailsConfig.Disclosure` | Removed; use `ui.Collapsible` or `headless.Disclosure` |

## The class rename: `ui-*` to `fui-*`

Every class the kit emits starts with `fui-`: `fui-button`, `fui-card`,
`fui-form`, `fui-data-table`, `fui-hero`, and so on through the catalog.
Two things keep their old names: the registered sheet names, and the
`data-fui-comp="ui-*"` markers that load those sheets. A card carries
`data-fui-comp="ui-card"` and `class="fui-card"`.

Hand-written markup on an old class renders unstyled. Call the
component instead of copying its markup. CSS that selected `.ui-*`
classes has nothing to select. Don't rename the selector to `.fui-*`:
an owned sheet may not select kit classes (GOFASTR1810). Use the
component's config, a variant, or a theme option.

The gallery gate in `framework/gallery` refuses any `ui-*` class in a
rendered catalog entry and any `.ui-*` selector in a `framework/ui`
sheet, so the old prefix cannot come back.

## Layouts: one primitive, no preset frames

A layout is a build function over an `*app.LayoutTree`. It returns the
static chrome and says where the routed content goes:

```go
layout := app.NewLayout("app", app.LayoutSpec{}, func(ctx context.Context, l *app.LayoutTree) render.HTML {
	nav, _ := component.SafeRenderCtx(ctx, ui.Sidebar(cfg))
	return ui.Stack(ui.StackConfig{Screen: true, Gap: ui.GapNone},
		ui.ContentRow(ui.ContentRowConfig{Sidebar: nav}, l.Primary()))
})
site.SetDefaultLayout(layout)
```

Around that build:

- **Outlets** are typed handles (`app.NewOutlet`). A screen or a screen
  group fills one by value, one URL can fill several, and fills apply on
  every navigation.
- **Route areas** (`LayoutTree.RouteArea`) stay in place and re-render
  on the server on every navigation the layout survives: breadcrumbs, a
  current-article mark.
- **Screen groups** nest layouts. A group prefix may carry params
  (`/projects/{project}`), and the layer key includes the value, so one
  project's panes stay mounted across its pages.
- **Resolvers** share values from the route, **region guards** gate an
  outlet, and an outlet nothing fills can answer 404.
- **Deferred outlets** load as parallel part requests, with **loading
  content** (`Loading.Show`, `After`, `Min`) while they wait.
- **Transitions** (`app.Slide`, `app.Crossfade`) animate a swap, and
  **route signals** (`route.*` in the browser) give client code the
  current route.
- The `layoutfunc` lint refuses route state read inside static chrome.

[Layouts](layouts.md) is the reference.

The framework ships no ready-made layouts. The page frame is composed
from components:

- `ui.Stack{Screen: true}` is the page-tall column. It pushes its last
  child to the bottom, so end it with a footer or a `ui.ContentRow`.
- `ui.ContentRow` is the sidebar row, with an optional toolbar band, a
  context aside that frees its column when its outlet is empty,
  `Viewport` for desktop scroll regions and `PhoneNavFlush`.
- `ui.Container` is the centered column. Its widths are theme tokens.
- `ui.Sidebar` gains `Compact` (a docs rail) and `NativeMobile` (a phone
  menu that works without script).
- `ui.ListDetail` keeps a scrolling list beside routed detail, with
  `MobileSinglePane` and `BackHref` for a list-or-detail phone flow.
- Dense variants: `ui.CardRow` record rows, `DetailListConfig.Inline`,
  `PageHeaderConfig.Compact` and `Badge`, `SectionConfig.Compact`,
  `FilterToolbarConfig.Compact`, `ToolbarConfig.Plain`,
  `StackConfig.TrimMargins`, and `BannerConfig.Strip`.

[UI composition recipes](ui-composition-recipes.md) builds the app
shell, list/detail, marketing site and docs site end to end.
`examples/tracker` and `examples/acme-site` are the running versions.

### What was removed

| Removed | Use instead |
| --- | --- |
| `app.NewLayout(name)` | `app.NewLayout(name, spec, build)` |
| `WithHeader`, `WithFooter` | the header and footer in the build, first and last children of `ui.Stack{Screen: true}` |
| `WithSidebar` | `ui.ContentRow(ui.ContentRowConfig{Sidebar: nav}, l.Primary())` |
| `WithContainer` | `ui.Container` around `l.Primary()`, the header and the footer |
| `WithStickyHeader`, `Layout.Wrap`, `app.LayoutBaseCSS` | compose the frame; `WithKey` and `WrapCtx` stay |
| `ui.SiteHeader`, `ui.SiteFooter`, `ui.DocLayout` and their parts | `gofastr generate package siteheader`, `sitefooter`, `docpage` |
| `DocCrumb{Label, Href}` | `ui.Crumb{Text, Href}` in `ui.Breadcrumbs` |
| `DocPager`, `DocPrevNext` | `docpage.Pager` |

`gofastr generate package <name>` copies a canonical package into the
app as owned code: the Go, its owned sheet, its tokens and their
generated Go, and its tests, with imports rewritten to the app's module.
The copy is the app's to change. A blueprint with marketing screens
writes the same packages through the same copy.

## Owned styles: where an app's CSS lives

Each kind of styling has one home, and nothing else ships CSS:

| Styling | Lives in |
| --- | --- |
| A component's look | its `framework/ui` file, registered with `registry.RegisterStyle` |
| Page frames | `ui.Stack`, `ui.ContentRow`, `ui.Container` |
| An app's own pieces (header, footer, docs page, a layout root) | an owned `<name>.style.css` beside the package's Go |
| Colours, sizes, fonts, dark mode | theme tokens |

An owned sheet works like this:

- Write `<name>.style.css` beside the Go that owns it, then run
  `gofastr generate styles`. It writes `<name>_style.gen.go`, a typed
  handle with one method per class.
- Hand the handle to its owner: `app.LayoutSpec{Style: x.Style}` for a
  layout, `Screen.WithStyle(x.Style)` for a screen,
  `x.Style.Scope(root)` for a component root, or `App.WithStyle` for the
  one `app.style.css` that covers every page.
- The compiled sheet is wrapped in `@scope` from the owner's
  `data-fui-scope` root. The scope stops at a nested owner and at kit
  markup marked `data-fui-internal`, so an owner styles the content it
  passes into a component and never the component's insides.
- Kit rules that place a component's root (margin, size, display, grid
  and flex placement) are lowered with `:where()`. An owned rule aimed at
  that root wins by scope proximity, whatever order the sheets load in.
- Every value reads a token. An app adds its own tokens in
  `<name>.tokens.css` as `@property` rules, and `generate styles`
  writes typed Go for them to pass to `theme.Default().Extend(...)`.

`gofastr verify` and `gofastr generate styles` run the same checks,
GOFASTR1801 to GOFASTR1822:

| Rule | Refuses |
| --- | --- |
| 1801 | CSS rules shipped in Go strings |
| 1806 | `var(--name)` naming a token nothing declares, fallback or not |
| 1807 | a literal equal to a token's value (`font-weight: 600` reads `var(--font-weight-semibold)`) |
| 1808 | a `var()` fallback that disagrees with the token |
| 1809 | a `.css` file that is not a `<name>.style.css`, so nothing owns it |
| 1810 | an owned sheet selecting a kit class or runtime attribute |
| 1811 | `!important` |
| 1812 | an `@media` width that is not a theme breakpoint |
| 1813 | an animation with no reduced-motion block (warning) |
| 1814 | an owned sheet changed since its Go was generated |
| 1816 | two sheets with one style name in a program |
| 1817 | an owned rule restyling a kit component's root |
| 1818 | a screen or layout style used outside its owner's package |
| 1819 | an app sheet selecting anything but a class |
| 1820 | an owned sheet redeclaring a theme token |
| 1821 | an app token whose value is another token's value |
| 1822 | one literal written in two owned sheets for one token type (warning) |

GOFASTR1815 is informational. It lists every owned sheet with its owner
and class count, so a sheet that reads like a missing component stays in
view until it moves into the design system. A line that is off-token on purpose
carries `/* gofastr:allow(GOFASTRnnnn) <reason> */`; a marker with no
reason waives nothing. [Theming](theming.md) and
[Contracts](contracts.md) have the details.

## Theme

- **Sizes and font weights are tokens.** `style.Size` (`--size-*`) and
  `style.FontWeight` (`--font-weight-*`) validate at boot.
  `Theme.Layout` holds the page dimensions: `PageWidth`, `PageGutter`,
  `HeaderHeight`, and `ui.Container`'s three widths.
- **The private `--ui-*` layout variables are gone.** Nothing reads an
  app's value for them any more:

  | Old variable | Set instead |
  | --- | --- |
  | `--ui-layout-container-width` | `Theme.Layout.PageWidth` (`--size-page-width`) |
  | `--ui-layout-gutter` | `Theme.Layout.PageGutter` (`--size-page-gutter`) |
  | `--ui-layout-header-height` | `Theme.Layout.HeaderHeight` (`--size-header-height`) |
  | `--ui-container-narrow`, `-default`, `-wide` | `NarrowWidth`, `ContentWidth`, `WideWidth` |

- **App tokens are typed.** `Theme.Extend` takes a struct of typed
  fields, and each emits under its type's prefix (`BrandGlow style.Color`
  is `--color-brand-glow`). A `<name>.tokens.css` file generates that
  struct.
- **Component options.** `Theme.Components` carries density, button
  treatment and radius, and field layout and radius. The compiler turns
  them into `--fui-*` variables that nest by inheritance. `gofastr theme
  edit` edits them as selects.
- **Contrast is checked at boot.** `ColorSet.DangerFg`
  (`--color-danger-fg`) gives the danger pair its own ink. `Theme.Validate`
  refuses a hex `primary`/`primary-fg` or `danger`/`danger-fg` pair below
  4.5:1, in light and dark. A theme that shipped such a pair now panics
  at `WithTheme`.
- **Scoped dark mode follows the document.** A theme override with a dark
  palette switches with `data-color-scheme`, like the root theme.
- **Scale steps spell `2xl` and `3xl`.** Auto-named tokens used to come
  out as `--spacing-xxl` and `--text-xxxl`, which nothing read. Rename
  those in app CSS and in `{spacing.xxl}` references.
- **`style.ThemeRef.Hash` is a method.** Read `ref.Hash()`.
  Registering a theme override in a package `init` is now safe.
- **`style.DarkSchemeCSS` is gone.** Set `Theme.DarkColors`.
- **Ten legacy colour aliases are gone.** Read the canonical token:

  | Removed | Replacement |
  | --- | --- |
  | `--color-muted`, `--color-surface-hover` | `var(--color-surface-soft)` |
  | `--color-border-subtle` | `var(--color-border)` |
  | `--color-border-hover` | `var(--color-border-strong)` |
  | `--color-primary-hover` | `color-mix(in srgb, var(--color-primary) 85%, var(--color-text))` |
  | `--color-primary-foreground` | `var(--color-primary-fg)` |
  | `--color-ring` | `var(--color-primary)` |
  | `--color-warn` | `var(--color-warning)` |
  | `--color-warn-soft` | `color-mix(in srgb, var(--color-warning) 15%, transparent)` |
  | `--color-warn-strong` | `color-mix(in srgb, var(--color-warning) 80%, var(--color-text))` |

## Tests and tooling

- **A render panic fails tests.** A recovered render panic fails
  `TestHarness` requests and generated app tests, and UI host requests in
  test binaries answer 500. Production keeps the screen's status. A test
  that panics on purpose wraps its handler in
  `testkit.AllowRenderPanics(t, handler)`.
- **Retired markup fails tests.** See [Moving an app](#moving-an-app).
- **`gofastr upgrade` reads code the way the compiler does.** It loads
  test files, files kept out of the host build by a `//go:build` line or
  a file suffix, nested modules, and `//go:build ignore` programs. A file
  no configuration builds is listed at the top of the report.
- **`gofastr verify` reports a `.css` file nothing owns** (GOFASTR1809).
- **Scaffolded files carry no "Code generated" line.** `main.go`,
  `screens.go`, `resource.go` and the e2e tests are the app's own code,
  and agent tools refused to edit files with that line. Files gofastr
  regenerates keep their `DO NOT EDIT` header.
- **Generated apps carry each body once.** Entities share
  `entities/events.go`, and a generated CLI shares `verbs.go`. The
  per-entity functions stay as one-line wrappers.
- **Shared helpers have one home.** Copies across packages were merged
  into `core/textsafe`, `core/query`, `core/netguard`, `core/handler`
  and others. Four checks got stricter by sharing the canonical version:
  the storage battery's fold refusal, the webhook battery's table-name
  check, `battery/auth`'s JSON content-type gate, and repolint's
  generated-file test.
- **Removed exports nothing used:** `html.ContainerType` (set
  `container-type` in CSS), `gallery.MustLookup` (call `Lookup`) and
  `ui.ToastStackSignal` (use `preset.ToastStack`). `gofastr pack` reads
  the auth form's `next` input only as an `html.Input` call.

The contract catalog holds 77 rules.

## Fixes

- `core/yaml` nests lines under a list item's first key when that key has
  no value.
- A compact `ui.Sidebar` draws its groups as groups.
- `interactive.SectionMenu`'s desktop rail shows every group.
- A blank line in a numbered `ui.CodeBlock` keeps its row.
- `gofastr theme init` writes a theme that boots.
- `ui.ThemeToggle`'s pill variant draws its pill.
- Client navigation waits for late component sheets before it scrolls.
- The render-panic fallback scrubs control bytes from the panic value.
- A page title that already ends with the app name doesn't get it twice.
- `ui.Hero` actions have a gap between them.
- A `ui.Section` a grid stretches keeps its heading above its body.
- A `headless.PaneHost` deep link no longer refetches the screen on Back
  and Forward.
- `widget.RuntimeTag` writes its JSON blocks before the runtime script,
  so registered behaviours load on pages built with it.
- `ui.PasswordInput`'s `ExtraAttrs` reach its root, and `ui.Switch` keeps
  a caller's `Class`.
- The choice and password sheets use logical properties, so they hold in
  right-to-left documents.
- `handler.DecodeStrict` keeps the `*http.MaxBytesError`, so an oversized
  body can answer 413.
- Two buttons of one toggle group clicked in one round trip end with one
  committed.
- An action module that fails halfway can't install its listeners twice.
- The per-module size budget and the JavaScript lints cover registered
  behaviours.
- The resource engine's pager keeps the active sort when it turns a page.
- A conditional region inside a hidden region stays disabled.
- A drop zone follows an input that a swap replaced.

## Common mistakes

- **Bumping `go.mod` before running the report.** `gofastr upgrade` then
  reads v0.86.0 as the current release and finds nothing to do. Pass
  `--from` with the old release.
- **Renaming `.ui-button` to `.fui-button` in app CSS.** An owned sheet
  may not select kit classes. Change the component's config, a variant
  or a theme option instead.
- **Setting `--ui-layout-*` or `--ui-container-*`.** Nothing reads them.
  Set `Theme.Layout`.
- **Ending `ui.Stack{Screen: true}` with a bare `l.Primary()`.** The
  stack pushes its last child to the bottom of the viewport. End it with
  the footer, or wrap the primary in `ui.ContentRow`.
- **Skipping "Check these".** Those lines build, and their behaviour
  changed anyway.
- **A `FormFieldConfig.Input` closure that ignores its argument.** It
  compiles, and the control loses its label link, description and
  invalid state.
- **Treating a generated `siteheader` as framework code.** It is the
  app's package. Restyle it in its own sheet, and expect no upstream
  updates to it.
