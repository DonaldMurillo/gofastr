package crud

import (
	"fmt"
	"log"

	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

func logf(format string, args ...any) {
	log.Print(textsafe.ScrubControlBytes(fmt.Sprintf(format, args...)))
}
