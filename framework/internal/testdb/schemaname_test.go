package testdb

import (
	"os"
	"strconv"
	"strings"
	"testing"
)

// A schema name must be unique across PROCESSES, not just within one.
//
// This is the regression net for a real CI failure. While every test process
// spawned its own ephemeral Postgres container, a process-local counter was
// enough. Once all processes share one server, the CI service, or a local
// `make postgres-up`, `go test -p 2` puts two packages on the same database,
// both reach `t_<sametest>_1`, and the second fails with `schema ... already
// exists`. CI's coverage-floors step re-runs packages in the same job, so it
// is not even a race: the same name comes round again by construction.
func TestNewSchemaNameCarriesProcessIdentity(t *testing.T) {
	name := NewSchemaName(t)
	pid := strconv.Itoa(os.Getpid())
	if !strings.Contains(name, "_"+pid+"_") {
		t.Errorf("NewSchemaName() = %q, which does not embed the pid (%s) — two concurrent test processes would collide on one shared Postgres", name, pid)
	}
}

// Postgres truncates identifiers past 63 bytes, and a truncated name drops the
// discriminator that makes it unique, reintroducing the collision the pid was
// added to prevent. The longest realistic test name must still fit.
func TestNewSchemaNameFitsPostgresIdentifierLimit(t *testing.T) {
	name := NewSchemaName(t)
	if len(name) > 63 {
		t.Errorf("NewSchemaName() = %q (%d bytes), over Postgres's 63-byte identifier cap", name, len(name))
	}
	// The discriminator must survive truncation of the test-name portion,
	// both halves of it, checked independently.
	//
	// The first version of this assertion was `!HasSuffix(name, "_1") &&
	// !Contains(name, "_<pid>_")`, which never fired: the name always
	// contains the pid marker, so the second operand was always false and
	// `&&` short-circuited the whole branch to dead code. It also pinned the
	// literal "_1", which only describes the first call in a process. Caught
	// in review, which is the correct outcome but an uncomfortable one for an
	// assertion living in the file that exists to stop vacuous tests.
	if !strings.Contains(name, "_"+strconv.Itoa(os.Getpid())+"_") {
		t.Errorf("NewSchemaName() = %q dropped the pid marker after truncation", name)
	}
	last := strings.LastIndexByte(name, '_')
	if last < 0 {
		t.Fatalf("NewSchemaName() = %q has no counter component", name)
	}
	if _, err := strconv.ParseUint(name[last+1:], 10, 64); err != nil {
		t.Errorf("NewSchemaName() = %q does not end in a numeric counter (%v)", name, err)
	}
}

// Successive calls in one process differ, or a single test opening two
// databases collides with itself.
func TestNewSchemaNameIsUniquePerCall(t *testing.T) {
	seen := map[string]bool{}
	for range 100 {
		n := NewSchemaName(t)
		if seen[n] {
			t.Fatalf("NewSchemaName() repeated %q within one process", n)
		}
		seen[n] = true
	}
}
