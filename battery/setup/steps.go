package setup

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/DonaldMurillo/gofastr/battery/auth"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
	"github.com/DonaldMurillo/gofastr/framework"
)

// AdminStep builds the "create initial admin" step plus the Complete
// predicate ("at least one user exists").
func AdminStep(m *auth.AuthManager, db *sql.DB, usersTable string) (Step, func(context.Context) (bool, error)) {
	if _, err := query.SafeIdent(usersTable); err != nil {
		panic(fmt.Sprintf("setup: AdminStep usersTable is invalid: %v", err))
	}
	quotedTable := query.QuoteFolded(usersTable)

	step := Step{
		Name:  "Create Admin",
		Title: adminStepTitle,
		Fields: []Field{
			{
				Name:     "ADMIN_EMAIL",
				Label:    adminEmailLabel,
				EnvVar:   "GOFASTR_ADMIN_EMAIL",
				Validate: validateAdminEmail,
			},
			{
				Name:   "ADMIN_PASSWORD",
				Label:  adminPasswordLabel,
				EnvVar: "GOFASTR_ADMIN_PASSWORD",
				Secret: true,
				Validate: func(v string) error {
					return auth.ValidatePasswordStrength(v)
				},
			},
		},
		Run: func(ctx context.Context, values map[string]string) error {
			email, err := canonicalAdminEmail(m, values["ADMIN_EMAIL"])
			if err != nil {
				return err
			}
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

func canonicalAdminEmail(m *auth.AuthManager, raw string) (string, error) {
	if err := validateAdminEmail(raw); err != nil {
		return "", err
	}
	canonicalize := m.Config().CanonicalizeEmail
	if canonicalize == nil {
		canonicalize = auth.CanonicalEmail
	}
	email, err := canonicalize(raw)
	if err != nil {
		return "", fmt.Errorf("admin email: %w", err)
	}
	if err := validateAdminEmail(email); err != nil {
		return "", err
	}
	return email, nil
}

func validateAdminEmail(v string) error {
	if !textsafe.Storable(v) {
		return i18n.NewError(reasonEmailInvalid)
	}
	if v == "" {
		return i18n.NewError(reasonEmailRequired)
	}
	return nil
}

// HealthStep builds a "verify adapters" step with no fields.
func HealthStep(app *framework.App) Step {
	return Step{
		Name:   "Verify Adapters",
		Title:  healthStepTitle,
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
				return i18n.NewError(reasonNotReady, map[string]any{"failures": strings.Join(failures, "\n  - ")})
			}
			return nil
		},
	}
}
