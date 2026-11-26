package runtime_test

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/runtime"
)

func TestEnvelopeResolvesTypedGroupKeys(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("running the served envelope module requires Node.js")
	}
	source, ok := runtime.Module("envelope")
	if !ok {
		t.Fatal("missing envelope module")
	}
	const setup = `
global.document = {
  querySelector: () => null,
  createElement: () => ({content: {querySelector: () => null}}),
};
global.window = {
  __gofastr_routes: [{path: '/projects/:project:int/detail'}],
  __gofastr: {
    _navHooks: {}, _navShown: true, _docSeed: () => '',
    _unlocalize: (p) => p, _routeSeg: (s) => s,
    _routeMatch: (pattern, segment) => pattern === ':project:int' ? /^\d+$/.test(segment) : pattern === segment,
  },
};`
	const assert = `
const {subst} = window.__gofastr._navHooks.envelope;
for (const param of ['{project:int}', ':project:int', '{project}']) {
  const got = subst('g:/projects/' + param + '/:project#aside', '/projects/42/detail');
  if (got !== 'g:/projects/42/:project#aside') {
    process.stderr.write('unresolved group address: ' + got + '\n');
    process.exitCode = 1;
  }
}`
	cmd := exec.CommandContext(t.Context(), node)
	cmd.Stdin = strings.NewReader(setup + "\n" + source + "\n" + assert)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("envelope substitution: %v\n%s", err, output)
	}
}
