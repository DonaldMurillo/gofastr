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
	// AuthNone sends no credential. The HMAC signature header is the
	// only proof of origin. The default.
	AuthNone AuthKind = ""
	// AuthBearer sends "Authorization: Bearer <Token>".
	AuthBearer AuthKind = "bearer"
	// AuthAPIKey sends "<Header>: <Token>". Header defaults to
	// DefaultAPIKeyHeader.
	AuthAPIKey AuthKind = "api_key"
	// AuthBasic sends "Authorization: Basic <base64(Username:Password)>".
	AuthBasic AuthKind = "basic"
)

// DefaultAPIKeyHeader is the header an AuthAPIKey credential goes in
// when Auth.Header is empty.
const DefaultAPIKeyHeader = "X-API-Key"

// Auth is a subscriber's outbound credential: what each delivery sends
// so a receiver that checks "Authorization: Bearer" or "X-API-Key"
// accepts it. SQLStore seals it with the same SecretCodec as
// Subscriber.Secret. A delivery that carries a credential never follows
// a redirect, whatever the HTTPClient says: a 3xx would hand the
// credential to the host the redirect names.
type Auth struct {
	Kind AuthKind
	// Token is the bearer token (AuthBearer) or the key (AuthAPIKey).
	Token string
	// Header is the API key's header name (AuthAPIKey only). Empty
	// means DefaultAPIKeyHeader.
	Header string
	// Username and Password are the AuthBasic pair. Username may not
	// contain ':'.
	Username string
	Password string
}

// headerName is the header the credential goes in.
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

// apply sets the credential on req. It runs after validate, so the
// header name and value are already known to be well-formed.
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

// reservedAuthHeaders are headers a credential may not take over: the
// ones the delivery itself sets or the transport owns.
var reservedAuthHeaders = map[string]bool{
	"Host": true, "Content-Type": true, "Content-Length": true, "Transfer-Encoding": true,
	"Connection": true, "User-Agent": true, "Te": true, "Trailer": true, "Upgrade": true,
}

// validate refuses a credential the delivery cannot send as written.
// It never echoes a credential value into the error.
func (a Auth) validate() error {
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

// authRecord is Auth's stored form, inside the sealed column.
type authRecord struct {
	Kind     AuthKind `json:"kind"`
	Token    string   `json:"token,omitempty"`
	Header   string   `json:"header,omitempty"`
	Username string   `json:"username,omitempty"`
	Password string   `json:"password,omitempty"`
}

// encodeAuth is the plaintext a SecretCodec seals: "" for AuthNone,
// else JSON.
func encodeAuth(a Auth) (string, error) {
	if a.Kind == AuthNone {
		return "", nil
	}
	b, err := json.Marshal(authRecord(a))
	return string(b), err
}

// decodeAuth reverses encodeAuth. A column that does not parse is an
// error, never "no auth": a delivery that silently dropped its
// credential would fail at the receiver with no local clue.
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
