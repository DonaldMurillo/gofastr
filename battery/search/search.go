package search

import "context"

// Document is one searchable record.
type Document struct {
	ID     string         `json:"id"`
	Type   string         `json:"type"`
	Text   string         `json:"text"`
	Fields map[string]any `json:"fields,omitempty"`
	// Owner is the user id the document belongs to, or empty for a document
	// that names nobody.
	Owner string `json:"owner,omitempty"`
}

// Result is a matched document with a simple score.
type Result struct {
	Document Document `json:"document"`
	Score    float64  `json:"score"`
}

// Query controls a search request.
type Query struct {
	Text   string
	Type   string
	Limit  int
	Offset int
	// FieldEquals restricts results to documents whose Fields contain every
	// given key with exactly the given value.
	FieldEquals map[string]string
}

// Backend is implemented by search engines.
type Backend interface {
	Index(ctx context.Context, doc Document) error
	Delete(ctx context.Context, id string) error
	Search(ctx context.Context, query Query) ([]Result, error)
}
