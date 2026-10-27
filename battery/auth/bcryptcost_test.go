package auth

import (
	"testing"

	"golang.org/x/crypto/bcrypt"
)

// The package's tests hash at bcrypt's minimum cost: at the default cost
// every password, backup code and dummy hash costs tens of milliseconds,
// and the suite makes hundreds of them. A package-level initializer runs
// before any init function, so the dummy and placeholder hashes init()
// precomputes are made at this cost too.
var _ = func() struct{} {
	bcryptCost = bcrypt.MinCost
	return struct{}{}
}()

// withDefaultBcryptCost restores the production cost for a test whose
// guard is bcrypt's own time.
func withDefaultBcryptCost(t *testing.T) {
	t.Helper()
	old := bcryptCost
	bcryptCost = bcrypt.DefaultCost
	t.Cleanup(func() { bcryptCost = old })
}
