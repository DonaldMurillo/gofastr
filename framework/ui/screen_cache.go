package ui

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

// InvalidateScreens adds root-relative paths, or "*", to the
// X-Gofastr-Invalidate header so the runtime drops those cached screens after
// a 2xx response; other values are dropped.
func InvalidateScreens(w http.ResponseWriter, paths ...string) {
	var valid []string
	for _, p := range paths {
		if p == "*" || (strings.HasPrefix(p, "/") && !strings.HasPrefix(p, "//") && !textsafe.HasControlBytes(p)) {
			valid = append(valid, p)
		}
	}
	if len(valid) == 0 {
		return
	}
	var list []string
	if existing := w.Header().Get("X-Gofastr-Invalidate"); existing != "" {
		if err := json.Unmarshal([]byte(existing), &list); err != nil {
			list = nil
		}
	}
	list = append(list, valid...)
	enc, err := json.Marshal(list)
	if err != nil {
		return
	}
	w.Header().Set("X-Gofastr-Invalidate", asciiJSON(enc))
}
