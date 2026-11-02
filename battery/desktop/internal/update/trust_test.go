package update

import (
	"errors"
	"testing"
)

func TestCheckWindowsTrustPolicy(t *testing.T) {
	acme := []byte("CN=Acme")
	other := []byte("CN=Other")
	trusted := func(s []byte) Trust { return Trust{State: Trusted, Subject: s} }
	unsigned := Trust{State: Unsigned}
	broken := Trust{State: Untrusted, Subject: acme, Detail: "TRUST_E_BAD_DIGEST"}
	msDLL := trusted([]byte("CN=Microsoft Corporation"))

	for _, tc := range []struct {
		name              string
		running, exe, dll Trust
		wantOK            bool
	}{
		{"signed to same publisher", trusted(acme), trusted(acme), msDLL, true},
		{"signed to another publisher", trusted(acme), trusted(other), msDLL, false},
		{"signed to unsigned", trusted(acme), unsigned, msDLL, false},
		{"signed to broken signature", trusted(acme), broken, msDLL, false},
		{"unsigned to unsigned", unsigned, unsigned, msDLL, true},
		{"unsigned to signed", unsigned, trusted(acme), msDLL, true},
		{"unsigned to broken signature", unsigned, broken, msDLL, false},
		{"running signature broken, update unsigned", broken, unsigned, msDLL, true},
		{"running signature broken, update broken", broken, broken, msDLL, false},
		{"unsigned loader", unsigned, unsigned, unsigned, false},
		{"broken loader", trusted(acme), trusted(acme), Trust{State: Untrusted}, false},
		{"trusted running with no subject", trusted(nil), trusted(nil), msDLL, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := CheckWindowsTrust(tc.running, tc.exe, tc.dll)
			if tc.wantOK && err != nil {
				t.Fatalf("refused: %v", err)
			}
			if !tc.wantOK && !errors.Is(err, ErrSignature) {
				t.Fatalf("err = %v, want ErrSignature", err)
			}
		})
	}
}
