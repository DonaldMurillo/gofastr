# Headless components

`framework/headless` is the structural half of GoFastr's design system. Components here emit semantic HTML, ARIA roles, label wiring, state attributes, and `data-hui-*` behavior hooks—with zero CSS and zero classes when `Classes` is `nil`. `framework/ui` builds on top of `framework/headless` by supplying its `.fui-*` class maps and theme-driven stylesheets.

Use `framework/ui` if you want the styled design system; import `github.com/DonaldMurillo/gofastr/framework/headless` directly if you want to bring your own CSS while keeping GoFastr's accessibility and runtime behavior guarantees.

## Core concepts

Every headless component is a pure function from props + `headless.Classes` to `render.HTML`:

- **Parts (`headless.Part`):** Named elements inside a component (`PartRoot`, `PartLabel`, `PartHint`, `PartError`, …), listed in `Spec.Anatomy`.
- **Class map (`headless.Classes`):** Flat map from part to CSS class (`nil` renders unstyled). Variants resolve as `<part>--<variant>`. Do not write `.fui-*` classes on custom markup—that prefix is reserved for `framework/ui`.
- **Hooks (`data-hui-*`):** Attributes that demand-loaded runtime behavior modules bind to.
- **`Parts{Slots, Attrs, Binds}`:** Per-part escape hatches. `Slots` replace fillable parts; `Attrs` adds sanitized attributes (refusing `id`, `style`, `data-hui-*`, `data-cui-*`, and duplicate case-folded names); `Binds` connects a part to a client signal.
- **`Strings`:** Typed i18n struct (`nil` uses English defaults; pass `ui.StringsFor(r.Context())` to localize).
- **`Action` & `Island`:** `ButtonProps.Action` carries typed RPC wiring; `Island{Endpoint, Signal}` wires in-page state updates.

```go
classes := headless.Classes{
    headless.PartRoot:  "field",
    headless.PartLabel: "field__label",
    headless.PartHint:  "field__hint",
    headless.PartError: "field__error",
}

headless.Field(headless.FieldProps{
    Label: "Port", For: "port", Hint: "1024 to 65535", Required: true,
}, classes, func(c headless.FieldControl) render.HTML {
    return headless.Input(headless.InputProps{
        Name: "port", ID: c.ID, Required: c.Required,
        DescribedBy: c.DescribedBy, Invalid: c.Invalid,
    }, inputClasses)
})
```

Passing `headless.FieldControl` guarantees that `id`, `aria-describedby`, `required`, and `aria-invalid` stay aligned with the label, hint, and error message.

## Islands & progressive enhancement

GoFastr draws a strict boundary between document state and in-page state:

- **URL-owned list state:** A list page's `Pagination` and `Table` sort headers can render plain query-string links (`?p=2`, `?sort=name`) that work with no JS and are intercepted by the client router. When embedded inside another page, pass `Island: headless.Island{Endpoint: "...", Signal: "..."}` so paging/sorting swaps only that region (with `pushState`).
- **In-page state requires an `Island`:** `ToolbarSearch`, dismissible `Alert`/`Tag`/`Toast`, and `Repeater` refuse to render without an `Island` so non-navigational state never gets modeled as routes.
- **Island forms:** Return `200 OK` with re-rendered form HTML on validation failure (so the runtime swaps the region and focuses the error summary); reserve non-2xx for transport or server errors.
- **No-script fallbacks:** `ConditionalField` renders visible on SSR and hides via `headless-when` (disabling hidden controls). `Repeater` and `StepWizard` include hidden default submit controls so pressing Enter in a field submits cleanly instead of triggering Back or Remove. `Repeater` defaults to `Name+"_add"` / `Name+"_remove"` (overridable via `AddName`, `AddValue`, `RemoveName`).

## Component inventory & behavior modules

