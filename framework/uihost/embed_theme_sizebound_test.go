package uihost

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
	fembed "github.com/DonaldMurillo/gofastr/framework/embed"
)

// resolveEmbedTheme must bound the DECODED payload size, not only the encoded
// one.
//
// The encoded bound (maxEmbedThemeParam = 6 KiB) is checked first and is
// already tested. The decoded bound (4<<10 = 4 KiB) guards the json.Unmarshal
// and the map allocation that follows it on an unauthenticated route, and it is
// reachable on its own: 6 KiB of base64url decodes to ~4.5 KiB, so a payload
// under the URL bound can still be over the decoded bound. Removing it must
// therefore let an oversize but otherwise-valid theme register a variant.
func TestResolveEmbedThemeRejectsADecodedPayloadPastTheByteBound(t *testing.T) {
	ds := hostWithTheme(t, style.DefaultTheme())
	eh, err := fembed.New(fembed.Config{
		Surfaces: []fembed.Surface{{
			Name:    "reports",
			Screen:  embedTestScreen{"/reports"},
			Origins: []string{embedTestOrigin},
			Theme:   fembed.ThemeConfig{AllowTokens: []string{"color-primary"}},
		}},
		BurnStore: fembed.NewMemoryBurnStore(),
	})
	if err != nil {
		t.Fatalf("embed.New: %v", err)
	}
	surface, ok := eh.Lookup("reports")
	if !ok {
		t.Fatal("Lookup reports: not found")
	}

	// A token map that is otherwise valid: one ALLOWED token plus a disallowed
	// padding key the allowlist drops. Grown past 4096 decoded bytes; the
	// encoded form stays under the 6 KiB URL bound, so only the decoded check
	// can reject it. Without the decoded bound this registers a variant.
	obj := map[string]string{"color-primary": "#ff0000"}
	seed, _ := json.Marshal(obj)
	const target = 4200 // decoded bytes: >4096, and encodes to ~5.6 KiB (<6144)
	if pad := target - len(seed); pad > 0 {
		obj["padding"] = strings.Repeat("a", pad)
	}
	raw, _ := json.Marshal(obj)
	if len(raw) <= 4<<10 {
		t.Fatalf("test setup: decoded payload is only %d bytes — need >4096", len(raw))
	}
	encoded := base64.RawURLEncoding.EncodeToString(raw)
	if len(encoded) > maxEmbedThemeParam {
		t.Fatalf("test setup: encoded %d exceeds the %d URL bound — adjust target", len(encoded), maxEmbedThemeParam)
	}

	if key, ok := ds.resolveEmbedTheme(surface, encoded); ok {
		t.Fatalf("resolveEmbedTheme accepted a decoded payload past 4 KiB: returned variant key %q", key)
	}
}
