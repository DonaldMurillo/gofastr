package crud

import (
	"fmt"
	"log"

	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

// logf writes one crud log line with its control bytes percent-encoded.
// The errors these lines carry come from drivers and hooks, and a driver
// quotes the request's own value back (Postgres names the invalid key it
// was handed), so a raw %v let a request forge or rewrite log lines.
func logf(format string, args ...any) {
	log.Print(textsafe.ScrubControlBytes(fmt.Sprintf(format, args...)))
}
