package service

import (
	"context"
	"runtime"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework"
)

func TestDaemonExitsWhenServerStops(t *testing.T) {
	t.Setenv("GOFASTR_ISOLATION", "off")
	t.Setenv("GOFASTR_ROLE", "")
	t.Setenv("GOFASTR_SETUP", "")
	originalTimeout := stopTimeout
	stopTimeout = 3 * time.Second
	t.Cleanup(func() { stopTimeout = originalTimeout })
	battery, _, _ := testBattery(t, runtime.GOOS, Config{})
	constructed := make(chan *framework.App, 1)
	done := make(chan error, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		done <- battery.Run(ctx, []string{"service", "run"}, func(rt Runtime) (*framework.App, error) {
			options, err := rt.AppOptions()
			if err != nil {
				return nil, err
			}
			application := framework.NewApp(options...)
			constructed <- application
			return application, nil
		})
	}()
	var application *framework.App
	select {
	case application = <-constructed:
	case err := <-done:
		t.Fatalf("daemon ended before building: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("daemon did not build")
	}
	t.Cleanup(func() {
		cancel()
		shutdown, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		_ = application.Shutdown(shutdown)
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("daemon goroutine did not exit")
		}
	})
	layout, err := battery.layoutFor(true)
	if err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(10 * time.Second); ; {
		if _, err := callControl(layout.socketPath(), "status"); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("daemon control channel did not become ready")
		}
		time.Sleep(10 * time.Millisecond)
	}
	shutdown, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	if err := application.Shutdown(shutdown); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		done <- err
		if err == nil {
			t.Fatal("a self-stopped server must produce a daemon failure")
		}
	case <-time.After(time.Second):
		t.Fatal("daemon waited for the stop budget after its server had already ended")
	}
}
