# Roadmap Worktree — Verification Handoff

## What's been done

All 8 sections of ROADMAP.md have been addressed. 32 new files created,
9 existing files modified, 5 bug fixes applied during test-driven development.

## How to verify

```bash
cd /Users/dom/programming/gofastr/.pi/worktrees/roadmap
cat t.sh | bash
```

Or step by step:
```bash
go build ./...
go test ./framework/routegroup/ ./framework/apiversions/ ./framework/lifecycle/ ./core/config/ ./core-ui/app/ ./framework/dsl/ ./framework/i18nui/ ./core/stream/ ./framework/ui/ -v -count=1
```

## Bug fixes applied during this pass

1. **DatePicker**: `html.Input` requires `Name` — display and hidden inputs now use proper Name fields; Label rendered conditionally (was panicking when empty); min/max passed to hidden input attrs.
2. **InlineEdit**: `html.Input` requires `Type` and `Name` — both inputs now use direct fields instead of only Attrs map.
3. **Wizard**: Hidden step-tracking input was using Attrs for Type/Name — now uses direct fields.
4. **Repeater**: Label requires `For` — now uses the items container ID.
5. **i18nui `humanize()`**: `byte(ch)+32` corrupted already-lowercase chars — fixed to use `unicode.ToUpper`/`unicode.ToLower`.
6. **WebSocket `Write()`**: Non-deterministic `select` could send to buffer after close — added pre-check of closed channel.

## File inventory

### New packages (14 dirs, 32 files)

§1 Route groups:
  NEW framework/routegroup/group.go           (6.6 KB)
  NEW framework/routegroup/group_test.go       (7.4 KB)
  NEW framework/reexports_routegroup.go        (399 B)

§2 Screen groups:
  NEW core-ui/app/screen_group.go              (5.6 KB)
  NEW core-ui/app/screen_group_test.go         (6.1 KB)

§3 API versioning:
  NEW framework/apiversions/version.go         (4.0 KB)
  NEW framework/apiversions/projection.go      (2.5 KB)
  NEW framework/apiversions/version_test.go    (4.6 KB)
  NEW framework/reexports_apiversions.go       (292 B)

§4a WebSocket:
  NEW core/stream/websocket.go                 (7.7 KB)
  NEW core/stream/websocket_test.go            (5.5 KB)
  NEW core/stream/hub.go                       (3.0 KB)
  NEW core/stream/hub_test.go                  (4.3 KB)

§4b CLI scaffolding:
  NEW cmd/gofastr/new.go                       (5.0 KB)

§4c Config:
  NEW core/config/config.go                    (6.0 KB)
  NEW core/config/config_test.go               (3.3 KB)

§4d Lifecycle:
  NEW framework/lifecycle/lifecycle.go         (4.4 KB)
  NEW framework/lifecycle/lifecycle_test.go    (2.1 KB)

§4e i18n:
  NEW framework/i18nui/i18nui.go               (7.2 KB)
  NEW framework/i18nui/i18nui_test.go          (3.3 KB)

§4f Battery follow-ups:
  NEW battery/redisidempotency/store.go        (2.4 KB)
  NEW battery/redisflags/store.go              (2.9 KB)

§5 UI components:
  NEW framework/ui/datepicker.go               (4.8 KB)
  NEW framework/ui/datepicker_test.go          (1.9 KB)
  NEW framework/ui/repeater.go                 (3.5 KB)
  NEW framework/ui/repeater_test.go            (2.0 KB)
  NEW framework/ui/wizard.go                   (5.1 KB)
  NEW framework/ui/wizard_test.go              (3.9 KB)
  NEW framework/ui/inlineedit.go               (2.8 KB)
  NEW framework/ui/inlineedit_test.go          (2.0 KB)
  NEW framework/ui/form_inputs.go              (6.3 KB)

§7 Performance:
  NEW framework/migrate/bulk.go                (3.8 KB)
  MOD framework/dsl/dsl.go                     (+bounded LRU cache for ParseDSL)
  NEW framework/dsl/dsl_test.go                (4.1 KB)

### Modified files (9)

  MOD framework/app.go                         (+Group, +GroupEntity, +registerGroupEndpoints)
  MOD framework/crud/crud.go                   (parsePagination: ?stream=true bypass)
  MOD framework/internal/casing/casing.go      (cached ToCamel/ToSnake, +PrecomputeMapping)
  MOD core-ui/runtime/runtime.js               (+findCommonScreenGroup, +swapScreenGroupContent)
  MOD core-ui/app/router.go                    (+ScreenGroup registration)
  MOD cmd/gofastr/main.go                      (+new subcommand)
  MOD ROADMAP.md                               (§1-§8 statuses updated, §7h marked done)
  MOD framework/ARCHITECTURE.md                (+routegroup, apiversions, i18nui, lifecycle to pkg map)
  MOD core-ui/ARCHITECTURE.md                  (+6 data-fui-* attributes)

### Documentation (2)
  MOD framework/ARCHITECTURE.md                (+routegroup, apiversions, i18nui, lifecycle to pkg map)
  MOD core-ui/ARCHITECTURE.md                  (+6 data-fui-* attributes)

## Sections 6 & 8 — pre-existing

§6 (Typed theme) was already fully implemented in core-ui/style/.
§8 (Runtime code-split) was already fully implemented with 31 modules.

## What's still deferred

- §5e Lightbox pinch-to-zoom (touch/gesture)
- §5f BottomSheet drag-to-dismiss (touch/gesture)
- §5g Carousel virtual-scroll (render optimization)
- §7a Default middleware chain cost
- §7c FilteredList pooling
- §7g CronTick allocations
- §7i SSE backpressure buffer
- §7j UI host page render pooling
- §7k Island RPC tail latency
- §7l Filtered list typed result struct
- §4f Passkeys auth (requires WebAuthn library)
