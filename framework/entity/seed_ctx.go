package entity

import (
	"context"
	"fmt"
	"io/fs"
)

type seedCtxKey struct{}

type seedCtx struct {
	FS   fs.FS
	Path string
}

// WithSeedDataContext attaches SeedFS and SeedPath to ctx for SeedDataFromContext. A nil sfs returns ctx unchanged.
func WithSeedDataContext(ctx context.Context, sfs fs.FS, path string) context.Context {
	if sfs == nil {
		return ctx
	}
	return context.WithValue(ctx, seedCtxKey{}, seedCtx{FS: sfs, Path: path})
}

// SeedDataFromContext returns SeedPath's bytes from SeedFS inside Seed, or an error without one.
func SeedDataFromContext(ctx context.Context) ([]byte, error) {
	v, ok := ctx.Value(seedCtxKey{}).(seedCtx)
	if !ok || v.FS == nil {
		return nil, fmt.Errorf("entity: SeedFS not configured on EntityConfig")
	}
	return fs.ReadFile(v.FS, v.Path)
}
