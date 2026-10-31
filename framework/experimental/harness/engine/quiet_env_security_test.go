package engine

import (
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/ids"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/tool"
)

// Quiet mode decides a Bash call from its argv summary, so a call's env
// (GIT_EXTERNAL_DIFF, PATH, LD_PRELOAD) and cwd (whose .git/config git
// reads) have to be in it: either one turns a read-only command into
// one that runs anything. A quiet-listed command carrying them is asked
// about, not run.
func TestQuietModeAsksForBashEnvAndCwd(t *testing.T) {
	for _, tc := range []struct {
		name, input string
		runs        bool
	}{
		{"plain", `{"cmd":"git diff"}`, true},
		{"empty env", `{"cmd":"git diff","env":{}}`, true},
		{"env", `{"cmd":"git diff","env":{"GIT_EXTERNAL_DIFF":"sh -c id"}}`, false},
		{"cwd", `{"cmd":"git diff","cwd":"/tmp/elsewhere"}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			call := tool.ToolCall{ID: ids.NewCallID(), Name: "Bash", Input: []byte(tc.input)}
			res, invoked := runPermCall(t, call, nil, 20*time.Millisecond, nil)
			if tc.runs {
				if !invoked {
					t.Fatalf("%s: quiet-listed call was not allowed: %+v", tc.input, res)
				}
				return
			}
			assertDenied(t, res, invoked)
		})
	}
}

// The Bash summary spells cwd and env the way a shell would run them,
// env keys sorted and every unsafe word single-quoted, so it is
// deterministic and a rule written for the bare command never matches.
func TestBashSummaryShowsEnvAndCwd(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{`{"cmd":"git diff"}`, `git diff`},
		{`{"cmd":"git diff","cwd":"/tmp/it's here"}`, `cd '/tmp/it'\''s here' && git diff`},
		{`{"cmd":"ls","env":{"B":"x y","A":"1"}}`, `env A=1 'B=x y' ls`},
		{`{"cmd":"ls","cwd":"/r","env":{"PATH":""}}`, `cd /r && env PATH= ls`},
		{`{"cmd":"ls","env":{"J":"j","C":"c","H":"h","A":"a","F":"f","B":"b","I":"i","E":"e","G":"g","D":"d"}}`,
			`env A=a B=b C=c D=d E=e F=f G=g H=h I=i J=j ls`},
	} {
		if got := FormatArgvSummary("Bash", []byte(tc.input)); got != tc.want {
			t.Errorf("FormatArgvSummary(%s) = %q, want %q", tc.input, got, tc.want)
		}
	}
}
