package crud

// JSONCase defines the casing convention for JSON keys in API responses.
//
// CaseCamel — camelCase (default, web standard)
// CaseSnake — snake_case (database-style)
type JSONCase string

const (
	CaseCamel JSONCase = "camelCase"
	CaseSnake JSONCase = "snake_case"
)
