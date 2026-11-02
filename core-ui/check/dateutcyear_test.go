package check

import (
	"path/filepath"
	"testing"
)

// dateUTCYearFixture holds the 65d0afa3 pre-fix DatePicker parse and
// month math, the pre-fix localform day(), the setUTCFullYear fix, and
// the shapes that must stay quiet.
const dateUTCYearFixture = `(() => {
  function parse(s) {
    const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(s || '');
    if (!m) return null;
    const t = Date.UTC(+m[1], +m[2] - 1, +m[3]);
    return iso(t) === s ? t : null;
  }
  function shift(s, n) {
    const d = new Date(parse(s));
    const y = d.getUTCFullYear();
    const m = d.getUTCMonth() + n;
    const last = new Date(Date.UTC(y, m + 1, 0)).getUTCDate();
    return iso(Date.UTC(y, m, Math.min(d.getUTCDate(), last)));
  }
  const day = (raw) => {
    const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(raw);
    if (!m) return null;
    const d = new Date(Date.UTC(+m[1], m[2] - 1, +m[3]));
    return d.getUTCFullYear() === +m[1] ? raw : null;
  };
  const local = (y, mo) => new Date(y, mo, 1);
  const old = Date.UTC(99, 0, 1);
  function utc(y, m, d) {
    const t = new Date(0);
    t.setUTCFullYear(y, m, d);
    return t.getTime();
  }
  function pinned(y) {
    const t = new Date(Date.UTC(y, 0, 1));
    t.setUTCFullYear(y);
    return t;
  }
  const epoch = Date.UTC(2024, 0, 1) + new Date(1999, 11, 31).getTime();
  const now = new Date();
  const copy = new Date(now.getTime());
  const s = 'Date.UTC(y, 0, 1) and new Date(y, 0)';
  // Date.UTC(y, 0, 1) in a comment
  const member = moment.Date.UTC(y, 0);
  const mine = myDate.UTC(y, 0) + toDate(y, 0);
  //gofastr:allow(dateutcyear) the year comes from a four-digit field that refuses 0-99
  const allowed = Date.UTC(y, 0, 1);
})();
`

func TestLintDateUTCYear_FiresOnYearParts(t *testing.T) {
	dir := writeRuntimeFixture(t, "date.js", dateUTCYearFixture)
	res, err := LintDateUTCYear(dir)
	if err != nil {
		t.Fatal(err)
	}
	// parse (5), the month math (12, 13), day() (18), the local parts
	// form (21) and a two-digit literal year (22).
	assertLines(t, res, []int{5, 12, 13, 18, 21, 22})
}

// TestLintDateUTCYear_Repo holds the shipped JavaScript to the rule: a
// date built from parts sets its year with setUTCFullYear.
//
//gofastr:test-reads core-ui/runtime/
//gofastr:test-reads *.js
func TestLintDateUTCYear_Repo(t *testing.T) {
	if testing.Short() {
		t.Skip("walks the repo")
	}
	repoRoot, err := findRepoRoot()
	if err != nil {
		t.Skipf("can't locate repo root: %v", err)
	}
	res, err := LintDateUTCYear(runtimeLintRoots(t, filepath.Join(repoRoot, "core-ui", "runtime"))...)
	if err != nil {
		t.Fatal(err)
	}
	if res.HasErrors() {
		t.Errorf("a date built from parts reads years 0-99 as 1900-1999:\n%s", res.Error())
	}
}
