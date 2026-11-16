//go:build !linux && !darwin && !windows

package framework

func defaultSandboxBackend() SandboxBackend { return nil }
