package auth

import "testing"

// When the id_token carries an email but no email_verified, the flag was
// taken from userinfo even when userinfo described another address:
// id_token email=victim plus userinfo {email=attacker, email_verified=true}
// came out as a verified victim address, and the callback auto-linked the
// victim's account (2026-10-27 A5). The userinfo flag now counts only for
// the address that is kept.
func TestOIDCVerifiedFlagNeedsSameEmail(t *testing.T) {
	cases := []struct {
		name     string
		uiEmail  string
		verified bool
	}{
		{"other address", "attacker@example.com", false},
		{"same address", "alice@example.com", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeIdP(t)
			f.claims = baseClaims(f) // email alice@example.com, no email_verified
			f.userinfo = map[string]any{"email": tc.uiEmail, "email_verified": true}
			p := newTestProvider(t, f)
			tok, err := p.ExchangeCode(ctxBg(), "any-code")
			if err != nil {
				t.Fatalf("ExchangeCode: %v", err)
			}
			info, err := p.FetchUserInfo(ctxBg(), tok.AccessToken)
			if err != nil {
				t.Fatalf("FetchUserInfo: %v", err)
			}
			if info.Email != "alice@example.com" {
				t.Fatalf("kept email = %q, want the id_token's", info.Email)
			}
			if info.EmailVerified != tc.verified {
				t.Errorf("SECURITY: EmailVerified = %v with userinfo email %q, want %v", info.EmailVerified, tc.uiEmail, tc.verified)
			}
		})
	}
}
