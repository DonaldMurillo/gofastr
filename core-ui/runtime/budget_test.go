package runtime

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/check"
	"github.com/DonaldMurillo/gofastr/core-ui/runtime/minify"
)

const (
	// 13213, raised 136 bytes from 13077 on 2026-09-07 (per-language
	// shells, #408/#411), under the same documented RULE EXCEPTION as
	// every raise below: the change was measured, not skipped.
	//
	// What bought the bytes, one SOURCE change that cannot be carved
	// into a demand module:
	//   - frag/nav.js's applyDocShell (19 lines): after any SPA swap it
	//     copies data-cui-lang / data-cui-skip-label off the swapped
	//     payload onto documentElement.lang and the skip link. <html
	//     lang> and the body-level link live OUTSIDE the shell the
	//     runtime swaps, so no partial payload can fix them; the sync
	//     must ride the swap itself, which is core's click path (a
	//     demand module loading after a swap leaves the document
	//     announcing the previous page's language, the bug this fixes).
	//     Plus one manifest word: 'lang' joined DOC_MANIFEST.htmlAttrs
	//     (kernel.js) so the write is manifest-governed.
	//
	// The merged bundle measures 13205 at level 6; the line carries 8
	// bytes of clearance, the same margin every raise here uses.
	// Re-measure after a merge, not before.
	// 13305, raised 92 bytes from 13213 on 2026-09-15 (the behaviour
	// registry, docs/spec-behavior-registry.md), same rule: measured,
	// not skipped. One SOURCE change that cannot be a demand module,
	// because it is what tells the kernel which demand modules exist:
	//   - frag/boot.js's _registered (10 lines): reads the registered
	//     behaviours' markers, window.__gofastr_behaviors from
	//     manifest.js or the inline #gofastr-behaviors block, and the
	//     scanner iterates them after its own table. A component's
	//     package now ships its module (registry.RegisterBehavior) and
	//     the kernel finds it without a table edit.
	// The merged bundle measures 13297 at level 6; 8 bytes of clearance.
	//
	// 13372, raised 67 bytes from 13305 on 2026-09-16 (loader
	// dependencies and readiness, docs/spec-behavior-registry.md
	// "Dependencies and readiness"), same rule: measured, not skipped.
	// One SOURCE change that cannot be a demand module, because it is
	// the machinery every demand load goes through:
	//   - frag/boot.js's loadModule: requirements (the r array the
	//     behaviours block carries) load before the module's script, the
	//     cached-promise tail that covers both, and resolution on
	//     registration (loadedModules[name] set) rather than on the
	//     script's load event, rejecting and dropping the cached promise
	//     when a script ran and never registered. The two action modules
	//     leaving the kernel's marker table for the behaviour seam bought
	//     a few bytes back; the net is the number above.
	//
	// 2026-09-20, the interaction bridge learns registered descriptors
	// (docs/spec-behavior-registry.md, sequence step 5's prerequisite):
	// frag/boot.js's _registered parse gains the x array and moves
	// above the bridge, whose install loop now iterates
	// _moduleMarkers.concat(_registered). The line did NOT move: the
	// four form modules retired on stack/07 shrank the real bundle to
	// 13304 before this change, and its +21 (13304 → 13325, +67 raw)
	// spends part of that room. The real bundle measures 13325 at
	// level 6; the line carries 47 bytes of clearance. The
	// anti-vacuity self-test was re-run against the padded fixture,
	// not assumed.
	//
	// 2026-09-20, the lightbox leaves the kernel for a registered
	// behaviour (framework/ui/lightbox.js): boot.js's _moduleMarkers
	// entry — the only one carrying interactions — and its interaction
	// literals left the table, taking the real bundle 13325 → 13242 at
	// level 6 (−83; raw 43948 → 43664). Measured on the rebased tree:
	// the first measurement read 13236, before the layer below gained
	// the guard that wraps the bridge's selector resolution, and the
	// rebase is exactly the merge the rule under this entry warns
	// about. The line did not move:
	// TestCoreBudgetRejectsCliffOverflow's padded fixture still crosses
	// the level-1 window (re-run, not assumed), so the bracket holds
	// [real 13242, fixture crossing] and the line carries 130 bytes of
	// clearance.
	// Re-measure after a merge, not before.
	//
	// 13456, raised 8 more bytes from 13446 in the same spike after the
	// envelope cache-shape fix (cacheScreen stores the fill records and
	// the seed element as separate entry fields): the bundle measures
	// 13448 at level 6; the line carries 8 bytes of clearance.
	//
	// 15563, likewise 27 more bytes from 15536 for the same fix: the real
	// bundle measures 15555 at level 1; 8 bytes of clearance, bracket
	// re-verified by TestCoreBudgetRejectsCliffOverflow.
	//
	// 13575, raised 119 more bytes from 13456 in the same spike for the
	// boot/full-document fills capture (captureLiveFills +
	// captureEnvelopeSnapshot, and the two capture call sites): the
	// bundle measures 13567 at level 6; 8 bytes of clearance.
	//
	// 13717, raised 142 more bytes from 13575 on 2026-09-25
	// (spike/layout-client, P4-B per-region busy marks): loadPage marks
	// every outlet/area of the kept layers plus the predicted swap slot
	// with aria-busy while a navigation is in flight (busyMarks +
	// busyDepth/busyLayouts/busySlot in frag/nav.js), so assistive tech
	// hears WHICH regions are about to change and the theme CSS can dim
	// them. Uncarvable: the marking must happen on the click path before
	// the fetch, the same class as the page-wide flag it extends. The
	// bundle measures 13709 at level 6; 8 bytes of clearance, bracket
	// re-verified by TestCoreBudgetRejectsCliffOverflow.
	//
	// 14000, raised 283 more bytes from 13717 on 2026-09-25
	// (spike/layout-client, P7-A route snapshot): the navigator parses
	// the #gofastr-route island wherever it travels (envelope child,
	// plain-partial head strip, full-document head), keeps the latest
	// snapshot on G._route and publishes it through the route module's
	// hook after every applied navigation and cache replay
	// (routeSnap/publishRoute + the cache entry's route field in
	// frag/nav.js). Uncarvable: publishing must ride the apply
	// transaction itself, after the fills and before gofastr:navigate;
	// the PUBLISHING logic (the atomic install-then-notify over the
	// route.* family) lives in the demand module src/route.js (677
	// bytes gz6), only the transport is core. The bundle measured 13992
	// at level 6. REVERTED by P7-B below (the island transport left the
	// tree); recorded so the number stays comparable in the spike
	// report.
	//
	// 13859, a NET +142 over the P4-B state (13717) after P7-B replaced
	// the P7-A transport with seeded route slices: mergeSeedFromDOM's
	// page-scoped keys now go through the NOTIFYING write path
	// (setSignal, so kept-layer bindings repaint), plus the boot
	// capture's routeSeedFromHead/routeSeedIsland synthesis so a Back
	// replay of the boot entry rewrites route state. No demand module.
	// The bundle measures 13851 at level 6; 8 bytes of clearance,
	// bracket re-verified by TestCoreBudgetRejectsCliffOverflow.
	//
	// 13923, raised 64 more bytes from 13859 on 2026-09-25
	// (spike/layout-client, P8-A fills skip completion): applyFillTargets
	// gains the force arm (no skip under refresh()/navigate(force), or
	// a post-mutation screen keeps the client-touched DOM the author
	// asked to replace), applyEnvelope threads it, and the signals html
	// mode calls the new kernel helper _clearFillHash so a fill the
	// client has written innerHTML under is never skipped as unchanged
	// (poll.js/sse.js call it too, module-side). The bundle measures
	// 13915 at level 6; 8 bytes of clearance, bracket re-verified by
	// TestCoreBudgetRejectsCliffOverflow.
	//
	// 13812, LOWERED 111 from 13923 on 2026-09-25 (spike/layout-client,
	// P8-B no hashes): the skip logic (hash compare, stamp, force arm)
	// and the kernel's _clearFillHash helper left the core; every fill
	// is re-applied unconditionally, and outlet DOM that must persist
	// lives in a nested layout layer the chain keeps. A SOURCE change
	// in the downward direction: the real bundle measures 13804 at
	// level 6; the line keeps its 8 bytes of clearance, bracket
	// re-verified by TestCoreBudgetRejectsCliffOverflow.
	//
	// 13941, raised 129 more bytes from 13812 on 2026-09-25
	// (spike/layout-client, P7-C atomic seed merge): applyPartialSeed
	// splits the page-scoped merge into install-silently-then-notify
	// (a computed over two page-scoped signals never sees a
	// mid-merge mix), skips unchanged values, and the cross-chain
	// full-document swap folds the FETCHED head island's route keys
	// through the same merge (routeSeedFrom + the swap-site call).
	// The bundle measures 13933 at level 6; 8 bytes of clearance,
	// bracket re-verified by TestCoreBudgetRejectsCliffOverflow.
	//
	// 14266, raised 325 more bytes from 13941 on 2026-09-25
	// (spike/layout-motion, P11-A view transitions): frag/nav.js's
	// _commitSwap wraps every navigation's DOM swap in
	// document.startViewTransition({update, types}) — types
	// forward/back/reload derived at the four entry points (click,
	// navigate, refresh, popstate via entry-id direction), the
	// cancelable gofastr:transition event, prefers-reduced-motion
	// gating, skipTransition of a still-running transition, an
	// epoch guard for the frame-later update callback, and the
	// _vtNames CSSOM mirror of data-cui-vt cells (a style attribute
	// is refused by the default CSP; a CSSOM write is not).
	// Uncarvable: the wrapper IS the swap path, core's click path.
	// The bundle measures 14258 at level 6; 8 bytes of clearance,
	// bracket re-verified by TestCoreBudgetRejectsCliffOverflow.
	//
	// 15368, the same spike's conclusion commit (opt-in gate + anchor
	// hardening): _commitSwap runs a transition only when the document
	// carries a [data-cui-vt] cell (Chrome delivers no pointer input to
	// the page while a transition animates — measured; an app that
	// never named a region must not pay that window on every soft
	// navigation), _winOf became a hoisted function declaration (the
	// reload-restore block runs at module-evaluation time; the const
	// arrow sat below it in the TDZ and threw on
	// reload-with-a-stored-position), and fixed/sticky elements are
	// skipped as anchors (they never move with scroll; a fixed header
	// as anchor restored "wherever we are now"). The bundle measures
	// 15360 at level 6; 8 bytes of clearance, bracket re-verified by
	// TestCoreBudgetRejectsCliffOverflow.
	//
	// 15322, raised 467 more bytes from 14855 on 2026-09-25
	// (spike/layout-motion, P12-B anchor restore): beside every
	// recorded pixel offset the capture stores an ANCHOR — the
	// innermost element crossing the window/pane's top edge (first
	// crosser in document order, descended into the deepest child
	// still crossing: a row, a paragraph, never the containers around
	// it) with a stable identity (id, data-key, else the DOM path) and
	// its offset — and the restore scrolls the anchor back to its
	// offset, falling back to the pixels when it is gone
	// (_anchorAt/_anchorId/_anchorEl + the anchor-first apply arms).
	// Uncarvable for the same reason as P12-A: it IS the restore path.
	// The bundle measures 15314 at level 6; 8 bytes of clearance,
	// bracket re-verified by TestCoreBudgetRejectsCliffOverflow.
	//
	// 14855, raised 589 more bytes from 14266 on 2026-09-25
	// (spike/layout-motion, P12-A element scroll restore): on leave
	// (popstate / a push out) the runtime records the window position
	// AND every scrolled element keyed by a DOM path anchored at the
	// nearest layout layer (_domPath/_pathEl, no CSS selectors — keys
	// contain ':' and '/'), stores {w, e} on the history entry, hands
	// finishNav a SNAPSHOT of the values (the store entry is live; the
	// scroll listener writes .w under the new entry id, and under P11
	// the commit runs a frame later — an aliased object handed finishNav
	// a clobbered position), and re-applies both after a Back swap
	// inside the settle pass. Uncarvable: capture must ride the history
	// choke points and the apply rides finishNav, both core.
	// The bundle measures 14847 at level 6; 8 bytes of clearance,
	// bracket re-verified by TestCoreBudgetRejectsCliffOverflow.
	//
	// 14482, raised 541 more bytes from 13941 on 2026-09-25
	// (spike/layout-loading, P9-A loading content): the navigator
	// clones the server-rendered inert <template data-cui-loading>
	// beside an outlet into the outlet after data-cui-after ms of
	// in-flight wait, parks the old nodes in an in-document hidden
	// div, restores them exactly on failure or a superseded
	// navigation, and honors the no-flash Min hold across the apply
	// transaction (findLoadingTpl/_showLoading/_settleLoading/
	// _restoreLoading/_sweepLoading + the busy-marks loop's scheduling
	// in frag/nav.js). Uncarvable: the show must be schedulable at
	// click time, before the fetch — the same class as the busy marks
	// it extends. The bundle measures 14474 at level 6; 8 bytes of
	// clearance, bracket re-verified by
	// TestCoreBudgetRejectsCliffOverflow.
	// 14614, raised 132 more bytes from 14482 on 2026-09-25
	// (spike/layout-loading, P9-ANIM exit wait): _exitLoading sets
	// data-cui-loadstate=exit and waits for the region's own
	// animationend (capped at 400ms, child animations ignored) before
	// the apply replaces the loading content; the apply path awaits
	// every shown region's exit in parallel (frag/nav.js). The default
	// fade itself is theme CSS (uihost frameworkDimCSS), not runtime
	// bytes. The bundle measures 14606 at level 6; 8 bytes of
	// clearance, bracket re-verified by
	// TestCoreBudgetRejectsCliffOverflow.
	// 14726, raised 112 more bytes from 14614 on 2026-09-25
	// (spike/layout-loading, P9-B per-route loading): the scheduling
	// loop falls back to the target route's MANIFEST loading entry
	// (routeEntry(path).loading, the screen's WithLoading declaration
	// rendered into the route table) when the swap slot carries no
	// server template — a synthetic <template> feeds the same
	// park/show/restore machinery, outlets never take the fallback,
	// and the kernel route projection normalizes the flat manifest
	// fields into that shape. The bundle measures 14718 at level 6;
	// 8 bytes of clearance,
	// bracket re-verified by TestCoreBudgetRejectsCliffOverflow.
	// 15398, raised 672 more bytes from 14726 on 2026-09-25
	// (spike/layout-loading, P10-B streamed envelopes): the navigator
	// reads a streamed envelope through the body reader and applies it
	// unit by unit — the sentinel split, the per-unit parse (seed
	// delta + fills), the per-region Min hold and exit wait, the
	// epoch-guarded read loop with reader cancellation, and the
	// stream-end cache capture, and the entry CLAIM that retires a
	// region's pending timer when its unit arrives first
	// (applyStreamEnvelope in frag/nav.js).
	// Uncarvable: the reader must be installed where the body would
	// otherwise be consumed by resp.text(), inside loadPage's apply
	// path. The bundle measures 15390 at level 6; 8 bytes of
	// clearance, bracket re-verified by
	// TestCoreBudgetRejectsCliffOverflow.
	// 17095, the spike/layout-all MERGE of the two layout spikes
	// (spike/layout-motion head, goal line 15474, + spike/layout-loading
	// head, goal line 15398): both behaviours in one bundle — the sum
	// of the two spikes' additions over their shared 13941 base plus
	// the merge glue in frag/nav.js, which routes a streamed
	// envelope's unit 0 (seed + primary + ready fills) through
	// _commitSwap like any other swap — loading content replaced by
	// the real fill rides the same view transition — while late units
	// still apply directly and a unit's Min holds and exit animations
	// complete before the commit (the update callback freezes
	// rendering). The bundle measures 17087 at level 6; 8 bytes of
	// clearance, bracket re-verified by
	// TestCoreBudgetRejectsCliffOverflow.
	// 17419, raised 324 more bytes from 17095 on 2026-09-25
	// (spike/layout-static, P13 outlets under static export): the
	// navigator reads a FULL document as an envelope (readDocEnvelope:
	// shared-prefix boundary against the fetched document's own chain,
	// its slot's content as the primary, every outlet/area outside it
	// as a fill, the route seed folded from its head island) and
	// re-captures the LEAVING page's cache entry keyed at the
	// navigation's swap layer (bug 2: Back to the first page no longer
	// replaces the kept group layer; the live route seed is tracked so
	// the replayed entry seeds the right route's bindings). Uncarvable:
	// both sit inside loadPage — the reader where resp.text() would
	// otherwise lose the document, the re-capture before the first DOM
	// write. The bundle measures 17411 at level 6; 8 bytes of
	// clearance, bracket re-verified by
	// TestCoreBudgetRejectsCliffOverflow.
	// 17584, raised 82 more bytes from 17502 on 2026-09-26
	// (tracker polish brief, F2+T7; the same nav.js change as the
	// level-1 entry below): the pointer-modality focus contract
	// (focusVisible:false on pointer-initiated swaps — the ring is
	// keyboard signal) and the gofastr:fill dispatch per applied
	// streamed region. Both sit inside core's swap/stream apply paths,
	// un-carvable for the same reason as the P13/P14 entries: the
	// focus call and the fill apply ARE the swap. The bundle measures
	// 17576 at level 6; 8 bytes of clearance, bracket re-verified by
	// TestCoreBudgetRejectsCliffOverflow.
	// 17609, raised 25 more bytes from 17584 on 2026-09-26 (tracker
	// polish round 3, S3; the same nav.js change as the level-1 entry
	// below): the streamed commit calls the idle active-link sweep at
	// the swap itself, so a kept nav marks the NEW row the moment the
	// new content lands instead of waiting for stream end. The call
	// sits inside applyStreamEnvelope's apply — the commit — beside the
	// gofastr:fill dispatch, un-carvable for the same reason: it
	// IS the swap. The bundle measures 17601 at level 6; 8 bytes of
	// clearance, bracket re-verified by
	// TestCoreBudgetRejectsCliffOverflow.
	// 17635, raised 26 more bytes from 17609 on 2026-09-26 (tracker
	// polish round 4, U1; the same nav.js change as the level-1 entry
	// below): _vtNames honours data-cui-vt-when — a media condition
	// that moves a view-transition name between the placed cell and
	// the region the build marks (Transition.Narrow, the master-detail
	// collapse on a phone) and CLEARS a non-matching mirror so a
	// resize across the breakpoint never leaves two live names (the
	// browser skips a transition with duplicate names). The mirror IS
	// the snapshot path, un-carvable for the same reason as the P11
	// entry that introduced it. The bundle measures 17627 at level 6;
	// 8 bytes of clearance, bracket re-verified by
	// TestCoreBudgetRejectsCliffOverflow.
	// 18370, raised 735 more bytes from 17635 on 2026-09-26
	// (spike/layout-parts: parallel part requests replace streaming):
	// the parts machinery (launchParts' capped fetch pump, the
	// land/commit buffer, markDeferredRegions, applyPart/applyPartNow,
	// killParts, and the part reset reload) MINUS the deleted stream
	// reader (applyStreamEnvelope and its unit splitter). Uncarvable:
	// launching the parts must ride the click path inside loadPage —
	// a demand module loading after the fetch starts would defeat the
	// point (the parts must be in flight at the same moment as the
	// page request), the same class as nav itself. The bundle measures
	// 18362 at level 6; 8 bytes of clearance, bracket re-verified by
	// TestCoreBudgetRejectsCliffOverflow.
	// 18933, raised 563 more bytes from 18370 on 2026-09-26
	// (spike/layout-resolve: resolution-driven navigation): the
	// keyed-transition pick (the X-Gofastr-Transition read, the
	// data-cui-vt-kinds vocabulary gate, the history.state.vt record,
	// and the Back/Forward edge rule in the popstate) plus the client's
	// param-substitution of manifest layer keys and deferred addresses
	// (routeMatch captures what the pattern matched; the server stays
	// the matcher). Uncarvable: the pick must ride the click path
	// inside loadPage and the edge rule inside popstate — the same
	// class as the parts pump and the direction types before it.
	// The bundle measures 18925 at level 6; 8 bytes of clearance,
	// bracket re-verified by TestCoreBudgetRejectsCliffOverflow.
	// 19018, raised 85 more bytes on 2026-09-26: a part reset waits
	// for the page's outcome and an error page discards its parts
	// (commitParts, resetParts). The opt-in split owes these back.
	// The bundle measured 19010 at level 6.
	//
	// 13662, the opt-in pass-2 line (2026-09-26, view transitions on
	// demand): every piece of the view-transition machinery — the
	// startViewTransition wrapper, the direction types, the cancelable
	// gofastr:transition event, the reduced-motion gate, the
	// click-during-transition re-delivery, the data-cui-vt CSSOM
	// mirror, and the keyed pick — left the core for the transition
	// demand module (src/transition.js), which loads only when the
	// document declares a transition. Before it loads the swap applies
	// directly, exactly as the pre-layout runtime did.
	//
	// The line is pinned to the MEASURED pre-layout core plus itemised
	// pieces only. Pre-layout baseline (origin/stack/20-theme-editor,
	// this toolchain): 12718 at level 6, 14721 at level 1. The first
	// opt-in pass pinned against 13372 — a line that already carried
	// 654 bytes of stale slack — and itemised only 633 of a real 1280;
	// this entry replaces that derivation wholesale.
	//
	// Every number below is an ablation measurement (revert the piece,
	// recompose, measure, restore), taken as one cumulative run from
	// the new core down to the pre-layout source; gzip context makes
	// adjacent pieces interact by ±1 byte, which is why the deltas sum
	// to 936 against a measured endpoint delta of 937 (13655 − 12718).
	//
	// The plain-page carve re-derived the itemisation below as one
	// EXACT cumulative
	// ablation — each step restores the true pre-layout bytes for one
	// named piece, so the steps sum to the measured endpoint delta
	// with no residue (the chain ends with nav/boot/kernel differing
	// from origin/stack/20-theme-editor by COMMENTS ONLY; the composed
	// bundle measures 12718 at level 6 and 14721 at level 1, the
	// pre-layout numbers, byte for byte).
	//
	// The core measures 13538 at level 6 (15624 at level 1); the
	// eight steps below sum to exactly 820 (903 at level 1):
	//   +162 the NS exports the envelope module borrows (_cacheScreen,
	//        _getCachedScreen, _announceRoute, _finishNav,
	//        _applyPartialSeed, _routeEntry, _swapAtSlot, _swapCommit,
	//        _navRetry, _navToast, _bumpEpoch, _takePendingScroll)
	//        plus the live _navEpoch/_entryId accessors. The module's
	//        API surface; every entry is a closure only core holds.
	//        (level 1: 179)
	//   +90  applyPartialSeed split out of mergeSeedFromDOM (the
	//        module's seed-merge entry) and the atomic merge:
	//        page-scoped seeds install silently then notify once, so a
	//        computed over two page signals never sees a mid-merge mix.
	//        A plain page with signals runs this on every navigation.
	//        (level 1: 98)
	//   +95  the seam CALLS: the transition module's take/pop/reload/
	//        shell hooks, the envelope module's capX scroll handoff and
	//        restore fallback (the pre-layout pixel restore stays), and
	//        the routeFold call — each a defensive `?.` call a plain
	//        page executes as a no-op; the modules they reach are
	//        demand-loaded. (level 1: 116)
	//   +148 the commit seam: the epoch-guarded _swapCommit with the
	//        transition delegation, the four closure-wrapped commit
	//        call sites (the pre-layout inline epoch checks remain
	//        inside), and cacheScreen's opaque x record (the module's
	//        snapshot rides the shared entries as one opaque blob).
	//        (level 1: 137)
	//   +187 the loadPage delegation split (gate + envelope-nav seam +
	//        plain body), boot's two marker rows (loading, transition)
	//        + the parts manifest trigger + the sse row's push-target
	//        selector, and the kernel's _navHooks table + the
	//        data-cui-vt-kinds htmlAttrs word. (level 1: 160)
	//   +83  error pages: a non-OK HTML answer applies through the
	//        swap path instead of toasting, and the toast names the
	//        status. Every site's error UX, not a layout feature.
	//        (level 1: 128)
	//   +50  F2 pointer-modality focus: a pointer-initiated swap passes
	//        focusVisible:false (the ring is keyboard signal). Every
	//        page's swap. (level 1: 77)
	//   +5   the data-cui-open anchor guard: a widget trigger's href is
	//        the no-script fallback, not a destination. (level 1: 8)
	//
	// What LEFT the core in the same carve (measured moves, now module
	// bytes): the transition direction and pick-decided threading
	// (the module binds its record to the navigation's epoch at take
	// time), the click re-delivery's pointer-hint channel (the module
	// dispatches a MouseEvent carrying detail 1), cacheScreen's five
	// module fields (one opaque x blob), and the parsed-document
	// parameters on domChainKeys/findSlot (the module carries its own
	// walkers).
	//
	//
	// The outlet-costs-nothing-until-navigation carve: the outlet
	// marker stopped boot-loading the envelope module — the FIRST
	// navigation that needs it starts the load beside its page
	// fetch. Core gains one measured entry:
	//   +199 the opt-in handoff: the marker check and X-Gofastr-Fills
	//        negotiation at the plain navigator's fetch point, the
	//        parallel module load and its whole-document-load
	//        fallback, the takeover/inherit arguments of the handoff,
	//        the plain prefetch path's envelope guard, and the
	//        deferred-manifest boot trigger (refined to layers LIVE in
	//        the document, so a marketing page never pays for deferred
	//        routes it cannot navigate from). (level 1: 250)
	// A further three moves took more layout plumbing out of the
	// plain navigator — the deferred trigger's address walk moved into
	// src/parts.js's evaluation, the stand-down shrank to a bare
	// hand-off (loadModule().then(nav, whole-document fallback); the
	// URL rollback, pending slot, hash carry and re-push folded into
	// the module's nav entry, which reads the SERVED page off the
	// performance navigation entry for its boot capture and records
	// every leave at its own entry), the transition module's pop seam
	// folded into take (entry ids are monotonic: a smaller id landing
	// is a Back move; a _pushURL wrap tells a click from a history
	// move), the pick read folded into cacheScreen's write, the
	// loading module's boot row moved into the envelope module's
	// evaluation (its scheduler is the envelope navigator's), and the
	// kernel's vt-kinds allowlist word went with a module-side
	// setAttribute. The derivation below is again one EXACT cumulative
	// ablation chain (each step restores the true pre-layout bytes,
	// keeps the removal, continues); the chain ends 73 bytes over the
	// pre-layout bundle (declaration relocations: the _navEpoch let's
	// position, the removed _pendingScroll channel, the loadPage
	// wrapper line), and the entries sum to the measured endpoint
	// exactly. Level-1 deltas in parentheses.
	//
	//   +60  the supersede rule (NS._navLive + the shared NS._navEpoch
	//        slot), the transition take seam and the pick read inside
	//        cacheScreen. (l1: 99)
	//   +30  the commit seam: _swapCommit's delegation and the four
	//        closure-wrapped commit sites — the transition module must
	//        reach the PLAIN navigator's commits (TestDemandModuleTriggers/vt,
	//        TestTransitionTypes). (l1: 15)
	//   +113 the opt-in hand-off and the loadPage delegation split:
	//        marker check, loadModule().then(nav, fallback), and the
	//        public NS._pushURL alias every runtime push goes through
	//        (the transition module's wrap reads it to tell a click
	//        from a history move; TestTransitionPickedByDestination's
	//        click legs). (l1: 110)
	//   +79  error pages through the swap path. (l1: 113)
	//   +55  F2 pointer-modality focus. (l1: 83)
	//   +33  the data-cui-open anchor guard (+chain residue). (l1: 23)
	//   +65  the atomic seed merge. (l1: 91)
	//   +71  the kernel's _navHooks table + epoch slot, boot's
	//        transition row (a plain page DECLARING data-cui-vt must
	//        load the module at boot; nothing else ever will) and the
	//        deferred-manifest one-liner (a page whose site defers
	//        needs parts at boot) + the sse row's push-target
	//        selector. (l1: 57)
	//   +73  the chain's residue: declaration relocations. (l1: 78)
	//   +79  the scroll settle's stylesheet wait: _settleScroll awaits the
	//        kernel's _stylesReady conjunction so hash/history scroll
	//        writes measure after cold component CSS lands, and each
	//        loadComponentCSS link promise is bounded by a 3s timer so a
	//        stalled fetch cannot poison the page-lifetime conjunction
	//        and freeze scrolling for the rest of the session (the
	//        conjunction is also initialized resolved). Measured as one
	//        piece: reverting the wait AND its bound together. (l1: 69)
	//   +67  the settle's cancel test is USER INTENT, not pixel identity:
	//        a wheel/touchmove/keydown/pointerdown counter replaces the
	//        pre-await scrollX/scrollY capture, because cold CSS landing
	//        after the swap can grow content above the viewport and the
	//        browser's scroll anchoring then moves scrollY with no user
	//        input (observed 1200 → 2682 on a 1482px growth), which the
	//        pixel test read as a user scroll and dropped the first
	//        write entirely. (l1: 49)
	//   +33  the first-navigation pointer modality rides the delegated
	//        opts: loadPage hands the module navigator core's
	//        _navPointer record with the load, because the module
	//        demand-loads beside the FIRST fetch — after the click —
	//        so the click listener it used to register missed that
	//        click and the swap tail focused with focusVisible:true
	//        (the ring painted on the first mouse navigation; the
	//        listener itself left the module, net -40 module bytes).
	//        Un-carvable: it is the click path. (l1: 34)
	//   +26  owned styles load by marker: scanAndLoadCSS reads
	//        data-cui-scope beside data-cui-comp, and nav.js's
	//        swapShell scans the new shell's parent, because the shell
	//        root itself carries its layout's data-cui-scope and the
	//        scan reads descendants only. Un-carvable: every component
	//        sheet load goes through the scan, and a styled layout or
	//        screen reached by client navigation renders unstyled
	//        without it. No ordering code: an owned rule is scoped, so
	//        it beats an equal-specificity kit rule by scope proximity
	//        whatever order the sheets load in (pinned in Chromium by
	//        TestOwnedRuleWinsTieInEitherOrder). Measured 13424 → 13450
	//        at level 6. (l1: 15)
	//
	// HALT NOTE: the bundle measures 13391 at level 6 — 291 over the
	// 13100 target (the Back/Forward leave capture every page runs is
	// back, pre-layout parity, +4 over the 13297 the carve measured;
	// the scroll settle's stylesheet wait added the +79 entry above).
	// Every byte between the pre-layout number and this
	// measurement is itemised above and each entry is pinned by a
	// test; the pieces still over are the accepted contracts (the
	// supersede rule, the sanctioned commit seam, the hand-off
	// spelling) plus the fixes and boot rows named with their tests.
	// The line is the measurement plus clearance (moved DOWN from
	// 13527, never up).
	coreGoalGZ = 12718 + 60 + 30 + 113 + 79 + 55 + 33 + 65 + 71 + 73 + 79 + 67 + 33 + 26 + 8
	// The window is still the constraint that matters, and the artifact still
	// fits inside it in every deployment: nothing in the framework compresses
	// runtime.js, so the bytes a browser receives are compressed by nginx, a
	// CDN, or whatever proxy fronts the app, all of which use zlib. This line
	// measures Go's own BestSpeed encoder as a worst-case proxy for those.
	//
	// Go 1.27 made that proxy pessimistic. Its compress/flate BestSpeed
	// encoder emits ~2% more for identical input: the same 41812-byte bundle
	// went from 14306 bytes under Go 1.26.6 to 14602 under Go 1.27.0, with the
	// runtime source unchanged by a single byte. The old line was clearing by
	// 30 bytes, so a compressor revision was always going to decide it.
	//
	// Same call as the 12 KB to 12.5 KB move recorded on
	// TestTypicalPagePayloadBudget: the code did not grow, the ruler moved.
	// That doc comment has the policy for the other case, where the code DOES
	// grow; a ruler change is not a licence to skip it. Before re-baselining
	// again, dump the bundle on both toolchains and confirm the byte count is
	// identical, the way this was confirmed.
	//
	// The value is bracketed on BOTH sides and cannot simply be raised.
	// TestCoreBudgetRejectsCliffOverflow builds a bundle sitting exactly on the
	// level-6 goal and requires it to cross this line; raise the line past that
	// fixture and the guard stops guarding instead of making room. The ceiling
	// is between 14800 (still guards) and 14825 (vacuous), measured by moving
	// the constant and watching that test, not by arithmetic.
	//
	// The band is tighter than it looks. The bundle measured 14602 when this
	// line was first re-baselined against a 41812-byte artifact; v0.69.0's
	// click-path work took it to 41968 raw and 14660 compressed, and the
	// native-submit confirm gate (#279: data-cui-confirm honored on plain POST
	// forms, which until then submitted unconfirmed) took it to 42215 raw and
	// 14745 compressed. The gate cannot be carved into a demand module: a
	// native submit navigates away before a module could load, the same class
	// of fatal-for-the-path as the click bridge. So the line moved to 14784.
	//
	// Three changes then landed against three different mains and each
	// measured this independently: the prefetch-failure retry
	// (mark-on-success), forwarding the widget trigger's ctx through the
	// core boot, and loadModule's module-name shape check -- the last of
	// which stops a "../../../evil" value in data-cui-prefetch from
	// normalizing out of the runtime serve route onto an arbitrary
	// same-origin script. None could see the others, so every one of them
	// reported more headroom than exists. The value below was re-measured
	// on the merged bundle, which is the only measurement that means
	// anything. Re-measure after a merge, not before.
	// 15071, lowered 5 bytes from 15076 on 2026-09-04, a SOURCE change in
	// the downward direction: the scroll-bottom selector guard added to
	// frag/signals.js (the data-cui-scroll-bottom-on-update lookup must
	// degrade, not throw, out of setSignal's fanout) carries a repeated
	// attribute literal the level-1 encoder dictionaries better than the
	// sortablelist filler the cliff fixture pads with, so the real core
	// got SMALLER at level 1 (15073 → 15067) while a bundle padded onto
	// the level-6 goal no longer crossed the old window (fixture 15072 ≤
	// 15076 — the anti-vacuity bracket had gone vacuous). The line moved
	// to the largest value below the fixture's crossing, restoring the
	// bracket [real 15067, fixture 15072]; verified by running
	// TestCoreBudgetRejectsCliffOverflow, not by arithmetic. The real
	// bundle measures 15067 at level 1; the line carries 4 bytes of
	// clearance.
	//
	// 15086, raised 15 bytes from 15071 on 2026-09-07 (round-5 security
	// fixes), a SOURCE change in the upward direction under the same
	// concession policy as the goal line above: the widgets-boot
	// deeplink try/catch (boot-class, un-carvable — the eager open
	// delegator must exist before the widget catalog resolves) and
	// boot's Map re-key of _modulePromises cost ~15 bytes at level 1
	// (real 15067 → 15082). The line keeps its 4 bytes of clearance.
	// The anti-vacuity bracket was re-verified by running
	// TestCoreBudgetRejectsCliffOverflow against the padded fixture,
	// not by arithmetic.
	//
	// 15246, raised 160 bytes from 15086 on 2026-09-07 (per-language
	// shells, #408/#411) under the same concession policy: frag/nav.js's
	// applyDocShell (document-language + skip-label sync after every SPA
	// swap; core's swap path, un-carvable for the same reason as the
	// click bridge: a demand module lands after the swap and leaves the
	// document in the previous language, the bug it fixes) took the real
	// bundle 15082 → 15242. The line keeps its 4 bytes of clearance;
	// the bracket was re-verified by running
	// TestCoreBudgetRejectsCliffOverflow, not by arithmetic.
	//
	// 15349, raised 103 bytes from 15246 on 2026-09-15 (the behaviour
	// registry, the same _registered block as the level-6 raise above):
	// the real bundle 15238 → 15341. The line keeps 8 bytes of
	// clearance; the bracket was re-verified by running
	// TestCoreBudgetRejectsCliffOverflow, not by arithmetic.
	// 15446, raised 97 bytes from 15349 on 2026-09-16 (loader
	// dependencies and readiness, the same loadModule change as the
	// level-6 raise above): the real bundle 15341 → 15438. The line
	// keeps 8 bytes of clearance; the bracket was re-verified by
	// running TestCoreBudgetRejectsCliffOverflow against the padded
	// fixture, not by arithmetic.
	//
	// 15443, lowered 3 bytes from 15446 on 2026-09-17, a SOURCE change
	// in the downward direction: the passwordinput module's retirement
	// (its marker-table row left frag/boot.js with the component's
	// move to the headless module) took the real bundle 15438 → 15422
	// at level 1, and a bundle padded onto the level-6 goal no longer
	// crossed the old window (fixture 15444 ≤ 15446 — the anti-vacuity
	// bracket had gone vacuous). The line moved to the largest value
	// below the fixture's crossing, restoring the bracket [real 15422,
	// fixture 15444]; verified by running
	// TestCoreBudgetRejectsCliffOverflow, not by arithmetic.
	//
	// 2026-09-20, the interaction bridge learns registered descriptors
	// (the same boot.js change as the level-6 entry above): +25 at
	// level 1 (15361 → 15386; the form-module retirements on stack/07
	// had already taken the real bundle to 15361, 82 under the line).
	// The line did not move; the real bundle measures 15386 at level 1
	// and the line carries 57 bytes of clearance. The anti-vacuity
	// bracket was re-verified by running
	// TestCoreBudgetRejectsCliffOverflow against the padded fixture,
	// not by arithmetic.
	//
	// 2026-09-20, the lightbox leaves the kernel for a registered
	// behaviour (the same boot.js removal as the level-6 entry above):
	// the real bundle 15386 → 15288 at level 1 (−98). Measured on the
	// rebased tree, for the reason the level-6 entry gives. The line
	// did not move — the padded fixture still crosses it (re-run, not
	// assumed), so the bracket holds [real 15288, fixture crossing]
	// and the line carries 155 bytes of clearance.
	// Re-measure after a merge, not before.
	//
	// 15536, raised 41 bytes from 15495 on 2026-09-25 (spike/layout-proto,
	// the fills envelope; the same nav.js change as the level-6 raise
	// above): the real bundle 15422 → 15528 at level 1. The line keeps 8
	// bytes of clearance; the anti-vacuity bracket was re-verified by
	// running TestCoreBudgetRejectsCliffOverflow against the padded
	// fixture, not by arithmetic.
	//
	// 15696, likewise 160 more bytes from 15536 in the same spike for
	// the boot/full-document fills capture (the same change as its
	// level-6 entry): the real bundle measures 15688 at level 1; 8 bytes
	// of clearance, bracket re-verified by
	// TestCoreBudgetRejectsCliffOverflow.
	//
	// 15880, likewise 184 more bytes from 15696 on 2026-09-25
	// (spike/layout-client, P4-B per-region busy marks; the same nav.js
	// change as its level-6 entry): the real bundle measures 15872 at
	// level 1; 8 bytes of clearance, bracket re-verified by
	// TestCoreBudgetRejectsCliffOverflow.
	// 16216, likewise 336 more bytes from 15880 on 2026-09-25
	// (spike/layout-client, P7-A route snapshot; the same nav.js change
	// as its level-6 entry): the real bundle measured 16208 at level 1.
	// REVERTED by P7-B (see the level-6 entry).
	//
	// 16031, a NET +151 over the P4-B state (15880) after P7-B (the
	// same change as its level-6 entry): the real bundle measures
	// 16023 at level 1.
	//
	// 16134, likewise 103 more bytes from 16031 on 2026-09-25
	// (spike/layout-client, P8-A; the same change as its level-6
	// entry): the real bundle measures 16126 at level 1.
	//
	// 15989, likewise LOWERED 137 from 16134 on 2026-09-25
	// (spike/layout-client, P8-B; the same change as its level-6
	// entry): the real bundle measures 15981 at level 1.
	//
	// 16133, likewise 144 more bytes from 15989 on 2026-09-25
	// (spike/layout-client, P7-C; the same change as its level-6
	// entry): the real bundle measures 16125 at level 1; 8 bytes of
	// clearance, bracket re-verified by
	// TestCoreBudgetRejectsCliffOverflow.
	//
	// 16528, likewise 395 more bytes from 16133 on 2026-09-25
	// (spike/layout-motion, P11-A; the same _commitSwap change as its
	// level-6 entry): the real bundle measures 16520 at level 1; 8
	// bytes of clearance, bracket re-verified by
	// TestCoreBudgetRejectsCliffOverflow.
	//
	// 17813, the same conclusion commit as the 15368 level-6 entry:
	// the real bundle measures 17805 at level 1; 8 bytes of clearance,
	// bracket re-verified by TestCoreBudgetRejectsCliffOverflow.
	//
	// 17735, likewise 497 more bytes from 17238 on 2026-09-25
	// (spike/layout-motion, P12-B; the same anchor change as its
	// level-6 entry): the real bundle measures 17727 at level 1; 8
	// bytes of clearance, bracket re-verified by
	// TestCoreBudgetRejectsCliffOverflow.
	//
	// 17238, likewise 710 more bytes from 16528 on 2026-09-25
	// (spike/layout-motion, P12-A; the same element-scroll change as
	// its level-6 entry): the real bundle measures 17230 at level 1; 8
	// bytes of clearance, bracket re-verified by
	// TestCoreBudgetRejectsCliffOverflow.
	// 16793, likewise 668 more bytes from 16133 on 2026-09-25
	// (spike/layout-loading, P9-A; the same change as its level-6
	// entry): the real bundle measures 16785 at level 1; 8 bytes of
	// clearance, bracket re-verified by
	// TestCoreBudgetRejectsCliffOverflow.
	// 16965, likewise 172 more bytes from 16793 on 2026-09-25
	// (spike/layout-loading, P9-ANIM; the same change as its level-6
	// entry): the real bundle measures 16957 at level 1; 8 bytes of
	// clearance, bracket re-verified by
	// TestCoreBudgetRejectsCliffOverflow.
	// 17099, likewise 134 more bytes from 16965 on 2026-09-25 (P9-B;
	// the same changes as its level-6 entry): the real bundle measures
	// 17099 at level 1; 8 bytes of clearance, bracket re-verified by
	// (spike/layout-loading, P9-B; the same change as its level-6
	// TestCoreBudgetRejectsCliffOverflow.
	// (superseded intermediate line 17059: the kernel projection fix
	// landed after it; recorded for the report.)
	// clearance, bracket re-verified by
	// TestCoreBudgetRejectsCliffOverflow.
	// 17905, likewise 798 more bytes from 17107 on 2026-09-25
	// (spike/layout-loading, P10-B; the same change as its level-6
	// entry): the real bundle measures 17897 at level 1; 8 bytes of
	// clearance, bracket re-verified by
	// TestCoreBudgetRejectsCliffOverflow.
	// 19844, the spike/layout-all merge of the two spikes (the same
	// change as its level-6 entry): the real bundle measures 19836 at
	// level 1; 8 bytes of clearance, bracket re-verified by
	// TestCoreBudgetRejectsCliffOverflow.
	// 20208, likewise 364 more bytes from 19844 on 2026-09-25
	// (spike/layout-static, P13; the same change as its level-6
	// entry, plus the live route-seed track that keeps a re-captured
	// entry's replay from seeding the ORIGIN route's bindings): the
	// real bundle measures 20200 at level 1; 8 bytes of clearance,
	// bracket re-verified by
	// TestCoreBudgetRejectsCliffOverflow.
	// 20319, likewise 111 more bytes from 20208 on 2026-09-25
	// (spike/layout-static, P14; the same nav.js change as its level-6
	// entry): the real bundle measures 20311 at level 1; 8 bytes of
	// clearance, bracket re-verified by
	// TestCoreBudgetRejectsCliffOverflow.
	// 20431, likewise 112 more bytes from 20319 on 2026-09-26 (tracker
	// polish brief, F2+T7; the same nav.js change as its level-6
	// entry): the real bundle measures 20423 at level 1; 8 bytes of
	// clearance, bracket re-verified by
	// TestCoreBudgetRejectsCliffOverflow.
	// 20472, likewise 41 more bytes from 20431 on 2026-09-26 (tracker
	// polish round 3, S3; the same nav.js change as its level-6
	// entry): the real bundle measures 20464 at level 1; 8 bytes of
	// 21334, likewise 820 more bytes from 20514 on 2026-09-26
	// (spike/layout-parts; the same nav.js change as its level-6
	// entry, plus the commit-buffer flush and the same-URL
	// cache:'no-store' that keeps Chrome's HTTP-cache write lock from
	// serializing the parts behind the page): the real bundle measures
	// 21326 at level 1; 8 bytes of clearance, bracket re-verified by
	// TestCoreBudgetRejectsCliffOverflow.
	// 21991, likewise 657 more bytes from 21334 on 2026-09-26
	// (spike/layout-resolve; the same nav.js change as its level-6
	// entry): the real bundle measures 21983 at level 1; 8 bytes of
	// clearance, bracket re-verified by
	// TestCoreBudgetRejectsCliffOverflow.
	// 22120, +129 on 2026-09-26: the part reset rule (see the level-6
	// entry); the bundle measured 22112 at level 1.
	//
	// 15813, LOWERED 382 from 16195 on 2026-09-26 (the opt-in pass 2,
	// view transitions on demand; the same change as its level-6
	// entry): the transition machinery left the core for the
	// transition demand module. Derived from the measured pre-layout
	// level-1 baseline (origin/stack/20-theme-editor: 14721) plus the
	// same itemised pieces measured at level 1 in the same cumulative
	// ablation run (+1087: fixes +264, transition seams +217, commit
	// seam +94, scroll seams +41, routeFold +11, cache fields +123,
	// d-params +1, loadPage split +37, NS exports +211, kernel +5,
	// boot +83; gzip context interacts by ±4 across the run — the
	// pieces sum to 1087 against a measured endpoint delta of 1083).
	// The real bundle measures 15804 at level 1; the line was then
	// re-derived by the documented procedure (move the constant, watch
	// TestCoreBudgetRejectsCliffOverflow's padded fixture cross it),
	// not by arithmetic: the largest value below the fixture's
	// crossing, keeping the bracket [real 15804, fixture crossing].
	//
	// 15885, +72 on 2026-09-26 (the click re-delivery's pointer hint,
	// loose ends; the same nav.js change as its level-6 entry): the
	// bundle measures 15877 at level 1; 8 bytes of clearance.
	//
	// 15632, LOWERED 253 from 15885 (the same exact-chain derivation
	// as the level-6 entry above): the pieces that left the core took
	// the real bundle 15877 → 15624 at level 1.
	//
	// 15397 (the same exact-chain derivation as its level-6 entry):
	// the same three moves took the real bundle 15577 → 15390 at
	// level 1; the restored Back/Forward leave capture adds 1 (15391
	// measured) and pushed the padded fixture's crossing onto the old
	// 15398 line, so the window sits at the largest value below the
	// crossing — 6 bytes of clearance; the bracket re-verified by
	// TestCoreBudgetRejectsCliffOverflow.
	//
	// 2026-09-29, the scroll settle's stylesheet wait (the +79 level-6
	// entry above, whose level-1 share is 69) took the real bundle
	// 15395 → 15422 at level 1, past this window; the same day's cancel
	// test rework (user intent, +49 at level 1) took it to 15471.
	// 15472, the smallest step that fits: both pieces are un-carvable
	// core — without the liveness bound one stalled stylesheet fetch
	// freezes hash and history scrolling for the rest of the session,
	// and without the intent counter scroll anchoring (late CSS growing
	// content above the viewport, no user input) cancels the very write
	// the wait exists to place. The bracket re-derived by the
	// documented procedure, not arithmetic: real 15471, padded fixture
	// crossing 15544, window between; re-verified by running
	// TestCoreBudgetRejectsCliffOverflow and TestRuntimeModuleSizeBudgets.
	//
	// 2026-09-29, later: the first-navigation pointer modality (the
	// +33 level-6 entry above; level-1 share 34) took the real bundle
	// to 15505. 15506, again the smallest step that fits — the piece
	// is the click path itself (the delegation hands the module
	// navigator the modality core recorded for the click, because the
	// module demand-loads beside the first fetch, after the click), so
	// no demand-module carve exists. Bracket by the same procedure:
	// real 15505, padded fixture crossing 15577 at the 13484 goal.
	//
	// 2026-09-30, owned styles load by marker (the +26 level-6 entry;
	// level-1 share 15) took the real bundle to 15520. 15521, the
	// smallest step that fits. The piece is the component-sheet scan
	// itself, which every sheet load goes through, so no carve exists;
	// nine spellings of the same scan were measured and none fit the
	// old line (level-1 gzip moves by single digits on spelling alone).
	//
	// 2026-10-04, the deploy-skew header (X-Gofastr-Markup on the click's
	// partial fetch plus the kernel's _markup generation that demand
	// modules read) took the real bundle 15520 -> 15532. 15533, the
	// smallest step that fits. The header is the click path itself, so
	// no demand-module carve exists: an old tab's first click after a
	// deploy is exactly the request the server has to recognise. Six
	// placements and spellings were measured (15532 to 15537); the
	// header alone, with no kernel property, measures 15527. Bracket
	// re-verified by TestCoreBudgetRejectsCliffOverflow.
	//
	// 2026-10-07, the themed confirm (data-cui-confirm opens the kit's
	// dialog from the confirm demand module instead of window.confirm)
	// took the real bundle 15533 -> 15534. 15534, the smallest step that
	// fits. What stays in core is the submit gate itself: the submit
	// event must be cancelled synchronously, before the module can load,
	// so no carve exists; the ask, the resubmit and the fallback all
	// moved to the module. Six spellings were measured (15534 to 15555).
	//
	// 2026-10-07, later: a navigation that changes only the query on the
	// same path (a list's sort, page, filter) keeps the scroll instead of
	// jumping to the top. It took the real bundle 15534 -> 15563. 15563,
	// the smallest step that fits. The decision is finishNav's own
	// scroll write, the tail every plain-page navigation runs, so no
	// carve exists. Three spellings were measured (15563 to 15567).
	coreCongestionWindowGZ = 14*1024 + 1227
)

