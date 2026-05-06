# 023 — Entity Validators

**Phase:** 3 (Framework) | **Depends on:** 019

## Goal
Built-in + custom field-level validators. Run before hooks. Structured error responses.

## Deliverables
- [ ] Built-in validators from field config: Required, Max, Min, Pattern, Email format, UUID format
- [ ] Custom validator registration: `func(ctx context.Context, value any) error`
- [ ] Register validators per entity per field
- [ ] Validators run before hooks
- [ ] Validation errors: structured `map[string][]string` (field → messages)
- [ ] Error response: JSON `{"errors": {"email": ["invalid format"], "age": ["must be at least 18"]}}`
- [ ] Reuse Schema primitive validation logic internally

## Acceptance Criteria
- All built-in validators work for their field types
- Custom validator errors appear alongside built-in errors
- Multiple errors per field collected (not fail-fast)
- Invalid input returns 400 with all field errors
