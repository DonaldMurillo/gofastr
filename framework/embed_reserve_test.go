package framework

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/framework/embed"
)

// reservingOnly implements only the reservation seam, like a battery mounted
// alongside (rather than being) the UI host.
type reservingOnly struct{ prefixes []string }

func (m *reservingOnly) Mount(*router.Router)            {}
func (m *reservingOnly) ReservedEmbedPrefixes() []string { return m.prefixes }
func newReserving(p ...string) *reservingOnly            { return &reservingOnly{prefixes: p} }

// fwTestScreen is a minimal embed.Screen for framework-root tests, which build
// an embed.Host directly (no UIHost.Mount) to exercise key derivation and
// reserved-prefix wiring — not rendering.
type fwTestScreen struct{ path string }

func (s fwTestScreen) RoutePath() string { return s.path }

func hostWithSurface(t *testing.T, path string) *embed.Host {
	t.Helper()
	h, err := embed.New(embed.Config{
		Surfaces:  []embed.Surface{{Name: "reports", Screen: fwTestScreen{path}, Origins: []string{"https://acme.com"}}},
		BurnStore: embed.NewMemoryBurnStore(),
	})
	if err != nil {
		t.Fatalf("embed.New(%q): %v", path, err)
	}
	return h
}

// reservingPlugin / reservingBattery are the other two places a privileged
// prefix can come from. All three are scanned, because a battery is not a
// mountable and a plugin is neither.
type reservingPlugin struct{ prefixes []string }

func (p *reservingPlugin) Name() string                    { return "reserving-plugin" }
func (p *reservingPlugin) Init(*App) error                 { return nil }
func (p *reservingPlugin) ReservedEmbedPrefixes() []string { return p.prefixes }

type reservingBattery struct{ prefixes []string }

func (b *reservingBattery) Name() string                    { return "reserving-battery" }
func (b *reservingBattery) Init(*App) error                 { return nil }
func (b *reservingBattery) ReservedEmbedPrefixes() []string { return b.prefixes }

// A prefix reported by a PLUGIN is enforced, not just one from a mountable.
func TestInitPluginsReadsReservationsFromPlugins(t *testing.T) {
	host := hostWithSurface(t, "/back-office")
	app := NewApp(WithSecret("reserve-test-secret-reserve-test-"))
	app.Mount(&embedMountable{host: host})
	app.RegisterPlugin(&reservingPlugin{prefixes: []string{"/back-office"}})

	defer func() {
		if recover() == nil {
			t.Fatal("a plugin's reserved prefix was not enforced")
		}
	}()
	_ = app.InitPlugins()
}

// And one reported by a BATTERY.
func TestInitPluginsReadsReservationsFromBatteries(t *testing.T) {
	host := hostWithSurface(t, "/back-office")
	app := NewApp(WithSecret("reserve-test-secret-reserve-test-"))
	app.Mount(&embedMountable{host: host})
	app.Batteries.Register(&reservingBattery{prefixes: []string{"/back-office"}})

	defer func() {
		if recover() == nil {
			t.Fatal("a battery's reserved prefix was not enforced")
		}
	}()
	_ = app.InitPlugins()
}

// A battery that relocated its prefix takes its protection with it.
//
// The built-in reserved list can only name DEFAULTS, so an app that sets
// admin.Config.PathPrefix = "/back-office" kept the guard on "/admin" — which it
// no longer serves — and lost it on the prefix it does. Surfaces are declared
// before anything is mounted, so this has to be caught at InitPlugins or not at
// all.
func TestInitPluginsRefusesASurfaceOverAMountedBatteryPrefix(t *testing.T) {
	host := hostWithSurface(t, "/back-office")

	app := NewApp(WithSecret("reserve-test-secret-reserve-test-"))
	app.Mount(&embedMountable{host: host})
	app.Mount(newReserving("/back-office"))

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("InitPlugins accepted a surface sitting on a mounted battery's prefix")
		}
		msg, _ := r.(string)
		if !strings.Contains(msg, "/back-office") {
			t.Fatalf("panic %q does not name the offending prefix", msg)
		}
	}()
	_ = app.InitPlugins()
}

// An unrelated prefix must not break an app that was fine.
func TestInitPluginsAcceptsUnrelatedBatteryPrefixes(t *testing.T) {
	host := hostWithSurface(t, "/reports")

	app := NewApp(WithSecret("reserve-test-secret-reserve-test-"))
	app.Mount(&embedMountable{host: host})
	app.Mount(newReserving("/back-office", "/telemetry"))

	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}
	// And the newly reserved prefix is now enforced for later declarations.
	if err := host.AddReservedPrefixes("/telemetry"); err != nil {
		t.Fatalf("re-registering an already-reserved prefix should be a no-op: %v", err)
	}
}

// An app with no embed host must not pay for any of this, and must not panic
// when a battery reports prefixes nobody is checking against.
func TestInitPluginsIgnoresReservationsWithoutAnEmbedHost(t *testing.T) {
	app := NewApp(WithSecret("reserve-test-secret-reserve-test-"))
	app.Mount(newReserving("/back-office"))

	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}
}

// A mountable that does not implement the seam is simply skipped.
func TestInitPluginsSkipsMountablesWithoutTheSeam(t *testing.T) {
	host := hostWithSurface(t, "/reports")

	app := NewApp(WithSecret("reserve-test-secret-reserve-test-"))
	app.Mount(&embedMountable{host: host})
	app.Mount(&embedMountable{}) // no prefixes, nil host

	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}
}
