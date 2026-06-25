// Package log is the GoFastr server-log plugin.
//
// It registers a *slog.Logger whose JSON-line output is fanned out to one
// or more Sinks (file, default file, webhook, console). It also installs
// HTTP access-log middleware and a panic-recovery middleware that route
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
//
// By default (Config.Console == ConsoleAuto, the zero value) a
// colorized ConsoleSink is also attached to stderr when stderr is a
// terminal and NO_COLOR is unset — giving local devs a readable feed
// with no config, and staying out of prod where stderr is captured.
// Set Config.Console to ConsoleOn/ConsoleOff to force it.
package log
