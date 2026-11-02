package fixedtmp_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/fixedtmp"
)

func TestFixedTmpFiresOnPredictableTempPaths(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), fixedtmp.Analyzer, "a")
}

// TestFixedTmpReadsEveryRootSpelling: "/tmp/"+name, Join("/tmp/x", …),
// fmt.Sprintf("/tmp/…%d", pid) and exec.Cmd{Dir: "/tmp/…"} are the same
// predictable shared-root path as Join(os.TempDir(), …).
func TestFixedTmpReadsEveryRootSpelling(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), fixedtmp.Analyzer, "spellings")
}
