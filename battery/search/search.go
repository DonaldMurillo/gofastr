package search

import "context"

// Document is one searchable record.
type Document struct {
	ID     string         `json:"id"`
	Type   string         `json:"type"`
	Text   string         `json:"text"`
	Fields map[string]any `json:"fields,omitempty"`
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
	// given key with exactly the given value. This is the scope hook —
	// callers put tenant/owner/permission columns in Document.Fields at index
	// time and filter here, in-query, instead of post-filtering.
	//
	// Matching is string-only and identical across backends: a document
	// matches iff for every key/value pair, Fields[key] is present AND its
	// value is a string equal to value. A field whose value is not a string
	// (a number, a slice, a struct) never satisfies a FieldEquals pair, even
	// if its fmt.Sprint form would match. The Postgres backend encodes this as
	// JSONB containment (fields @> '{"tenant":"acme"}'), whose type-strict
	// containment is the natural mirror of the string-equality rule.
	FieldEquals map[string]string
}

// Backend is implemented by search engines.
type Backend interface {
	Index(ctx context.Context, doc Document) error
	Delete(ctx context.Context, id string) error
	Search(ctx context.Context, query Query) ([]Result, error)
}
