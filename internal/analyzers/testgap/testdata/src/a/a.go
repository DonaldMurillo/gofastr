package a

// seed: the pre-fix core/upload serve.go shape. The doc-family guard
// enumerates html, xhtml, svg, xml; the test table exercises only html
// and svg, so the xhtml and xml arms are unpinned.
func isAllowedDocType(ext string) bool { // want `testgap: isAllowedDocType: 2 of 4 enumerated values never appear in this package's tests: xhtml, xml`
	switch ext {
	case "html", "xhtml":
		return true
	case "svg":
		return true
	case "xml":
		return true
	}
	return false
}

// extOK carries no validator marker in its name: the bool returned
// from inside a string switch is what enumerates it.
func extOK(ext string) bool { // want `testgap: extOK: 1 of 2 enumerated values never appear in this package's tests: gif`
	switch ext {
	case "png":
		return true
	case "gif":
		return true
	}
	return false
}

// allowedSchemes is the slice-literal shape; the fixture corpus never
// mentions ftp.
var allowedSchemes = []string{"http", "https", "mailto", "ftp"} // want `testgap: allowedSchemes: 1 of 4 enumerated values never appear in this package's tests: ftp`

// schemeAllowed consults the slice. Name-matched but switchless: no
// enumerable arms, so it stays silent.
func schemeAllowed(s string) bool {
	for _, v := range allowedSchemes {
		if v == s {
			return true
		}
	}
	return false
}
