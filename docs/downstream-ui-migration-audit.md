# Downstream UI migration audit

Date: 2026-10-18. Scope: fastr-docs and gofastr-plugins, including generated docs projects, the hand-authored docs fixture, standalone plugin demos, the gallery, recipes, and ejected plugin consumers.

Use framework/ui first, then framework/headless. Keep custom code only for a stated content, plugin-engine, or app-owned publication requirement. This audit distinguishes regressions, API gaps, downstream migrations, and test/environment findings.

## Framework findings and fixes

| ID | Finding | Resolution |
| --- | --- | --- |
| F01 | The broker can scan before following deferred adapters register. | Wait for the parser-deferred sequence; retain immediate startup when injected after DOMContentLoaded. Chromium tests cover both cases. |
| F02 | Sidebar close labels are fixed in English. | SidebarConfig.DrawerCloseLabel, including request-resolved configuration and escaping. |
| F03 | Active navigation opens only the nearest disclosure. | Open every ancestor inside that navigation; do not open an enclosing site menu. |
| F04 | Active children remain hidden in button-dialect groups. | Update the controlling button's aria-expanded and panel hidden state. |
| F05 | Unicode hrefs differ from encoded browser pathnames. | Compare canonical URL pathnames, preserving encoded separators and distinct origins. |
| F06 | Lazy drawers can retain a server-selected link for the previous route. | Recompute framework-owned selection on widget-open, including the first open. |
| F07 | Select options cannot retain language and domain metadata. | ExtraAttrs on styled and headless options, with reserved state and protocol attributes filtered. |
| F08 | Sidebar Count content is dropped on groups. | Render the Count slot on group controllers as well as leaves. |
| F09 | Route-area address wrappers create a box that interferes with sticky/header composition. | AreaSpec.Contents retains the address with display:contents. |
| F10 | The TOC phone sheet close label is fixed in English. | TOCConfig.CloseLabel and PhoneSheetConfig.CloseLabel. |
| F11 | Widget openers do not consistently mirror the widget lifecycle in ARIA. | Update matching openers' aria-controls and aria-expanded on open/close. |
| F12 | Theme preferences do not follow changes in another tab, and returning from an explicit scheme to auto can miss OS changes. | Listen for the framework storage keys and retain the OS listener across mode changes. |
| F13 | Standalone plugin documents cannot collect the CSS for rendered framework controls without a UIHost. | ComponentCSS for external stylesheets; WithComponentCSS for documents that permit inline styles. Neither helper adds scripts or behaviors. |
| F14 | Disclosure persistence can restore a closed state after active-link handling opens the new route. | A current sidebar leaf takes precedence during restore; other groups retain their saved state. |
| F15 | A framed CodeBlock repeats the caller's ID on the wrapper and body. | Keep the caller ID on the root; use ID-code for its body and copy target. |
| F16 | Verify and upgrade scans treat a custom-named static export as authored UI, reporting generated framework CSS as ownerless. | Recognize exported framework assets and skip the output tree; preserve module source directories. |
| F17 | Static area navigation can retain the prior document language; page meta/alternate links do not follow live navigation or cache replay. | Carry inert page metadata with navigation payloads and cached content, refresh marked meta/link nodes, and retain global head nodes. Scripts are not replayed. |
| F18 | Golden comparisons can fail only because Windows checked out CRLF. | Normalize line endings in the headless golden comparisons and runtime symbol manifest comparison. Golden content is unchanged. |
| F19 | An option-only Markdown fence treats its title as a language and drops the filename. | Parse option-only info as metadata; native Markdown renders the title, numbering and highlights without a downstream fence parser. |

F02, F07-F10, and F13 include backward-compatible API additions. These are not all regressions introduced by the dependency upgrade.

## Downstream changes

