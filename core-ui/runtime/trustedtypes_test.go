package runtime

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/middleware"
)

// The kernel's policy name is the one the default CSP allows.
func TestRuntimePolicyIsTheOneTheCSPAllows(t *testing.T) {
	want := "createPolicy('" + middleware.TrustedTypesPolicy + "'"
	core, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	embed, err := EmbedJS()
	if err != nil {
		t.Fatal(err)
	}
	for name, src := range map[string]string{"runtime.js": core, "embed runtime": embed} {
		if strings.Count(src, "createPolicy(") != 1 || !strings.Contains(src, want) {
			t.Errorf("%s does not create exactly one Trusted Types policy, %s", name, want)
		}
	}
}
