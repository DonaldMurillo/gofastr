package framework

import (
	"context"
	"database/sql"
	"github.com/DonaldMurillo/gofastr/framework/access"
	"net/http"
	"time"
)

// Root spellings of framework/access.

type (
	Permission             = access.Permission
	Policy                 = access.Policy
	RolePolicy             = access.RolePolicy
	GrantStore             = access.GrantStore
	UnknownCapabilityError = access.UnknownCapabilityError
	CachedResolver         = access.CachedResolver
	RoleWithOrigin         = access.RoleWithOrigin
	Ref                    = access.Ref
	Decision               = access.Decision
	Decider                = access.Decider
)

const (
	DecisionAbstain = access.DecisionAbstain
	DecisionAllow   = access.DecisionAllow
	DecisionDeny    = access.DecisionDeny
)

func NewRolePolicy() *access.RolePolicy { return access.NewRolePolicy() }

func NewGrantStore(db *sql.DB, policy *access.RolePolicy, opts ...access.GrantStoreOption) *access.GrantStore {
	return access.NewGrantStore(db, policy, opts...)
}

func RequirePermission(permission access.Permission) func(http.Handler) http.Handler {
	return access.RequirePermission(permission)
}

func GetPermissions(ctx context.Context) []access.Permission { return access.GetPermissions(ctx) }

func WithPolicy(ctx context.Context, policy *access.RolePolicy) context.Context {
	return access.WithPolicy(ctx, policy)
}

func WithRoles(ctx context.Context, roles []string) context.Context {
	return access.WithRoles(ctx, roles)
}

func GetRoles(ctx context.Context) []string { return access.GetRoles(ctx) }

func Can(ctx context.Context, permission access.Permission) bool { return access.Can(ctx, permission) }

// AccessMiddleware is access.Middleware.
func AccessMiddleware(policy *access.RolePolicy, roles func(ctx context.Context) []string) func(http.Handler) http.Handler {
	return access.Middleware(policy, roles)
}

func NewCachedResolver(resolve func(context.Context) []string, opts ...access.CachedResolverOption) *access.CachedResolver {
	return access.NewCachedResolver(resolve, opts...)
}

// WithResolverTTL is access.WithTTL.
func WithResolverTTL(ttl time.Duration) access.CachedResolverOption { return access.WithTTL(ttl) }

func CanResource(ctx context.Context, capability access.Permission, resource access.Ref) bool {
	return access.CanResource(ctx, capability, resource)
}

func WithDecider(ctx context.Context, d access.Decider) context.Context {
	return access.WithDecider(ctx, d)
}

func GetDecider(ctx context.Context) access.Decider { return access.GetDecider(ctx) }

func DeciderMiddleware(d access.Decider) func(http.Handler) http.Handler {
	return access.DeciderMiddleware(d)
}
