# UI components index

This is a one-page index of every UI component the framework ships.
The canonical reference for any given component is its **live demo
page** at `/components/<slug>` plus the Go doc comments on the
component's `Config` / constructor. Those stay current automatically
because each release lands the demo + the code together.

---

## Where to look for what

| You want…                            | Look at                                                          |
| ------------------------------------ | ---------------------------------------------------------------- |
| Live behavior (click it, drag it)    | Run the website: `./scripts/dev-watch.sh` → `/components/<slug>` |
| Constructor signature + every field  | `go doc github.com/DonaldMurillo/gofastr/framework/ui.<Name>`    |
| Full-page composition choice          | `gofastr docs ui-composition-recipes`                           |
| Widget presets (Modal, Drawer, …)    | `go doc github.com/DonaldMurillo/gofastr/core-ui/widget/preset`  |
| Runtime data-cui-\* attributes       | [runtime-contract](runtime-contract.md)                          |
| What's coming / deferred             | [`ROADMAP.md` §2](../../../ROADMAP.md)                                  |

The website's components index page lists every component with a
one-line description ([`examples/site/components.go`](../../../examples/site/components.go)
is the source of truth). The `examples/site` test suite
keeps every registered `/components/<slug>` paired with:

- at least one chromedp e2e test
- zero axe-core violations against the default WCAG 2.0/2.1 A/AA rule set
- at least one `*_test.go` for the package that defines it

---

## Catalog

Live demos at `http://localhost:8082/components/<slug>` once the
dev server is up.

