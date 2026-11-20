package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"errors"
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/ids"
)

// Channel shows a 6-digit mint confirmation code, with a description of the mint, to the user out of band.
type Channel interface {
	Show(ctx context.Context, code string, desc string) error
}

// Issuer mints tokens: Begin shows a code on the Channel, Confirm with that code within 60s returns the token.
type Issuer struct {
	enc     *Encoder
	channel Channel
	now     func() time.Time
	ttl     time.Duration

	mu      sync.Mutex
	pending map[string]pendingMint
}

type pendingMint struct {
	code    string
	claims  Claims
	expires time.Time
}

// NewIssuer returns an Issuer; with a nil channel every Begin returns ErrNoChannel.
func NewIssuer(enc *Encoder, channel Channel) *Issuer {
	return &Issuer{
		enc:     enc,
		channel: channel,
		now:     time.Now,
		ttl:     60 * time.Second,
		pending: make(map[string]pendingMint),
	}
}

// WithClock overrides the clock function (for tests).
func (i *Issuer) WithClock(now func() time.Time) *Issuer {
	i.now = now
	return i
}

// Begin shows a fresh code on the channel and returns the mint ID to pass to Confirm.
func (i *Issuer) Begin(ctx context.Context, claims Claims, desc string) (mintID string, err error) {
	if i.channel == nil {
		return "", ErrNoChannel
	}
	if claims.JTI == "" {
		claims.JTI = ids.NewJTI()
	}
	if claims.ExpiresAt == 0 {
		claims.ExpiresAt = i.now().Add(24 * time.Hour).Unix()
	}
	code, err := generateCode()
	if err != nil {
		return "", err
	}
	mintID = string(ids.NewJTI())
	i.mu.Lock()
	i.pending[mintID] = pendingMint{
		code:    code,
		claims:  claims,
		expires: i.now().Add(i.ttl),
	}
	i.mu.Unlock()
	if err := i.channel.Show(ctx, code, desc); err != nil {
		i.mu.Lock()
		delete(i.pending, mintID)
		i.mu.Unlock()
		return "", err
	}
	return mintID, nil
}

// Confirm returns the token when code matches; any attempt, right or wrong, ends the mint.
func (i *Issuer) Confirm(mintID, code string) (string, error) {
	i.mu.Lock()
	p, ok := i.pending[mintID]
	if !ok {
		i.mu.Unlock()
		return "", ErrUnknownMint
	}
	delete(i.pending, mintID)
	i.mu.Unlock()

	if i.now().After(p.expires) {
		return "", ErrCodeExpired
	}
	if subtle.ConstantTimeCompare([]byte(code), []byte(p.code)) != 1 {
		return "", ErrCodeMismatch
	}
	return i.enc.Encode(p.claims)
}

// IssueDirect encodes claims with no confirmation, for callers already trusted in process.
func (i *Issuer) IssueDirect(claims Claims) (string, error) {
	return i.enc.Encode(claims)
}

func generateCode() (string, error) {
	max := big.NewInt(1_000_000)
	n, err := rand.Int(rand.Reader, max)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}

// The Issuer's refusals.
var (
	ErrNoChannel    = errors.New("auth: token issuance requires an interactive confirmation channel; mint the token from an interactive harness session and reuse it for headless setups")
	ErrUnknownMint  = errors.New("auth: unknown mint ID")
	ErrCodeMismatch = errors.New("auth: confirmation code mismatch")
	ErrCodeExpired  = errors.New("auth: confirmation code expired (60s window)")
)

// PrintTTYChannel is a Channel that writes the code to W.
type PrintTTYChannel struct {
	W interface{ Write(p []byte) (int, error) }
}

func (c PrintTTYChannel) Show(ctx context.Context, code string, desc string) error {
	if c.W == nil {
		return errors.New("auth: PrintTTYChannel has no writer")
	}
	msg := fmt.Sprintf("\n  gofastr harness: confirm token mint %s\n  code: %s\n\n", desc, code)
	_, err := c.W.Write([]byte(msg))
	return err
}

var _ Channel = PrintTTYChannel{}

// ClaimsFor returns 24h claims bound to one session and identity class, with no mint right.
func ClaimsFor(session ids.SessionID, identity control.IdentityClass) Claims {
	return Claims{
		Ver:            VerCurrent,
		JTI:            ids.NewJTI(),
		Sessions:       []ids.SessionID{session},
		IdentityClass:  identity,
		ExpiresAt:      time.Now().Add(24 * time.Hour).Unix(),
		CanMint:        false,
		CriticalClaims: []string{"sessions", "identity_class"},
	}
}
