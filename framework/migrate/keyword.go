package migrate

import (
	"strings"

	"github.com/DonaldMurillo/gofastr/core/query"
)

func ddlIdent(name string) string { return query.QuoteKeyword(name) }

func ddlIdents(names []string) []string { return query.QuoteKeywords(names) }

func unquoteDDLIdent(name string) string {
	if !strings.Contains(name, `"`) {
		return name
	}
	return strings.ReplaceAll(name, `"`, "")
}
