package verifycmd

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestVerifyFlagErrorsAreJSON(t *testing.T) {
	for _, args := range [][]string{
		{"--json", "--nope"},
		{"--unknown", "--json"},
		{"--root", "--json"},
		{"--explain", "BOGUS", "--json"},
		{"--json", "--explain", "GOFASTR100"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			output, code := captureVerify(t, args)
			var document struct {
				Error string `json:"error"`
			}
			if err := json.Unmarshal([]byte(output), &document); err != nil || document.Error == "" || code != 2 {
				t.Fatalf("exit=%d, error=%v, stdout=%s", code, err, output)
			}
		})
	}
}
