// Package handler fakes the core/handler principal seam for the
// unseated arms' fixtures.
package handler

import "context"

// RecheckPrincipal re-runs the installed principal checks.
func RecheckPrincipal(ctx context.Context) (context.Context, bool) { return ctx, true }

// GetUser reads the resolved user.
func GetUser(ctx context.Context) (any, bool) { return nil, false }

// GetTenantID reads the resolved tenant.
func GetTenantID(ctx context.Context) string { return "" }
