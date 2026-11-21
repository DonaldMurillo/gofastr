//go:build !windows

package service

func redirectDaemonOutput(logPath string) error {
	return trimLog(logPath)
}
