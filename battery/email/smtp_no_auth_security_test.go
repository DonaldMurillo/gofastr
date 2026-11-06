package email

import (
	"errors"
	"strconv"
	"strings"
	"testing"
)

func TestSMTPRequiresAdvertisedAuth(t *testing.T) {
	relay := newFakeSMTP(t, "")
	t.Cleanup(func() { _ = relay.ln.Close() })
	host, portText := relay.addr()
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	sender := NewSMTPSender(SMTPConfig{Host: host, Port: port, Username: "user", Password: "pw", AllowCleartext: true})
	err = sender.Send(t.Context(), simpleMail())
	if !errors.Is(err, ErrSendFailed) || !strings.Contains(err.Error(), "AUTH") {
		t.Errorf("credentials without advertised AUTH: %v, want ErrSendFailed naming AUTH", err)
	}
	relay.mu.Lock()
	defer relay.mu.Unlock()
	if relay.gotData {
		t.Error("message delivered without the configured authentication")
	}
}
