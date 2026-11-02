// Package lib stands in for core/i18n: HTTPError is a status sink
// before its clamp, Clamped is the fixed spelling.
package lib

import "net/http"

// HTTPError writes status as it is: a sink at index 2.
func HTTPError(w http.ResponseWriter, r *http.Request, status int, msg string) { // want HTTPError:`status\(safe=\[\] sinks=\[2\] checks=\[\]\)`
	w.WriteHeader(status)
}

// Clamped is HTTPError after the fix: not a sink.
func Clamped(w http.ResponseWriter, r *http.Request, status int, msg string) {
	if status < 400 || status > 599 {
		status = http.StatusInternalServerError
	}
	w.WriteHeader(status)
}

// StatusFor maps an error to a constant status: a safe result.
func StatusFor(err error) int { // want StatusFor:`status\(safe=\[0\] sinks=\[\] checks=\[\]\)`
	if err == nil {
		return http.StatusOK
	}
	return http.StatusInternalServerError
}