Variant-taking components panic on unknown variants; apps extend the
sets with `ui.RegisterButtonVariant` / `RegisterButtonSize` /
`RegisterCardVariant` / `RegisterStatusVariant` (one status
registration covers StatusBadge, Tag, Callout, and Notification). See
"Custom variants on framework components" in `ui-getting-started`.
A host that receives the variant as a string (kiln's node renderer,
the generated resource screens, uihost's trusted node renderer)
resolves it with `ui.ParseButtonVariant`, which knows the four built-in
variants and every one the app registered through
`ui.RegisterButtonVariant`, and answers `ok=false` for any other
spelling so the host applies its own default instead of panicking.

Every component Config carries `ExtraAttrs html.Attrs`, forwarded to
the component's root element for `data-*` test hooks, analytics
markers, and ARIA overrides. On components following the sanitized
contract, component-owned keys (`class`, `id`, `data-cui-*`,
behavior-critical attributes) are dropped; a legacy set still forwards
raw (enumerated in `framework/ui/extraattrs_contract_test.go`). See
"Attribute pass-through" in `ui-getting-started`.

### Primitives & semantic markup

- **kbd**: `core-ui/html.Kbd`, semantic `<kbd>` for keyboard input
- **shortcuthint**: `framework/ui.ShortcutHint`, OS-aware chord chips (⌘ on Mac / Ctrl elsewhere)
- **avatar**: `framework/ui.Avatar`, circular avatar with image → initials fallback (sm/md/lg/xl); `Square` rounds it to the medium radius for a logo tile, and `--ui-avatar-bg` / `--ui-avatar-fg` recolour the initials surface
- **avatargroup**: `framework/ui.AvatarGroup`, readable 10% overlap, compact corner presence dots, and an adaptive-surface overflow chip
- **thumbnail**: `framework/ui.Thumbnail`, a square lazily loaded image preview (sm 2rem / md 4rem / lg 8rem, `object-fit: cover`); a `Src` the image URL policy refuses draws nothing, so the caller draws its own empty mark
- **icon**: `framework/ui.Icon`, inline-SVG primitive backed by `RegisterIcon`; built-ins cover chevrons, close, menu, more, search, the status family and a navigation set (home, user, users, file, receipt, card, box, layers, activity, shield, key, lock, sliders, chart, calendar, clock, plus, list, grid, database, inbox, folder, tag, mail, bell, globe, repeat, cpu, star, pencil, trash, rotate-ccw), `currentColor` stroke, `AriaLabel` flips to `role="img"`
- **link**: `framework/ui.Link`, typed anchor with external-link affordances + unsafe-scheme href sanitizing
- **muted**: `framework/ui.Muted`, subdued inline `<span>` for secondary text
- **inline code**: `framework/ui.InlineCode`, a short escaped `<code>` in the mono font on the soft surface, for an operator, a field name or an example inside text; blocks of code use `CodeBlock`

### Buttons & form controls

- **button**: `framework/ui.Button`, semantic button with typed variants (primary / secondary / danger / ghost) + sizes, rendered through the headless structure with the `fui-button` class map. `Disabled` renders the disabled state (a `disabled` key in `ExtraAttrs` panics pointing at the field). Every `data-cui-*` key in `ExtraAttrs` is runtime wiring and travels the typed `Action` seam; a key outside the vocabulary panics naming it — under the old carrier contract it rendered as a dead attribute. The admitted vocabulary: `data-cui-rpc`, `-rpc-method`, `-rpc-body`, `-rpc-signal`, `-rpc-navigate`, `-rpc-open`, `-rpc-close`, `-rpc-reset`, `-rpc-after-text`, `-rpc-after-disable`, `-rpc-scroll-to`, `-rpc-refresh` (re-poll a widget after success), `-confirm`, `-signal-set`, `-signal-inc`, `-signal-toggle`, `-push-state`, `-open`, `-deeplink`, `-toast`, `-pane-open`, `-pane-key` (a keyed pane's deep-link identity), `-pane-close`, `-prefetch`, `-intercept-close` — the shapes `interactive.Action.Attrs()` / `OpenOnClick` / `PaneKey` and friends produce; every other `data-cui-*` key belongs to the component that renders its own markup for it
  `Icon` draws a registered icon before the label, and `IconOnly` the icon alone in a square button with the label as its accessible name. `Shortcut` ("Mod+S", needs `ID`) binds a chord that clicks the button, drawn after the label as ShortcutHint chips and announced through `aria-keyshortcuts`. `QuietUntilDirty` draws the button in the secondary look until the form it submits has unsaved edits (the leave guard's `data-hui-dirty`)
- **linkbutton**: `framework/ui.LinkButton`, anchor styled as a Button, for CTAs that navigate. `IconOnly` draws the `Icon` alone, as Button does. `External` owns `target`/`rel` (noopener). Carries the four link-legal wiring keys (`data-cui-push-state`, `-prefetch`, `-open`, `-deeplink`); every other `data-cui-*` key is refused, as always — a link navigates, a button acts
- **toggle**: `framework/ui.Checkbox` / `Radio` / `Switch`, labelled native inputs rendered through `headless.Choice` / `headless.Switch` — one inline run, the label wrapping the control; FieldErrors-aware, an error wrapping the run and its message in one unit
- **checkboxgroup**: `framework/ui.CheckboxGroup` / `RadioGroup`, `headless.Group` fieldset of checkboxes / radios; the group's message (error, else hint) belongs to the group, never to each leaf
- **changelist**: `framework/ui.ChangeList`, what an edit changed, one row per field: the field's label, the old value struck in a `<del>`, an arrow, the new value in an `<ins>`; screen readers hear "from" and "to" (`i18nui.KeyChangeFrom` / `KeyChangeTo`) in place of the arrow, and an empty side reads as `EmptyValue`. Values are `render.HTML`, so a caller passes the same cell markup its table draws. `ui.Ago(ctx, now, t)` beside it says a past time the way an activity feed does ("just now", "5m ago", "2h ago", "3d ago"), through `i18nui.KeyAgo*`
- **colorfield**: `framework/ui.ColorField`, colour swatch beside a text input holding the same value, as one control (`headless.Color`'s affix shell); the text input is the source of truth and requires a `Name`, so values the native picker cannot represent (`transparent`, `var(--x)`) survive and mark the shell instead of degrading — the swatch falls back to black; the headless module keeps the swatch and the text one value for every value the picker can show (the short `#abc` form expands for the swatch and is pickable, not an error). Use `ColorPicker` when a swatch + label is enough
- **segmented**: `framework/ui.SegmentedControl`, radio-group styled as a sliding pill bar
- **counter**: `framework/ui.Counter`, signal-driven numeric counter with +/− buttons
- **signaltoggle**: `framework/ui.SignalToggle`, `role="switch"` button bound to a boolean signal
- **toggleaction**: `framework/ui.ToggleAction`, three-state commit/untoggle button (idle → pending → committed) with optional mutex groups
- **passwordinput**: `framework/ui.PasswordInput`, password field with a show/hide reveal button rendered through `headless.Password` — the headless behaviour module binds the reveal and its words come from `ui.StringsFor`; a control, not a field: the error belongs to the FormField around it
- **searchinput**: `framework/ui.SearchInput`, search field with icon prefix + clear button
- **inputgroup**: `framework/ui.InputGroup`, input with prepend / append addons
- **rating**: `framework/ui.RatingInput`, 1-N star/heart with Size / Gap / Shape / Icon knobs
- **rating-display**: `framework/ui.Rating` (`ui.RatingDisplayConfig`), a read-only score drawn as glyphs sized to themselves, one `role="img"` with a localized "4 out of 5" name. Shares Shape / Size and the `--ui-rating-color` knob with RatingInput. Use it for a testimonial or a product average, where a disabled RatingInput would announce a form control and space its stars on 44px tap targets.
- **slider**: `framework/ui.Slider`, `<input type=range>` with optional live value mirror
- **rangeslider**: `framework/ui.RangeSlider`, dual-thumb range with cross-clamp
- **numberinput**: `framework/ui.NumberInput`, number field with explicit +/- buttons
- **textarea**: `framework/ui.TextArea`, multi-line input with typed Autogrow and Monospace (the mono font token, for JSON and code)
- **colorpicker**: `framework/ui.ColorPicker`, styled native `<input type=color>`
- **timepicker**: `framework/ui.TimePicker`, styled native `<input type=time>`
- **select**: `framework/ui.Select`, labelled native `<select>` with help, error, placeholder, and required marker; `Action` draws a control after the select on its row (an open-record link)
- **taginput**: `framework/ui.TagInput`, free-form chips, Enter/comma to commit, Backspace to remove
- **multiselect**: `framework/ui.MultiSelect`, checkbox group inside a disclosure with a chips strip above; submits as a plain form (the field name repeats per checked option, no script needed), the chips are the enhancement the `headless-multiselect` module rebuilds from the checkboxes' own state
- **form**: `framework/ui.Form`, opinionated `<form>` wrapper with submit + error summary; `Wide` lifts the readable-measure cap so a FormFrame body can split into its side column
- **formfield**: `framework/ui.FormField`, labelled input with required + help + error states
- **control**: `framework/ui.Control`, styled native input (email, password, datetime-local, file, tel, url, search…) for a FormField builder
- **textfield**: `framework/ui.TextField`, typed labelled native text field with required, help, error, autocomplete, and length attributes
- **numberfield**: `framework/ui.NumberField`, typed labelled native number field with explicit min/max/step bounds
- **datefield**: `framework/ui.DateField`, typed labelled native date field with HTML-date min/max bounds
- **formsection**: `framework/ui.FormSection`, grouped fields with a shared heading + description
- **formframe**: `framework/ui.FormFrame`, record form in two columns — a wide main column beside a narrow side rail (one of three named `SideWidth` sizes, each a modifier class reading a `--ui-form-frame-side-*` token) that drops under the main one below 48rem of the form's own width, a container query, so a drawer on a wide screen stacks and a full page sits side by side; the main column stops at 45rem, and `SidePanel` makes the rail a bordered panel that sticks beside the fields
- **validationsummary**: `framework/ui.ValidationSummary`, inline summary of form validation errors
- **conditionalfield**: `framework/ui.ConditionalField`, form region visible on first paint and hidden by the runtime until the watched field matches — one value (`WhenValue`) or any of a list (`WhenValues`)
- **formrepeater**: `framework/ui.FormRepeater`, dynamic list of repeating field groups (add / remove rows)
- **repeater**: `framework/ui.Repeater`, dynamic add / remove item list with min / max limits
- **stepwizard**: `framework/ui.StepWizard`, multi-step form with a progress indicator bar

### Selection & input composition

- **combobox**: `framework/ui.Combobox`, typed combobox over the headless primitive: a labelled input with `role=combobox` wiring, a GET-form no-script fallback to the same endpoint, an RPC-driven listbox whose options are real links, and a status region the module announces results through; `core-ui/patterns/combobox` is retired
- **picker**: `framework/ui.Picker`, a form field that picks one record out of many: a search input over a server-searched listbox (`headless.Combobox` in Pick mode) and a hidden input that submits the picked `Value`. The search input is detached from the host form (its `form` attribute names no form), so only the value submits. `Endpoint` answers each search, a POST of `{"q": …}`, with `ui.PickerRows(id, options, more)`; `Options` are the first rows shown on focus and `More` a note row after them ("Showing 20 of 312"). Focus selects the shown label so typing searches afresh; typed text that was never picked goes back to the picked label on blur, and an emptied input clears the value. `Action` sits after the input (an open or a New button). `headless.ComboboxRows` renders the rows for any island combobox answer.
- **commandpalette**: `framework/ui.CommandPalette`, ⌘K modal + combobox composition
- **globalsearch**: `framework/ui.GlobalSearch`, sticky inline `/`-shortcut search bar
- **dropzone**: `framework/ui.FileDropzone`, hero file-drop surface with image previews
- **fileupload**: `framework/ui.FileUpload`, drag-drop file picker over native `<input type=file>`

### Navigation

- **recordsummary**: `framework/ui.RecordSummary`, compact dominant record or event summary with status, next-decision, balanced phone metrics, a bounded support rail, ownership, and a lead-region natural-width action that stays early on phones
- **skiplink**: `framework/ui.SkipLink`, focus-visible bypass link for jumping to main content
- **pageheader**: `framework/ui.PageHeader`, top-of-page header with title / eyebrow / subtitle / actions
- **anchoredrail**: `framework/ui.AnchoredRail`, sticky in-page nav rail with scrollspy-tracked active state
- **contentrow**: `framework/ui.ContentRow`, the page's content row — a start column (the nav landmark `ContentRowConfig.NavLabel` names, wrapping a `ui.Sidebar` whose own inner nav names only the links list), the main column, an optional toolbar band above main beside the nav (a region landmark `ToolbarLabel` names), and an optional context aside that releases its width around an empty outlet. `Breakpoint` picks the stack width (below md by default, below lg to match `SidebarConfig.DrawerBreakpoint`); `Viewport: true` fills the rest of the viewport below the header and scrolls each column on its own — the row reads the header's height from `--size-header-height` and cannot style its parent, so recipes pair it with a page-tall `ui.Stack{Screen: true}` and the fixed header band.
- **tabs-signal**: `framework/ui.Tabs`, signal-driven tab strip (click sets the signal; CSS shows the panel); `StateAttrs` adds `data-state=active/inactive` to the buttons, `ID` wires `aria-controls`/`id` pairs, `VacateHidden` ships hidden panels empty with their content in a JSON stash (restored on show by the `headless-tabs` module) so page-scoped test locators cannot match hidden text — the contract knobs a port needs, each off by default
- **tabnav**: `framework/ui.TabNav`, a navigation strip of links where at most one is current (`aria-current="page"`; a second `Current` panics): the view tabs above a list, the sub-navigation of a record screen. Every tab is a plain `<a>`, so a tab click is a navigation the client router intercepts and the URL stays shareable — the query-param contract since list state moved off islands. `Badge` rides inside the link, `aria-hidden`, for a view's row count. `End` draws a control at the strip's trailing edge, outside the `<nav>` landmark, such as a "Save view" dropdown; the root is then a `<div>` holding both. Distinct from `tabs-signal`, which swaps panels in place under one URL
- **breadcrumbs**: `framework/ui.Breadcrumbs`, `<nav aria-label>` landmark over an ordered trail; the last step (or the one carrying `Current`) is `aria-current="page"` text, never a link to itself, and separators are `aria-hidden`
  `CompactMobile: true` shows the final two steps below md and hides the leading separator; the full trail remains on desktop.
- **pagination**: `framework/ui.Pagination`, numeric page pager with typed query props (`Path`, `Query url.Values`, `PageParam` default `p`) — every href is built through `net/url` with the page parameter replaced, never a `%d` pattern; `Window` sizes the page neighbourhood, `OmitPrevNext` drops the ends. The Island is optional, the Table posture: a list screen's page anchors are plain navigations the client router intercepts, and a pager inside an island region (a `DataTable` footer) carries the RPC contract beside its hrefs and the `data-hui-page` hook the table module restores focus through. `core-ui/patterns/pagination` is retired: this is the only pager
- **sidebar**: `framework/ui.Sidebar`, responsive primary nav with persistent, collapsible (local-storage persisted), off-canvas, and auto-hide variants; `Collapse` moves the collapsed state to the server, `GroupMarkup` swaps `<details>` groups for `button[aria-expanded][aria-controls]` + `hidden` container, `CollapseLabel`/`ExpandLabel` rename the toggle and `CollapseText` its visible foot label (the toggle is a sticky row at the column's foot, icon-only in the rail), `SectionLabels` draws group headers as small uppercase section labels over flush links, and `--ui-sidebar-row-height` sets a row's height (2.25rem under a fine pointer, the 44px touch target on a coarse one) (see [Sidebar: server-owned collapse state](#sidebar-server-owned-collapse-state)); set `NavLabel` when a page has multiple navigation landmarks and mount the matching drawer with `MountSidebar`
- **sidebarbrand**: `framework/ui.SidebarBrand`, the product mark at a sidebar's head: a square logo tile (the `Logo` image, or the name's initial on the inverted surface), the name, and an optional muted `Sub` line; `--ui-sidebar-brand-tile` sizes the tile. A collapsed rail keeps a `Prepend` that is only a brand and draws its tile alone; any other `Prepend` hides there
- **sidebardrawertrigger**: `framework/ui.SidebarDrawerTrigger`, the sidebar's drawer toggle rendered on its own — the relocated hamburger a header row carries while the sidebar itself lives in the body; pass the SAME `SidebarConfig` the `ui.Sidebar` render uses and the pair stays one logical control (the trigger hides itself at >= md exactly like the sidebar's inline copy, so `SuppressDrawerTrigger` and a relocated trigger never draw two)
- **menu**: `framework/ui.Menu`, keyboard-driven dropdown built on `<details>`; `MenuItem.ID` gives a row an addressable `id` (caller-owned uniqueness, ignored on separators; `ExtraAttrs` still cannot set `id`); `MenuConfig.TriggerElement` swaps the framework `<summary>` for a caller-owned trigger — pass the inline HTML of your own `<button>` (or `<a>`) and the runtime makes it the controller: it wires `aria-haspopup`/`aria-controls`/`aria-expanded` at hydration, toggles on click/Enter/Space (activation is prevented — put navigation on menu items), focuses the first menuitem on open, and returns focus to your element on Escape. Use it whenever the page owns the trigger's markup or classes (avatar buttons, pill buttons): routing such an element through `TriggerHTML` nests it inside the summary, which axe reports as `nested-interactive`. `TriggerElement` overrides `Label` and `TriggerHTML`; give each trigger menu a distinct `ID` when two structurally identical ones share a page. `MenuConfig.LazyPanel: true` keeps the panel's rows out of the document tree until the menu is first opened: SSR ships them inside an inert `<template data-cui-menu-lazy>` as the panel's only child (the panel `<div>` stays, so `aria-controls` still resolves), and the runtime mounts them on first open — before its focus-on-open lookup, so the keyboard contract is unchanged. Use it when live-DOM queries must not see closed-menu rows: host Playwright contracts that pin `getByText('Theme')` to the first visible match or `getByLabel` to exactly one element. The rows are still in the HTML source, so this hides nothing from a crawler that parses the response. The cost: rows are not in the DOM until first open, so host JS that binds menu rows by id at page load must use delegated listeners instead, and with JavaScript disabled the menu opens empty (only the disclosure module mounts the rows); the zero value renders rows inline exactly as before. `MenuConfig.IconOnly: true` draws the trigger as the `more` icon with `Label` kept as its visually hidden accessible name, for a table row's action menu. `MenuConfig.Avatar` draws the trigger as that avatar (round, borderless, 32px, a 44px box on coarse pointers) with `Label` as its hidden name, for an account menu; it panics beside `TriggerHTML`, `TriggerElement` or `IconOnly`. `MenuConfig.Icon` draws a registered icon before `Label` on the default trigger (a list's Columns menu); it panics on an unregistered name and beside `TriggerHTML`, `TriggerElement`, `IconOnly` or `Avatar`. A row's action is one of `Href`, `RPC`, `Action`, `Do` (a built `*interactive.Action`, so the row carries its confirm, navigate and toasts) or `Copy` (`&ui.MenuCopy{Target: "<element id>", Toast: "Copied"}` copies that element's text through the headless copy module); a row naming two panics at render. `MenuItem.Check: true` makes a row a checkbox option (`role="menuitemcheckbox"`, `aria-checked` from `Checked`): the state is the server's, so pair it with `Href` or `RPC` that toggles it, as a list's Columns menu does; it panics beside `Radio` or `Children`.
- **tree**: `framework/ui.Tree`, WAI-ARIA treeview on the headless primitive: roving tabindex, arrows/Home/End/type-ahead bound by the registered `headless-tree` module; leaf hrefs are real anchors, a static branch is real markup, and a `LazyPath` branch keeps the kernel's rpc wiring on its toggle with a hidden signal-bound group (`LazySignalPrefix` names the signal)
- **toc**: `framework/ui.TableOfContents`, auto-built sticky nav from `<h2>` / `<h3>`
- **steprail**: `framework/ui.StepRail`, vertical numbered step rail with an active step + anchor links
- **steps**: `framework/ui.ProgressSteps`, linear step indicator (horizontal + vertical)

### Disclosure / surface widgets

- **collapsible**: `framework/ui.Collapsible`, styled `<details>` with clickable summary + Escape-to-close
- **dropdown**: `framework/ui.Dropdown`, a trigger button whose panel floats under it and holds any content (a list's filter fields, a "save view" form), where a `Menu` holds only command rows. A native `<details>` (headless `Disclosure` with `Dismiss`): it opens with no script and closes on a click outside, Escape, or a client-side navigation. `Icon` names a registered icon, `Count` draws a badge (the filters applied), `Align: ui.DropdownEnd` lines the panel up with the trigger's end edge. Fields inside a closed dropdown still submit with their form. Knobs: `--ui-dropdown-min-width`, `--ui-dropdown-max-width`; the viewport bounds both.
- **modal**: `core-ui/widget/preset.Modal`, focus-trapped dialog with deeplink
- **drawer**: `core-ui/widget/preset.Drawer`, edge-mounted sliding panel
- **bottomsheet**: `core-ui/widget/preset.BottomSheet`, bottom-anchored Drawer variant
- **popover**: `core-ui/widget/preset.Popover`, click-triggered floating surface
- **floatingpanel**: `core-ui/widget/preset.FloatingPanel`, corner-anchored persistent panel
- **tooltip**: `framework/ui.Tooltip`, CSS-only hover/focus reveal
- **toast**: `core-ui/widget/preset.ToastStack`, client-side slide-in notifications (no SSE, no server queue)
- **notificationbell**: `framework/ui.NotificationBell`, bell + unread badge + popover dropdown
- **confirmaction**: `framework/ui.ConfirmAction`, trigger + alertdialog Modal
- **commandpalette**: *(also under Selection; same component)*

### Layout & display

- **layout**: `framework/ui.Stack` / `Cluster` / `Grid` / `Center` / `Spacer` / `Box`; `Cluster` wraps by default and exposes the explicit `NoWrap` opt-out; `Shrink` lets a no-wrap row narrow, its last child (a trail that ellipsizes) taking the squeeze while the earlier children keep their size. `StackConfig.Screen: true` makes the stack the page column: at least one viewport tall (100dvh) with its last child pushed to the bottom — the option a recipe uses to keep a short page's footer at the bottom of the screen
- **listdetail**: `framework/ui.ListDetail`, a labelled, keyboard-scrollable list beside a detail slot. Keep it in the group layout to preserve its node and scroll position while detail navigation swaps the primary slot. On phones the detail stacks above the list. `ExtraAttrs` accepts the two markers returned by `LayoutTree.VTRegion()` for whole-region transitions.
- **listdetailplaceholder**: `framework/ui.ListDetailPlaceholder`, marks an unselected detail screen for `ListDetailConfig.MobileSinglePane`. This opt-in phone mode shows the list on an index and the detail on an issue, including a missing issue; desktop remains split. `BackHref` (with `BackLabel`, default "Back") adds a link to the list at the top of the detail pane, shown only while a phone shows the detail alone; it requires `MobileSinglePane`. The default ListDetail still stacks both regions on phones.
- **container**: `framework/ui.Container`, max-width page wrapper with breakpoint padding; `Width: ContainerPage` caps at the page measure (`--size-page-width`, 66rem) — the editorial column a contained page's main and its header/footer bands share. Like the bands, its content box is the measure and the page gutter (`--size-page-gutter`, clamp(20px, 5vw, 32px)) sits outside it, so main's text starts on the header brand's edge at every width; `Pad` is the page's block rhythm: `ContainerPadPage` pads under the header (`--ui-container-pad-start`, clamp(40px, 6vw, 64px)) and above the footer (`--ui-container-pad-end`, clamp(48px, 7vw, 80px)), `ContainerPadEnd` only above the footer; an unknown value panics; `Start: true` pins the column to the inline start and drops its gutter, for a measure inside a frame that already pads its content (a settings page in an app shell's pane)
- **section**: `framework/ui.Section`, labelled content section with heading + description; `Overline` draws the heading as a small upper-case muted group label over a run of cards (a dashboard's "Billing"), still the section's h2
- **responsive**: `framework/ui.Responsive`, viewport-swap pair (independent desktop / mobile variants). `Below` picks the breakpoint the mobile variant shows below: `ui.StackBelowMD` (the zero value, 48rem) or `ui.StackBelowLG` (64rem)
- **panehost**: `framework/ui.PaneHost`, primary pane + openable secondary/tertiary side panes with a responsive overlay-drawer collapse
- **themed**: `framework/ui.Themed`, wraps a subtree in a registered section-level theme override
- **workbench**: `framework/ui.Workbench`, viewport-height inspector shell: a fixed-width rail that scrolls on its own beside a pane that fills the rest (an `<iframe>` in the pane fills it edge to edge); stacks below 720px
- **card**: `framework/ui.Card`, a surface with header/body/footer over `headless.Card`; `Href` makes the whole card one link; `Action` puts a control at the header's end, level with the heading (a feed's "View all" link), and panics beside `Href`
  An interactive card with `aria-current="page"` receives the active surface treatment, including marks applied by the runtime's navigation sweep.
- **sticky**: `framework/ui.Sticky`, theme-token sticky wrapper for top or bottom edge pinning
- **aspectratio**: `framework/ui.AspectRatio`, CLS-safe aspect-ratio wrapper for media and embeds
- **image**: `framework/ui.OptimizedImage`, responsive `<picture>` with CLS-safe Width/Height
- **pipelineimage**: `framework/ui.PipelineImage`, multi-format `<picture>` consuming `framework/image` VariantSet output (typed sources + LQIP/BlurHash)
- **divider**: `framework/ui.Divider`, semantic separator (horizontal, vertical, labelled)
- **gallery**: `framework/ui.Gallery`, Grid / Strip / Masonry thumbnail surface
- **lightbox**: `framework/ui.Lightbox`, zoom-overlay modal; pairs with Gallery
- **carousel**: `framework/ui.Carousel`, horizontal scroll-snap slider
- **sortablelist**: `framework/ui.SortableList`, drag-and-drop + keyboard reorderable list on the headless primitive (Space grabs, arrows move, Space drops, Esc cancels); the commit is the pattern's server round trip — 2xx confirms, non-2xx reverts, linked columns share `Group`, a versioned 409 refetches fresh rows (`SortableListItems` is that fragment)
- **optimisticaction**: `framework/ui.OptimisticAction`, button that flips to its SSR-declared success state on click; the RPC fires underneath and rolls back with a shake on non-2xx
- **toggleaction**: `framework/ui.ToggleAction`, OptimisticAction's three-state cousin: idle ↔ committed with optional untoggle endpoint and `Group` mutex (committing one reverts its siblings)
- **networkretrybanner**: `framework/ui.NetworkRetryBanner`, persistent banner that shows on RPC-failure threshold or SSE silence; retry button pings a health endpoint to recover

### Data display

- **metricband**: `framework/ui.MetricBand`, flat semantic signal band (one row wide, two columns on phones) for related facts that should not become a wall of cards; `Hint` adds a trend or qualifier
- **datatable**: `framework/ui.DataTable`, sortable / paginated / island-swappable rows; a sort header is an anchor in both postures — the URL is a list screen's state, and an embedded table's anchors carry the island contract beside their hrefs. Every sortable header shows a muted up-down glyph, and the sorted one an up or down arrow, drawn from its `aria-sort`; `--ui-data-table-sort-icon-size` sizes it. A cell holds its value on one line, so a table wider than its box scrolls inside it; `Column.Wrap` lets a prose column wrap, at least `--ui-data-table-wrap-width` (16rem) wide. `Column.Fit` shrinks a column to its content (a checkbox, a row menu) so the others take the spare width; a fitted column cannot also Wrap. `Column.SelectAll` names the row checkboxes a header checkbox governs: it checks and clears that table's boxes of the name and shows mixed when only some are checked; an empty table draws no box. A sort header's label sits level with the values under it. `Responsive: ui.ResponsiveCards` turns each row into a labelled card below 640px of container; `Responsive: ui.ResponsiveRows` turns it into a two-line phone row below 720px: the `Column.Phone` slots place cells (`PhoneTitle` over `PhoneSubtitle`, `PhoneMeta` over `PhoneDetail` at the end, `PhoneLead` before for a checkbox, `PhoneEnd` after for a row menu), a column with no slot is not drawn on the phone, and one column must be the `PhoneTitle`.
- **selection**: `framework/ui.Selection`, a `Bar` that acts on selected rows over the `Body` that holds them, a list's bulk-action form over its table. The bar shows only while a checkbox in the body is checked, through CSS `:has()` with no script; a browser without `:has()` shows it always. Body checkboxes join the bar's form with `form=`. Knob: `--ui-selection-gap`
- **statcard**: `framework/ui.StatCard`, metric card with label/value/trend. `Href` links the label to what the number counts, `Icon` names a registered icon for the card's head, and `Action` puts a control (a "New" LinkButton) in the head beside it, outside the link. A 4-card dashboard row lives in a `ui.Grid`; the Grid default `Min: "16rem"` wraps 3+1 inside a sidebar-narrowed content column (~900px). For a 4-up row that fits (and degrades to 2+2 on tablet), pass `Grid(GridConfig{Min: "13rem"}, …)`; the `Min` knob is the intended control, not a Grid default change (16rem stays right for general content cards). A card fills its grid cell, so a row of cards shares one height even when each sits in a wrapper (a polled region); `GridConfig.Fill` keeps a lone card, or a short last row, at a column's width instead of stretching across the row.
- **animatedcounter**: `framework/ui.AnimatedCounter`, IntersectionObserver-driven tick. It renders the figure alone, with no step buttons (`headless.CounterProps.Display`): a number that animates in is read, not operated
- **timeline**: `framework/ui.Timeline`, vertical event rail; each event has a `Title` or, in its place, a `Lead`: headline markup the caller draws (a bold name, a link) from escaped parts; `TimelineEvent.Icon` draws a registered icon in the marker, a bordered circle (`--ui-timeline-icon-size`, 28px) with the icon tinted by the variant
- **sparkline**: `framework/ui.Sparkline`, pure-SVG inline trend chart
- **piechart**: `framework/ui.PieChart`, SVG ratio chart (donut variant via InnerRadius)
- **barchart**: `framework/ui.BarChart`, categorical SVG bar chart. Legible by default: value labels ride above every bar cap (opt out with `HideValues`), the y-scale rounds up to a clean maximum so uniform / near-equal data keeps visible headroom (no wall of full-height slabs), a hairline baseline grounds the bars, and long `ShowLabels` category labels wrap onto two lines (a single over-long word ellipsizes, full text preserved in the bar's `<title>`). `FitHeight` sizes the SVG to hug the tallest bar (a 96px cap) instead of the fixed `Height`, so no blank band pads the space above the caps — bar ratios stay identical, only the padding goes. `ShowAxis` adds left value-axis ticks + gridlines. Per-bar `Color` accepts a palette token (primary/info/success/warning/danger), a registered status variant name, or a hex/rgb/hsl/oklch/var() CSS color; any other value falls back to the theme primary.
- **linechart**: `framework/ui.LineChart`, multi-series time-series chart with area + legend. Edge x-axis labels anchor inward so the first/last tick don't clip against the SVG boundary.
- **codeblock**: `framework/ui.CodeBlock`, styled `<pre><code>` sample block; the `HighlightLines` *func* pre-tokenizes lines for syntax highlighting, and the `HighlightLines []LineRange` *field* (fence `{1,3-5}`), `Diff`, `HighlightWords`, and `Wrap` add line bands, diff marking, word marks, and soft wrapping — see [CodeBlock: line highlighting, diffs, wrapping](#codeblock-line-highlighting-diffs-wrapping)
- **codetabs**: `framework/ui.CodeTabs`, the same snippet in several languages (Go / TypeScript / curl …) behind a zero-JS tab strip; pure composition of `headless.Tabs` + `CodeBlock` with copy buttons. Selection is per-tabset, not a page-wide language preference. The SDK docs site (`framework/sdkdocs`) is the flagship consumer.
- **counter**: `framework/ui.Counter`, numeric counter with +/− buttons mutating a client-side signal
- **jsonviewer**: `framework/ui.JSONViewer`, collapsible tree of arbitrary values
- **diffviewer**: `framework/ui.DiffViewer`, unified or split diff renderer
- **markdown**: `framework/ui.Markdown`, themed wrapper over `core/markdown`. `Measure: true` caps the line length at a reading width (`--ui-markdown-measure`, 72ch) for long-form pages in a wide column; generated markdown blocks set it. Fence options reach it through the `data-meta` attribute `core/markdown` emits and forward onto `CodeBlockConfig`: `title=`, `showLineNumbers`, `scroll`, `{1,3-5}` / `highlight=`, `diff`, `words=`, and `wrap` (see the [CodeBlock section](#codeblock-line-highlighting-diffs-wrapping) for the table). Unknown options are ignored, so an option added later degrades to a plain block rather than breaking one, and the raw info string stays on the block root in `data-meta`
- **factbox**: `framework/ui.FactBox`, single labelled fact (compact label + value pair; label-first or value-first)
- **detaillist**: `framework/ui.DetailList`, label/value description list for record detail views; stacks labels above values in narrow containers, including desktop context cards, and on phones; `Stacked` draws a read-only value in a form, label above and the value boxed like a control; `Spread` draws compact side-column facts, label at the start and value at the end, a long first value (an id) truncating ahead of a copy button
- **progress**: `framework/ui.Progress`, native `<progress>` with theme styling; determinate (`Value` 0..`Max`, clamped at render) or indeterminate (`Value` < 0), labelled visibly (`ShowLabel`) or through `aria-label`. A named-stage walk is `ProgressSteps`, a different component
- **terminalblock**: `framework/ui.TerminalBlock`, terminal transcript with a labelled header and `TerminalOut` / `TerminalOK` lines
- **skeleton**: `framework/ui.SkeletonCard` / `SkeletonRow` / `SkeletonAvatar` / `SkeletonTimeline` / `SkeletonLine`, loading placeholders over `framework/headless.Skeleton`: hidden shimmer bars, one polite "Loading…" announcement per preset; the timeline preset draws event-shaped rows (dot, name line, two text lines) matching what `ui.Timeline` arrivals look like, and `SkeletonLine` draws ONE capped bar — the loading twin of a breadcrumb trail or a one-line label (an area's `Loading.Show`, a crumbs skeleton)
- **spinner**: `framework/ui.Spinner`, inline CSS loading indicator

### Tags, badges, filters

- **tag**: `framework/ui.Tag`, interactive pill (linked / removable / status-variant)
- **statuspill**: `framework/ui.StatusPill`, compact status pill with optional leading dot (neutral / accent tone)
- **statusbadge**: `framework/ui.StatusBadge`, small inline pill conveying state (success / warning / danger / info / neutral); `Dot` draws a filled circle in the label's tone before the word, the status-column shape, sized by `--ui-badge-dot-size` (0.375rem)
- **filtertoolbar**: `framework/ui.FilterToolbar`, the filter/sort control strip above a list (facet `<select>` or radio-pill groups + search + sort + Apply/Reset), a single URL-driven GET form; wraps → stacks responsively so nothing clips on mobile. `Dropdown: true` moves everything but the search into a Filters dropdown, `Extra` adds controls of the same form, `Applied` adds to the Filters badge, and `Tools` draws links and link menus at the row's end
- **filterchipbar**: `framework/ui.FilterChipBar`, `role=toolbar` of removable filter chips; a chip's × POSTs its `DismissBody` to `DismissPath` and the bar swaps in the response (the × is a link to `DismissPath` without script)
- **shortid**: `framework/ui.ShortID`, a long identifier (a UUID, a job id, a hash) in a table cell: its first `Length` characters (default 8) in monospace with an ellipsis, the full value as their title, and an inline copy button that copies all of it. Screen readers hear the full value once, from a visually hidden copy the button targets. A value no longer than `Length` shows whole
- **copybutton**: `framework/ui.CopyButton`, clipboard button with SR-announced confirmation; `IconOnly` with `Icon` draws a registered icon (`"copy"`, `"link"`, `"arrow-up-right"`) in place of the glyph; `Inline` draws it quiet (no border or fill until hover, a 24px target) for a copy control inside a line of text; `Ghost` draws it like a small ghost icon `Button`, for a row of icon buttons
- **drawerbar**: `framework/ui.DrawerBar`, the bar across the top of an intercepted drawer: a close button (`data-cui-intercept-close`), the layer's path in mono, a copy-link button when `CopyURL` is set, an open-as-page link (`data-cui-intercept-page`) when `PageURL` is set, then `Actions`. `Prev` and `Next` draw up and down icon links after the path, each marked `data-cui-intercept-swap` so the target renders in the same drawer; an empty one draws disabled, and both empty draw neither. It sticks to the layer's top edge while the layer scrolls. Draw it when `app.OverlayFromContext` reports a drawer
- **toolbar**: `framework/ui.Toolbar`, `role=toolbar` wrapper for grouped actions

### Status & banners

- **themetoggle**: `framework/ui.ThemeToggle`, dark/light/auto toggle that persists color-scheme mode; fresh scaffolds mount the adaptive `framework/ui/theme.Default()` palette, while app-owned themes must keep `DarkColors` complete
- **themepicker**: `framework/ui.ThemePicker`, switches the whole page between the app theme and registered overrides (`style.RegisterThemeOverride`) by putting the override class on `<html>`; the choice persists per browser (see theming → "Page themes")
- **backtotop**: `framework/ui.BackToTop`, fixed scroll affordance that appears after a threshold
- **banner**: `framework/ui.Banner`, page-level persistent status strip
- **callout**: `framework/ui.Callout`, persistent inline info / warning / danger / neutral block
- **notification**: `framework/ui.Notification`, toast-styled inline notification (variant + dismiss)
- **emptystate**: `framework/ui.EmptyState`, centered title + description + optional CTA for no-data screens; `Compact` draws one muted line in a small panel, for an empty list inside another screen
- **signout**: `framework/ui.SignOut`, logout control: minimal form POSTing to the auth sign-out endpoint; compatible with `auth.WithBFFPosture`, whose logout handler enforces same-origin submission
- **pollingindicator**: `framework/ui.PollingIndicator`, pulsing dot + label confirming a polling RPC is firing
- **seo**: `core-ui/seo` + `uihost.WithSitemap` / `WithRobots` + `ScreenCanonical` / `ScreenHreflangs` / `ScreenSchema`, per-page SEO + sitewide sitemap.xml / robots.txt
- **seo-bundle**: `ScreenSEO()` returning an `SEO` struct, per-screen bundle of description + canonical + hreflangs + robots + OG + Twitter Card + JSON-LD in one declaration; alternative to the per-method calls above

### Marketing & page sections

- **hero**: `framework/ui.Hero`, centered landing hero (eyebrow + title + subtitle + actions + optional media)
- **herosplit**: `framework/ui.HeroSplit`, two-column hero (copy + media) with equal / copy-wide / media-wide ratios
- **pricingcard**: `framework/ui.PricingCard`, plan tile (price + period + feature list + CTA), optional featured highlight; cards placed directly in a `ui.Grid` that holds only plans share their row lines, so prices and feature lists start level across a row; a card in a wrapper cell or a mixed grid keeps its own rows
- **authcard**: `framework/ui.AuthCard`, centered card shell for login / register / reset forms (title + alert + body + footer)


## Sidebar: server-owned collapse state

A sidebar group has `Children` and no `Href`: its label opens and closes
the group. Put the section's overview page in the group's first child
link instead. Setting both `Children` and `Href` panics with this fix.

`SidebarConfig.CurrentPath` already splits who decides the active item:
set it and the server renders the highlight, leave it empty and the
runtime stamps it after hydration. `Collapse` does the same for the
collapsible rail.

The zero value (`SidebarCollapseAuto`) keeps the localStorage behaviour:
the runtime restores the collapsed state after hydration and persists
every toggle. `SidebarCollapseCollapsed` and `SidebarCollapseExpanded`
make the server own the state: the collapsed rail (or expanded column)
ships in the SSR bytes as `data-collapsed` on the root, the toggle
button renders `aria-expanded="false"` plus the expand label when
collapsed, and the runtime neither reads nor writes localStorage for
that sidebar.

Use it when the collapse preference is per-user server data — a setting
restored from the database that must survive first paint on a device
whose localStorage says otherwise:

```go
// pref is the signed-in user's stored sidebar preference.
collapse := ui.SidebarCollapseAuto
switch pref.Sidebar {
case "collapsed":
    collapse = ui.SidebarCollapseCollapsed
case "expanded":
    collapse = ui.SidebarCollapseExpanded
}
sbCfg := ui.SidebarConfig{
    Title:         "myapp",
    Variant:       ui.SidebarCollapsible,
    Collapse:      collapse,
    CollapseLabel: "Collapse sidebar",
    ExpandLabel:   "Expand sidebar",
    Items:         items,
}
```

Three more knobs round out the contract surface:

- `CollapseLabel` / `ExpandLabel` rename the toggle button. The button
  carries the expand label and `aria-expanded="false"` while collapsed,
  the collapse label and `aria-expanded="true"` while expanded; both
  names ride along as data attributes so client-side toggles keep the
  custom wording. Defaults: "Collapse navigation" / "Expand navigation".
- `GroupMarkup: ui.SidebarGroupButton` renders groups (items with
  `Children`) as `button[aria-expanded][aria-controls]` plus a container
  that carries `hidden` when closed, instead of the default
  `<details><summary>`. The sidebar runtime module toggles both on
  click. For hosts whose specs pin that markup shape. Two `<details>`
  behaviours do not carry over: group open state is not persisted
  across navigation (every swap resets a group to its server-rendered
  state; the details dialect carries `data-hui-disclosure-persist`),
  and the dialect needs JavaScript — without the runtime module a
  closed group's links are unreachable, while `<details>` opens
  natively with JS off.
- `Variant: ui.SidebarAutoHide` renders the persistent column as a
  64px icon rail at >= md viewports and reveals the full column on
  hover or keyboard focus (`:focus-within`), straight from the
  component's stylesheet — no JavaScript, no host CSS.
- `Prepend` is a component rendered between the title and the `<nav>`
  on every body path: the inline column, `SidebarBody`, and the
  `MountSidebar` drawer. A docs site whose phone header hides its
  section tabs puts the section `<select>` here, so the drawer (the
  only navigation at that width) carries it without forking the mount.
  It takes a `component.Component` rather than HTML because
  `MountSidebar` runs once at boot while the drawer body renders per
  request: a Prepend that implements `component.ContextComponent`
  sees the request on the inline and drawer paths alike, so the select
  can mark the current section. Wrap static markup in
  `app.NewStaticComponent`. It hides with the title in the collapsed
  rail and the auto-hide rest state; `Footer` stays below the nav.
- `ui.MountSidebarFunc(r, build)` mounts the drawer from a builder
  instead of a value: the drawer body calls `build(ctx)` on every
  chrome request, so a sidebar the layout already builds per request
  (`ui.Sidebar(build(ctx))`, a footer with the signed-in user's
  `ui.SignOut`) shows the same footer and items in the phone drawer.
  `MountSidebar(r, cfg)` renders `cfg` as given, so a config built once
  at boot with `context.Background()` gives every phone user the
  anonymous footer. The drawer's name, variant and header brand come
  from `build(context.Background())` at mount time.
- `Count` ends a link's row in a small muted figure (`render.Text("12")`);
  the collapsed rail hides it. In a kept shell, hand in an inline route
  area (`app.AreaSpec{Inline: true}`, `l.RouteArea`) so the figure
  re-renders with each client navigation; a plain value keeps the
  figure the page loaded with.
- `MatchPath` rides the rendered link as `data-cui-match-prefix` (the
  value, not the href, is the prefix): the server marks the item
  current on first paint, and the runtime's active-link sweep keeps it
  lit across client navigations into the section — without it the
  exact-href sweep clears the highlight the moment the URL grows a
  deeper segment inside a kept shell.
- `SuppressDrawerTrigger: true` plus `ui.SidebarDrawerTrigger(cfg)`
  moves the hamburger into your own chrome (the page header, left of
  the brand): the standalone button is the same trigger — same class,
  same `data-cui-open` widget contract, same `>= md` self-hiding from
  the component's stylesheet. `MountSidebar` still mounts the drawer
  itself, once, as always.
- `NativeMobile: true` adds a native disclosure for browsers with scripting
  disabled. CSS selects it with `(scripting: none)`; scripted browsers keep
  the mounted drawer and its focus and Escape behavior. Keep calling
  `MountSidebar`. The fallback uses the same filtered items and group markup.

---

## Filter toolbars: the URL-driven pattern

`ui.FilterToolbar` is the control strip that sits above a `DataTable` or
card grid on a list screen. It renders **one `<form method="GET">`** whose
controls carry the current filter/sort/search state. Submitting it (Apply)
navigates to `<action>?facet=value&sort=…&q=…`; the screen's `Load(ctx)`
reads those params and renders the filtered list server-side. Refresh,
share, and back-button all reduce to "same URL → same view" with no client
state. This is the "URL params are the source of truth" contract from
`core-ui/ARCHITECTURE.md`. Reset is a plain link back to the bare action, so
it clears every param with zero JavaScript. It works with the runtime
disabled; the runtime just makes the Reset link a soft SPA nav.

Facets render as a native `<select>` (default) or, per `Kind: FacetPills`,
a wrapping radio-pill group (short, glanceable choices). The toolbar is
responsive by construction: it declares itself a container and lays its
controls out with flex-wrap, degrading row → wrapped rows → single-column
stack as *its own* width shrinks (correct even inside a slim sidebar on a
wide screen). Every control, including Apply/Reset, stays on-screen and
tappable; nothing overflows a narrow ancestor, and pill labels never wrap
mid-label ("Waiting On Customer" stays one line).

```go
// Screen.Render: the toolbar reflects the current URL state.
func (s *CustomersScreen) Render() render.HTML {
    return ui.Stack(ui.StackConfig{Gap: ui.GapLG},
        ui.FilterToolbar(ui.FilterToolbarConfig{
            Action: "/customers", // the list route (the form GETs here)
            Facets: []ui.Facet{
                {Name: "status", Label: "Status", Value: s.status, Options: []ui.FacetOption{
                    {Label: "Open", Value: "open"}, {Label: "Closed", Value: "closed"},
                }},
                {Name: "plan", Label: "Plan", Kind: ui.FacetPills, Value: s.plan, Options: []ui.FacetOption{
                    {Label: "Free", Value: "free"}, {Label: "Pro", Value: "pro"},
                }},
            },
            Search:    &ui.FilterSearch{Name: "q", Value: s.query, Placeholder: "Search customers…"},
            Sort:      []ui.SortOption{{Label: "Newest", Value: "created_desc"}, {Label: "Name A–Z", Value: "name_asc"}},
            SortValue: s.sort,
        }),
        ui.DataTable(/* … rows filtered per s.status / s.plan / s.query / s.sort … */),
    )
}

// Screen.Load: read the URL params the toolbar submits.
func (s *CustomersScreen) Load(ctx context.Context) error {
    q := app.QueryFromContext(ctx)
    s.status, s.plan = q.Get("status"), q.Get("plan")
    s.query, s.sort = q.Get("q"), q.Get("sort")
    return nil // fetch + filter rows from s.* here
}
```

An empty facet value (the auto-prepended "All" choice) submits `status=`;
the server treats an empty param as "no filter". Pair with `FilterChipBar`
below the toolbar to show the *active* filters as removable chips.

`Dropdown: true` is the compact layout a dense list wants: the row is
the search field (`--ui-filter-toolbar-search-width`, 22.5rem), then a **Filters** button, then the
`Tools`. The facets, the sort, the `Extra` controls and the one
Apply/Reset pair move into the button's `ui.Dropdown` panel, and its
badge counts the facets set plus `Applied` (the filters an `Extra`
control holds, such as a typed filter expression). Everything stays in
the one form, so Enter in the search submits the panel's fields whether
the panel is open or not. With nothing for the panel there is no Filters
button and no Apply; the search submits on Enter. `Tools` are links and
link menus (a Columns menu, a layout switch) drawn at the row's end;
they render inside the form, so a tool holding a `<form>` panics.
`--ui-filter-toolbar-panel-width` sets the panel's minimum width.

> **`SearchFields` integration.** When the entity declares
> `SearchFields`, `FilterSearch{Name: "q"}` maps 1:1 onto the list
> endpoint: the `?q=` param the toolbar emits is exactly what the
> auto-CRUD List handler free-text-searches. No manual wiring needed:
> the server-side search is automatic. Without `SearchFields`, the
> `?q=` param is ignored by the CRUD layer and the screen must filter
> rows itself (the pre-existing "wired manually" behaviour).


---

## CodeBlock: line highlighting, diffs, wrapping

`ui.Markdown` forwards every fence option it recognizes onto
`CodeBlockConfig`, so a documentation fence can highlight lines, mark a
diff, wrap long lines, and cap its height without leaving Markdown:

| Fence option | Maps to | Effect |
|---|---|---|
| `title="main.go"` / `title=main.go` (`filename=` too) | `Filename` | chrome header with the filename |
| `showLineNumbers` (`showLineNumbers=false` off) | `LineNumbers` | left line-number gutter |
| `scroll` | `Scroll` | caps body height at `--ui-code-block-scroll-max`, scrolls |
| `{1,3-5}` or `highlight=1,3-5` | `HighlightLines []LineRange` | background band on those lines (1-based, inclusive) |
| `diff` | `Diff` | lines starting `+` (and `+++`) get an added band, `-` (and `---`) a removed band; the marker stays in the text |
| `words="err,nil"` / `words=err,nil` | `HighlightWords []string` | literal matches wrapped in `<mark class="fui-code-block__mark">` |
| `wrap` (`nowrap` / `wrap=false` off) | `Wrap` | soft-wrap long lines instead of horizontal scrolling |

Unknown options are ignored, and so is an invalid `highlight=` or
`words=` spec: a fence carrying options for another tool degrades to a
plain block rather than a broken one. The raw info string stays
addressable on the rendered block's `data-meta` attribute. A fence whose
*language* is `diff` gets no diff marking; only the `diff` option does,
because a highlighter may legitimately know the `diff` grammar.

All of these are also plain `CodeBlockConfig` fields for direct callers:
`ParseLineRanges("1,3-5")` builds the `HighlightLines` value, and the
same features work on the `Lines` (pre-highlighted) path, where diff
classification skips a leading token span and word marks stay inside
text nodes so they never split a caller's markup.

This exact fence renders live on the docs site:

```go title="checkout.go" showLineNumbers {2,4-5} words="sum,total"
func total(items []Item) int {
	sum := 0
	for _, it := range items {
		sum += it.Price
	}
	return sum
}
```

And the diff form, header lines and all:

```diff title="api.go" diff
--- a/api.go
+++ b/api.go
@@ handler @@
 func get(w, r) {
-	user := load(r)
+	user := mustAuth(r)
 	render(w, user)
 }
```

Line bands derive from the theme's status colours
(`--color-primary` for highlight, `--color-success`/`--color-danger` for
added/removed, `--color-warning` for word marks) and are tunable through
`--ui-code-block-highlight-bg`, `--ui-code-block-added-bg`,
`--ui-code-block-removed-bg`, and `--ui-code-block-mark-bg`.

**Common mistake**: writing the line numbers as `highlight={2}`. The
braces are the bare form; `highlight=` takes the bare list. Both
`{2,5-7}` and `highlight=2,5-7` work, `{2,5-7}` after `highlight=` does
not.

---

## Adding a new component: checklist

The framework's drift tests catch most of these; this list is a
helpful pre-flight read for human reviewers.

1. **Implementation**: `framework/ui/<name>.go`.
2. **Theme-token CSS only**: register your own `RegisterStyle`; use
   `var(--color-*, fallback)` etc. No top-level `.ui-*` rules in
   `examples/site/styles.go`, the site chrome is page-only.
3. **Unit tests**: `<name>_test.go` exercising panic paths + emitted
   markup + variant classes.
4. **`/components/<slug>` screen** in `examples/site/`:
   register in `main.go`, add an entry to `componentCatalog` in
   `components.go`. The catalog drives the site's page-level test
   loops (axe, single-`<main>`, …), so an unregistered page is an
   untested page.
5. **Chromedp e2e** in `examples/site/e2e_new_components_test.go`
   or `e2e_new_components_interactions_test.go`, ARIA shape for
   static components, real interaction (click / type / drag) for
   runtime-driven ones.
6. **`core-ui/ARCHITECTURE.md`**: any new `data-cui-*` attribute the
   runtime reads must land in the table here OR in the drift-test
   whitelist (with a justification comment). The
   `TestRuntimeAttrsAreDocumented` gate in
   `core-ui/runtime/attrdoc_test.go` enforces it.
7. **Axe**: `TestAxe_AllPagesAreClean` runs axe-core against
   every catalog page and fails on any violation. The most common authoring
   mistakes it catches: missing tap target floor (44×44),
   role/`aria-allowed-role` mismatches, color-contrast on tinted
   backgrounds, scrollable regions without `tabindex="0"`.
8. **Composition first**: before writing a new runtime module, see if
   `preset.Modal` / `preset.Popover` / `preset.Drawer` +
   `data-cui-open` + `data-cui-deeplink` + signal-binding already
   covers the case. Lightbox and NotificationBell each ship without
   a runtime module by composing existing primitives.
9. **Behaviour registers like style**: when a module is warranted, it
   lives in your package, embedded beside the Go, and registers with
   its markers: `registry.RegisterBehavior("<name>", js,
   registry.Markers("[data-<prefix>-x]"))`. It may also
   `registry.Requires("action")` (or any embedded module or registered
   behaviour): the loader has the requirement registered before the
   module's script runs, on every load path. The host serves it at
   `/__gofastr/runtime/<name>.js`, the kernel scans the markers, and
   the module loads once when one appears. Keep the module contract
   (`window.__gofastr.loadedModules[name] = true` on attach, a scanner
   under `window.__gofastr._moduleScanners[name]`), bind by attribute
   only, and use your own `data-` prefix: a `data-cui-*` marker is
   admitted only when the attribute is already documented. The module
   is a runtime module and the runtime's source lints hold it: `const`
   and `let`, never `var`; no selector or storage key built from a raw
   value (`core-ui/check`, found through your `//go:embed`). See
   `core-ui/ARCHITECTURE.md` "Component behaviour".

---

## Common mistakes

- **Writing a new runtime module when composition already covers it.**
  Check `preset.Modal` / `preset.Popover` / `preset.Drawer` +
  `data-cui-open` + `data-cui-deeplink` + signal binding first.
  NotificationBell ships with zero new JS by composing them; the
  Lightbox composed the same primitives and then earned a module for
  the behaviour composition cannot give (gallery stepping,
  pinch-zoom). New modules are the expensive path (budget, tests,
  docs).
- **Styling a component from `examples/site/styles.go`.** Top-level
  `.ui-*` rules in the site stylesheet are forbidden; the site chrome
  is page-only. A component owns its CSS via `registry.RegisterStyle`
  with theme tokens, so it works in every host, not just the demo
  site.
- **Hardcoding colors/spacing instead of theme tokens.** Use
  `{colors.*}` / `{spacing.*}` / `var(--color-*, fallback)` so themed
  hosts and dark mode don't break your component.
- **Skipping the demo page + e2e pairing.** Every `/components/<slug>`
  page ships with at least one chromedp test and a package unit test
  (suite convention), and `TestAxe_AllPagesAreClean` automatically
  fails the build on any axe-core violation for every catalog page.
  Register the page and you've signed up for all three.
- **Adding a `data-cui-*` attribute without documenting it.** The
  runtime contract lives in `core-ui/ARCHITECTURE.md`; every attribute
  the runtime reads must be in its table (or an explicitly justified
  whitelist) before the change lands.

---

## See also

- [`docs/widgets.md`](widgets.md): widget framework (mount, deeplink, signal lifecycle).
- [`docs/ui-getting-started.md`](ui-getting-started.md): first-time setup for the UI layer.
- [runtime-contract](runtime-contract.md): the SSR/hydration/island/SSE model + `data-cui-*` attribute reference (embedded extract of `core-ui/ARCHITECTURE.md`).
- [`framework/ARCHITECTURE.md`](../../../framework/ARCHITECTURE.md): package layout + extraction rules.
- [`ROADMAP.md` §2](../../../ROADMAP.md): deferred UI components.
