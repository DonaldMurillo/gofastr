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
// and is unusable — and the failure surfaces at whichever remote
// verifier tries it, not here. Mount must refuse it instead.
func TestSigning_RejectsOffCurvePoint(t *testing.T) {
	good, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	// Move Y off the curve while leaving both coordinates non-nil, so
	// this gets past the nil guard and reaches the point check.
	bad := *good
	bad.PublicKey.Y = new(big.Int).Add(good.PublicKey.Y, big.NewInt(1))

	pv := signingMountPanic(WithAgentReady(AgentReadyConfig{
		BaseURL: "https://ok.example",
		AgentCard: &AgentCardConfig{
			Name: "X", Description: "d",
			SigningKeys: []AgentCardSigningKey{{KeyID: "bent", Signer: &bad}},
		},
	}))
	if pv == nil {
		t.Fatal("Mount succeeded with an off-curve EC point; want panic")
	}
	err, ok := pv.(error)
	if !ok || !strings.Contains(err.Error(), "not a valid point") {
		t.Fatalf("panic = %v, want the point-validity refusal naming the key", pv)
	}
	if !strings.Contains(err.Error(), "bent") {
		t.Errorf("refusal does not name the offending key: %v", err)
	}
}
