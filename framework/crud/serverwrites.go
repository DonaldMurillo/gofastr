package crud

import "context"

// serverWritesKey is an unexported context-key type so the marker can only
// be set by calling WithServerWrites from Go code. There is no path for an
// HTTP-derived context to carry it: the auto-CRUD HTTP handlers never set
// it, so the default — skip ReadOnly/Hidden fields — always holds on the
// wire.
type serverWritesKey struct{}

// WithServerWrites returns a context marked as trusted server-origin.
// CreateOne / UpdateOne / UpsertOne / batch methods that receive it will
// persist ReadOnly and Hidden fields present in the body, instead of the
// default behaviour of silently skipping them.
//
// SECURITY:
//   - Set this ONLY from your own server-side Go (a service method, cron
//     job, seed script, or admin action you have already gated with a
//     permission check). There is NO built-in role check here.
//   - NEVER derive this flag from request data (headers, query params,
//     JSON body). Hosts pipe near-raw maps into CreateOne via factories
//     and seeders; an unguarded derivation there becomes a mass-assignment
//     bypass.
//   - The owner and tenant columns remain protected regardless: InjectOwner
//     still stamps/overwrites the owner value, and the tenant column is
//     always taken from the context-derived tenant id — never the body.
//
// Note: Hidden fields stay absent from the RETURNING clause / response maps
// even when persisted. visibleFields shapes the projection, not the write
// set.
func WithServerWrites(ctx context.Context) context.Context {
	return context.WithValue(ctx, serverWritesKey{}, true)
}

// serverWrites reports whether ctx was marked trusted server-origin via
// WithServerWrites.
func serverWrites(ctx context.Context) bool {
	v, _ := ctx.Value(serverWritesKey{}).(bool)
	return v
}