// TestCoreBudgetAtPreLayout pins the opt-in budget derivation
// (docs/DESIGN-layout-outlets.md "### Opt-in": "The core runtime
// budget (coreGoalGZ) is pinned at its pre-layout number as a hard
// gate"): the core line is the measured pre-layout number plus
// exactly the itemised bytes below. Raising the line without adding
// a measured, named entry here fails this test — the itemisation IS
// the review.
//
// The baseline (12718 at level 6, 14721 at level 1) was measured on
// origin/stack/20-theme-editor with this repo's own gzipSize harness
// (RuntimeJS(), level 6), in a throwaway worktree. Every entry below
// is one step of a single EXACT cumulative ablation on THIS tree:
// revert the piece to its true pre-layout bytes, recompose, measure,
// keep the revert, continue with the next piece. The chain ends with
// nav/boot/kernel differing from the pre-layout source by comments
// only — the composed bundle measures 12718/14721 byte for byte — so
// the entries sum to the measured endpoint delta EXACTLY (level 6:
// 13538 − 12718 = 820; level 1 in parentheses, 15624 − 14721 = 903).
// The rule each entry is reviewed against: code a plain page never
// runs is not core — the pieces that failed it were moved into the
// demand modules in the same carve (see the derivation comment on
// coreGoalGZ).
func TestCoreBudgetAtPreLayout(t *testing.T) {
	const preLayoutCoreGZ = 12718
	const clearanceGZ = 8
	itemised := map[string]int{
		"supersede rule + shared epoch + transition take/pick seams":            60,  // (l1: 99)
		"commit seam (_swapCommit delegation + wrapped sites)":                  30,  // (l1: 15)
		"opt-in hand-off + delegation split + public push alias":                113, // (l1: 110)
		"p14 error pages apply through the swap path":                           79,  // (l1: 113)
		"f2 pointer-modality focus ring":                                        55,  // (l1: 83)
		"data-cui-open anchor guard (+chain residue)":                           33,  // (l1: 23)
		"p7-c atomic seed merge":                                                65,  // (l1: 91)
		"kernel navHooks/epoch + transition row + deferred line + sse selector": 71,  // (l1: 57)
		"ablation residue (declaration relocations)":                            73,  // (l1: 78)
		"scroll settle stylesheet wait + its stall bound":                       79,  // (l1: 69)
		"scroll settle cancels on user intent, not anchoring drift":             67,  // (l1: 49)
		"first-nav pointer modality rides the delegated opts":                   33,  // (l1: 34)
		"owned styles load by marker (data-cui-scope scan)":                     26,  // (l1: 15)
	}
	sum := preLayoutCoreGZ + clearanceGZ
	for _, v := range itemised {
		sum += v
	}
	if coreGoalGZ != sum {
		t.Fatalf("coreGoalGZ = %d, but the measured pre-layout number (%d) plus the itemised pieces (%v) plus clearance (%d) = %d — the line moved without a measured, named entry", coreGoalGZ, preLayoutCoreGZ, itemised, clearanceGZ, sum)
	}
	// Anti-vacuity: the real bundle must actually sit under the line.
	core, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	if got := gzipSize(t, core); got > coreGoalGZ {
		t.Fatalf("core runtime.js gzip = %d > line %d", got, coreGoalGZ)
	}
}

