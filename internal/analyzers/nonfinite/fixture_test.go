package nonfinite_test

import (
	"math"
	"testing"

	fixture "github.com/DonaldMurillo/gofastr/internal/analyzers/nonfinite/testdata/src/a"
)

func TestElementStoreRejectsBeforeMutation(t *testing.T) {
	for _, input := range []string{"NaN", "+Inf", "-Inf", "invalid", "1.5"} {
		t.Run(input, func(t *testing.T) {
			out := map[string]float64{"rate": 7}
			err := fixture.ElementStoreGated(map[string]string{"RATE": input}, out)
			if input == "1.5" {
				if err != nil || out["rate"] != 1.5 {
					t.Fatalf("finite input: rate=%v err=%v", out["rate"], err)
				}
				return
			}
			if err == nil || out["rate"] != 7 || math.IsNaN(out["rate"]) {
				t.Fatalf("rejected input mutated output: rate=%v err=%v", out["rate"], err)
			}
		})
	}
}
