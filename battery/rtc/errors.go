package rtc

// HTTPError lets Config.Authorize choose the refusal status (401, 404,
// 410 …) instead of the default 403. ServeHTTP writes Status and
// Message as the response; anything else about the error is discarded
// so a refusal leaks nothing beyond what the host chose to say.
type HTTPError struct {
	Status  int
	Message string // plain, short; written as the response body
}

func (e *HTTPError) Error() string { return e.Message }
