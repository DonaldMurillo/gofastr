package analyzers

import (
	"fmt"
	"strings"

	"github.com/DonaldMurillo/gofastr/framework/contracts"
)

func checkFlexReverse(rel string, lineNo int, scan, orig string) []contracts.Diagnostic {
	scan = strings.ToLower(scan)
	if !strings.Contains(scan, "-reverse") {
		return nil
	}
	var out []contracts.Diagnostic
	report := func(prop, value string) {
		prop = strings.ToLower(prop)
		if prop != "flex-direction" && prop != "flex-flow" {
			return
		}
		for _, tok := range cssTokens(strings.ToLower(value)) {
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
			return
		}
	}
	if strings.Contains(scan, ":") {
		for _, m := range reBareDeclCSS.FindAllStringSubmatch(scan, -1) {
			report(m[1], m[2])
		}
	}
	if strings.Contains(scan, `",`) {
		for _, m := range reBareDeclPair.FindAllStringSubmatch(scan, -1) {
			report(m[1], m[2])
		}
	}
	return out
}
