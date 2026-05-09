//go:build !unix

package main

import "syscall"

func childProcessGroup() *syscall.SysProcAttr { return nil }
