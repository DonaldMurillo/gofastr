# 043 — Plugin System

**Phase:** 5 (Testing & Integration) | **Depends on:** 003, 004, 005

## Goal
Plugin registry. Struct implementing optional interfaces. Framework calls whatever plugin implements.

## Deliverables
- [ ] `Plugin` interface: `Name() string`
- [ ] Optional interfaces:
  ```go
  type RoutesPlugin interface { AddRoutes(*core.Router) }
  type MiddlewarePlugin interface { AddMiddleware(*core.MiddlewareStack) }
  type ToolsPlugin interface { AddTools(*core.MCPServer) }
  type HooksPlugin interface { AddHooks(*framework.App) }
  type CLIPlugin interface { AddCommands(*cobra.Command) }
  ```
- [ ] `app.Register(plugin)` — register, framework calls optional methods
- [ ] Plugin lifecycle: init → register → start → graceful stop
- [ ] Plugin config: each plugin reads from app config under its name
- [ ] Example plugin: simple logging or analytics

## Acceptance Criteria
- Plugin implementing RoutesPlugin gets routes added
- Plugin without RoutesPlugin is just ignored (no error)
- Plugin config loaded from app config
- Multiple plugins coexist without conflict
