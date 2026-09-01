package clientp

import (
	"net/http"
	"time"
)

func bad() *http.Client { return &http.Client{} } // want `http.Client with no Timeout`

func badWithTransport() *http.Client {
	return &http.Client{Transport: http.DefaultTransport} // want `http.Client with no Timeout`
}

func good() *http.Client { return &http.Client{Timeout: 5 * time.Second} }

// A file that deadlines its calls does not need Client.Timeout.
