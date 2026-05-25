package entity

import (
	"context"
	"fmt"
	"io/fs"
)

type seedCtxKey struct{}

// seedCtx carries the SeedFS/SeedPath references from EntityConfig into
// the Seed function. Populated by the framework migrate package before
// invoking Seed; consumed by host seed code via SeedDataFromContext.
type seedCtx struct {
	FS   fs.FS
	Path string
}

// WithSeedDataContext attaches a SeedFS + SeedPath pair to ctx for
// retrieval by [SeedDataFromContext] inside a Seed function. The
// framework calls this internally; hosts should not need to invoke it
// directly.
func WithSeedDataContext(ctx context.Context, sfs fs.FS, path string) context.Context {
	if sfs == nil {
		return ctx
	}
	return context.WithValue(ctx, seedCtxKey{}, seedCtx{FS: sfs, Path: path})
}

// SeedDataFromContext returns the bytes referenced by the entity's
// SeedFS + SeedPath. Use inside a Seed function:
//
//	Seed: func(ctx context.Context, db *sql.DB) error {
//	    data, err := entity.SeedDataFromContext(ctx)
//	    if err != nil {
//	        return err
//	    }
//	    var rows []FoodRow
//	    if err := json.Unmarshal(data, &rows); err != nil {
//	        return err
//	    }
//	    // ...insert rows...
//	}
//
// Returns an error when no SeedFS was configured on the EntityConfig.
// Name matches the framework convention (TxFromContext, SessionFromContext,
// RegistryFromContext); the older *FromCtx shape is a battery/auth outlier.
func SeedDataFromContext(ctx context.Context) ([]byte, error) {
	v, ok := ctx.Value(seedCtxKey{}).(seedCtx)
	if !ok || v.FS == nil {
		return nil, fmt.Errorf("entity: SeedFS not configured on EntityConfig")
	}
	return fs.ReadFile(v.FS, v.Path)
}
