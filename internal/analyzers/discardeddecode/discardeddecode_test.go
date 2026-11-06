package discardeddecode_test

import (
	"encoding/json"
	"testing"
	"time"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/discardeddecode"
)

func TestDiscardedDecodeFiresOnParseDrops(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), discardeddecode.Analyzer, "a")
}

// TestDiscardedDecodeEncodeArm: a JSON-RPC envelope encoded with its
// error dropped, when it can fail to encode (3516c4d9b).
func TestDiscardedDecodeEncodeArm(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), discardeddecode.Analyzer, "encode")
}

func TestReviewMapKeyEncoding(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), discardeddecode.Analyzer, "review")
}

type pointerTextKey struct{}

func (*pointerTextKey) MarshalText() ([]byte, error) { return []byte("key"), nil }

type wrongTextKey struct{}

func (wrongTextKey) MarshalText() string { return "key" }

type wrongTextResultKey struct{}

func (wrongTextResultKey) MarshalText() ([]byte, bool) { return nil, false }

func TestReviewJSONMapKeyContract(t *testing.T) {
	for _, test := range []struct {
		name     string
		value    any
		rejected bool
	}{
		{"interface", map[any]string{"x": "y"}, false},
		{"pointer-only", map[pointerTextKey]string{{}: "y"}, true},
		{"wrong-signature", map[wrongTextKey]string{{}: "y"}, true},
		{"wrong-result", map[wrongTextResultKey]string{{}: "y"}, true},
		{"empty-float", map[float64]string{}, false},
		{"empty-interface", map[any]string{}, false},
		{"pointer", map[*pointerTextKey]string{new(pointerTextKey): "y"}, false},
		{"text-marshaler", map[time.Time]string{{}: "y"}, false},
		{"string", map[string]string{}, false},
		{"integer", map[int]string{1: "y"}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			encoded, err := json.Marshal(test.value)
			t.Logf("json.Marshal(%T) = %s, error = %v", test.value, encoded, err)
			if (err != nil) != test.rejected {
				t.Fatalf("json.Marshal(%T) error = %v, want rejected = %v", test.value, err, test.rejected)
			}
		})
	}
}

// TestDiscardedDecodeRepoDecodersFire: handler.UnmarshalStrict /
// DecodeStrict, jsonexact.Unmarshal and the yaml.v3 Decoder are the same
// one-error parse family as json.Unmarshal.
func TestDiscardedDecodeRepoDecodersFire(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), discardeddecode.Analyzer, "github.com/DonaldMurillo/gofastr/repodecoders")
}
