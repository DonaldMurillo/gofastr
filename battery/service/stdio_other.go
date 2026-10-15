//go:build !windows

package service

// redirectDaemonOutput trims the log launchd appends to. launchd opens
// it from the plist's StandardOutPath before the daemon runs, and
// systemd sends output to the journal, so nothing is redirected here.
func redirectDaemonOutput(logPath string) error {
	return trimLog(logPath)
}
