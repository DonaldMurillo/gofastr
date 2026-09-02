// Package guardmatch pins the len==1 exemption: it must bind the ranged
// expression to the guarded expression as the SAME variable, not as the
// same source text. Shadowing keeps types.ExprString identical while the
// ranged map is a different, unbounded one.
package guardmatch

import (
	"strconv"
	"strings"
)

func shadowedGuard(singleton, big map[int]string) string {
	var sb strings.Builder
	if len(singleton) == 1 {
		singleton := big
		for k := range singleton {
			sb.WriteString(strconv.Itoa(k)) // want `writing to output while ranging a map`
		}
	}
	return sb.String()
}
