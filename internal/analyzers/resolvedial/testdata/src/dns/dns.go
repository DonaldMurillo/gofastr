// Package dns is a cross-package resolve-and-check helper: its fact
// travels to the caller in package a.
package dns

import (
	"errors"
	"net"
)

// CheckHost resolves host and refuses loopback answers.
func CheckHost(host string) error {
	ips, err := net.LookupIP(host)
	if err != nil {
		return err
	}
	for _, ip := range ips {
		if ip.IsLoopback() {
			return errors.New("internal")
		}
	}
	return nil
}

// Wrapped reaches the lookup one hop down.
func Wrapped(host string) error { return CheckHost(host) }

// Literal checks an IP literal without DNS.
func Literal(host string) error {
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return errors.New("internal")
	}
	return nil
}
