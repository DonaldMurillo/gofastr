# 002 — Schema Primitive

**Phase:** 1 (Core Primitives) | **Tier:** 1 | **Depends on:** nothing

## Goal
Define field types, validate values against field definitions, generate JSON Schema. The foundation for entity declarations and input validation.

## Deliverables
- [ ] `FieldType` enum: String, Text, Int, Float, Decimal, Bool, Enum, UUID, Timestamp, Date, JSON, Relation, Image, File
- [ ] `Field` struct:
  ```go
  type Field struct {
      Name     string
      Type     FieldType
      Required bool
      Unique   bool
      Default  any
      Max      *float64    // for numeric/string
      Min      *float64    // for numeric/string
      Pattern  string      // regex for string validation
      Values   []string    // for Enum
      To       string      // target entity for Relation
      Many     bool        // has-many for Relation
  }
  ```
- [ ] `Validate(field Field, value any) error` — validate any value against a Field definition
- [ ] Per-type validation:
  - String/Text: required, min/max length, regex pattern
  - Int/Float/Decimal: required, min/max range
  - Bool: accepts true/false/1/0
  - Enum: value in allowed values list
  - UUID: valid UUID format
  - Timestamp/Date: valid time parse
  - JSON: valid JSON
  - Relation: valid reference format
- [ ] `JSONSchema(fields []Field) map[string]any` — generate JSON Schema from field definitions
- [ ] `Schema` struct: collection of Fields with helpers
- [ ] `ValidateAll(schema Schema, values map[string]any) ValidationResult`
- [ ] `ValidationResult`: valid bool, errors map[string][]string (field → messages)
- [ ] Tests for every field type validation

## Acceptance Criteria
- Every FieldType has validation tests (valid + invalid cases)
- JSON Schema output is valid JSON Schema draft-07
- Zero dependencies outside Go stdlib
- No `any` leaking into public API where possible — use generics for type safety
