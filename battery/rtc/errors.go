package rtc

import "github.com/DonaldMurillo/gofastr/core/i18n"

// HTTPError lets Config.Authorize choose the refusal status (401, 404,
// 410 …) instead of the default 403. ServeHTTP writes Status and
// Reason, translated per request, as the response (a zero Reason is the
// status's generic phrase); anything else about the error is discarded
// so a refusal leaks nothing beyond what the host chose to say. Keep the
// text short: a WebSocket refusal carries it as the close reason.
type HTTPError struct {
	Status int
	Reason i18n.Phrase
	Params map[string]any
}

func (e *HTTPError) Error() string { return e.Reason.Format(e.Params) }

// Phrased implements i18n.Phrased.
func (e *HTTPError) Phrased() (i18n.Phrase, map[string]any) { return e.Reason, e.Params }
