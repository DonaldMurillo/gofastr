package framework

import "github.com/DonaldMurillo/gofastr/framework/apiversions"

// Re-export apiversions types so external callers can use them
// without a direct import.

// APIVersion is re-exported from apiversions.
type APIVersion = apiversions.APIVersion

// APIProjection is re-exported from apiversions.
type APIProjection = apiversions.Projection
