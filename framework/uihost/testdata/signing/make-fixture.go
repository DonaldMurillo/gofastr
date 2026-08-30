//go:build ignore

// make-fixture.go regenerates the signed-card fixtures the Node/WebCrypto
// verifier (verify-card.mjs) checks: card.json + jwks.json exactly as a
// signing-enabled GoFastr host serves them, produced with deterministic
// keys so regeneration never changes the key set.
//
// Run:  go run testdata/signing/make-fixture.go
//
// The pinned base is a fixture-only origin: the Go host signs URLs under
// it, and verify-card.mjs asserts the served jku matches. Nothing is
// served publicly at that origin.

package main

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	uihost "github.com/DonaldMurillo/gofastr/framework/uihost"
)

const pinnedBase = "https://card-signing.fixture.test"

func main() {
	edSeed := sha256.Sum256([]byte("gofastr a2a card signing fixture"))
	edKey := ed25519.NewKeyFromSeed(edSeed[:])

	// Deterministic P-256 key from a fixed scalar (reduced mod N).
	dBytes := sha256.Sum256([]byte("gofastr a2a card signing fixture ec"))
	n := elliptic.P256().Params().N
	d := new(big.Int).Mod(new(big.Int).SetBytes(dBytes[:]), n)
	d.Sub(d, big.NewInt(1)) // keep in [1, N-1] range deterministically
	if d.Sign() == 0 {
		d.SetInt64(42)
	}
	ecKey := new(ecdsa.PrivateKey)
	ecKey.D = d
	ecKey.Curve = elliptic.P256()
	ecKey.X, ecKey.Y = elliptic.P256().ScalarBaseMult(d.Bytes())

	a := app.NewApp("card-signing-fixture")
	host := uihost.New(a, uihost.WithAgentReady(uihost.AgentReadyConfig{
		BaseURL: pinnedBase,
		AgentCard: &uihost.AgentCardConfig{
			Name:        "Fixture Agent",
			Description: "signed-card fixture",
			Version:     "9.9.9",
			MCPEndpoint: "/mcp",
			SigningKeys: []uihost.AgentCardSigningKey{
				{KeyID: "fixture-ed-1", Signer: edKey},
				{KeyID: "fixture-ec-1", Signer: ecKey},
			},
		},
	}))
	srv := httptest.NewServer(host)
	defer srv.Close()

	write := func(name, path string) {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			panic(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			panic(fmt.Sprintf("%s: status %d", path, resp.StatusCode))
		}
		var v any
		if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
			panic(err)
		}
		out, err := os.Create(name)
		if err != nil {
			panic(err)
		}
		defer out.Close()
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		if err := enc.Encode(v); err != nil {
			panic(err)
		}
		fmt.Printf("wrote %s\n", name)
	}
	write("testdata/signing/card.json", "/.well-known/agent-card.json")
	write("testdata/signing/jwks.json", "/.well-known/jwks.json")
}
