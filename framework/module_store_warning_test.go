package framework

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/fanout"
)

func TestMemoryModulesWarnWithFanout(t *testing.T) {
	for _, shared := range []bool{false, true} {
		var output bytes.Buffer
		options := []AppOption{WithFanout(fanout.NewInProcess()), WithLogger(slog.New(slog.NewTextHandler(&output, nil)))}
		if shared {
			database := openSQLiteMem(t)
			t.Cleanup(func() { _ = database.Close() })
			options = append(options, WithDB(database))
		}
		NewApp(options...)
		warned := strings.Contains(output.String(), "module toggles remain local")
		if warned == shared {
			t.Fatalf("shared=%v, warning=%v: %s", shared, warned, output.String())
		}
	}
}
