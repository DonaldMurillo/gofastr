package query

import "testing"

func TestMatchingWithoutProjectionSelectsAll(t *testing.T) {
	for _, got := range []string{
		matchingSQL(Update("records").Matching()),
		matchingSQL(Delete("records").Matching()),
	} {
		if got != "SELECT * FROM records" {
			t.Errorf("query = %q", got)
		}
	}
}
func matchingSQL(sql string, _ []any) string { return sql }
