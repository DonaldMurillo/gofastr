package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/auth"
	"github.com/DonaldMurillo/gofastr/core/dotenv"
	"github.com/DonaldMurillo/gofastr/kiln/freeze"
	"github.com/DonaldMurillo/gofastr/kiln/world"
)

// Property family: a ${NAME} secret reference emitted by kiln freeze into
// the COMMITTED gofastr.yml must not survive the generated app's own .env
// load path as the literal credential value.
//
// The chain, every leg of which runs in this test:
//
//	kiln/freeze envRef         jwt_secret "s3cr3t" -> "${JWT_SECRET}"
//	                           (blueprint.go:864-877; the doc comment says
//	                           "same rule the generated app already follows")
//	cmd/gofastr envQuote       the ref is a clean bareword, so it is written
//	   (blueprint.go:3209-3215) BARE: `JWT_SECRET=${JWT_SECRET}`
//	core/dotenv parseValue      bareword values are returned verbatim;
//	   (dotenv.go:104-119)      ${VAR} expansion is double-quoted-only
//	generated app.go:7266       JWTSecret: os.Getenv("JWT_SECRET") -> the
//	                           13-char literal "${JWT_SECRET}"
//	battery/auth manager.go:481 Init fails closed only on an EMPTY secret;
//	                           NewJWTAuth (jwt.go:36) uses it as the HMAC key
//
// Net effect: the app boots in production mode signing session JWTs with a
// constant that is printed in the committed gofastr.yml, and seeds the
// bootstrap admin with password "${ADMIN_SEED_PASSWORD}". The auth
// battery's fail-closed check never fires: the literal is non-empty.
//
// Note the adjacent, already-pinned contract this must NOT disturb:
// TestEnvValuesRoundTrip (blueprint_secrets_test.go) requires a NON-ref
// value to round-trip byte-exact through .env. The pin here is about the
// composition: a value freeze produced AS A REFERENCE (matches
// ^\$\{[A-Z_]+\}$) must either resolve to the real environment value or
// stop the app, never boot as the credential itself.
//
// Severity note (traced, not assumed): a token forged with the committed
// literal verifies against the app's own key. battery/auth Init rejects
// only an empty secret (manager.go:481) and NewJWTAuth applies no length
// floor (jwt.go:36-42; the >=32-char check in framework/app.go:1483-1501
// covers GOFASTR_SECRET, a different key). The forge leg below
// demonstrates the final leg: ValidateToken accepts the forged token.
func TestFreezeEnvRefNeverBootsAsLiteral(t *testing.T) {
	w := world.New()
	w.App = world.AppConfig{
		Name: "forge", Module: "example.com/forge", DBDriver: "sqlite", DBURL: "forge.db",
		Auth:  world.AuthConfig{Enabled: true, DevMode: false, JWTSecret: "live-freeze-time-secret"},
		Admin: world.AdminConfig{Enabled: true, SeedEmail: "admin@example.com", SeedPassword: "fixture-not-a-real-secret"}, // not-a-secret: obviously-fake fixture value, never a credential
	}

	// Leg 1: freeze replaces both secrets with env references.
	buf, err := freeze.BlueprintYAML(w)
	if err != nil {
		t.Fatalf("BlueprintYAML: %v", err)
	}
	bp, err := decodeBlueprintString(string(buf))
	if err != nil {
		t.Fatalf("decodeBlueprintString rejected freeze output: %v\n%s", err, buf)
	}
	if bp.App.Auth.JWTSecret != "${JWT_SECRET}" {
		t.Fatalf("precondition: freeze no longer emits a JWT_SECRET ref (got %q); update this test with the new freeze contract", bp.App.Auth.JWTSecret)
	}
	if bp.App.Admin.SeedPassword != "${ADMIN_SEED_PASSWORD}" {
		t.Fatalf("precondition: freeze no longer emits an ADMIN_SEED_PASSWORD ref (got %q)", bp.App.Admin.SeedPassword)
	}
	if err := validateBlueprint(bp); err != nil {
		t.Fatalf("validateBlueprint rejected a frozen production-auth blueprint: %v", err)
	}

	// Leg 2: the generator writes those refs into the .env artifact a
	// committed app would carry, and the app loads it exactly this way
	// (main.go: dotenv.LoadAndApply before openDB; NewApp auto-load uses
	// the same no-override Apply).
	files := mustRenderBlueprintFiles(t, bp)
	envContent, ok := filesByName(files)[".env"]
	if !ok {
		t.Fatalf("generator emitted no .env for an auth blueprint; files: %v", fileNames(files))
	}
	envPath := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(envPath, []byte(envContent), 0o600); err != nil {
		t.Fatalf("write .env: %v", err)
	}
	// dotenv.Apply never overrides an existing process var; scrub so the
	// result reflects the .env alone, as on a fresh deploy.
	envrefScrubEnv(t, "JWT_SECRET", "ADMIN_SEED_PASSWORD")
	if err := dotenv.LoadAndApply(envPath); err != nil {
		t.Fatalf("dotenv.LoadAndApply on the generated .env: %v\n.env:\n%s", err, envContent)
	}

	// Leg 3: the loaded values must not be the literal refs. Today they
	// are: envQuote left them bare and dotenv returns barewords verbatim.
	jwt := os.Getenv("JWT_SECRET")
	if jwt == "${JWT_SECRET}" {
		t.Errorf("JWT_SECRET booted as the literal ref \"${JWT_SECRET}\" (the value printed in the committed gofastr.yml) "+
			"instead of resolving to the environment's real secret or failing closed\n.env:\n%s", envContent)
	}
	if pw := os.Getenv("ADMIN_SEED_PASSWORD"); pw == "${ADMIN_SEED_PASSWORD}" {
		t.Errorf("ADMIN_SEED_PASSWORD booted as the literal ref \"${ADMIN_SEED_PASSWORD}\": the bootstrap admin "+
			"(app.go WithSeed, blueprint.go:7289-7297) gets a publicly-known password\n.env:\n%s", envContent)
	}

	// Leg 4 (impact, traced): a token forged with the committed literal
	// verifies against the key the app actually booted with.
	if jwt == "" || jwt == "${JWT_SECRET}" {
		return // already failing above; nothing further to demonstrate
	}
	forger := auth.NewJWTAuth("${JWT_SECRET}", time.Hour)
	tok, err := forger.GenerateToken(&auth.BasicUser{ID: "attacker", Email: "attacker@example.com", Roles: []string{"admin"}})
	if err != nil {
		t.Fatalf("forge token with the committed literal: %v", err)
	}
	appAuth := auth.NewJWTAuth(jwt, time.Hour)
	if claims, err := appAuth.ValidateToken(tok); err == nil {
		t.Errorf("a session JWT forged with the committed gofastr.yml literal verified against the app's signing key "+
			"(claims: id=%s roles=%v): known-to-attacker auth secret", claims.UserID, claims.Roles)
	}
}

// envrefScrubEnv unsets keys for the duration of the test and restores
// them after. dotenv.Apply skips keys already in the environment, so an
// ambient value would mask what the generated .env alone produces.
func envrefScrubEnv(t *testing.T, keys ...string) {
	t.Helper()
	for _, k := range keys {
		old, had := os.LookupEnv(k)
		os.Unsetenv(k)
		t.Cleanup(func() {
			if had {
				os.Setenv(k, old)
			} else {
				os.Unsetenv(k)
			}
		})
	}
}
