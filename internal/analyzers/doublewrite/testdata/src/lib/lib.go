// Package lib holds a respond helper whose writesResponse fact crosses
// into importers.
package lib

import "net/http"

func Refuse(w http.ResponseWriter, code int) { // want Refuse:`writesResponse\[0\]`
	http.Error(w, http.StatusText(code), code)
}