// TestPlainSiteLineMatchesCoreBudget keeps framework/uihost's served-bytes
// copy of the line equal to coreGoalGZ. A raised line once left that copy
// behind, and the plain-site test failed on a correctly itemised bundle.
func TestPlainSiteLineMatchesCoreBudget(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "framework", "uihost", "plain_site_e2e_test.go"))
	if err != nil {
		t.Fatal(err)
	}
	want := []byte(fmt.Sprintf("const coreLineGZ = %d\n", coreGoalGZ))
	if !bytes.Contains(src, want) {
		t.Fatalf("framework/uihost/plain_site_e2e_test.go must declare %q to match coreGoalGZ", bytes.TrimSpace(want))
	}
}

func coreBudgetViolation(t *testing.T, src string, budget int) (level, got, limit int) {
	t.Helper()
	if got := gzipSize(t, src); got > budget {
		return gzip.DefaultCompression, got, budget
	}
	if got := gzipSizeAt(t, src, gzip.BestSpeed); got > coreCongestionWindowGZ {
		return gzip.BestSpeed, got, coreCongestionWindowGZ
	}
	return 0, 0, 0
}
func TestCoreBudgetRejectsCliffOverflow(t *testing.T) {
	core, err := RuntimeJS()
	if err != nil {
		t.Fatalf("RuntimeJS: %v", err)
	}
	filler, ok := Module("rtc")
	if !ok {
		t.Fatal("rtc module not embedded")
	}

	grown := core
	// Fill the level-6 headroom as finely as it takes. A fixed 64-byte
	// step loses the last chunk once the real core sits close enough
	// to the goal (the sidebar scanner widening in #298 consumed
	// exactly that margin): the fixture then stops short of the window
	// and this self-test fails for no budget reason. Halving the step
	// keeps the fixture sitting ON the goal; if even 4-byte chunks
	// cannot push it past the window, the core has genuinely outgrown
	// the line and the bracket comment on coreCongestionWindowGZ is
	// the thing to revisit.
	const step = 64
	chunk := step
	for i := 0; i+chunk <= len(filler); {
		candidate := grown + filler[i:i+chunk]
		if gzipSize(t, candidate) > coreGoalGZ {
			if chunk <= 1 {
				break
			}
			chunk /= 2
			continue
		}
		grown = candidate
		i += chunk
	}
	defaultSize := gzipSize(t, grown)
	bestSpeedSize := gzipSizeAt(t, grown, gzip.BestSpeed)
	if defaultSize > coreGoalGZ {
		t.Fatalf("fixture exceeds the level-6 budget: %d > %d", defaultSize, coreGoalGZ)
	}
	if bestSpeedSize <= coreCongestionWindowGZ {
		t.Fatalf("fixture does not cross the level-1 congestion window: %d <= %d", bestSpeedSize, coreCongestionWindowGZ)
	}

	level, _, _ := coreBudgetViolation(t, grown, coreGoalGZ)
	if level != gzip.BestSpeed {
		t.Fatalf("core budget accepted %d bytes at level 6 although level 1 is %d bytes, past the %d-byte congestion window",
			defaultSize, bestSpeedSize, coreCongestionWindowGZ)
	}
}

