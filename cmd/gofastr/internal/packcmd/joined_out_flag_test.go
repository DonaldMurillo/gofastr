package packcmd

import "testing"

func TestPackAcceptsJoinedOutFlag(t *testing.T) {
	for _, flag := range []string{"--out=x.yml", "-o=x.yml"} {
		opts, err := parsePackArgs([]string{".", flag})
		if err != nil || opts.out != "x.yml" {
			t.Errorf("parsePackArgs(%q) = %+v, %v; want out=x.yml", flag, opts, err)
		}
	}
	for _, flag := range []string{"--out=", "-o="} {
		if _, err := parsePackArgs([]string{flag}); err == nil {
			t.Errorf("parsePackArgs(%q) accepted an empty file name", flag)
		}
	}
}
