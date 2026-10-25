package loopback

import (
	"strings"
	"testing"
)

// A wildcard bind must still accept the Host a browser actually sends.
//
// net.Listen(":8090") reports "[::]:8090", and pinning the guard to that
// literal made every request 403, including from the URL the tool prints, and
// from the `--addr=:8090` invocation the theming docs give as an example. The
// tool was unusable in its own documented form.
func TestLoopbackGuardsAcceptAWildcardBind(t *testing.T) {
	for _, addr := range []string{"[::]:8090", "0.0.0.0:8090", ":8090"} {
		hosts, origins := LoopbackGuards(addr)
		joined := strings.Join(hosts, " ")
		for _, want := range []string{"localhost:8090", "127.0.0.1:8090"} {
			if !strings.Contains(joined, want) {
				t.Errorf("loopbackGuards(%q) hosts = %v, missing %q", addr, hosts, want)
			}
		}
		if !HostAllowed("localhost:8090", hosts) {
			t.Errorf("loopbackGuards(%q): a browser's Host would be refused", addr)
		}
		if len(origins) == 0 {
			t.Errorf("loopbackGuards(%q) produced no origins", addr)
		}
	}

	// An explicit non-loopback bind stays pinned to exactly what was asked for.
	hosts, _ := LoopbackGuards("192.168.1.10:8090")
	if len(hosts) != 1 || hosts[0] != "192.168.1.10:8090" {
		t.Errorf("an explicit host must stay pinned, got %v", hosts)
	}
}
