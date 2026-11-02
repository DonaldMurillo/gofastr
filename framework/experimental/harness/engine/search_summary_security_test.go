package engine

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/ids"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/tool/permission"
)

// The permission engine decides a call from its argv summary alone, so
// the summary of a Grep or Glob call has to name where it reads: Grep's
// path and glob, Glob's root. A summary of the pattern only let an
// approval of a search of the working directory, granted at argv scope,
// allow the same pattern over any directory (~/.ssh, ~/.aws), the shape
// the Bash summary closed for cwd and env. And the summary is
// injective: no pattern-only call may spell the summary of a call that
// names a directory, or approving the first would grant the second.
func TestArgvSummaryNamesSearchRoot(t *testing.T) {
	for _, tc := range []struct {
		tool, plain string
		wider       []string
	}{
		{"Grep", `{"pattern":"TODO"}`, []string{
			`{"pattern":"TODO","path":"/home/u/.ssh"}`,
			`{"pattern":"TODO","glob":"id_*"}`,
			`{"pattern":"TODO --path /home/u/.ssh"}`,
		}},
		{"Glob", `{"pattern":"*.pem"}`, []string{
			`{"pattern":"*.pem","root":"/etc"}`,
			`{"pattern":"*.pem --root /etc"}`,
		}},
	} {
		eng := permission.New(nil)
		eng.StrictPermissions = true
		session := ids.NewSessionID()
		plain := FormatArgvSummary(tc.tool, []byte(tc.plain))
		rule, ok := permission.AnswerToRule(tc.tool, plain, control.AnswerPermission{
			Scope: control.ScopeArgvGlob, Decision: control.DecisionAllow,
		})
		if !ok {
			t.Fatal("argv-scope answer minted no rule")
		}
		eng.AddSessionRule(session, rule)
		if got := eng.Evaluate(session, tc.tool, plain, false); got != permission.DecisionAllow {
			t.Fatalf("%s %s: approved call = %v, want allow", tc.tool, tc.plain, got)
		}
		seen := map[string]string{plain: tc.plain}
		for _, in := range tc.wider {
			s := FormatArgvSummary(tc.tool, []byte(in))
			if got := eng.Evaluate(session, tc.tool, s, false); got != permission.DecisionAsk {
				t.Errorf("%s: approving %s also decides %s (summary %q) as %v, want ask", tc.tool, tc.plain, in, s, got)
			}
			if prev, dup := seen[s]; dup {
				t.Errorf("%s: %s and %s share the summary %q", tc.tool, prev, in, s)
			}
			seen[s] = in
		}
	}
}

// A pattern with nothing unsafe in it keeps the summary it had, so an
// "Allow always" rule saved for it still matches.
func TestArgvSummaryKeepsPlainSearchSpelling(t *testing.T) {
	for _, tc := range []struct{ tool, input, want string }{
		{"Grep", `{"pattern":"TODO"}`, "Grep:TODO"},
		{"Glob", `{"pattern":"**/*.go"}`, "Glob:**/*.go"},
		{"Grep", `{"pattern":"a b","path":"src"}`, "Grep:'a b' --path src"},
		{"Glob", `{"pattern":"*.go","root":"/my dir"}`, "Glob:*.go --root '/my dir'"},
	} {
		if got := FormatArgvSummary(tc.tool, []byte(tc.input)); got != tc.want {
			t.Errorf("FormatArgvSummary(%s, %s) = %q, want %q", tc.tool, tc.input, got, tc.want)
		}
	}
}
