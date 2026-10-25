package generatecmd

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appgen"
)

func TestGeneratorHelpListsEveryGenerator(t *testing.T) {
	help := generatorHelp()
	for _, g := range appgen.Generators {
		if !strings.Contains(help, "gofastr generate "+g.Usage) {
			t.Errorf("help missing %q", g.Name)
		}
	}
}
