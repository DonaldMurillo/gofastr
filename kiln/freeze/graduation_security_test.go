package freeze_test

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/kiln/freeze"
	"github.com/DonaldMurillo/gofastr/kiln/world"
)

// Property: the graduation gate refuses a PWA whose start_url or scope
// resolves cross-origin.
//
// validateGraduation checks `strings.HasPrefix(value, "/")`, which a
// scheme-relative "//evil.example/pwa" or backslash form "/\evil.example"
// satisfies while still resolving to the attacker's origin. The world's
// PWA config is agent-authored (set_app_config over HTTP) and graduates
// into a committed gofastr.yml whose generator emits the manifest the
// operator installs; kiln/render's live preview applies the same values
// with no check at all (pinned separately there).
func TestFreezePWACrossOriginRefused(t *testing.T) {
	for name, tc := range map[string]struct {
		startURL, scope string
		bad             bool
	}{
		"scheme-relative start_url": {"//evil.example/pwa", "/", true},
		"absolute start_url":        {"https://evil.example/", "/", true},
		"backslash start_url":       {"/\\evil.example/", "/", true},
		"scheme-relative scope":     {"/app", "//evil.example/", true},
		"ordinary paths":            {"/app", "/app", false},
	} {
		w := world.New()
		w.App = world.AppConfig{Name: "pwa", Module: "example.com/pwa", DBDriver: "sqlite", DBURL: "pwa.db"}
		w.App.PWA = world.PWAConfig{
			Enabled: true, Display: "standalone",
			StartURL: tc.startURL, Scope: tc.scope,
		}
		_, err := freeze.BlueprintYAML(w)
		if tc.bad && err == nil {
			t.Errorf("SECURITY: %s: start_url=%q scope=%q graduated; the frozen app's manifest installs a PWA that launches on the attacker's origin", name, tc.startURL, tc.scope)
		}
		if !tc.bad && err != nil {
			t.Errorf("%s: ordinary PWA paths rejected: %v", name, err)
		}
	}
}

// Property: production auth cannot graduate without a JWT secret. The
// dev_mode escape hatch is documented; with it off and auth on, freeze
// must refuse rather than ship an app that signs tokens with an empty
// key.
func TestProdAuthWithoutSecretRefused(t *testing.T) {
	newWorld := func(devMode bool) *world.World {
		w := world.New()
		w.App = world.AppConfig{Name: "a", Module: "example.com/a", DBDriver: "sqlite", DBURL: "a.db"}
		w.App.Auth = world.AuthConfig{Enabled: true, DevMode: devMode}
		return w
	}
	if _, err := freeze.BlueprintYAML(newWorld(false)); err == nil {
		t.Error("SECURITY: production auth (dev_mode off) with an empty jwt_secret graduated")
	}
	if _, err := freeze.BlueprintYAML(newWorld(true)); err != nil {
		t.Errorf("dev_mode auth refused: %v", err)
	}
}

// Property: freeze must refuse a seed the live runtime itself refuses.
//
// Every add_seed runs through kiln/render.ApplySeeds
// (live.applySideEffects), which rejects an entity with no table (no
// such table) and validates the entity name and every row key as a SQL
// identifier — pinned in kiln/render/render_security_test.go
// (TestSeedIdentRejectsInjection). validateGraduation checks neither,
// so a world whose add_seed calls all failed live still graduates a
// gofastr.yml whose seed section targets a table that does not exist
// (or columns the framework refuses to emit): the operator watched the
// preview refuse the seed, and ships an app that fails applying it at
// first boot. The graduation gate exists exactly to keep the artifact
// from diverging from the preview (see the multi_tenant refusal above).
func TestFreezeRefusesSeedsLiveRefuses(t *testing.T) {
	newWorld := func(seed *world.Seed) *world.World {
		w := world.New()
		w.App = world.AppConfig{Name: "s", Module: "example.com/s", DBDriver: "sqlite", DBURL: "s.db"}
		w.Entities["posts"] = &world.Entity{Name: "posts", Fields: []world.Field{{Name: "title", Type: "string"}}}
		w.Seeds = []*world.Seed{seed}
		return w
	}
	for name, seed := range map[string]*world.Seed{
		"orphan entity": {
			Entity: "ghost", Rows: []map[string]any{{"title": "x"}},
		},
		"non-ident entity name": {
			Entity: `posts" ; DROP TABLE posts; --`, Rows: []map[string]any{{"title": "x"}},
		},
		"non-ident row key": {
			Entity: "posts", Rows: []map[string]any{{`title") VALUES('x'); --`: "x"}},
		},
	} {
		if _, err := freeze.BlueprintYAML(newWorld(seed)); err == nil {
			t.Errorf("SECURITY: %s graduated. kiln/render's ApplySeeds (the code path every live\n"+
				"add_seed runs through) refuses this seed, so the preview shows the refusal while the\n"+
				"frozen app ships it and fails at first boot. The graduation gate must refuse what the\n"+
				"live runtime refuses.", name)
		}
	}
	// Guard: an ordinary seed for a real entity still graduates.
	if _, err := freeze.BlueprintYAML(newWorld(&world.Seed{Entity: "posts", Rows: []map[string]any{{"title": "ok"}}})); err != nil {
		t.Errorf("ordinary seed refused at graduation: %v", err)
	}
}
