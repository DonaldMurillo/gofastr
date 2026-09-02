# Screens, layouts, and layout chains

A screen is one route's content. A layout is the chrome around it:
header, sidebar, footer. This page covers how layouts nest, what the
server renders for each level, and how the client swaps only the part
of the page that changes on navigation.

## One layout

```go
site := app.NewLayout("site").
    WithHeader(header).
    WithFooter(footer)

application.SetDefaultLayout(site)
application.Register("/", &HomeScreen{}, nil)        // uses the default
application.Register("/tools", &ToolsScreen{}, tool) // explicit layout REPLACES the default
```

A screen with its own layout renders in that layout alone. Only screens
without one fall back to `SetDefaultLayout`, and only screens in a
`ScreenGroup` nest under it.

## Nesting with ScreenGroup

`ScreenGroup` is the nesting API: a URL prefix plus a layout that wraps
every screen under it. Groups nest inside each other, and the whole
group nests under the app default layout.

```go
docs := app.NewScreenGroup("/docs", app.NewLayout("docs").WithSidebar(docsNav))
docs.Screen(app.NewScreen("intro", &IntroScreen{}), nil)
guides := docs.SubGroup("guides", app.NewLayout("guides").WithSidebar(guideNav))
guides.Screen(app.NewScreen("deploy", &DeployScreen{}), nil)
application.Router.ScreenGroup(docs)
```

`/docs/guides/deploy` renders inside three layers: the app default
(outermost, owns the single `<main id="main-content">`), the docs
layer, and the guides layer. Rules that shape the chain:

- A group's screens inherit the group layout; `group.Screen(s, other)`
  replaces it for that one screen. The group boundary is kept, so the
  route is still a sibling of the others for navigation purposes, but
  its layer compares as different, and navigating to it re-renders the
  shell (see keys below).
- `SubGroup(prefix, nil)` inherits the parent's layout. The inherited
  layer renders once. The level still exists as an addressable marker,
  it just adds no duplicate chrome.
- `Standalone()` on a group suppresses the app default layout for
  everything under it. Use it when a feature ships its own full shell
  (the admin back-office does this to avoid a sidebar inside a sidebar).
- Drawer/sheet/dialog screens and layout-less pages have no chain.

## What the server renders

Every layer gets three attributes:

| Attribute | Where | What it is |
|---|---|---|
| `data-fui-layout="<name>"` | layer wrapper div | the layout's name; pairs with the `.layout-<name>` class for CSS |
| `data-fui-layout-key="<key>"` | layer wrapper div | the layer's identity: `l:<name>` for a plain layout, `g:<prefix>:<name>` for a group layer |
| `data-fui-layout-slot="<key>"` | the layer's content cell | the swap target: `<main>` at the root, a `.layout-content` div below it |

The route manifest carries each route's chain as the `layouts` array of
those keys, outermost first. A group layer's key embeds its layout
name, so a per-screen layout override inside a group compares as a
different layer than its siblings and gets its shell re-rendered on
navigation instead of silently keeping whichever was on screen.

Each level also wraps its slots in elements of its own: the header
component renders inside a `<header role="banner">`, the sidebar inside
a `<nav aria-label="Sidebar">`, the footer inside a
`<footer role="contentinfo">`, and the content cell is the page's
single `<main id="main-content">` at the root layer or a
`.layout-content` div below it. Your components supply the inner
content; the landmark elements belong to the layout.

That wrapper matters for CSS. A sticky element only travels inside its
parent's box, so a header component with `position: sticky` sticks for
the header's own height and then scrolls away: its parent is the
layout's `<header>`, whose box ends where the header ends. To pin the
header for the whole page, make the layout's wrapper the sticky
element:

```go
site := app.NewLayout("site").
    WithHeader(header).
    WithFooter(footer).
    WithStickyHeader()
```

`WithStickyHeader()` sticks the layout's own `<header role="banner">`
wrapper to the top of the viewport (the landmark stays, the component
keeps supplying the content) and gives the wrapper a background so
page content does not scroll visibly underneath it. Stacking uses the
theme's `--z-sticky` layer; the background comes from
`--ui-layout-header-bg`, overridable per app like the other
`--ui-layout-*` variables. It composes with `WithContainer()` and
`WithSidebar()` (both modifiers land on the same wrapper).