`framework/headless` ships primitives across five families:
- **Forms & inputs:** `Form`, `Field`, `FieldRow`, `Fieldset`, `Input`, `Textarea`, `Select`, `Password`, `Color`, `Choice`, `Switch`, `Group`, `InputGroup`, `FileUpload`, `ConditionalField`, `ValidationSummary`, `NumberInput`, `Slider`, `RangeSlider`, `Rating`, `TagInput`, `Repeater`, `StepWizard`, `DatePicker` (+ `DatePickerMonth`), `Combobox` (+ `ComboboxRows`).
- **Layout & display:** `Card`, `Stack`, `Cluster`, `Grid`, `Container`, `Section`, `Divider`, `Spacer`, `PageHeader`, `EmptyState`, `StatCard`, `DetailList`, `Steps`, `Timeline`, `Badge`, `Tag`, `Spinner`, `Skeleton`, `JSONTree`, `Gallery`.
- **Actions & feedback:** `Button`, `OptimisticAction`, `ToggleAction`, `Counter`, `Alert`, `SystemBanner`, `Toast`, `ToastStack`, `ToastTemplate`, `NotificationBell`, `Selection`, `Tooltip`.
- **Navigation & overlays:** `Table`, `Pagination`, `Toolbar` (`ToolbarGroup`, `ToolbarSpacer`, `ToolbarSearch`), `Tabs`, `Disclosure`, `Menu`, `Carousel`, `Rail`, `TableOfContents`, `PaneHost`, `Sidebar` (`SidebarRegion`, `SidebarDrawerTrigger`), `BackToTop`.

Each interactive family ships a demand-loaded runtime module registered via `registry.RegisterBehavior` and armed automatically by the kernel on initial load, island swaps, and SPA navigations (`headless`, `headless-when`, `headless-validity`, `headless-controls`, `headless-collections`, `headless-wizard`, `headless-copy`, `headless-feedback`, `headless-bell`, `headless-selection-copy`, `headless-tooltip`, `headless-navigation`, `headless-rail`, `headless-toc`, `headless-disclosure`, `headless-menu`, `headless-combobox`, `headless-datepicker`, `headless-tabs`, `headless-carousel`, `headless-panehost`, `headless-sidebar`). Native `<dialog>` modals retain Escape and Tab focus trapping even when opened above an intercepted pane, and focus restores automatically after repeater row removal or toast dismissal.

A `Card` with `TitleHref` accepts audio and video in `TitleContent` when the
media has no `controls` attribute. Interactive content belongs outside the title link.

## Spec harness & golden tests

Every exported component registers a `Spec` declaring its anatomy, fillable parts, published hooks, and test cases. The harness checks determinism, zero stray classes/inline styles at nil `Classes`, resolved `aria-*` references, no hardcoded English outside `Strings`, and golden parity:

```bash
go test ./framework/headless/
GOFASTR_UPDATE_GOLDEN=1 go test ./framework/headless/   # after an intentional markup change
```

Live demos comparing bare headless beside themed `framework/ui` across five themes run at `/examples/headless/{theme}/landing` and `/examples/headless/{theme}/dashboard` in `examples/site`.

## Common mistakes

- **Arming `data-hui-*` hooks manually.** The kernel already runs every module's scanner on load, island insertion, and SPA navigation—never add a second `MutationObserver` or `gofastr:navigate` scanner.
- **Querying components by CSS class from JS.** Behavior modules bind exclusively to `data-hui-*` hooks so class maps can rename any class freely.
- **Hardcoding English copy in a headless component or JS module.** Put user-visible strings in `Strings` and pass them through `data-hui-*` attributes so localization works everywhere.
- **Passing `data-cui-*` attributes via `Parts.Attrs` on bare headless components.** `Parts.Attrs` strips `data-cui-*` and `data-hui-*` keys on headless primitives; use `Action`, `Island`, or `Parts.Binds` instead. (By design, the themed `ui.Button` wrapper accepts typed runtime action attributes via `ExtraAttrs`.)
- **Hand-building field/hint IDs instead of using `headless.FieldControl`.** Let `Field` pass IDs and `aria-describedby` down to the control so label associations never drift.
- **Passing empty values to `Attrs` for boolean/presence attributes.** `Attrs` drops empty values; use `Flag` for HTML booleans and `Mark` for presence hooks.