| Area | Migration |
| --- | --- |
| Dependency and compiler | All four downstream modules use Go 1.27 with the Go 1.27.2 toolchain and a pushed framework fix commit. Generated starter projects use the same dependency. No local framework replace/workspace is required. |
| Docs navigation | MountSidebar, Button, Icon, StatusBadge and Count. Parent pages receive a real Overview child link. Removed the custom drawer, sidebar active-state observer, trigger retargeting, and drawer ARIA/focus repair. |
| Docs chrome | Header and navigation render through route areas. Removed chrome templates and DOM copying. Skip labels use WithSkipLabelFunc. The owned header supplies one mobile navigation control. |
| Locale/version selectors | Select with option language/short-label metadata. Destination selection uses normal framework navigation, including static prefixes. |
| TOC | TableOfContents with PhoneSheet. Removed the custom select, scroll spy, storage and observer. |
| Markdown controls | Direct ui.Markdown rendering, including native fence options, CodeBlock highlights, numbering, diff and copy controls. Removed the duplicate fence parser, copy-button markup rewriting, extra highlight wrappers, and control skin overrides. Heading copy actions use Button. |
| Docs search and recovery | Framework palette close control, Button/ShortcutHint trigger, SearchInput, EmptyState and LinkButton. One global palette retains docs-specific localization and search-index adapters. |
| OpenAPI console | SearchInput, Select, FormField/Control, TextArea and Button. Preserved request metadata and browser request behavior; removed control and badge skins. |
| Owned styles | Regenerated the header/document owned styles against current semantic tokens. The canonical framework templates already contain the token forms. |
| Plugin demo host controls | All six plugins use framework buttons, selects and fields; Monaco settings use Checkbox, Grid, Card and CodeBlock inside a native dialog. Removed custom control skins and CSS tooltips. |
| Plugin gallery | Framework UIHost screens, Sidebar/drawer, ContentRow, Card/Grid, and ThemeToggle. Real gallery routes replace the custom iframe hash controller. Old hash deep links redirect to those routes. The gallery uses the canonical adaptive theme, so the theme toggle changes rendered colors. |
| Recipes | Current layout builders and app-owned header/footer/document packages; current headless field wiring; standard callouts and pager; SQLite through sqlite/stdlib. |
| JSON request envelopes | All six plugin handlers use handler.DecodeStrict, retaining their size and capability gates. Tests reject duplicate, case-mismatched, unknown and trailing keys. |
| Ejection | Standalone demo composition imports only gofastr; every ejected plugin builds in an independent module. |

## Deliberate retained code

- The header, footer, document layout, brand, and published Markdown typography are app-owned publication code.
- File trees and numbered prose steps are semantic content adapters. Workflow progress components are not equivalent content.
- Pagefind/JSON index adaptation, translated route-family matching, publication filtering, and plugin request data remain domain logic. Framework components own their control markup and generic behavior.
- ProseMirror, Monaco, PDF, MapLibre and tour internals implement the plugins' engines and isolation contract. Host controls now use the framework.
- The standalone Monaco settings surface uses native dialog focus containment and dismissal because an imported/ejected plugin must run without claiming a second UIHost. Its content uses framework components. The opener is focused before showModal so Safari can return focus to it.

## Additional findings and verification

- The default locale's drawer uses a pinned short name. A server-side trigger initially derived a locale suffix for it; fixed and covered by a docs regression test.
- A translated publication drawer missed section-select binding. Fixed its widget-name lifecycle matching.
- The PDF browser test treated redaction completion as export persistence completion. The host reports the export URL asynchronously. The test now waits for the export URL or error before fetching it.
- Four docs MCP checks encounter malformed chunked framing on local /mcp. The same path-dependent failure reproduced with a bare Node server while /rpc worked. This is an environment limitation, not a confirmed framework regression. The tests remain enabled.
- A parallel Go run exhausted a Chrome startup timeout for the PDF JPX case. The complete serial plugin Go run passed.
- Earlier drawer/TOC startup and WebKit New post navigation timeouts passed focused repeats. Their original evidence remains recorded; they are not claimed as diagnosed regressions.
- A WebKit publish journey left the page before its native save POST completed. The trace records "Frame load interrupted". Wait for the save and status redirects before navigating away.
- A wheel test could start at the bottom of the page, where no scroll event occurs. Reset the scroll position before checking that scrolling dismisses the drag handle.
- An intermediate browser run overlapped the preceding server teardown, producing a port-bind failure and connection failures. That run is invalid for acceptance and is rerun with isolated server lifetimes.
- The broad contract scan also reports app-owned publication CSS, existing registered SSR styles, isolated plugin-engine assets, and sites whose URL/prefix values are validated before the flagged operation. These are analyzer findings to review, not hundreds of confirmed upgrade regressions. The complete findings are saved as MIGRATION_CONTRACT_FINDINGS.json in each downstream repo. No baseline or blanket suppression was added.
- Validation results and the upstream PR link follow after the final acceptance runs.
