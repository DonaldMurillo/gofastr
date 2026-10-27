// Package quoteident keeps SQL identifier quoting to one spelling:
// query.QuoteFolded. AutoMigrate writes its DDL unquoted, so Postgres
// folds a declared "Widgets" to widgets, and query.QuoteIdent, which
// keeps case, then quotes a name no table has. It also wraps a
// schema-qualified "app.users" as one identifier with a dot inside it.
// QuoteFolded folds ASCII case and quotes each dotted part, so it names
// what the DDL created; for a lowercase constant both spellings agree.
//
// The bug class: the 2026-10-26 bug bash fixed about twenty sites where
// a declared table or column (an entity table, the auth users table and
// its field map, a custom audit table, export, erasure and admin
// queries) went through QuoteIdent and broke on a mixed-case or
// schema-qualified name on Postgres.
//
// It fires on every use of core/query.QuoteIdent outside core/query,
// call or function value, and stays silent inside core/query (where
// QuoteIdent builds QuoteFolded) and in _test.go files. A name read back
// from the database catalog (information_schema, sqlite_master, a PRAGMA
// result) is already in its stored case and carries
// //gofastr:allow(quoteident) <why>.
package quoteident

import (
	"go/token"
	"go/types"
	"slices"
	"strings"

	"golang.org/x/tools/go/analysis"
)

var Analyzer = &analysis.Analyzer{
	Name: "quoteident",
	Doc:  "forbids query.QuoteIdent outside core/query; quote declared SQL names with query.QuoteFolded, which names what AutoMigrate created",
	Run:  run,
}

const queryPkg = "github.com/DonaldMurillo/gofastr/core/query"

func run(pass *analysis.Pass) (any, error) {
	if pass.Pkg.Path() == queryPkg {
		return nil, nil
	}
	var uses []token.Pos
	for id, obj := range pass.TypesInfo.Uses {
		fn, ok := obj.(*types.Func)
		if !ok || fn.Name() != "QuoteIdent" || fn.Pkg() == nil || fn.Pkg().Path() != queryPkg {
			continue
		}
		if !strings.HasSuffix(pass.Fset.Position(id.Pos()).Filename, "_test.go") {
			uses = append(uses, id.Pos())
		}
	}
	slices.Sort(uses)
	for _, pos := range uses {
		pass.Reportf(pos,
			"query.QuoteIdent keeps the name's case, but AutoMigrate creates names folded: quote a declared name with query.QuoteFolded; a name read back from the catalog carries //gofastr:allow(quoteident) <why>")
	}
	return nil, nil
}
