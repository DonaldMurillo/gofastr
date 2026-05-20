// Package log is the GoFastr server-log plugin.
//
// It registers a *slog.Logger whose JSON-line output is fanned out to one
// or more Sinks (file, default file, webhook). It also installs HTTP
// access-log middleware and a panic-recovery middleware that route
// through the same sinks, and logs app start/stop lifecycle events.
//
// Usage:
//
//	app := framework.NewApp(...)
//	app.RegisterPlugin(log.New(log.Config{
//	    Level: slog.LevelInfo,
//	    Sinks: []log.Sink{
//	        log.MustFileSink("/var/log/app.log", log.FileOpts{MaxSize: 100 << 20, MaxBackups: 5}),
//	        log.WebhookSink("https://hooks.example.com/x", log.WebhookOpts{}),
//	    },
//	}))
//
// If Sinks is nil/empty, a DefaultFileSink is used (resolved from the OS
// state dir + AppConfig.Name).
package log