// Per-module gzip size budget.
//
// Two purposes:
//
//  1. Catch regressions: if a module grows past its current high-water
//     mark, fail loudly. Cheaper than waiting for a Lighthouse drop.
//  2. Pin the runtime-size goals from runtime-minification.md:
//     core ≤ 12.5 KB at gzip level 6, core ≤ 14 KB at level 1, and every
//     demand module ≤ 3 KB at level 6.
//
// Never add or raise an override to silence a regression: split or shrink the
// module instead.
func TestRuntimeModuleSizeBudgets(t *testing.T) {
	// 12.5 KB, not 12: the budget was 12 KB measured at gzip level 9,
	// which nothing ships at. Re-measuring at the level browsers
	// actually receive (see gzipSize) moved the same artifact from 12287
	// to 12317 bytes, the code did not grow, the ruler was wrong. The
	// line is set where it covers the real number with room to work
	// rather than where it silently passed.
	const moduleGoalGZ = 3 * 1024

	// The layout demand modules carry their own rows (the Opt-in
	// spec's instruction): each is pinned at its measured size plus
	// clearance, the same discipline the core line uses. The envelope
	// module is over the generic 3 KB goal ON PURPOSE: it owns the
	// whole layout navigator (the full loadPage, fills envelopes,
	// snapshots, busy marks, leave-capture and scroll anchors) — the
	// bytes the core shed to return to its pre-layout size. A page
	// that never loads it never downloads it.
	moduleOverrides := map[string]int{
		// envelope 7696 measured (7694 + 2 clearance; that measure
		// already carries the seen-path tracking, the stable pane
		// keys and the hash-carrying pending slot);
		// first measure 7535: the module carries its own navigator — the screen
		// cache (seeded at evaluation from the live DOM), the atomic seed
		// merge with the route fold, the swap/focus/finish tail, the
		// toast, the retry, the epoch bump — and the scroll anchors went
		// path-keyed with the _pushURL wrap and the popstate listener.
		// Before that: 6041 (before the supersede rule moved in).
		// 7662 measured after the leave capture started stripping
		// in-flight rpc state (cui-loading, aria-busy, disabled,
		// data-state=pending) from the cached markup, so a cached
		// screen never restores a control stuck busy.
		// 7664 measured after the session-id rewrite took a replacer
		// function, so a `$&` or `$1` in the header is never read as a
		// replacement pattern.
		// 7679 measured after finishNav kept the scroll on a query-only
		// change of the same path (core's rule; the module carries its
		// own navigation tail, so it spells the same decision).
		"envelope": 7679,
		// widgets 3077 measured (2026-10-07): a widget-scoped form's
		// data-cui-confirm gate cancels the submit and hands it to the
		// confirm demand module, the same synchronous gate core runs for
		// the document; the 5 bytes over the generic goal are that
		// hand-off. Pinned at the measured size plus 2 clearance.
		"widgets": 3079,
		// loading 1367 measured after the area-address lookup
		// (2026-09-26, "Areas take loading content"): the scheduler
		// reads a marked region's data-cui-area beside its outlet and
		// slot attributes, so an area's loading template is found by
		// its "~" address like an outlet's "#".
		"loading": 1368,
		// parts 1997 measured after the session-reset retry
		// (2026-09-27, spike/layout-showcase): a `session` 409 waits
		// for the page commit (whose Set-Cookie stores the re-minted
		// token) and re-requests the part once — the retried/sessRetry
		// sets, the enqueue seam on the navigation's pump, and the
		// commit-side flush. Before that: 1834 (the row was 1865).
		// parts 2009 (2007 + 2): the _navLive conversion.
		"parts": 2089,
		// The transition module owns the whole view-transition
		// machinery (the startViewTransition wrapper, the direction
		// types, the gofastr:transition event, the reduced-motion
		// gate, the click re-delivery, the CSSOM name mirror, and the
		// keyed pick) since the opt-in pass 2 moved it out of core.
		// 1336 measured (1334 + 2 clearance) once take began binding
		// the record to an epoch and rebinding it across a takeover.
		// Before that: 1316, when the module took over the
		// navigation-direction and pick-decided state the core's
		// threading used to carry (bound to the navigation's epoch at
		// take time, read back by epoch at commit), and the click
		// re-delivery began dispatching a MouseEvent carrying detail
		// 1 — the pointer shape — instead of el.click()'s detail 0
		// plus the core-side hint channel. Before that: 1183 measured
		// after the re-delivery's pointer-modality hint.
		"transition": 1395,
		// rpc 3195 measured (3193 + 2 clearance) after the 2xx success
		// toast (admin rebuild W2, 2026-10-06): data-cui-rpc-success-
		// toast dispatches through _toastOrFallback BEFORE the trigger's
		// data-cui-rpc-navigate, so the toast outlives the SPA swap its
		// own RPC caused — the save that bounces the reader back to the
		// record page still says "Saved". It cannot live in a module
		// beside rpc: the dispatch is part of the RPC response effects
		// this module already owns, and a demand module loaded after the
		// navigate would render into a page that already swapped.
		"rpc": 3195,
	}
	const coreOverride = 0

	core, err := RuntimeJS()
	if err != nil {
		t.Fatalf("RuntimeJS: %v", err)
	}
	coreBudget := coreOverride
	if coreBudget == 0 {
		coreBudget = coreGoalGZ
	}
	level, got, limit := coreBudgetViolation(t, core, coreBudget)
	switch level {
	case gzip.DefaultCompression:
		t.Errorf("core runtime.js gzip = %d bytes — exceeds %d byte budget (goal %d)", got, limit, coreGoalGZ)
	case gzip.BestSpeed:
		t.Errorf("core runtime.js gzip at level 1 = %d bytes — exceeds the %d-byte initial congestion window; carve a feature into a demand module first, and if it cannot be carved, move the line by the smallest step that fits and say in the commit what bought it", got, limit)
	}

	for _, name := range ModuleNames() {
		src, ok := Module(name)
		if !ok {
			t.Errorf("module %q not embedded", name)
			continue
		}
		budget := moduleGoalGZ
		if o, ok := moduleOverrides[name]; ok {
			budget = o
		}
		if got := gzipSize(t, src); got > budget {
			t.Errorf("module %s gzip = %d bytes — exceeds %d byte budget (goal %d)", name, got, budget, moduleGoalGZ)
		}
	}
	// Registered behaviours (docs/spec-behavior-registry.md) are modules
	// from the host down, so the per-module budget holds them too.
	// ModuleNames() cannot: it sees only the registrations linked into
	// THIS test binary, and the packages that register (framework/ui,
	// framework/headless, the examples) live above core-ui/runtime. The
	// budget therefore discovers their sources through the walk every
	// clean-tree gate uses — check.RegisteredBehaviorSources follows the
	// //go:embed beside each RegisterBehavior call in the tree — and
	// holds each FILE to the same goal, minified through the production
	// minifier under the same gate. A registration that escapes the
	// inventory fails rather than skirting the budget: check's own
	// TestRegisteredBehaviorSources_RefusesWhatItCannotRead pins that
	// a source the walk cannot follow is an error (asserted below by
	// failing the test on err), and its
	// TestRegisteredBehaviorSources_FindsTheTreesModules pins the
	// tree's registrations by path, so a new module cannot land without
	// the walk seeing it.
	sources, err := check.RegisteredBehaviorSources(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("registered behaviours: %v", err)
	}
	if len(sources) == 0 {
		t.Fatal("no registered behaviour sources found under the repo root: the walk is broken, not the tree empty")
	}
	for _, f := range sources {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		src := string(raw)
		if !nominify() {
			src = minify.Minify(src)
		}
		if got := gzipSize(t, src); got > moduleGoalGZ {
			t.Errorf("registered behaviour %s gzip = %d bytes — exceeds %d byte budget (goal %d): split or shrink the module", filepath.Base(f), got, moduleGoalGZ, moduleGoalGZ)
		}
	}
}

