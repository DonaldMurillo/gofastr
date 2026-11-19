package appgen

// clientCore is the doc text that differs between the generated client
// packages: the entity SDK (renderClient) and the --from-openapi CLI's
// self-contained client (renderCLISelfClient). Each field is a whole
// comment block, "// " lines ending in a newline.
type clientCore struct {
	ClientDoc    string
	NewClientDoc string
	APIErrorDoc  string
	// Timeout is the default *http.Client's Timeout expression, or ""
	// for none (the SDK's Watch holds an SSE stream open indefinitely).
	Timeout string
}

// render emits the Client type, NewClient and APIError. The default
// client both packages build refuses redirects: every request carries the
// token, and a 3xx would re-send it to whatever origin the response names.
func (c clientCore) render() string {
	newClient := "\t\t//gofastr:allow(clienttimeout) requests carry the caller's context (http.NewRequestWithContext); a Client.Timeout would kill Watch's long-lived SSE stream mid-subscription\n" +
		"\t\thttpClient = &http.Client{\n"
	if c.Timeout != "" {
		newClient = "\t\thttpClient = &http.Client{\n\t\t\tTimeout: " + c.Timeout + ",\n"
	}
	return c.ClientDoc + `type Client struct {
	BaseURL string
	HTTP    *http.Client
	Token   string
}

` + c.NewClientDoc + `func NewClient(baseURL string, httpClient *http.Client) *Client {
	if httpClient == nil {
` + newClient + `			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
	}
	return &Client{BaseURL: baseURL, HTTP: httpClient}
}

` + c.APIErrorDoc + `type APIError struct {
	Status int
	Body   []byte
}

func (e *APIError) Error() string {
	return fmt.Sprintf("api: %d: %s", e.Status, string(e.Body))
}
`
}

var sdkClientCore = clientCore{
	ClientDoc: `// Client is a typed HTTP client targeting the gofastr server's CRUD routes.
// Pass any *http.Client (httptest, retryable wrapper, etc.): Client never
// closes it.
//
// Token, when set, is sent as "Authorization: Bearer <Token>" on every
// request. Pair it with the server's API-token middleware
// (auth.TokenMiddleware) and a scoped token minted via the app's
// /auth/tokens endpoints; leave empty for cookie/session or public APIs.
`,
	NewClientDoc: `// NewClient constructs a Client with the default http.Client when one is
// not supplied. BaseURL should NOT include a trailing slash. The default
// refuses redirects: requests carry the bearer token when Token is set,
// and a 3xx would re-send it to whatever origin the response names. Pass
// your own *http.Client to keep a redirect policy.
`,
	APIErrorDoc: `// APIError is returned for non-2xx responses. Status is the HTTP code;
// Body holds the raw response (capped at maxBodyBytes) so callers can
// decode application-level error fields if they want.
`,
}

var cliSelfClientCore = clientCore{
	ClientDoc: `// Client sends requests against the API's base URL. Pass any
// *http.Client (httptest, retryable wrapper, ...): Client never closes
// it. Token, when set, is sent on every request via the header above.
`,
	NewClientDoc: `// NewClient constructs a Client with a deadline-bearing default when one
// is not supplied (a zero-timeout client waits forever on an unresponsive
// peer). BaseURL should NOT include a trailing slash. The default refuses
// redirects: every request carries the token, and Go re-sends
// Authorization to a redirect on the same host at another port and any
// other header (an apiKey one) to any host at all. A 3xx comes back as an
// *APIError. Pass your own *http.Client to keep a redirect policy.
`,
	APIErrorDoc: `// APIError is returned for non-2xx responses. Status is the HTTP code;
// Body holds the raw response so callers can decode application-level
// error fields.
`,
	Timeout: "30 * time.Second",
}
