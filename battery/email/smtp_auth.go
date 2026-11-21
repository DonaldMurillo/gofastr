package email

import (
	"errors"
	"fmt"
	"net/smtp"
	"slices"
	"strings"
)

func (s *SMTPSender) pickAuth(offered string) (smtp.Auth, error) {
	var mechs []string
	for _, m := range strings.Fields(offered) {
		if saslName(m) {
			mechs = append(mechs, strings.ToUpper(m))
		}
	}
	user, pass, host := s.config.Username, s.config.Password, s.config.hostname()
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

func saslName(m string) bool {
	for i := 0; i < len(m); i++ {
		c := m[i]
		if !(c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

type loginAuth struct {
	username, password, host string
	step                     int
}

func (a *loginAuth) Start(server *smtp.ServerInfo) (string, []byte, error) {
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

func loopbackName(name string) bool {
	return name == "localhost" || name == "127.0.0.1" || name == "::1"
}
