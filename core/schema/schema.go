package schema

// FieldType enumerates all supported field types in a GoFastr entity schema.
type FieldType int

const (
	String    FieldType = iota // short string
	Text                       // long text / textarea
	Int                        // integer
	Float                      // floating point
	Decimal                    // fixed-precision decimal (stored as string)
	Bool                       // boolean
	Enum                       // one of a fixed set of strings
	UUID                       // UUID v4 identifier
	Timestamp                  // RFC 3339 timestamp
	Date                       // calendar date (2006-01-02)
	JSON                       // arbitrary JSON blob
	Relation                   // reference to another entity
	Image                      // image URL or path
	File                       // file URL or path
)

// Field defines a single field in an entity schema.
type Field struct {
	Name     string    // field name (must be unique within a Schema)
	Type     FieldType // field type
	Required bool      // value must be present and non-zero
	Unique   bool      // value must be unique across entities
	Default  any       // default value when omitted
	Max      *float64  // upper bound (numeric max / string max-length)
	Min      *float64  // lower bound (numeric min / string min-length)
	Pattern  string    // regex pattern for string validation
	Values   []string  // allowed values for Enum
	To       string    // target entity name for Relation
	Many     bool      // has-many relation flag
}

// Schema is an ordered collection of Fields with convenience helpers.
type Schema struct {
	Fields []Field
}

// FieldByName returns the field with the given name and true,
// or the zero Field and false if not found.
func (s Schema) FieldByName(name string) (Field, bool) {
	for _, f := range s.Fields {
		if f.Name == name {
			return f, true
		}
	}
	return Field{}, false
}

// Names returns the ordered list of field names.
func (s Schema) Names() []string {
	names := make([]string, len(s.Fields))
	for i, f := range s.Fields {
		names[i] = f.Name
	}
	return names
}

// RequiredFields returns only the required fields.
func (s Schema) RequiredFields() []Field {
	var out []Field
	for _, f := range s.Fields {
		if f.Required {
			out = append(out, f)
		}
	}
	return out
}

// Validate checks a single value against a Field definition.
// Returns nil on success or a descriptive error on failure.
func Validate(field Field, value any) error {
	return validateField(field, value)
}

// ValidationResult holds the outcome of validating a full map of values.
type ValidationResult struct {
	Valid  bool
	Errors map[string][]string // field name → list of error messages
}

// ValidateAll validates a map of values against every Field in the Schema.
// Unknown keys are ignored; missing required fields produce errors.
func ValidateAll(s Schema, values map[string]any) ValidationResult {
	result := ValidationResult{
		Errors: make(map[string][]string),
	}

	for _, f := range s.Fields {
		val, present := values[f.Name]
		if !present {
			if f.Required && f.Default == nil {
				result.Errors[f.Name] = append(result.Errors[f.Name], "is required")
			}
			continue
		}
		if err := validateField(f, val); err != nil {
			result.Errors[f.Name] = append(result.Errors[f.Name], err.Error())
		}
	}

	result.Valid = len(result.Errors) == 0
	return result
}