## What the client swaps

On navigation the runtime compares the target's chain against the
`data-fui-layout-key` spine in the DOM, position by position, and swaps
at the deepest layer the two share:

- Sibling inside a group → only the innermost content cell changes.
  The sidebar keeps its DOM nodes, scroll, and open disclosures.
- Different branch under a shared root → the shared chrome stays; the
  diverging layers re-render.
- Nothing shared → full page fetch, whole shell replaced. Still no
  hard reload.

The partial request names the origin route in `X-Gofastr-From`; the
server renders only the layers the two routes do not share and echoes
the boundary in `X-Gofastr-Swap`. Shared chrome is never re-sent. A
boundary the DOM doesn't have (a deploy changed the chains mid-session)
falls back to a full-page load.

One difference between routes always overrides the chain comparison:
document-lifetime scripts. A script registered with
`uihost.RegisterDocumentScript(src, scope)` ships only on pages the
scope accepts, tagged `data-fui-doc`, and the route manifest carries
each route's set as `docScripts`. The runtime compares the
destination's set against the live document's tags at every soft-nav
entry point; a difference — entering or leaving the scope — is a real
navigation, never a partial swap, because removing a script tag does
not uninstall what the script installed in the document (WebMCP tools
are the case that made this a boundary) and a partial swap never runs
a body script. Same-set routes keep swapping at their deepest shared
layer as usual, and Back/Forward across an edge loads the destination
document fresh.

## Prefetch

A route can declare that the client may fetch its content before the
user clicks:

```go
application.Register("/pricing", &PricingScreen{}, nil, app.Preload(app.PreloadHover))
```

`PreloadHover` fetches on link hover or keyboard focus, `PreloadVisible`
when a link to the route scrolls into view, `PreloadEager` at idle
after page load. The prefetched response lives in a small 30-second
cache the router checks before fetching; `ui.InvalidateScreens`
selectors evict it like the screen cache. Prefetch requests carry
`X-Gofastr-Prefetch: 1`, skip session side effects, and are never used
for routes that would open as overlays.

## Scroll and history

The runtime restores scroll position per history entry: Back and
Forward land where the user left, and a reload keeps its position. A
history move whose only URL change is in-page state (a pane or widget
deep-link parameter) closes or reopens that state without refetching
the screen. Search and pagination parameters are screen identity and
refetch as before.

Modules that write in-page state into the URL must go through
`__gofastr._pushURL(url)`. A raw `history.pushState` leaves the router
unaware of the change and breaks both behaviors above.

## Common mistakes

- **Giving a screen its own layout and expecting the default around
  it.** An explicit layout on `Register` replaces the default; only
  `ScreenGroup` screens nest under it. To add a section sidebar inside
  the site shell, put the screen in a group.
- **Giving the header component `position: sticky` and watching it
  scroll away.** The layout renders the component inside its own
  `<header>` wrapper, and a sticky element only travels inside its
  parent's box, so it sticks for exactly the header's height and then
  leaves. Call `WithStickyHeader()` on the layout; the wrapper is the
  element that sticks.
- **Reusing one layout name for two different layouts.** Layer keys
  embed the name; two distinct `*Layout` values named `"docs"` at the
  same depth compare as the same layer, and navigation between them
  keeps the wrong shell. Names should be unique per shape.
- **Expecting `Standalone()` per screen.** It is a group property: the
  whole group opts out of the default layout, not one route.
- **Prefetching mutating or per-user-expensive routes.** Prefetch is a
  real render on the server (policy, `Load`, DB reads) for a page the
  user may never open. Declare it on cheap, frequently-next routes;
  leave heavy dashboards to the click.
- **Writing `history.pushState` directly in custom client code.** Use
  `__gofastr._pushURL(url)`. A raw push leaves `currentPath` stale, so
  the next Back either refetches an in-page state change or skips a
  real navigation, and scroll restoration loses the entry.
