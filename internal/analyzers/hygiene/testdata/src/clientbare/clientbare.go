// Package clientbare pins the bare-client arm: the http.Get/Post
// sugar and http.DefaultClient run on a client with no deadline, and
// the file has no context deadline convention to credit them with.
package clientbare

import (
	"net/http"
)

// sugarGet is the cmd/kiln portFree shape: an http.Get against a peer
// that may never answer.
func sugarGet(url string) bool {
	resp, err := http.Get(url) // want `zero-timeout HTTP client: http.Get`
	if err != nil {
		return true
	}
	resp.Body.Close()
	return false
}

// sugarPost is cmd/gofastr remoteQuery's spelling.
func sugarPost(url string) error {
	resp, err := http.Post(url, "application/json", nil) // want `zero-timeout HTTP client: http.Post`
	if err != nil {
		return err
	}
	return resp.Body.Close()
}

// defaultClient is the DefaultClient.Do spelling with no deadline
// anywhere in the function.
func defaultClient(url string) (*http.Response, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	return http.DefaultClient.Do(req) // want `zero-timeout HTTP client: http.DefaultClient`
}

// bareMention hands DefaultClient to a helper: same zero-timeout
// client, reported at the mention.
func bareMention() *http.Client {
	return http.DefaultClient // want `zero-timeout HTTP client: http.DefaultClient`
}
