package sinkmatch

import (
	. "fmt"
	"strconv"
	"strings"
)

func dotImportedFprintf(m map[int]string) string {
	var sb strings.Builder
	for k := range m {
		Fprintf(&sb, "%s", strconv.Itoa(k)) // want `writing to output while ranging a map`
	}
	return sb.String()
}
