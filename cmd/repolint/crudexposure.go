package main

import "strings"

// crudExposureClassified lists the files allowed to read Exposure.CRUD,
// each with the reason its read is not an exposure decision.
var crudExposureClassified = map[string]string{
	"framework/app.go":                                 "defines the predicate (entityCRUDEnabled) and ANDs a.DB != nil at every other read",
	"framework/entity/entity.go":                       "marshals the declaration; not an exposure decision",
	"framework/entity/declaration.go":                  "merges the declaration; not an exposure decision",
	"cmd/gofastr/internal/appgen/generate_entities.go": "emits the declared flag back into generated source; not an exposure decision",
	"cmd/gofastr/internal/packcmd/pack.go":             "serializes the declared flag back to YAML; not an exposure decision",
	"framework/openapi/openapi.go":                     "takes a crudMounted predicate; the bare read is its documented nil fallback for direct callers",
	"framework/sdkdocs/sdkdocs.go":                     "takes a CRUDMounted predicate; the bare read is the Exposure-only prefilter",
	"battery/admin/entity_admin.go":                    "admin mounts only with a DB attached, so nil really is auto-true here",
	"battery/auth/verify_private.go":                   "warns that an auth entity is exposed; omitting the DB check makes it warn in the SAFE direction",
	"framework/crud/owner.go":                          "relationReachable refuses an explicit CRUD:false opt-out on another entity's mounted route; a DB-less app mounts no route that could reach it, so nil stays reachable",
}

func crudExposureRederivedLines(rel string, body []byte) []int {
	if !strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") {
		return nil
	}
	if strings.Contains(rel, "/testdata/") || strings.HasPrefix(rel, "cmd/repolint/") {
		return nil
	}
	if _, ok := crudExposureClassified[rel]; ok {
		return nil
	}
	var out []int
	var st goLexState
	for i, line := range strings.Split(string(body), "\n") {
		var code string
		inRaw := st.inRawString
		code, st = stripGoComments(line, st)
		if strings.Contains(outsideGoStrings(code, inRaw), "Exposure.CRUD") {
			out = append(out, i+1)
		}
	}
	return out
}
