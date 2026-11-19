//go:build !linux

package main

import "time"

var stopGrace = 5 * time.Second

func stopCellProcesses(string) {}

func lockAbandoned(string, string) bool { return false }
