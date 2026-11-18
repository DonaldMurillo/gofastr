// Package kilntest holds the protocol.Tools fixture shared by the kiln
// adapter tests (protocol, acp, agent/mcp). Tests only: nothing outside a
// _test.go file imports it.
package kilntest

import (
	"testing"

	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"

	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/kiln/db"
	"github.com/DonaldMurillo/gofastr/kiln/journal"
	"github.com/DonaldMurillo/gofastr/kiln/live"
	"github.com/DonaldMurillo/gofastr/kiln/protocol"
)

// NewTools returns protocol.Tools over a live session backed by an
// ephemeral SQLite database named for the calling test suite.
func NewTools(t *testing.T, suite string) *protocol.Tools {
	t.Helper()
	d, cleanup, err := db.EphemeralSQLite(suite)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	factory := func() *framework.App { return framework.NewApp(framework.WithDB(d)) }
	l, err := live.New(journal.NewMemory(), factory)
	if err != nil {
		t.Fatal(err)
	}
	return protocol.New(l)
}
