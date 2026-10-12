// Package inflect holds the repo's one English singularizer for entity
// display names, shared by the blueprint generator and the admin battery.
package inflect

import "strings"

// Singular turns a plural English noun into its singular form with a few
// suffix rules: categories → category, statuses → status, boxes → box,
// batches → batch, users → user. Words that already read as singular
// (address, status, analysis) come back unchanged. It is naive by design:
// entity names are identifiers, and an irregular plural (people) is left as
// it is.
func Singular(s string) string {
	switch {
	case strings.HasSuffix(s, "ies") && len(s) > 3:
		return s[:len(s)-3] + "y"
	case strings.HasSuffix(s, "sses"),
		strings.HasSuffix(s, "uses"),
		strings.HasSuffix(s, "xes"),
		strings.HasSuffix(s, "ches"),
		strings.HasSuffix(s, "shes"):
		return s[:len(s)-2]
	case strings.HasSuffix(s, "ss"),
		strings.HasSuffix(s, "us"),
		strings.HasSuffix(s, "is"):
		return s
	case strings.HasSuffix(s, "s"):
		return s[:len(s)-1]
	}
	return s
}
