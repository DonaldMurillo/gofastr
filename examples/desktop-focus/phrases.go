package main

import "github.com/DonaldMurillo/gofastr/core/i18n"

// The shell's own text: menu rows, the tray tooltip and the settings
// labels. The shell renders them in the OS language, so a catalog
// entry for a key translates the native menu with no code change.
var (
	menuFile           = i18n.Define("focus.menu.file", "File")
	menuView           = i18n.Define("focus.menu.view", "View")
	menuShow           = i18n.Define("focus.menu.show", "Show Focus")
	menuStartPause     = i18n.Define("focus.menu.start_pause", "Start / Pause")
	menuTimerWidget    = i18n.Define("focus.menu.timer_widget", "Timer widget")
	menuNewTask        = i18n.Define("focus.menu.new_task", "New task")
	menuHistory        = i18n.Define("focus.menu.history", "History")
	menuSettings       = i18n.Define("focus.menu.settings", "Settings…")
	menuCheckUpdates   = i18n.Define("focus.menu.check_updates", "Check for updates…")
	trayTooltip        = i18n.Define("focus.tray.tooltip", "Focus timer")
	prefWorkMinutes    = i18n.Define("focus.pref.work_minutes", "Work minutes")
	prefWorkMinutesTip = i18n.Define("focus.pref.work_minutes.help", "Length of one work session.")
	prefBreakMinutes   = i18n.Define("focus.pref.break_minutes", "Break minutes")
	prefNotifyOnDone   = i18n.Define("focus.pref.notify_on_done", "Notify when a session ends")
	prefTrayCountdown  = i18n.Define("focus.pref.tray_countdown", "Countdown in the menu bar")
	prefSound          = i18n.Define("focus.pref.sound", "Session sound")
)
