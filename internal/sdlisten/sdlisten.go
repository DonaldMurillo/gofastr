// Package sdlisten takes a listening socket a service manager opened
// for this process: systemd's socket activation.
//
// It is what lets an app be restarted without its port ever closing.
// systemd holds the socket; while the app is down, between one release
// and the next or after a crash, connections wait in the socket's
// queue; the app that starts takes the socket and answers them. With
// the app opening its own port, the same restart is a second of
// refused connections.
package sdlisten

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
)

// Marker is a string every binary that links this package holds. A
// deploy looks for it before it hands the port to systemd: a binary
// without it would open the port itself and find it taken.
const Marker = "gofastr:systemd-socket-v1"

// firstFD is the first descriptor a service manager passes: the
// protocol's SD_LISTEN_FDS_START.
const firstFD = 3

// Inherited returns the socket passed to this process, or nil when
// there is none, which is the ordinary case. The protocol is two
// variables: LISTEN_PID names the process the sockets are for, and
// LISTEN_FDS says how many, starting at descriptor 3. A LISTEN_PID that
// is not this process means the variables were inherited from a parent
// they were meant for, and the descriptors are not ours to take.
//
// One socket is taken. The variables are cleared, so a child this
// process starts does not take them for its own.
func Inherited() (net.Listener, error) {
	return inherited(os.Getenv, os.Getpid(), func(fd int) *os.File { return os.NewFile(uintptr(fd), marker()) })
}

// marker keeps Marker in the binary: a constant nothing reads at run
// time is not kept by the linker.
func marker() string { return Marker }

func inherited(getenv func(string) string, pid int, open func(fd int) *os.File) (net.Listener, error) {
	if getenv("LISTEN_PID") != strconv.Itoa(pid) {
		return nil, nil
	}
	n, err := strconv.Atoi(getenv("LISTEN_FDS"))
	if err != nil || n < 1 {
		return nil, nil
	}
	_ = os.Unsetenv("LISTEN_PID")
	_ = os.Unsetenv("LISTEN_FDS")
	_ = os.Unsetenv("LISTEN_FDNAMES")
	if n > 1 {
		return nil, fmt.Errorf("sdlisten: %d sockets were passed and one is served: give the unit one ListenStream", n)
	}
	f := open(firstFD)
	if f == nil {
		return nil, errors.New("sdlisten: descriptor 3 is not open")
	}
	// FileListener works on a copy of the descriptor; ours is closed.
	defer f.Close()
	ln, err := net.FileListener(f)
	if err != nil {
		return nil, fmt.Errorf("sdlisten: descriptor 3 is not a listening socket: %w", err)
	}
	return ln, nil
}
