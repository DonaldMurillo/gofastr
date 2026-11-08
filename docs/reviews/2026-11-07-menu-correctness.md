# Headless menu correctness review — November 7, 2026

Revision: `953ccc08256e825bdc17254c463a5695a27092b6`.
One independently reproduced conditional P3 root. No production changes.

## C19 — Tab leaves an all-disabled native-summary menu open

Root: `framework/headless/menu.js:124` handles trigger-side Tab only when
the target has a `[data-hui-menu-trigger]` ancestor. A native top-level
summary has no such wrapper and is not a menuitem, so the handler returns
at line 144 before the menuitem Tab-close branch can run.

Contract: the menu module header at `menu.js:16` and the package's menu
contract at `menu.go:22` say Tab closes the disclosure chain and falls
through naturally. `MenuItem.Disabled` explicitly removes rows from keyboard
navigation while keeping them in the panel (`menu.go:63`). Two unavailable
commands constitute a supported nonempty menu; no enabled-row minimum is
declared or enforced.

The coordinator authored a fresh public Go fixture generator using
`headless.Menu` with Publish and Archive link rows. The matrix varies
Disabled and TriggerElement independently. The initial focus is the trigger;
Enter opens the disclosure. No subsequent manual refocus is performed.
When both rows are disabled, the production toggle handler finds no enabled
first row (`menu.js:112`), so focus naturally remains on the summary.

| Trigger | Rows | Focus after Enter | Tab / Shift+Tab result |
| --- | --- | --- | --- |
| Native summary | Both disabled | Summary | Focus reaches After / Before; menu remains open |
| Caller-owned button | Both disabled | Trigger button | Focus reaches After / Before; menu closes |
| Native summary | Enabled | First link | Focus escapes; menu closes |
| Caller-owned button | Enabled | First link | Focus escapes; menu closes |

All eight cases leave Tab unprevented. The observation script passes 40
checks pinning current behavior and controls, rather than claiming a fixed
regression test passes. Impact is a stranded open command panel after normal
keyboard traversal when commands are unavailable; no data loss or focus trap
is claimed. Forward/backward traversal share one missing-summary branch and
count as one root.

## Reproduction

Retained coordinator sources and output:
`/tmp/gofastr-coordinator-menu/{render/main.go,observe.cjs,fixtures.json,observation.log}`.
The Go source imports the public renderer from this checkout. The Node script
executes the unmodified production menu and disclosure modules, using scope
39's retained DOM/event adapter at `/tmp/scope39-probe/dom39.js`. It adds its
own scenario, observations and assertions, not the worker's weaker manual
summary-refocus scenario.

```sh
go build -p 2 -o /tmp/gofastr-coordinator-menu/render-probe /tmp/gofastr-coordinator-menu/render/main.go
/tmp/gofastr-coordinator-menu/render-probe > /tmp/gofastr-coordinator-menu/fixtures.json
node /tmp/gofastr-coordinator-menu/observe.cjs
node /tmp/scope39-probe/selftest39.js
```

The adapter models Enter's native summary default action, toggle delivery,
focus and Tab traversal. This is not a browser/layout/physical-keyboard or
native-focus-engine measurement. The enabled-row controls are evaluated in
the same model; the defect itself is the production handler's missing close,
not a visual-layout inference. Ordinary non-trap disclosure has no separate
focusout or Tab closer that compensates for it.

Fresh browser-free Go render-contract selection: 18 named menu/disclosure
tests pass (0.353s). Coordinator also reran the reviewer's adapter self-test
and six production-module probes: 49 + 73 + 47 + 44 + 38 + 41 + 2 = 294
passing assertions. Log: `/tmp/gofastr-coordinator-menu/reviewer-rerun.log`.
No browser-launching tests were run for this adjudication.

## Rejected candidate: Space during type-ahead

The worker observed that Space does not click an anchor row while the
800ms type-ahead buffer is active. This is intentional, not a second bug:
`framework/headless/menu_space_e2e_test.go:49` explicitly fails if the Space
inside `s`, `i`, `g`, `n`, Space, `o` activates a link, then checks focus on
Sign out. The test is read as contract evidence, not claimed executed here.
The existing stale-buffer Space-activation control also passes in the Node
probe. The original programmatic-summary-refocus observation alone is not
used to establish a natural reachable failure.
