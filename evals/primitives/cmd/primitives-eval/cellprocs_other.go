//go:build !linux

package main

import "time"

var stopGrace = 5 * time.Second

func stopCellProcesses(string) {}

func fileHeldOpen(string) bool { return true }
