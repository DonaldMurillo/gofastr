# 021 — Entity Relationships

**Phase:** 3 (Framework) | **Depends on:** 019, 006

## Goal
Declare relationships between entities. Auto-generate foreign keys, join tables. Smart loading strategy.

## Deliverables
- [ ] Relationship types: BelongsTo (many-to-one), HasMany (one-to-many), HasOne, ManyToMany
- [ ] Declaration via Field: `{Type: Relation, To: "users"}` (BelongsTo), `{Type: Relation, To: "tags", Many: true}` (HasMany/ManyToMany)
- [ ] Auto-generate: foreign key columns, join tables for ManyToMany
- [ ] Include syntax: `.Include("author", "tags")` on query → eager load relations
- [ ] Smart loading: JOIN for BelongsTo/HasOne, batched SELECT for HasMany/ManyToMany
- [ ] Nested includes: `.Include("posts.author", "posts.tags")`
- [ ] Filter across relations: `Where("author.name = ?", "dom")` generates JOIN + WHERE
- [ ] Relationship validation: target entity exists, reciprocal relation valid
- [ ] Reciprocal auto-detection: if User has posts and Post has user, wire both sides

## Acceptance Criteria
- BelongsTo loads related record via JOIN
- HasMany loads via batched SELECT (N+1 prevention)
- ManyToMany loads via join table
- Nested includes work (post → author + tags)
- Filter across relation generates correct SQL
