package email

import (
	"context"
	"net"
	"strconv"
	"testing"
)

// An IPv6 host connects and logs in. "%s:%d" made "::1:587", which the
// dialer refuses ("too many colons"). The bracketed spelling a host may
// already write keeps working, and reaches PLAIN's checks as the bare
// literal: loopback, and the same name the credentials were set for.
func TestSMTPDialsIPv6Host(t *testing.T) {
	for _, host := range []string{"::1", "[::1]"} {
		for _, login := range []bool{false, true} {
			name := host
			if login {
				name += "/login"
			}
			t.Run(name, func(t *testing.T) {
				ln, err := net.Listen("tcp", "[::1]:0")
				if err != nil {
					t.Skipf("no IPv6 loopback: %v", err)
				}
				srv := &fakeSMTP{ln: ln}
				cfg := SMTPConfig{Host: host, AllowCleartext: true}
				if login {
					srv.advertise = "AUTH PLAIN"
					cfg.Username, cfg.Password = "u", "p" // not-a-secret: fixture login for a fake relay
				}
				go srv.serve()
				defer ln.Close()
				_, portStr := srv.addr()
				if cfg.Port, err = strconv.Atoi(portStr); err != nil {
					t.Fatal(err)
				}
				if err := NewSMTPSender(cfg).Send(context.Background(), Email{
					From: "a@b.test", To: []string{"x@y.test"}, Subject: "s", TextBody: "b",
				}); err != nil {
					t.Fatalf("Send over %s: %v", name, err)
				}
				if !srv.receivedData() {
					t.Fatal("the message never reached the IPv6 server")
				}
			})
		}
	}
}
