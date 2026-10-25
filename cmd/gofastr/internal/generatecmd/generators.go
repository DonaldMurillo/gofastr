package generatecmd

import (
	"fmt"
	"strings"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appgen"
)

func generatorNames() string {
	names := make([]string, 0, len(appgen.Generators))
	for _, g := range appgen.Generators {
		names = append(names, g.Name)
	}
	return strings.Join(names, ", ")
}

// generatorHelp renders the registry for `gofastr generate --help`,
// grouped by kind.
func generatorHelp() string {
	var b strings.Builder
	for _, kind := range []appgen.GeneratorKind{appgen.ScaffoldKind, appgen.DerivedKind} {
		if kind == appgen.ScaffoldKind {
			b.WriteString("Scaffold generators write code you own, once (a second run needs --force):\n")
		} else {
			b.WriteString("\nDerived generators rebuild DO NOT EDIT code from a source on every run:\n")
		}
		for _, g := range appgen.Generators {
			if g.Kind != kind {
				continue
			}
			fmt.Fprintf(&b, "  gofastr generate %s\n      %s\n", g.Usage, g.Summary)
		}
	}
	return b.String()
}
