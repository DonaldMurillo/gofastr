# 034 — DSL Query Parser

**Phase:** 3 (Framework) | **Depends on:** 006, 019

## Goal
Parse DSL query strings into type-safe query structs. AI-friendly input, compile-time validation via codegen.

## Deliverables
- [ ] DSL syntax: `Entity.where(field=value).include(relations).order(field ASC).limit(N).after(cursor)`
- [ ] Operators: `=`, `!=`, `>`, `<`, `>=`, `<=`, `contains`, `in`
- [ ] Context variables: `currentUser.id`, `params.slug`
- [ ] Parser: DSL string → intermediate AST
- [ ] AST → Query struct (composable with Query primitive)
- [ ] Compile-time validation: entity exists, field exists, relation exists, operator valid for type
- [ ] Codegen: DSL string → Go query struct code (in `.gofastr/` directory)
- [ ] Error messages: agent-friendly with suggestions for typos ("did you mean 'author'?")

## Acceptance Criteria
- Parse valid DSL into correct query struct
- Invalid field name produces helpful error with suggestion
- Codegen produces compilable Go code
- Context variables resolved at runtime
