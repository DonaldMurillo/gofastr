# #710 component semantics: adversarial review, 2026-11-23

The reviewer for the #710 branch (`claude/great-brahmagupta-rdu0e4`,
hard rule 13). One agent that wrote none of the change reviewed
`0a09f476..0e315436`, told to break it rather than confirm it, and
confirmed its findings in Chromium probes against a copy of the tree. The
lead fixed every finding below that is marked fixed, then proved each new
guard by mutation on the merged tree.

## Findings and triage

| # | Severity | Finding | Triage |
|---|---|---|---|
| 1 | blocker | `examples/site` `TestE2E_TabsSwitchPanels` failed: `checkVisibility()` on a `hidden="until-found"` panel is true, since only its contents are skipped | Fixed: the probe reads `hidden` and the panel's first child |
| 2 | major | A fragment URL on first load revealed a panel before `headless-tabs` loaded; `wire()` then hid it again and the strip stayed on tab 0 (vacated panels never restored) | Fixed: `adoptRevealed` selects an inactive panel the browser already revealed. `TestE2E_TabsFragmentOnLoadSelectsItsTab` (three cases) fails with the call removed |
| 3 | major | The server painted `aria-hidden` on inactive panels, so a panel revealed with no script stayed hidden from assistive technology (axe `aria-hidden-focus`) | Fixed: only the module adds `aria-hidden`; `TestTabsServerPaintsNoAriaHidden` |
| 4 | major | A pointer released in the gap under the dragged row moved it to the end of the list | Fixed: a gap lands by position. `TestE2E_SortablePointerDragInAGapLandsByPosition` reproduces `b,c,d,a` with the old branch |
| 5 | major | A board re-rendered mid-drag left the drag live (the removed grip loses capture at the document) and the release moved the detached row into the fresh list, duplicating it | Fixed: a lost capture on a detached grip ends the drag, and a detached row is never moved. `TestE2E_SortablePointerDragEndsWhenTheListIsReRendered` reproduces `a,b,c,a` with the guards removed |
| 6 | minor | The carousel's `aria-disabled` ends did not follow a resize | Fixed: `syncDots` re-syncs the ends; `TestE2E_CarouselEndsFollowTheLayout` |
| 7 | minor | `ui.Card` `HeadingHref` with `Header` silently dropped the link | Fixed: `headless.Card` panics on `TitleHref` with a filled header slot |
| 8 | minor | The nested-link refusal matched `<abbr>`, `<aside>` and other tags starting with a | Fixed: an `<a>` tag regexp; `TestCardTitleLinkAllowsTagsThatStartWithA` |
| 9 | minor | The stretched card covered body controls not in a fixed tag list | Fixed: `[tabindex]`, `role=button`, `role=link`, `contenteditable`, `iframe`, `video` and `audio` are lifted too; the Chromium test clicks three of them |
| 10 | minor | Meridian's Spanish catalog lacked `ui.copy.notCopied` and `ui.codeBlock.notCopied` | Fixed |
| 11 | minor | No auto-scroll while pointer-dragging near the edge of a long list | Not fixed: recorded as a known gap; native drag-and-drop had it, the pointer drag does not yet |
| nit | | An out-of-range `data-active` leaves no tab in the Tab order | Not fixed: predates this branch (the CSS showed no panel either); `settleSignal` handles the seed case |
| nit | | The section menu's own `[hidden]` reset would still hide an until-found element | Fixed for consistency with the uihost reset |
| nit | | A copy target that is a password field copied its value | Fixed: a password field copies nothing; `TestE2E_CopyNeverReadsAPasswordField` |
| nit | | The copy button has no accessible name while it shows the failed state | Not fixed: the copied state already behaves the same way; both belong in one follow-up |
| nit | | A stale comment said the carousel arrows are named by `aria-label` | Fixed |
| nit | | The `[hidden]` reset's until-found exemption is guarded by a string match only | Not fixed: the browser tests exercise until-found without the uihost sheet; the exemption is one selector |

## Areas the reviewer checked and found sound

Nested strips revealed by one fragment, vacated panels restored through the
Trusted Types insertion path, a disabled tab's panel staying shut, the
removal of `aria-sort="none"` against every CSS and test that keyed on it,
the absence of any other styling of the renamed or removed hooks, no new
HTML sink, implicit touch capture on the grip's SVG, a second pointer
during a drag, and the carousel pause button under reduced motion.
