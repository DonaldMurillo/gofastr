// Package migrate is a versioned SQL migration runner with optional per-group
// migration streams.
//
// An app composed of optional features can organize migrations into named
// GROUPS so each module owns its schema independently. A migration with an
// empty Group belongs to the DEFAULT group — the single flat, version-ordered
// list every existing app uses. Two groups may each have a version 1; version
// uniqueness is enforced per group.
//
//	-- +migrate Version 1
//	-- +migrate Group knowledge
//	-- +migrate Name create_kb_tables
//	-- +migrate Up
//	CREATE TABLE kb_docs (...);
//
// # Selection
//
// Up/Down/Status take an optional variadic list of group names. With no
// arguments every registered group is in scope (the existing behavior). With
// one or more names, only those groups' migrations are applied, rolled back, or
// reported — so enabling a feature later runs just its pending set. Force takes
// at most one group (0 = default, 1 = that group, more = error).
//
// # Ordering
//
// Within a group migrations apply strictly by version. When one run applies
// multiple groups the global order is (version, group_name) ascending. Groups
// MUST be self-contained: no cross-group schema dependencies. The ordering rule
// is a deterministic tiebreak, not a dependency mechanism.
//
// # Compatibility
//
// An app that never uses groups sees byte-identical behavior: the same tracking
// table, the same SQL, the same advisory lock. The group_name column and the
// composite (group_name, version) primary key are created only when a
// non-default group is registered or selected. A pre-existing default-group
// table that later needs groups is upgraded in place (ALTER on Postgres, an
// atomic create/copy/rename rebuild on SQLite) the first time a group-aware
// operation runs.
//
// See https://github.com/DonaldMurillo/gofastr for documentation.
package migrate
