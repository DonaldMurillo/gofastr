package email

import (
	"errors"
	"fmt"
	"net/smtp"
	"slices"
	"strings"
)

// pickAuth chooses the login for a relay's AUTH line (the mechanisms
// after "250 AUTH"). CRAM-MD5 goes first because it never sends the
// password; PLAIN next, then LOGIN, which Office 365 offers alone.
func (s *SMTPSender) pickAuth(offered string) (smtp.Auth, error) {
	var mechs []string
	for _, m := range strings.Fields(offered) {
		if saslName(m) {
			mechs = append(mechs, strings.ToUpper(m))
		}
	}
	user, pass, host := s.config.Username, s.config.Password, s.config.Host
	switch {
	case slices.Contains(mechs, "CRAM-MD5"):
		return smtp.CRAMMD5Auth(user, pass), nil
	case slices.Contains(mechs, "PLAIN"):
		return smtp.PlainAuth("", user, pass, host), nil
	case slices.Contains(mechs, "LOGIN"):
		return &loginAuth{username: user, password: pass, host: host}, nil
	}
	return nil, fmt.Errorf("%w: the relay offers AUTH %s; the sender speaks CRAM-MD5, PLAIN and LOGIN", ErrSendFailed, strings.Join(mechs, " "))
}

// saslName reports whether m is spelled like a SASL mechanism name
// (RFC 4422: letters, digits, hyphens and underscores). The relay's
// words reach an error string, so nothing else passes.
func saslName(m string) bool {
	for i := 0; i < len(m); i++ {
		c := m[i]
		if !(c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

// loginAuth is the LOGIN mechanism, which net/smtp does not ship: the
// relay prompts for the username, then the password, and each answer is
// sent base64-encoded. pickAuth builds one per send, so step starts at
// zero.
type loginAuth struct {
	username, password, host string
	step                     int
}

func (a *loginAuth) Start(server *smtp.ServerInfo) (string, []byte, error) {
	// smtp.PlainAuth's refusals: LOGIN puts the password on the wire as
	// PLAIN does, so only over TLS or to this machine, and only to the
	// host the credentials were configured for.
	if !server.TLS && !loopbackName(server.Name) {
		return "", nil, errors.New("unencrypted connection")
	}
	if server.Name != a.host {
		return "", nil, errors.New("wrong host name")
	}
	return "LOGIN", nil, nil
}

func (a *loginAuth) Next(_ []byte, more bool) ([]byte, error) {
	if !more {
		return nil, nil
	}
	a.step++
	switch a.step {
	case 1:
		return []byte(a.username), nil
	case 2:
		return []byte(a.password), nil
	}
	return nil, errors.New("unexpected LOGIN challenge")
}

// loopbackName is net/smtp's isLocalhost.
func loopbackName(name string) bool {
	return name == "localhost" || name == "127.0.0.1" || name == "::1"
}