func TestComputeModuleSizeBudget(t *testing.T) {
	const budgetGZ = 3 * 1024
	src, ok := Module("compute")
	if !ok {
		t.Fatal("compute module not embedded")
	}
	got := gzipSize(t, src)
	t.Logf("compute module gzip = %d bytes", got)
	if got > budgetGZ {
		t.Fatalf("compute module gzip = %d bytes — exceeds %d byte budget", got, budgetGZ)
	}
}

// Typical-page payload budget: core + the widgets module.
//
// The per-module budgets above keep the core honest, but they have a
// blind spot: features can migrate out of core into widgets.js (which
// nearly every real app loads, any page that mounts a widget pulls
// it), keeping the core number pure while the payload users actually
// download quietly bloats. This test pins the realistic first-load
// cost.
//
// Why these numbers: TCP's initial congestion window is ~10 packets
// (≈14 KB), so the CORE arriving in the first round trip is what the
// 12.5 KB budget protects, that's the cliff; shrinking below it buys
// nothing, exceeding it costs a whole RTT on cold connections. The
// typical-page line (20 KB) is core 12.5 + widgets 5 + drift room.
// When either budget trips, the FIRST answer is carving a feature into a
// demand module, but nav and island RPC must stay in core: a demand
// module costs one request at first use, which is fine for drag-dismiss
// and fatal for the click path. When carving is not available, see the
// policy below for what moving the line costs and what has to be said.
//
// These lines move. Not often and not quietly, but a budget that can
// never move is one that gets lied to instead, so the rule is what has to
// be true before it does, not that it never does. Two kinds of change
// move it and they are not the same kind of decision.
//
// A RULER change re-measures the same bytes. The level-6 line went from
// 12 KB to 12.5 KB when the measurement was corrected from gzip level 9
// to level 6 (see gzipSize): the artifact did not grow, the ruler was
// wrong. A compressor revision is the same thing arriving from outside,
// and Go's compress/flate has changed what BestSpeed emits for identical
// input before. Re-baseline, and say in the commit which release moved
// it and by how much, so the number keeps meaning something.
//
// A SOURCE change is the concession, and gets written down here. The
// level-1 line went from 14336 to 14464 for one: HTML matches the
// underscore target keywords ASCII-case-insensitively, so `target="_SELF"`
// is `_self`, and comparing raw turned a soft navigation into a full page
// load. The fix cost 5 gzipped bytes and the core had 2.
//
// Carve first: that fix took the bytes because the usual answer was
// unavailable, not because it was easier. The guard IS the click path,
// which this comment already says stays in core. When a feature CAN move
// to a demand module, it moves. When it cannot, take the smallest step
// that fits and name what bought it.
//
// What does not move is the cliff. TCP's initial congestion window is
// about 10 packets, ~14600 bytes at a 1460-byte MSS, and a core past it
// costs a whole round trip on a cold connection. Every byte above 14336
// is borrowed from the first paint of every cold visit, so spend it
// deliberately and keep the running total in view.
func TestTypicalPagePayloadBudget(t *testing.T) {
	// sites are loadPage's own apply arms). Smallest step that fits:
	// measured 20563 + 8. Spike (spike/layout-showcase): pointer-initiated
	// focus and the gofastr:fill event sit in the same apply arms,
	// measured 20645 + 8. The real PR revisits the whole layout cost.
	// Round 3 of the same spike: the streamed commit calls the
	// active-link sweep at the swap (S3), measured 20670 + 8.
	// Round 4 (U1): _vtNames's data-cui-vt-when media gate (the name
	// moves between the placed cell and the region the build marks,
	// across the breakpoint), measured 20696 + 8. Spike
	// (spike/layout-parts, parallel part requests replacing the stream
	// reader — the same nav.js change as the core line): measured
	// 21431, line to 21447 (+735 + 8).
	// +563 on 2026-09-26 (spike/layout-resolve): the core's
	// keyed-transition pick and param-key substitution, un-carvable for
	// the reasons the core line names; widgets unchanged.
	// +85 on 2026-09-26: the core's part reset rule; measured 22079.
	const typicalBudgetGZ = 20*1024 + 173 + 25 + 26 + 735 + 563 + 85

	core, err := RuntimeJS()
	if err != nil {
		t.Fatalf("RuntimeJS: %v", err)
	}
	widgets, ok := Module("widgets")
	if !ok {
		t.Fatal("widgets module not embedded")
	}
	got := gzipSize(t, core) + gzipSize(t, widgets)
	if got > typicalBudgetGZ {
		t.Errorf("typical page payload (core+widgets) gzip = %d bytes — exceeds %d byte budget; carve a feature into a demand module first, and if it cannot be carved, move the line by the smallest step that fits and say in the commit what bought it", got, typicalBudgetGZ)
	}
}

