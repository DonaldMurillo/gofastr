package headless

import (
	"regexp"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

// RepeaterProps.Action is documented as "the same-origin URL the add
// and remove operations go to"; it was checked and then dropped, the
// controls posting to Island.Endpoint instead.
func TestRepeaterActionIsWhereOperationsGo(t *testing.T) {
	h := string(Repeater(RepeaterProps{Name: "guests", Label: "Guests",
		Items:  []RepeaterItem{{Fields: []render.HTML{render.HTML(`<input name="g">`)}}},
		Action: "/island/guests/ops",
		Island: Island{Endpoint: "/island/guests", Signal: "guests"}}, nil))
	rpcs := regexp.MustCompile(`data-cui-rpc="([^"]*)"`).FindAllStringSubmatch(h, -1)
	if len(rpcs) != 2 {
		t.Fatalf("want the remove and the add carrying the RPC contract, got %v", rpcs)
	}
	for _, m := range rpcs {
		if !strings.HasPrefix(m[1], "/island/guests/ops?") {
			t.Fatalf("an operation posts to %q, not the Action", m[1])
		}
	}
	if !strings.Contains(h, `data-cui-rpc-signal="guests"`) {
		t.Fatalf("the answer no longer lands in the Island's signal: %s", h)
	}
}
