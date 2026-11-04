package desktopcmd

import "testing"

func TestReleaseOutputNeedsValue(t *testing.T) {
	for _, args := range [][]string{{"-o"}, {"-o", "--help"}, {"--out", "--key=x"}} {
		for _, verb := range []string{"keygen", "feed"} {
			t.Run(verb+args[0], func(t *testing.T) {
				_, code := verbOutput(t, func() {
					if verb == "keygen" {
						parseDesktopKeygenFlags(args)
					} else {
						parseDesktopFeedFlags(args)
					}
				})
				if code != 1 {
					t.Errorf("%s %v exited %d, want refusal", verb, args, code)
				}
			})
		}
	}
}
