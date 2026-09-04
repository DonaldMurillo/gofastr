// Package lifecycleish mirrors framework's boot/shutdown path (App.Start
// → App.Shutdown → BatteryManager.StopAll), reduced to the shape that
// stays OUT of this rule: the battery hooks are reached by narrowing a
// registry value with a TYPE ASSERTION (battery.go:253/268,
// `.(BatteryLifecycle)`), and the coordinator isolates lifecycle panics
// by a direct fix, not by this rule. The signal loop makes the whole
// chain hot, so the assertion posture is the only thing keeping the
// hook call quiet.
package lifecycleish

import "context"

type Battery interface{ Name() string }

type Lifecycle interface {
	OnStart(ctx context.Context) error
	OnStop(ctx context.Context) error
}

type Manager struct {
	entries map[string]Battery
}

// serveSignals mirrors the signal-driven drain: a wait loop whose
// shutdown call makes everything below it hot.
func serveSignals(sig <-chan struct{}, m *Manager) {
	for {
		select {
		case <-sig:
			_ = shutdown(context.Background(), m)
			return
		}
	}
}

func shutdown(ctx context.Context, m *Manager) error {
	return m.stopAll(ctx)
}

// stopAll mirrors BatteryManager.StopAll: the hook runs through an
// assertion-narrowed local on a hot path. Quiet by posture.
func (m *Manager) stopAll(ctx context.Context) error {
	for _, name := range []string{"a"} {
		if lc, ok := m.entries[name].(Lifecycle); ok {
			_ = lc.OnStop(ctx) // quiet: assertion-narrowed receiver, the coordinator's direct fix
		}
	}
	return nil
}

// startAll mirrors BatteryManager.StartAll, the same assertion shape on
// the boot side. Quiet by posture.
func (m *Manager) startAll(ctx context.Context) error {
	for _, name := range []string{"a"} {
		if lc, ok := m.entries[name].(Lifecycle); ok {
			_ = lc.OnStart(ctx) // quiet: assertion-narrowed receiver
		}
	}
	return nil
}
