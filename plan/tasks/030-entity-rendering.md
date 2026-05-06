# 030 — Entity Rendering

**Phase:** 3 (Framework) | **Depends on:** 019, 013

## Goal
Auto-generate HTML for entity list, detail, and form views using Render primitive.

## Deliverables
- [ ] List view: table with columns from entity fields, pagination controls
- [ ] Detail view: field labels + values, relation links
- [ ] Create form: form fields generated from schema (input type per field type)
- [ ] Edit form: same as create, pre-filled with existing values
- [ ] Field type → input mapping: Text→textarea, Enum→select, Date→date input, Image→file input, Bool→checkbox
- [ ] Validation error display: re-render form with error messages per field
- [ ] Relationship rendering: link to related entity detail view
- [ ] Customizable: entity can override default template per view

## Acceptance Criteria
- List view renders entity records as HTML table
- Create form generates correct input types per field
- Validation errors show inline on form re-render
- Custom template override works for specific views
