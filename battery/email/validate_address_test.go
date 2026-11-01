package email_test

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/email"
)

// ValidateAddress accepts exactly the single mailboxes Send takes and
// refuses the rest.
func TestValidateAddressOneMailbox(t *testing.T) {
	for _, ok := range []string{
		"o'brien@example.com",
		"Ada <ada@example.com>",
		`"Support Team" <help@example.com>`,
		"first.last+tag@example.com",
	} {
		if err := email.ValidateAddress(ok); err != nil {
			t.Errorf("ValidateAddress(%q) = %v, want nil", ok, err)
		}
	}
	for _, bad := range []string{
		"", "no-at-sign", "a@example.com, b@example.com",
		"a@example.com\r\nBcc: x@evil.test", "a​@example.com",
		`"a>b"@example.com`, "<script>@evil.test",
	} {
		if err := email.ValidateAddress(bad); err == nil {
			t.Errorf("ValidateAddress(%q) = nil, want an error", bad)
		}
	}
}
