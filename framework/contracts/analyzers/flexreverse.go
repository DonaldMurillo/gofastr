package analyzers

import (
	"fmt"
	"strings"

	"github.com/DonaldMurillo/gofastr/framework/contracts"
)

func checkFlexReverse(rel string, lineNo int, decls []cssDeclaration, orig string) []contracts.Diagnostic {
	var out []contracts.Diagnostic
	for _, decl := range decls {
		prop := decl.property
		if prop != "flex-direction" && prop != "flex-flow" {
			continue
		}
		for _, tok := range cssTokens(strings.ToLower(decl.value)) {
			if tok != "row-reverse" && tok != "column-reverse" {
				continue
			}
			out = append(out, contracts.Diagnostic{
				RuleID: contracts.RuleFlexReverse,
				File:   rel,
				Line:   lineNo,
				Message: fmt.Sprintf("%s: %s paints the children against their document order, so a screen reader, Tab and a radio group's arrows move the other way from what the reader sees; render them in reading order and lay them out forward",
					prop, tok),
				Snippet: strings.TrimSpace(orig),
			})
			break
		}
	}
	return out
}