// Embed budgets. They are asymmetric because the two files land in completely
// different places.
//
// The loader runs on a customer's page, a site whose performance we do not
// control and whose owner did not choose GoFastr. It is the tightest budget
// here on purpose: crossing it means behaviour was added to the loader that
// belongs inside the frame, where it costs the host page nothing.
//
// The loader is currently 1586 bytes at gzip level 1. Its 1536-byte level-6
// line is a discipline budget, not a transport cliff, so applying core's
// level-1 rule here would invent a second limit with no physical basis.
//
// The frame runtime blocks nothing on the host page, so it is not bound by the
// initial-congestion-window argument that sets core's line. It is still capped
// at 12 KB for a simpler reason: an embedded surface has no business
// shipping MORE javascript than a first-party page does. It ships less today
// (no nav fragment).
func TestEmbedSizeBudgets(t *testing.T) {
	const (
		loaderBudgetGZ = 1536
		frameBudgetGZ  = 12 * 1024
	)

	loader, err := EmbedLoaderJS()
	if err != nil {
		t.Fatalf("EmbedLoaderJS: %v", err)
	}
	if got := gzipSize(t, loader); got > loaderBudgetGZ {
		t.Errorf("embed loader gzip = %d bytes — exceeds %d byte budget; move the behaviour into boot-embed rather than raising the line, the loader runs on someone else's page", got, loaderBudgetGZ)
	} else {
		t.Logf("embed loader gzip = %d bytes (budget %d)", got, loaderBudgetGZ)
	}

	frame, err := EmbedJS()
	if err != nil {
		t.Fatalf("EmbedJS: %v", err)
	}
	if got := gzipSize(t, frame); got > frameBudgetGZ {
		t.Errorf("embed runtime gzip = %d bytes — exceeds %d byte budget", got, frameBudgetGZ)
	} else {
		t.Logf("embed runtime gzip = %d bytes (budget %d)", got, frameBudgetGZ)
	}

	// The embed composition must stay SMALLER than the full one. If it ever
	// isn't, a fragment landed in embed that the full bundle does not carry,
	// which means the two are diverging rather than composing.
	full, err := RuntimeJS()
	if err != nil {
		t.Fatalf("RuntimeJS: %v", err)
	}
	if gzipSize(t, frame) >= gzipSize(t, full) {
		t.Errorf("embed runtime (%d gz) is not smaller than the full runtime (%d gz) — embed omits nav, so it must be", gzipSize(t, frame), gzipSize(t, full))
	}
}

// gzipSize measures at gzip level 6, the DEFAULT, and what actually
// reaches a browser.
//
// It used to measure at level 9. Nothing ships at level 9: GoFastr
// installs no compression middleware, so the wire bytes are produced by
// whatever proxy or CDN fronts the app, at its own setting. Go's
// httptest and most CDNs use 6; nginx's gzip_comp_level default is 1.
// Measuring the single most favourable level meant the gate certified a
// number no user receives, the core measured 12287 against a 12288
// budget while the level-6 artifact was already 12317.
func gzipSize(t *testing.T, s string) int {
	t.Helper()
	return gzipSizeAt(t, s, gzip.DefaultCompression)
}

func gzipSizeAt(t *testing.T, s string, level int) int {
	t.Helper()
	var buf bytes.Buffer
	w, err := gzip.NewWriterLevel(&buf, level)
	if err != nil {
		t.Fatalf("gzip writer at level %d: %v", level, err)
	}
	if _, err := w.Write([]byte(s)); err != nil {
		t.Fatalf("gzip write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	return buf.Len()
}
