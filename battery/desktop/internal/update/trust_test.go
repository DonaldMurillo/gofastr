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
	acmeDLL := trusted(acme)

	for _, tc := range []struct {
		name                          string
		running, runningDLL, exe, dll Trust
		wantOK                        bool
	}{
		{"signed to same publisher", trusted(acme), msDLL, trusted(acme), msDLL, true},
		{"signed to another publisher", trusted(acme), msDLL, trusted(other), msDLL, false},
		{"signed to unsigned", trusted(acme), msDLL, unsigned, msDLL, false},
		{"signed to broken signature", trusted(acme), msDLL, broken, msDLL, false},
		{"unsigned to unsigned", unsigned, msDLL, unsigned, msDLL, true},
		{"unsigned to signed", unsigned, msDLL, trusted(acme), msDLL, true},
		{"unsigned to broken signature", unsigned, msDLL, broken, msDLL, false},
		{"running signature broken, update unsigned", broken, msDLL, unsigned, msDLL, true},
		{"running signature broken, update broken", broken, msDLL, broken, msDLL, false},
		{"unsigned loader", unsigned, msDLL, unsigned, unsigned, false},
		{"broken loader", trusted(acme), msDLL, trusted(acme), Trust{State: Untrusted}, false},
		{"trusted running with no subject", trusted(nil), msDLL, trusted(nil), msDLL, false},
		{"loader from another publisher", trusted(acme), msDLL, trusted(acme), acmeDLL, false},
		{"loader from another publisher, unsigned app", unsigned, msDLL, unsigned, acmeDLL, false},
		{"installed loader with no subject", unsigned, trusted(nil), unsigned, msDLL, false},
		{"installed loader untrusted, new one trusted", unsigned, Trust{State: Untrusted}, unsigned, acmeDLL, true},
		{"installed loader missing, new one trusted", unsigned, unsigned, unsigned, msDLL, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := CheckWindowsTrust(tc.running, tc.runningDLL, tc.exe, tc.dll)
			if tc.wantOK && err != nil {
				t.Fatalf("refused: %v", err)
			}
			if !tc.wantOK && !errors.Is(err, ErrSignature) {
				t.Fatalf("err = %v, want ErrSignature", err)
			}
		})
	}
}
