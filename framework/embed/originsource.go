package embed

import "context"

// OriginSource supplies a surface's allowed origins per customer at request time.
// Its answers are untrusted and any failure fails closed.
type OriginSource interface {
	// Origins returns the origins that may frame surface for an attacker-chosen customer id.
	Origins(ctx context.Context, surface, customer string) ([]string, error)

	// Allows reports whether a normalized origin may frame surface for any customer; keep it cheap.
	Allows(ctx context.Context, surface, origin string) (bool, error)
}

const maxCustomerIDBytes = 256

// ResolveCustomerOrigins returns the normalized, capped origins src lists for
// customer, or an error for every case that must frame for no one.
func ResolveCustomerOrigins(ctx context.Context, src OriginSource, surface, customer string) ([]string, error) {
	if src == nil {
		return nil, errOriginSourceUnavailable
	}
	if customer == "" {
		return nil, errEmptyCustomerID
	}
	if len(customer) > maxCustomerIDBytes {
		return nil, errCustomerIDTooLong
	}
	raw, err := src.Origins(ctx, surface, customer)
	if err != nil {
		return nil, err
	}
	set, err := buildCustomerOriginSet(raw)
	if err != nil {
		return nil, err
	}
	return set.List(), nil
}

var (
	errOriginSourceUnavailable = originSourceError("embed: no origin source configured")
	errEmptyCustomerID         = originSourceError("embed: empty customer id: a source requires one; serving no origins")
	errCustomerIDTooLong       = originSourceError("embed: customer id exceeds the length limit")
)

type originSourceError string

func (e originSourceError) Error() string { return string(e) }
