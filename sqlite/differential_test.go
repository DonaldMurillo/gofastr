package sqlite

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"

	// Registers modernc.org/sqlite as "sqlite3" with the DSN defaults every
	// GoFastr app ships, foreign_keys(1) included. The whole point of this
	// file is to compare against what production actually runs, so it opens
	// the driver through the same registration production does.
	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

// This file is the differential harness: the same SQL script executed against
// both engines in this repo, with every statement's accept/refuse outcome and
// every probe's rows compared.
//
// It exists because inspection kept missing things. Four consecutive
// hand-review passes over the foreign-key work each declared it complete;
// a one-off script that ran both engines side by side then found four
// enforcement paths that had never been wired — DROP TABLE among them, which
// silently orphaned every child row forever. Nobody had to think of DROP
// TABLE. The harness found it because the harness does not think, it compares.
//
// The rule that keeps it honest is the same in both directions. A scenario
// with wantDiff == "" must agree on every statement and every probe. A
// scenario with wantDiff set must DISAGREE — if a documented divergence ever
// stops diverging, this fails too, so the allowlist cannot rot into a list of
// excuses for behaviour that has since been fixed.

type diffScenario struct {
	name string
	// steps run in order on both engines. Each step's outcome (accepted or
	// refused) is compared; the error text is not, since two independent
	// implementations have no reason to word a refusal alike.
	steps []string
	// probes run after the steps on both engines; their rows are compared.
	probes []string
	// wantDiff, when non-empty, marks a divergence that is chosen rather than
	// accidental, and says why. The scenario then FAILS if the engines agree.
	wantDiff string
}

// transcript is what one engine did with one scenario: the compared lines,
// and — kept beside them, never compared — the refusal text, because when the
// two disagree the first question is always "refused for which reason".
type transcript struct {
	lines []string
	notes map[int]string
}

func (tr *transcript) add(line string) { tr.lines = append(tr.lines, line) }

func (tr *transcript) note(err error) {
	if err == nil {
		return
	}
	if tr.notes == nil {
		tr.notes = map[int]string{}
	}
	tr.notes[len(tr.lines)-1] = err.Error()
}

// runDiff executes one scenario against one open database and returns a
// canonical transcript: one line per step (ok / refused) and one per probe row.
// Error text is deliberately excluded from the compared lines — two
// independent implementations have no reason to word a refusal alike.
func runDiff(t *testing.T, db *sql.DB, sc diffScenario) *transcript {
	t.Helper()
	tr := &transcript{lines: make([]string, 0, len(sc.steps)+len(sc.probes))}
	out := tr
	for i, step := range sc.steps {
		_, err := db.Exec(step)
		if err != nil {
			out.add(fmt.Sprintf("step %d: refused", i))
			out.note(err)
			continue
		}
		out.add(fmt.Sprintf("step %d: ok", i))
	}
	for i, probe := range sc.probes {
		rows, err := db.Query(probe)
		if err != nil {
			out.add(fmt.Sprintf("probe %d: refused", i))
			out.note(err)
			continue
		}
		cols, err := rows.Columns()
		if err != nil {
			_ = rows.Close()
			out.add(fmt.Sprintf("probe %d: refused", i))
			continue
		}
		n := 0
		for rows.Next() {
			cells := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for j := range cells {
				ptrs[j] = &cells[j]
			}
			if err := rows.Scan(ptrs...); err != nil {
				out.add(fmt.Sprintf("probe %d: scan refused", i))
				break
			}
			rendered := make([]string, len(cells))
			for j, c := range cells {
				rendered[j] = renderCell(c)
			}
			out.add(fmt.Sprintf("probe %d row %d: %s", i, n, strings.Join(rendered, "|")))
			n++
		}
		_ = rows.Close()
		if err := rows.Err(); err != nil {
			out.add(fmt.Sprintf("probe %d: iteration refused", i))
		}
		out.add(fmt.Sprintf("probe %d: %d rows", i, n))
	}
	return tr
}

