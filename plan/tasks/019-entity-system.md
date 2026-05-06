# 019 — Entity System Core

**Phase:** 3 (Framework) | **Depends on:** ALL core primitives (002-013)

## Goal
The heart of GoFastr. Declare entities → framework generates structs, registers routes, wires everything.

## Deliverables
- [ ] `EntityConfig` struct:
  ```go
  type EntityConfig struct {
      Name       string
      Fields     []Field
      CRUD       bool
      MCP        bool
      SoftDelete bool
      Endpoints  []Endpoint
      Validators []Validator
      Hooks      Hooks
      Access     Access
  }
  ```
- [ ] `app.Entity(name, config)` — register entity, validate config
- [ ] `app.EntityFromFile(path)` — load entity from JSON
- [ ] `app.EntitiesFromDir(path)` — load all entities from directory
- [ ] Entity registry: map name → EntityConfig + metadata
- [ ] Config validation: field types valid, relation targets exist, no duplicate names
- [ ] Entity metadata: table name, primary key, indexes, relation map
- [ ] Field type resolution: Image/File → wire Upload primitive, Relation → wire Query joins

## Acceptance Criteria
- Register entity with fields → appears in registry with correct metadata
- Invalid config returns clear errors (bad field type, missing relation target)
- JSON loading produces same result as Go declaration
- Relation targets validated at registration time
