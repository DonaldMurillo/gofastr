package runtime

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/core/i18n"
)

func TestRegisterStringsRefusesAZeroPhrase(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("RegisterStrings took a zero Phrase")
		}
	}()
	RegisterStrings(i18n.Phrase{})
}
