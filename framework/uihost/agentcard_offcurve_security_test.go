package uihost

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"math/big"
	"strings"
	"testing"
)

// An off-curve point serializes into a JWK perfectly happily and
// verifies nowhere, so publishing one means a key set that looks right
// and is unusable — with the failure surfacing at whichever remote
// verifier tries it, not here.
//
// Swept over all three supported curves on purpose. A P-256-only test
// pins half the guard: "refuses a bad point" is checked, "accepts a
// good one" is not, so a regression that refused every P-384 and P-521
// key would ship green. Nothing else in the repo mounts those two
// curves.
func TestSigning_RejectsOffCurvePoint(t *testing.T) {
	for _, tc := range []struct {
		name  string
		curve elliptic.Curve
	}{
		{"P-256", elliptic.P256()},
		{"P-384", elliptic.P384()},
		{"P-521", elliptic.P521()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			good, err := ecdsa.GenerateKey(tc.curve, rand.Reader)
			if err != nil {
				t.Fatal(err)
			}

			// The valid key must mount. This half is what a P-256-only
			// test misses.
			if pv := signingMountPanic(cardWithKey(good)); pv != nil {
				t.Fatalf("a valid %s key was refused at mount: %v", tc.name, pv)
			}

			// Move Y off the curve, leaving both coordinates non-nil so
			// this gets past the nil guard and reaches the point check.
			bad := *good
			bad.PublicKey.Y = new(big.Int).Add(good.PublicKey.Y, big.NewInt(1))

			pv := signingMountPanic(cardWithKey(&bad))
			if pv == nil {
				t.Fatalf("Mount succeeded with an off-curve %s point; want panic", tc.name)
			}
			err, ok := pv.(error)
			if !ok || !strings.Contains(err.Error(), "not a valid point") {
				t.Fatalf("panic = %v, want the point-validity refusal", pv)
			}
			if !strings.Contains(err.Error(), "bent") {
				t.Errorf("refusal does not name the offending key: %v", err)
			}
		})
	}
}

func cardWithKey(k *ecdsa.PrivateKey) Option {
	return WithAgentReady(AgentReadyConfig{
		BaseURL: "https://ok.example",
		AgentCard: &AgentCardConfig{
			Name: "X", Description: "d",
			SigningKeys: []AgentCardSigningKey{{KeyID: "bent", Signer: k}},
		},
	})
}
