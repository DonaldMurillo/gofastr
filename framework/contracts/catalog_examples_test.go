package contracts

import (
	"strings"
	"testing"
)

// TestPollExamplesNamePollSrc: a catalog Good example that shows
// data-cui-poll must carry data-cui-poll-src, the attribute poll.js
// actually reads (wireOne returns early without it), and must not
// invent attributes the runtime has no reader for. The bespoke
// EventSource rule once offered `data-cui-poll="5s"
// data-cui-island="orders-count"`: markup that never polls.
func TestPollExamplesNamePollSrc(t *testing.T) {
	for _, r := range AllRules() {
		for i, ex := range r.Examples {
			if !strings.Contains(ex.Good, "data-cui-poll") {
				continue
			}
			if !strings.Contains(ex.Good, "data-cui-poll-src=") {
				t.Errorf("%s example %d: Good shows data-cui-poll with no data-cui-poll-src, markup that never polls: %s", r.ID, i, ex.Good)
			}
			if strings.Contains(ex.Good, "data-cui-island") {
				t.Errorf("%s example %d: Good names data-cui-island, which nothing reads: %s", r.ID, i, ex.Good)
			}
		}
	}
}
