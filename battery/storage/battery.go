package storage

import (
	"github.com/DonaldMurillo/gofastr/framework"
)

// Battery is the framework.Battery adapter for the storage battery.  It
// participates in the App's dependency-resolved lifecycle so host apps can
// declare that other batteries depend on the file-storage backend being
// available.
//
// Construct via [NewBattery] and register:
//
//	st := storage.NewLocalStorage("/var/uploads")
//	app.Batteries.Register(storage.NewBattery(st))
//
// The battery's Init is a no-op — the backend is fully constructed before
// registration.  None of the built-in backends (LocalStorage, MemoryStorage,
// S3Storage) run background goroutines, so no OnStop hook is needed.  If a
// future backend does, implement io.Closer on that type.
type Battery struct {
	s Storage
}

// NewBattery wraps s in a framework lifecycle adapter.
func NewBattery(s Storage) *Battery {
	return &Battery{s: s}
}

// Storage returns the underlying Storage so callers can inject it where
// needed without reaching through the battery wrapper.
func (b *Battery) Storage() Storage { return b.s }

// Name implements framework.Battery.
func (b *Battery) Name() string { return "storage" }

// Init implements framework.Battery.  The backend is fully constructed by
// the caller before registration so Init is a no-op.
func (b *Battery) Init(_ *framework.App) error { return nil }

// Compile-time interface check.
var _ framework.Battery = (*Battery)(nil)
