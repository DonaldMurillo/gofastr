package sdk

import (
	"crypto/sha256"
	"fmt"

	"github.com/DonaldMurillo/gofastr/core/jcs"
)

// OpenAPIHash returns a "sha256:<hex>" digest of an OpenAPI document's
// JSON in its RFC 8785 canonical form, so key order and whitespace never
// move it. It errors on bytes that are not one JSON value.
func OpenAPIHash(doc []byte) (string, error) {
	canonical, err := jcs.CanonicalizeJSON(doc)
	if err != nil {
		return "", fmt.Errorf("sdk: OpenAPI document: %w", err)
	}
	return fmt.Sprintf("sha256:%x", sha256.Sum256(canonical)), nil
}
