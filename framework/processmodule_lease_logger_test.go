package framework

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestLeaseRecoveryLoggerReentersSupervisor(t *testing.T) {
	const name = "example"
	store := &redLeaseFlakyStore{SQLProcessModuleStore: newTestStore(t)}
	entered := make(chan struct{})
	result := make(chan bool, 1)
	var sup *ProcessModuleSupervisor
	sup, err := NewProcessModuleSupervisor(SupervisorConfig{
		Store: store,
		Logf: func(format string, args ...any) {
			if strings.Contains(format, "lease recovered") {
				close(entered)
				info, err := sup.Info(name)
				result <- err == nil && !info.LeaseFailing
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	desc := ProcessModuleDescriptor{
		Name: name, Version: "1", ArtifactPath: "/unused",
		ArtifactSHA256: strings.Repeat("0", 64), TrustTier: TrustTrusted,
	}
	desc.SurfaceSHA256, err = ComputeSurfaceSHA256(desc)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sup.Register(context.Background(), desc, nil); err != nil {
		t.Fatal(err)
	}

	slot := sup.Slot(name)
	store.broken.Store(true)
	slot.reconcile()
	if !slot.snapshot().leaseFailing {
		t.Fatal("lease did not expire during the store outage")
	}
	store.broken.Store(false)
	go slot.reconcile()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("lease recovery did not call the logger")
	}
	select {
	case ok := <-result:
		if !ok {
			t.Fatal("logger did not observe the recovered lease")
		}
	case <-time.After(time.Second):
		t.Fatal("lease recovery logger blocked while querying supervisor state")
	}
}
