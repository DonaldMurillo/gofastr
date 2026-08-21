package embed

import "context"

// OriginSource supplies a surface's allowed origins at request time, keyed by
// customer, so onboarding a customer is a row in the app's own table rather
// than a config change and a deploy.
//
// It backs the per-customer shell response: when a Host is constructed with a
// source, the embed shell reads a customer id off the request and serves ONLY
// that customer's origins in the CSP frame-ancestors directive, instead of
// the whole static allowlist on every response. That changes the
// enumerability trade-off: a caller who guesses another customer's id learns
// THAT customer's origins, never the whole list, and gains no framing (the
// browser enforces against the real ancestor chain, and a grant stays bound
// to the origin it was minted for).
//
// Two methods, because the two callers ask different questions. The shell
// needs the LIST of a customer's origins to build the directive. The grant
// path is MintNonce, Exchange, VerifyGrant. It needs only a yes/no about one
// origin, and it does not know which customer is asking: the app calls
// MintNonce with an origin, not a customer id.
//
// Without Allows, a customer added through the source could be framed but
// never granted: the shell would name their origin in frame-ancestors and
// MintNonce would then refuse it, so onboarding still needed a deploy. That
// is the whole point of the feature, so the second method earns its place.
//
// A source is NOT a trusted input. Everything it returns goes through the
// same NormalizeOrigin validation as a boot-time origin (no wildcards, no
// userinfo, no paths, ports compared numerically), is de-duplicated, and is
// capped at response time. A source that errors, returns nothing, or returns
// an over-size list fails closed: the shell answers frame-ancestors 'none'
// rather than widening to everyone.
type OriginSource interface {
	// Origins returns the exact origins allowed to frame the named surface
	// for the named customer, in declaration order. The strings need not be
	// pre-normalized. ResolveCustomerOrigins normalizes them.
	//
	// An empty slice or an error fails the shell closed. The customer id is
	// attacker-chosen (it arrives on an unauthenticated navigation), so a
	// source must treat it as an untrusted key: parameterize any query and
	// bound its length server-side.
	Origins(ctx context.Context, surface, customer string) ([]string, error)

	// Allows reports whether origin may frame the named surface for ANY
	// customer. It is the grant path's question: MintNonce is handed an
	// origin, not a customer, so this is what decides whether a
	// source-managed origin can obtain a credential at all.
	//
	// It is on the hot path. VerifyGrant calls it on every embed request
	// whose origin is not in the static allowlist, so an implementation
	// must be cheap. Cache it; a table scan per request is not acceptable.
	//
	// An error fails closed. Origins are compared after NormalizeOrigin, so
	// an implementation receives the canonical form and should store the
	// same.
	Allows(ctx context.Context, surface, origin string) (bool, error)
}

// maxCustomerIDBytes bounds the attacker-chosen customer id before it reaches
// the source. The shell route is unauthenticated, so an unbounded id would
// travel into the app's lookup (and any log line) at request-line length,
// three orders of magnitude larger than any real identifier.
const maxCustomerIDBytes = 256

// ResolveCustomerOrigins is the single entry point a shell request uses to turn
// an OriginSource into the normalized, capped origin list for a CSP
// frame-ancestors directive.
//
// Every failure returns a non-nil error so the caller can fail closed by
// serving frame-ancestors 'none'. The cases that fail closed are named
// deliberately:
//
//   - no source (programming error by the caller),
//   - empty customer id (the app opted into a source, so a request without
//     one is a misconfigured snippet, not a wildcard),
//   - an over-long customer id (the id is attacker-chosen and unauthenticated),
//   - a source that errors (a store is not trusted to widen framing),
//   - a source that returns no origins (a customer with no framers is a
//     configuration mistake, not allow-everyone),
//   - any origin that fails NormalizeOrigin (a store is not a trusted input),
//   - a joined list over the per-response cap (proxies truncate or reject
//     oversized response headers; failing this one customer closed is strictly
//     better than the old boot-time refusal that broke every customer at once).
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

// Sentinel errors for the fail-closed cases a caller can distinguish. They
// carry no detail about the customer or the source's internals: the shell
// route is unauthenticated, and which check failed is not an oracle worth
// handing a caller probing with guessed ids.
var (
	errOriginSourceUnavailable = originSourceError("embed: no origin source configured")
	errEmptyCustomerID         = originSourceError("embed: empty customer id: a source requires one; serving no origins")
	errCustomerIDTooLong       = originSourceError("embed: customer id exceeds the length limit")
)

// originSourceError lets ResolveCustomerOrigins return typed fail-closed
// errors without wrapping caller detail into them.
type originSourceError string

func (e originSourceError) Error() string { return string(e) }
