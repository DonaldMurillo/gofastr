package discardeddecode_test

import (
	"testing"

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

// TestDiscardedDecodeRepoDecodersFire: handler.UnmarshalStrict /
// DecodeStrict, jsonexact.Unmarshal and the yaml.v3 Decoder are the same
// one-error parse family as json.Unmarshal.
func TestDiscardedDecodeRepoDecodersFire(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), discardeddecode.Analyzer, "github.com/DonaldMurillo/gofastr/repodecoders")
}
