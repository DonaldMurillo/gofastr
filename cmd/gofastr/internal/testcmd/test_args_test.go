package testcmd

import (
	"slices"
	"strings"
	"testing"
)

// gofastr test read only --run=, --bench=, --timeout= and three bare
// flags, dropped everything else and always ran ./..., so
// `gofastr test --run TestFoo` or `gofastr test ./pkg` ran the whole
// suite (2026-10-27 G7).
func TestTestArgsKeepPackagesAndRefuseUnknown(t *testing.T) {
	for _, tc := range []struct {
		in      []string
		want    []string
		wantErr string
	}{
		{nil, []string{"test", "./...", "-v"}, ""},
		{[]string{"./pkg", "./other/..."}, []string{"test", "./pkg", "./other/...", "-v"}, ""},
		{[]string{"--run", "TestFoo", "./pkg"}, []string{"test", "./pkg", "-v", "-run", "TestFoo"}, ""},
		{[]string{"--run=TestFoo", "--race", "--timeout", "5m"}, []string{"test", "./...", "-v", "-run", "TestFoo", "-race", "-timeout", "5m"}, ""},
		{[]string{"--bench=.", "--cover", "--short"}, []string{"test", "./...", "-v", "-bench", ".", "-cover", "-short"}, ""},
		{[]string{"--count=1"}, nil, "unknown flag --count"},
		{[]string{"-run", "X"}, nil, "unknown flag -run"},
		{[]string{"--run"}, nil, "--run needs a value"},
		{[]string{"--run", "--race"}, nil, "--run needs a value"},
	} {
		got, err := goTestArgs(tc.in)
		if tc.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("%q: err = %v, want %q", tc.in, err, tc.wantErr)
			}
			continue
		}
		if err != nil || !slices.Equal(got, tc.want) {
			t.Errorf("%q: got %q, %v; want %q", tc.in, got, err, tc.want)
		}
	}
}
