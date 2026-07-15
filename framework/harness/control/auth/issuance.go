package auth

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/harness/ids"
)

// Channel is the surface that displays a 6-digit confirmation code
// to the user out-of-band. Implementations: TTY printer, system
// notification, or a bundled trusted-client modal. See § Token
// issuance.
type Channel interface {
	// Show displays the code and a short description (which session
	// / which identity class is being minted) for the user to see.
	Show(ctx context.Context, code string, desc string) error
}

// Issuer mints capability tokens. Each issuance follows the
// out-of-band confirmation protocol described in the architecture
// doc:
//
//  1. Caller invokes Begin with desired claims.
//  2. Issuer picks a 6-digit code, asks Channel.Show to display it.
//  3. Caller POSTs the code back via Confirm within 60s.
//  4. On match, Issuer returns the encoded token.
type Issuer struct {
	enc     *Encoder
	channel Channel
	now     func() time.Time
	ttl     time.Duration

	mu      sync.Mutex
	pending map[string]pendingMint // by mint-attempt ID
}

type pendingMint struct {
	code    string
	claims  Claims
	expires time.Time
}

// NewIssuer constructs an Issuer using the given encoder and channel.
//
// If channel is nil the issuer refuses to mint (the doc commits to:
// "Refuses token issuance with an explicit error: 'Token issuance
// requires an interactive confirmation channel.'")
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

// Begin starts a mint attempt. Returns a mint ID the caller passes
// back to Confirm, plus the chosen claim set (the issuer may have
// added defaults like JTI and exp).
//
// If channel is nil, returns ErrNoChannel — the issuer refuses to
// proceed.
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
	mintID = string(ids.NewJTI()) // reuse ULID space — internal identifier
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

// Confirm completes a mint attempt by checking the user-supplied
// code against the one the channel showed. On match, returns the
// encoded token.
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
	if code != p.code {
		return "", ErrCodeMismatch
	}
	return i.enc.Encode(p.claims)
}

// IssueDirect bypasses the confirmation flow. Used by pre-provisioned
// CI flows where the user has already vouched out-of-band (e.g. by
// placing a token file with mode 0400). Never expose this on a
// network transport.
func (i *Issuer) IssueDirect(claims Claims) (string, error) {
	return i.enc.Encode(claims)
}

// generateCode returns a fresh 6-digit numeric code.
func generateCode() (string, error) {
	max := big.NewInt(1_000_000)
	n, err := rand.Int(rand.Reader, max)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}

// Errors surfaced as user-facing strings per § User-facing errors.
var (
	// No --auth-token-file flag exists (yet) — do not point users at it.
	ErrNoChannel    = errors.New("auth: token issuance requires an interactive confirmation channel; mint the token from an interactive harness session and reuse it for headless setups")
	ErrUnknownMint  = errors.New("auth: unknown mint ID")
	ErrCodeMismatch = errors.New("auth: confirmation code mismatch")
	ErrCodeExpired  = errors.New("auth: confirmation code expired (60s window)")
)

// PrintTTYChannel is a Channel that writes the code to the given
// io.Writer. The TTY-displaying main loop wires this with os.Stderr.
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

// Compile-time assertion: PrintTTYChannel satisfies Channel.
var _ Channel = PrintTTYChannel{}

// ClaimsFor builds a default claim set for a specific session +
// identity class with the doc's recommended defaults.
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
