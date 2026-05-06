# 013 — Render Primitive

**Phase:** 1 (Core Primitives) | **Tier:** 2 | **Depends on:** 002, 003

## Goal
Type-safe template engine built from scratch, Templ-inspired. Templates are Go code producing HTML with compile-time type checking and auto-escaping.

## Deliverables
- [ ] HTML builder: programmatic HTML construction in Go (Tag, Attr, Text, Raw)
- [ ] Auto-escaping: all text content HTML-escaped by default, explicit `Raw()` for unsafe
- [ ] Component model: any Go function returning `HTML` is a reusable component
- [ ] Layout system: base layout with named slots, pages fill slots
- [ ] Type-safe data: templates are generic `Render[T]` functions — compile-time checked
- [ ] Template registry: register components by name, compose them
- [ ] Integration with Router: handler returns `HTML` type, middleware sets content-type
- [ ] Hot reload in dev mode: recompile templates without restart
- [ ] `html/template` funcs available: title, upper, lower, date formatting
- [ ] Custom func registration

## Acceptance Criteria
- Compile-time error if template references wrong data type
- XSS: `<script>` in data renders as escaped text
- Components compose: layout + page + partials
- HTML output is well-formed
- Zero dependencies outside Go stdlib
