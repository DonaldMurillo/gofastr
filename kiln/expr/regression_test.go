package expr

import (
	"math"
	"testing"
)

func TestExpressionRegressions(t *testing.T) {
	for _, tc := range []struct {
		src  string
		want any
	}{
		{"1 + 2", int64(3)},
		{"9007199254740993 == 9007199254740992.0", false},
		{"9007199254740992.0 < 9007199254740993", true},
		{"-9223372036854775808", int64(math.MinInt64)},
		{"abs(x)", int64(5)},
		{"+'text'", nil},
		{"9223372036854775807 + 1", nil},
		{"(-9223372036854775807 - 1) - 1", nil},
		{"9223372036854775807 * 2", nil},
		{"-(-9223372036854775807 - 1)", nil},
		{"(-9223372036854775807 - 1) / -1", nil},
	} {
		t.Run(tc.src, func(t *testing.T) {
			e, err := Compile(tc.src)
			if err != nil {
				t.Fatal(err)
			}
			got, err := e.Eval(MapScope{"x": -5}, nil)
			if tc.want == nil {
				if err == nil {
					t.Fatalf("got %v, want evaluation error", got)
				}
			} else if err != nil || got != tc.want {
				t.Fatalf("got (%v, %v), want %v", got, err, tc.want)
			}
		})
	}
}