// renderCell normalises the two drivers' Go types onto one spelling. A value
// that reads back as int64 from one engine and []byte from the other is a
// driver-surface difference, not a semantic one, and this harness is about
// semantics; the storage-class tests elsewhere in this package cover the
// other question.
func renderCell(c any) string {
	switch v := c.(type) {
	case nil:
		return "NULL"
	case []byte:
		return string(v)
	case int64:
		return fmt.Sprintf("%d", v)
	case float64:
		return strings.TrimSuffix(fmt.Sprintf("%.6f", v), ".000000")
	case string:
		return v
	case bool:
		if v {
			return "1"
		}
		return "0"
	default:
		return fmt.Sprintf("%v", v)
	}
}

func openDiffDB(t *testing.T, driverName string) *sql.DB {
	t.Helper()
	db, err := sql.Open(driverName, ":memory:")
	if err != nil {
		t.Fatalf("open %s: %v", driverName, err)
	}
	// modernc gives every pooled connection its own private :memory: database,
	// so a script spread across connections would run against empty schemas.
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestEnginesAgreeOnForeignKeys(t *testing.T) {
	for _, sc := range fkDiffScenarios {
		t.Run(sc.name, func(t *testing.T) {
			house := runDiff(t, openDiffDB(t, "gofastr-sqlite"), sc)
			real := runDiff(t, openDiffDB(t, "sqlite3"), sc)
			assertScenarioRan(t, sc, real)
			compareTranscripts(t, sc, house, real)
		})
	}
}

// assertScenarioRan is the anti-vacuity check, and it is not optional: two
// engines that both refuse EVERY statement agree perfectly, so a scenario with
// a typo in its first CREATE TABLE passes while testing nothing at all. It
// runs against the reference engine, which is the one entitled to define
// whether the script is valid SQL.
func assertScenarioRan(t *testing.T, sc diffScenario, real *transcript) {
	t.Helper()
	steps := 0
	for _, line := range real.lines {
		if strings.HasPrefix(line, "step ") && strings.HasSuffix(line, ": ok") {
			steps++
		}
	}
	if steps == 0 {
		t.Fatalf("no statement in this scenario succeeded on the reference engine, so it establishes no state "+
			"and comparing the two engines proves nothing.\ntranscript:\n%s\nrefusals: %v",
			strings.Join(real.lines, "\n"), real.notes)
	}
	if len(sc.probes) == 0 {
		return
	}
	for _, line := range real.lines {
		if strings.HasPrefix(line, "probe ") && strings.HasSuffix(line, " rows") {
			return
		}
	}
	t.Fatalf("every probe was refused on the reference engine, so no rows were ever compared.\ntranscript:\n%s",
		strings.Join(real.lines, "\n"))
}

func compareTranscripts(t *testing.T, sc diffScenario, house, real *transcript) {
	t.Helper()
	same := strings.Join(house.lines, "\n") == strings.Join(real.lines, "\n")
	if sc.wantDiff != "" {
		if same {
			t.Errorf("scenario is on the documented-divergence list (%s) but the engines now AGREE.\n"+
				"Either the divergence was fixed — delete the wantDiff and keep the scenario as a "+
				"parity test — or the scenario stopped exercising it.\ntranscript:\n%s",
				sc.wantDiff, strings.Join(house.lines, "\n"))
		}
		return
	}
	if same {
		return
	}
	t.Errorf("engines disagree and no divergence is documented.\n%s", diffTranscripts(house, real))
}

func diffTranscripts(house, real *transcript) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("%-46s | %s\n", "gofastr/sqlite", "modernc (what apps ship)"))
	n := len(house.lines)
	if len(real.lines) > n {
		n = len(real.lines)
	}
	for i := 0; i < n; i++ {
		h, r := "", ""
		if i < len(house.lines) {
			h = house.lines[i]
		}
		if i < len(real.lines) {
			r = real.lines[i]
		}
		mark := "  "
		if h != r {
			mark = "!!"
		}
		b.WriteString(fmt.Sprintf("%s %-44s | %s\n", mark, h, r))
		if note := house.notes[i]; note != "" {
			b.WriteString(fmt.Sprintf("     gofastr/sqlite said: %s\n", note))
		}
		if note := real.notes[i]; note != "" {
			b.WriteString(fmt.Sprintf("     modernc said:        %s\n", note))
		}
	}
	return b.String()
}
