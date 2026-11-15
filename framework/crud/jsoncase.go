package crud

// JSONCase is the casing of JSON keys in API responses; CaseCamel is the default.
type JSONCase string

const (
	CaseCamel JSONCase = "camelCase"
	CaseSnake JSONCase = "snake_case"
)
