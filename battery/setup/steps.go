package setup

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/DonaldMurillo/gofastr/battery/auth"
	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/framework"
)

// AdminStep builds the "create initial admin" step plus the Complete
// predicate ("at least one user exists").
//
// The step collects an email + password, validates the password against
// the auth battery's strength policy (RecommendedMinPasswordBytes), hashes
// it with the same bcrypt hasher the battery uses, and creates a user
// with roles ["admin"].
//
// The Complete predicate probes the users table directly —
// auth.UserStore has no count/list surface and the brief forbids adding
// methods to battery/auth. The narrowest exported surface is the *sql.DB
// the host already holds (the same one passed to
// auth.NewEntityUserStore).
//
// usersTable MUST match the table name passed to
// auth.NewEntityUserStore (typically "auth_users"). The identifier is
// validated with query.SafeIdent and quoted with query.QuoteIdent since
// it can't be parameterized. An empty or unsafe name panics — fail loud,
// never silently fall back to a hardcoded default.
//
// EnvVars: GOFASTR_ADMIN_EMAIL / GOFASTR_ADMIN_PASSWORD.
func AdminStep(m *auth.AuthManager, db *sql.DB, usersTable string) (Step, func(context.Context) (bool, error)) {
	if _, err := query.SafeIdent(usersTable); err != nil {
		panic(fmt.Sprintf("setup: AdminStep usersTable is invalid: %v", err))
	}
	quotedTable := query.QuoteIdent(usersTable)

	step := Step{
		Name: "Create Admin",
		Fields: []Field{
			{
				Name:   "ADMIN_EMAIL",
				Label:  "Admin email",
				EnvVar: "GOFASTR_ADMIN_EMAIL",
				Validate: func(v string) error {
					if v == "" {
						return fmt.Errorf("email is required")
					}
					return nil
				},
			},
			{
				Name:   "ADMIN_PASSWORD",
				Label:  "Admin password",
				EnvVar: "GOFASTR_ADMIN_PASSWORD",
				Secret: true,
				Validate: func(v string) error {
					return auth.ValidatePasswordStrength(v)
				},
			},
		},
		Run: func(ctx context.Context, values map[string]string) error {
			email := values["ADMIN_EMAIL"]
			password := values["ADMIN_PASSWORD"]

			hash, err := auth.HashPassword(password)
			if err != nil {
				return fmt.Errorf("hash password: %w", err)
			}

			store := m.UserStore()
			if _, err := store.CreateUser(ctx, email, hash, []string{"admin"}); err != nil {
				return fmt.Errorf("create admin: %w", err)
			}
			return nil
		},
	}

	complete := func(ctx context.Context) (bool, error) {
		var exists bool
		// SELECT 1 FROM <quotedTable> LIMIT 1 — one row means at least
		// one user. The table is guaranteed to exist (EnsureSchema ran
		// in AuthManager.Init before setup).
		err := db.QueryRowContext(ctx, "SELECT 1 FROM "+quotedTable+" LIMIT 1").Scan(&exists)
		if err == sql.ErrNoRows {
			return false, nil
		}
		if err != nil {
			return false, fmt.Errorf("check users: %w", err)
		}
		return true, nil
	}

	return step, complete
}

// HealthStep builds a "verify adapters" step with no fields. Its Run
// executes the app's registered readiness checks via the exported
// RunReadinessChecks method and fails with a per-check, actionable
// error list. In the wizard this renders as a "verify adapters" step.
func HealthStep(app *framework.App) Step {
	return Step{
		Name:   "Verify Adapters",
		Fields: nil,
		Run: func(ctx context.Context, _ map[string]string) error {
			resp := app.RunReadinessChecks(ctx)
			var failures []string
			for _, rc := range resp.Checks {
				if rc.Status != "ok" {
					failures = append(failures,
						fmt.Sprintf("%s: %s", rc.Name, rc.Error))
				}
			}
			if len(failures) > 0 {
				return fmt.Errorf("readiness checks failed:\n  - %s",
					strings.Join(failures, "\n  - "))
			}
			return nil
		},
	}
}
