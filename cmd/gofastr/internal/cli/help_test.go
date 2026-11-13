package cli_test

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/cli"
)

func TestHelpFlagBoundaries(t *testing.T) {
	for _, fixture := range []struct {
		name     string
		args     []string
		wantHelp bool
		wantAny  bool
	}{
		{name: "empty"},
		{name: "long", args: []string{"query", "--help"}, wantHelp: true, wantAny: true},
		{name: "short", args: []string{"query", "-h"}, wantHelp: true, wantAny: true},
		{name: "before-end", args: []string{"--help", "--", "needle"}, wantHelp: true, wantAny: true},
		{name: "literal-long", args: []string{"query", "--", "--help"}, wantAny: true},
		{name: "literal-short", args: []string{"query", "--", "-h"}, wantAny: true},
		{name: "not-help", args: []string{"query", "--helpful"}},
		{name: "not-end", args: []string{"query", "--=value", "--help"}, wantHelp: true, wantAny: true},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			if got := cli.HasHelpFlagBeforeTerminator(fixture.args); got != fixture.wantHelp {
				t.Errorf("bounded help = %v, want %v", got, fixture.wantHelp)
			}
			if got := cli.HasHelpFlag(fixture.args); got != fixture.wantAny {
				t.Errorf("existing help scan = %v, want %v", got, fixture.wantAny)
			}
		})
	}
}
