package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/service"
	"github.com/DonaldMurillo/gofastr/framework"
)

// The App is exercised the way the daemon builds it, minus the
// listener: buildApp with a Runtime whose state dir is a temp dir, and
// TestHarness through the real router. The loopback gate and the
// sign-in flow are battery/service's own tests.

type localUser struct{ id string }

func (u localUser) GetID() string { return u.id }

func newTestApp(t *testing.T) *framework.App {
	t.Helper()
	app, err := buildApp(service.Runtime{DataDir: t.TempDir(), Secret: "service-hello-test-secret-0123456789abcdef", Dev: true}) // not-a-secret: test fixture
	if err != nil {
		t.Fatal(err)
	}
	// The harness never Starts, so create the tables Start would.
	if err := framework.AutoMigrate(app.DB, app.Registry); err != nil {
		t.Fatal(err)
	}
	return app
}

func TestPulseCreatedAndListed(t *testing.T) {
	app := newTestApp(t)
	ta := framework.TestHarness(t, app).AsUser(localUser{id: "local"})

	created := ta.Post("/api/pulses", map[string]any{"note": "written by hand"}).AssertStatus(t, http.StatusCreated)
	var envelope struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(created.Body()), &envelope); err != nil {
		t.Fatal(err)
	}
	id, _ := envelope.Data["id"].(string)
	if id == "" {
		t.Fatalf("create returned no id: %s", created.Body())
	}

	for _, path := range []string{"/", "/pulses"} {
		list := ta.Get(path).AssertStatus(t, http.StatusOK)
		if !strings.Contains(list.Body(), "written by hand") || !strings.Contains(list.Body(), `href="/pulses/`+id+`"`) {
			t.Fatalf("%s does not list the pulse with a link under /pulses", path)
		}
	}
	record := ta.Get("/pulses/"+id).AssertStatus(t, http.StatusOK)
	if !strings.Contains(record.Body(), "written by hand") {
		t.Fatal("the record page does not show the pulse")
	}
	create := ta.Get("/pulses/create").AssertStatus(t, http.StatusOK)
	if !strings.Contains(create.Body(), `name="note"`) {
		t.Fatal("the create screen has no note field")
	}
	if strings.Contains(ta.Get("/").Body(), "<style") {
		t.Fatal("bespoke inline CSS on the list screen")
	}
}

func TestPulsesRecordedInBackground(t *testing.T) {
	app := newTestApp(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		recordPulses(ctx, app.MustCrudHandler("pulses"), true)
		close(done)
	}()
	ta := framework.TestHarness(t, app).AsUser(localUser{id: "local"})
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(ta.Get("/api/pulses").Body(), "dev run started") {
		if time.Now().After(deadline) {
			t.Fatal("no start pulse recorded")
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("recordPulses did not stop when its context ended")
	}
}
