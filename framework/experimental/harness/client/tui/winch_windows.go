//go:build windows

package tui

import "os"

func notifyWindowResize(_ chan<- os.Signal) {}
