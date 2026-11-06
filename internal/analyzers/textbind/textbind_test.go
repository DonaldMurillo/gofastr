package textbind_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/allow"
	"github.com/DonaldMurillo/gofastr/internal/analyzers/textbind"
)

func TestTextBind(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), allow.Guard(textbind.Analyzer), "filter", "a", "fields", "fielduse", "helperbind", "grow")
}
