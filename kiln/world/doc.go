// Package world is the JSON-clean intermediate representation of a GoFastr
// application being built live by an agent inside Kiln.
//
// A World captures every declarative knob the framework exposes:
// app config, entities (with fields, relations, custom endpoints, declarative
// hooks), pages (UI element trees), seed data, custom routes, and middleware.
// All types are marshalable as JSON without function pointers; this is what
// lets the journal serialize edits and lets freeze emit canonical source.
//
// The IR is intentionally separate from framework.EntityConfig and the
// core-ui component types: those carry Go function fields (handlers, hooks,
// renderers) that the agent cannot author. Kiln maps World → those types
// at render time, plugging in declarative actions evaluated by kiln/expr.
package world
