package auth

import (
	"strings"
	"testing"
	"time"
)

// A two-factor URL that is not a same-origin path can never be redirected
// to: every sign-in sends it through safeRedirectURL, which answers "/",
// and "/" refuses the pending session. Init checked only that the option
// was set, so an absolute URL (the spelling MagicLinkConfig.BaseURL takes)
// passed and every 2FA sign-in dead-ended. Each plugin now refuses it at
// Init, with or without a 2FA plugin beside it.
func TestTwoFactorURLThatCannotBeRedirectedToIsRefusedAtInit(t *testing.T) {
	plugins := map[string]func(url string) AuthPlugin{
		"WithTwoFactorURL":             func(u string) AuthPlugin { return NewCorePlugin().WithTwoFactorURL(u) },
		"MagicLinkConfig.TwoFactorURL": func(u string) AuthPlugin { return NewMagicLinkPlugin(MagicLinkConfig{DevMode: true, TwoFactorURL: u}) },
		"PasskeyConfig.TwoFactorURL": func(u string) AuthPlugin {
			return NewPasskeyPlugin(PasskeyConfig{RPID: "example.com", Origins: []string{"https://example.com"}, TwoFactorURL: u})
		},
		"OAuth2Config.TwoFactorURL": func(u string) AuthPlugin { return NewOAuth2Plugin(OAuth2Config{TwoFactorURL: u}) },
	}
	for option, build := range plugins {
		for _, bad := range []string{"https://app.example.com/sign-in/code", "//evil.example/2fa", "login/2fa", "/2fa\\x"} {
			mgr := New(AuthConfig{JWTSecret: "x", SessionTTL: time.Hour, SessionCookie: "session_id",
				UserStore: newMemoryUserStore(), AllowInMemoryStores: true})
			mgr.Use(build(bad))
			err := mgr.Init(nil)
			if err == nil || !strings.Contains(err.Error(), option) || !strings.Contains(err.Error(), "same-origin path") {
				t.Errorf("%s = %q: Init = %v; want a refusal naming the option", option, bad, err)
			}
		}
		// A same-origin path, query included, still boots.
		mgr := New(AuthConfig{JWTSecret: "x", SessionTTL: time.Hour, SessionCookie: "session_id",
			UserStore: newMemoryUserStore(), AllowInMemoryStores: true})
		mgr.Use(build("/login/2fa?from=form"))
		if err := mgr.Init(nil); err != nil {
			t.Errorf("%s = a same-origin path: Init = %v; want nil", option, err)
		}
	}
}
