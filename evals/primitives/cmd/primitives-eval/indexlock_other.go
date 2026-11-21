//go:build !linux

package main

func lockAbandoned(string, string) bool { return false }

var gitRunning = func() bool { return true }
