package analyzers_test

import (
	"slices"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/contracts"
)

func TestSweepDataDialectDriftNormalizesSpelling(t *testing.T) {
	src := "package main\n\n" +
		"const listJobsPostgres = `SELECT j.id FROM jobs j WHERE j.\"status\" IN ($1, $2) AND j.owner = $3`\n\n" +
		"const listJobsSQLite = \"SELECT id FROM jobs WHERE status IN (?) AND owner = ?\"\n"
	if got := ruleLines(t, contracts.RuleDialectDrift, src); got != nil {
		t.Errorf("GOFASTR1414 lines %v, want none: aliases, quoting, placeholders and IN-lists normalize away", got)
	}
}

func TestSweepDataDialectDriftReportsThePoorerSideOfASubset(t *testing.T) {
	src := "package main\n\n" +
		"const claimJobsPostgres = \"SELECT id FROM jobs WHERE status = 'pending' AND next_at <= $1 AND id NOT IN ( SELECT job_id FROM locks )\"\n\n" +
		"const claimJobsSQLite = \"SELECT id FROM jobs WHERE status = 'pending'\"\n"
	if got := ruleLines(t, contracts.RuleDialectDrift, src); !slices.Equal(got, []int{5}) {
		t.Errorf("GOFASTR1414 lines %v, want [5]: the SQLite side lacks next_at, and {jobs} is a subset of {jobs, locks}", got)
	}
}

func TestSweepDataAbsoluteAttemptsReadsTheWholeSetValue(t *testing.T) {
	src := `package main

import "database/sql"

func f(db *sql.DB) {
	db.Exec("UPDATE jobs SET attempts = GREATEST(0, $1) WHERE id = $2")
	db.Exec("UPDATE jobs SET attempts = LEAST($1, max_attempts) WHERE id = $2")
	db.Exec("UPDATE jobs SET status = 'failed' WHERE attempts = $1")
}
`
	if got := ruleLines(t, contracts.RuleAbsoluteAttempts, src); !slices.Equal(got, []int{6, 7}) {
		t.Errorf("GOFASTR1408 lines %v, want [6 7]: a wrapped host value is absolute, max_attempts is not the counter, and a WHERE is no write", got)
	}
}

func TestSweepDataDDLConstraintNeedsIfNotExistsAndTwoFields(t *testing.T) {
	src := `package main

import "github.com/DonaldMurillo/gofastr/core/schema"

type DB interface{ Exec(string, ...any) }

func ensure(db DB) {
	db.Exec("CREATE TABLE seats (id TEXT PRIMARY KEY, code TEXT UNIQUE, name TEXT)")
	db.Exec("CREATE TABLE IF NOT EXISTS pins (id TEXT PRIMARY KEY, pin TEXT UNIQUE, label TEXT)")
}

func SeatFields() []schema.Field {
	return []schema.Field{{Name: "code", Type: schema.String}, {Name: "name", Type: schema.String}}
}

func PinFields() []schema.Field {
	return []schema.Field{{Name: "pin", Type: schema.String}}
}
`
	if got := ruleLines(t, contracts.RuleDDLConstraint, src); got != nil {
		t.Errorf("GOFASTR1416 lines %v, want none: a plain CREATE TABLE runs or fails, and one field name is no entity match", got)
	}
}

func TestSweepDataPerformanceQuietShapes(t *testing.T) {
	src := `package main

import (
	"database/sql"
	"regexp"
)

type items struct{}

func (items) List(id string) []string { return nil }

type users struct{}

func (users) GetByID(id string) string { return id }

func f(db *sql.DB, ids []string, pat string, list items, userRepo users) {
	_ = regexp.MustCompile(pat)
	for _, id := range ids {
		go func() { db.Query("SELECT 1 FROM t WHERE id = $1", id) }()
		_ = list.List(id)
		_ = userRepo.GetByID(id)
		db.Query("SELECT count(*) FROM t")
	}
}
`
	if got := ruleLines(t, contracts.RuleRegexpCompilePerCall, src); got != nil {
		t.Errorf("regexp-per-call lines %v, want none: a variable pattern cannot be hoisted", got)
	}
	if got := ruleLines(t, contracts.RuleQueryInLoop, src); !slices.Equal(got, []int{21}) {
		t.Errorf("query-in-loop lines %v, want [21]: a closure in the loop, a non-repository List and a query without the loop variable stay quiet", got)
	}
}
