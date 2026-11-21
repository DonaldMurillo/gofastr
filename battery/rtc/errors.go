package rtc

import "github.com/DonaldMurillo/gofastr/core/i18n"

// HTTPError lets Config.Authorize choose the refusal status (401, 404,
// 410 …) instead of the default 403.
type HTTPError struct {
	Status int
	Reason i18n.Phrase
	Params map[string]any
}

func (e *HTTPError) Error() string { return e.Reason.Format(e.Params) }

// Phrased implements i18n.Phrased.
func (e *HTTPError) Phrased() (i18n.Phrase, map[string]any) { return e.Reason, e.Params }
