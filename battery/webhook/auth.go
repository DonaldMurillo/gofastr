package webhook

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/textsafe"
	"golang.org/x/net/http/httpguts"
)

// AuthKind names how a delivery authenticates to its receiver.
type AuthKind string

const (
	// AuthNone sends no credential. The default.
	AuthNone AuthKind = ""
	// AuthBearer sends "Authorization: Bearer <Token>".
	AuthBearer AuthKind = "bearer"
	// AuthAPIKey sends "<Header>: <Token>"; Header defaults to
	// DefaultAPIKeyHeader.
	AuthAPIKey AuthKind = "api_key"
	// AuthBasic sends "Authorization: Basic <base64(Username:Password)>".
	AuthBasic AuthKind = "basic"
)

// DefaultAPIKeyHeader is the header an AuthAPIKey credential goes in when
// Auth.Header is empty.
const DefaultAPIKeyHeader = "X-API-Key"

// Auth is a subscriber's outbound credential. A delivery carrying one never
// follows a redirect.
type Auth struct {
	Kind AuthKind
	// Token is the bearer token (AuthBearer) or the key (AuthAPIKey).
	Token string
	// Header is the API key's header name (AuthAPIKey only); empty means
	// DefaultAPIKeyHeader.
	Header string
	// Username and Password are the AuthBasic pair; Username may not contain
	// ':'.
	Username string
	Password string
}

func (a Auth) headerName() string {
	switch a.Kind {
	case AuthAPIKey:
		if a.Header != "" {
			return a.Header
		}
		return DefaultAPIKeyHeader
	case AuthBearer, AuthBasic:
		return "Authorization"
	}
	return ""
}

func (a Auth) apply(req *http.Request) {
	switch a.Kind {
	case AuthBearer:
		req.Header.Set("Authorization", "Bearer "+a.Token)
	case AuthAPIKey:
		req.Header.Set(a.headerName(), a.Token)
	case AuthBasic:
		req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(a.Username+":"+a.Password)))
	}
}

const redactedCredential = "[redacted]"

func (a Auth) redact(text string) string {
	if a.Kind == AuthNone || text == "" {
		return text
	}
	for _, s := range a.secrets() {
		text = strings.ReplaceAll(text, s, redactedCredential)
	}
	return text
}

func (a Auth) secrets() []string {
	if a.Kind == AuthNone {
		return nil
	}
	all := []string{a.Token, a.Password}
	if a.Kind == AuthBasic {
		all = append(all, base64.StdEncoding.EncodeToString([]byte(a.Username+":"+a.Password)))
	}
	out := all[:0]
	for _, s := range all {
		if len(s) >= 4 {
			out = append(out, s)
		}
	}
	return out
}

func (a Auth) redactHead(text string, keep int) string {
	if keep >= len(text) {
		return a.redact(text)
	}
	covered := make([]bool, len(text))
	for _, s := range a.secrets() {
		for from := 0; ; {
			i := strings.Index(text[from:], s)
			if i < 0 {
				break
			}
			for j := from + i; j < from+i+len(s); j++ {
				covered[j] = true
			}
			from += i + 1
		}
	}
	var b strings.Builder
	for i := 0; i < keep; i++ {
		switch {
		case !covered[i]:
			b.WriteByte(text[i])
		case i == 0 || !covered[i-1]:
			b.WriteString(redactedCredential)
		}
	}
	return b.String()
}

func (a Auth) longestSecret() int {
	n := 0
	for _, s := range a.secrets() {
		n = max(n, len(s))
	}
	return n
}

var reservedAuthHeaders = map[string]bool{
	"Host": true, "Content-Type": true, "Content-Length": true, "Transfer-Encoding": true,
	"Connection": true, "User-Agent": true, "Te": true, "Trailer": true, "Upgrade": true,
}

func (a Auth) validate() error {
	if (a.Kind == AuthBearer || a.Kind == AuthAPIKey) && (a.Username != "" || a.Password != "") {
		return errors.New("webhook: Subscriber.Auth Username and Password apply to basic only")
	}
	switch a.Kind {
	case AuthNone:
		if a.Token != "" || a.Header != "" || a.Username != "" || a.Password != "" {
			return errors.New("webhook: Subscriber.Auth carries a credential but no Kind; set Kind to bearer, api_key or basic")
		}
		return nil
	case AuthBearer:
		if a.Token == "" {
			return errors.New("webhook: Subscriber.Auth bearer needs a Token")
		}
		if a.Header != "" {
			return errors.New("webhook: Subscriber.Auth.Header applies to api_key only")
		}
	case AuthAPIKey:
		if a.Token == "" {
			return errors.New("webhook: Subscriber.Auth api_key needs a Token")
		}
		name := a.headerName()
		if !httpguts.ValidHeaderFieldName(name) {
			return fmt.Errorf("webhook: Subscriber.Auth.Header %q is not a valid header name", textsafe.ScrubControlBytes(name))
		}
		canon := http.CanonicalHeaderKey(name)
		if reservedAuthHeaders[canon] || strings.HasPrefix(canon, "X-Gofastr-") || strings.EqualFold(canon, SignatureHeader) {
			return fmt.Errorf("webhook: Subscriber.Auth.Header %q is a header the delivery sets itself", canon)
		}
	case AuthBasic:
		if a.Username == "" {
			return errors.New("webhook: Subscriber.Auth basic needs a Username")
		}
		if strings.Contains(a.Username, ":") {
			return errors.New("webhook: Subscriber.Auth basic Username may not contain ':'")
		}
		if a.Header != "" || a.Token != "" {
			return errors.New("webhook: Subscriber.Auth basic takes Username and Password only")
		}
	default:
		return fmt.Errorf("webhook: Subscriber.Auth.Kind %q is not one of bearer, api_key, basic", textsafe.ScrubControlBytes(string(a.Kind)))
	}
	for _, v := range []string{a.Token, a.Username, a.Password} {
		if !httpguts.ValidHeaderFieldValue(v) {
			return errors.New("webhook: Subscriber.Auth holds a control byte; a header value cannot carry it")
		}
	}
	return nil
}

type authRecord struct {
	Kind     AuthKind `json:"kind"`
	Token    string   `json:"token,omitempty"`
	Header   string   `json:"header,omitempty"`
	Username string   `json:"username,omitempty"`
	Password string   `json:"password,omitempty"`
}

func encodeAuth(a Auth) (string, error) {
	if a.Kind == AuthNone {
		return "", nil
	}
	b, err := json.Marshal(authRecord(a))
	return string(b), err
}

func decodeAuth(s string) (Auth, error) {
	if s == "" {
		return Auth{}, nil
	}
	var r authRecord
	if err := json.Unmarshal([]byte(s), &r); err != nil {
		return Auth{}, errors.New("webhook: corrupt auth column")
	}
	return Auth(r), nil
}
