# 039 — CLI: `gofastr dev`

**Phase:** 4 (CLI & DX) | **Depends on:** 038

## Goal
Hot-reload development server. Watch files → regenerate → rebuild → restart.

## Deliverables
- [ ] Watch: .go files, entity JSON, templates, static assets
- [ ] On change: regenerate → rebuild → restart server
- [ ] Configurable watch paths and ignore patterns
- [ ] Debounced rebuild (wait for write to settle, 200ms default)
- [ ] Graceful server restart (drain connections)
- [ ] Configurable dev server port
- [ ] Build error display in terminal (colored, clear)
- [ ] Print file change events

## Acceptance Criteria
- Changing a .go file triggers rebuild + restart
- Changing entity JSON triggers codegen + rebuild + restart
- Debounce prevents rebuild storm on multi-file saves
- Server accessible on configured port after startup
