package desktoptest_test

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
	"github.com/DonaldMurillo/gofastr/battery/desktop/desktoptest"
	appui "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

type earlyExitShell struct{ desktop.Shell }

func (earlyExitShell) Run(context.Context, desktop.WindowConfig, func(desktop.Window)) error {
	return nil
}

func TestMain(tests *testing.M) {
	mode := os.Getenv("GOFASTR_TEST_NATIVE_BOOT")
	if mode == "" {
		os.Exit(tests.Run())
	}
	os.Exit(desktoptest.NativeMain(tests, func() (*framework.App, *desktop.Battery, error) {
		var shell desktop.Shell = earlyExitShell{desktoptest.NewShell()}
		if mode == "unsupported" {
			shell = desktop.NewUnsupportedShell()
		}
		app := framework.NewApp()
		app.Mount(uihost.New(appui.NewApp("Readiness")))
		battery := desktop.New(desktop.Config{ID: "test.native.readiness", Shell: shell})
		app.RegisterBattery(battery)
		return app, battery, nil
	}))
}

func TestNativeBootProbe(t *testing.T) {
	if os.Getenv("GOFASTR_TEST_NATIVE_BOOT") == "" {
		t.Skip("subprocess readiness probe")
	}
	desktoptest.Native(t)
}

func TestNativeBootRequiresReadiness(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"early", "unsupported"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, executable, "-test.run=^TestNativeBootProbe$")
			command.WaitDelay = time.Second
			command.Env = append(os.Environ(), "GOFASTR_TEST_NATIVE_BOOT="+mode,
				"GOFASTR_DESKTOP_DATA_DIR="+t.TempDir(), "GOFASTR_ISOLATION=off", "GOFASTR_NATIVE_TIMEOUT=5s")
			output, err := command.CombinedOutput()
			if mode == "early" && (err == nil || !strings.Contains(string(output), "before page readiness")) {
				t.Errorf("clean exit before readiness must fail explicitly: err=%v\n%s", err, output)
			}
			if mode == "unsupported" && err != nil {
				t.Errorf("unsupported shell must retain its skip: %v\n%s", err, output)
			}
		})
	}
}
