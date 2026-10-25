package appspec

import (
	"strings"
	"testing"
)

// A primary_key the runtime would refuse at boot is refused when the spec
// loads, naming the problem.
func TestPrimaryKeyRefusedAtLoad(t *testing.T) {
	for name, tc := range map[string]struct{ key, want string }{
		"empty":      {"[]", "non-empty list"},
		"scalar":     {"student_id", "non-empty list"},
		"undeclared": {"[student_id, term]", `"term" is not a declared field`},
		"repeated":   {"[student_id, student_id]", `"student_id" twice`},
		"unkeyable":  {"[student_id, score]", `"score" has a type`},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := loadBPWithEntity(t, `  - name: enrollments
    primary_key: `+tc.key+`
    fields:
      - name: student_id
        type: string
      - name: score
        type: float
`)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("primary_key %s: err = %v; want it to mention %q", tc.key, err, tc.want)
			}
		})
	}
}
