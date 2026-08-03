package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/contracts"
	_ "github.com/DonaldMurillo/gofastr/framework/contracts/analyzers"
)

// writeContractFixture lays down a tree with one error-severity finding
// (GOFASTR1002 — a `:id` route pattern).
func writeContractFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"go.mod": "module example.com/app\n\ngo 1.26\n",
		"main.go": `package main

import "github.com/DonaldMurillo/gofastr/core/router"

func main() {
	r := router.New()
	r.Handle("GET", "/users/:id", nil)
}
`,
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestBuildContractsGateBlocksAnErrorFinding(t *testing.T) {
	dir := writeContractFixture(t)
	if buildContractsGate(dir) {
		t.Error("the build gate let an error-severity finding through")
	}
}

// `gofastr verify --baseline-write` is the documented way to adopt
// contracts on an existing app: accept what is there, fail on what is
// added. The build gate has to honour the same file, or adopting the
// baseline fixes `verify` and leaves `build` permanently red — and the
// only way out a user finds is --no-contracts, which turns everything off.
func TestBuildContractsGateHonoursTheBaseline(t *testing.T) {
	dir := writeContractFixture(t)

	cfg, err := contracts.LoadConfig(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	pass, err := contracts.NewPass(dir, cfg)
	if err != nil {
		t.Fatal(err)
	}
	report, err := contracts.Run(pass, contracts.RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	b := contracts.NewBaseline(report, time.Now(), "test")
	if b.Total() == 0 {
		t.Fatal("fixture produced no gating findings to baseline")
	}
	if err := contracts.WriteBaseline(filepath.Join(dir, contracts.BaselineFileName), b); err != nil {
		t.Fatal(err)
	}

	if !buildContractsGate(dir) {
		t.Error("the build gate ignored a recorded baseline; adopting one fixes verify and leaves build red")
	}
}

// A baseline accepts what was there, not what comes after.
func TestBuildContractsGateStillBlocksNewFindings(t *testing.T) {
	dir := writeContractFixture(t)

	cfg, _ := contracts.LoadConfig(dir, "")
	pass, _ := contracts.NewPass(dir, cfg)
	report, _ := contracts.Run(pass, contracts.RunOptions{})
	b := contracts.NewBaseline(report, time.Now(), "test")
	if err := contracts.WriteBaseline(filepath.Join(dir, contracts.BaselineFileName), b); err != nil {
		t.Fatal(err)
	}

	// A second violating file the baseline never saw.
	extra := `package main

import "github.com/DonaldMurillo/gofastr/core/router"

func More(r *router.Router) { r.Handle("GET", "/orders/:oid", nil) }
`
	if err := os.WriteFile(filepath.Join(dir, "more.go"), []byte(extra), 0o644); err != nil {
		t.Fatal(err)
	}
	if buildContractsGate(dir) {
		t.Error("a finding the baseline never recorded was let through")
	}
}
