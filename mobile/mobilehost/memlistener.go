package mobilehost

import (
	"context"
	"net"
	"sync"
)

// memListener is the in-process transport: framework.App.Serve accepts on
// it, and the host's HTTP client dials it. Each dial is one net.Pipe, so a
// request crosses goroutines, never the kernel's network stack. Nothing
// outside this process can connect: there is no port to find, which is the
// property the desktop shell buys with a boot token and a Host pin.
type memListener struct {
	conns  chan net.Conn
	once   sync.Once
	closed chan struct{}
}

func newMemListener() *memListener {
	return &memListener{conns: make(chan net.Conn), closed: make(chan struct{})}
}

func (l *memListener) Accept() (net.Conn, error) {
	select {
	case c := <-l.conns:
		return c, nil
	case <-l.closed:
		return nil, net.ErrClosed
	}
}

func (l *memListener) Close() error {
	l.once.Do(func() { close(l.closed) })
	return nil
}

func (l *memListener) Addr() net.Addr { return memAddr{} }

// dial is the http.Transport's DialContext.
func (l *memListener) dial(ctx context.Context, _, _ string) (net.Conn, error) {
	client, server := net.Pipe()
	select {
	case l.conns <- server:
		return client, nil
	case <-l.closed:
		client.Close()
		server.Close()
		return nil, net.ErrClosed
	case <-ctx.Done():
		client.Close()
		server.Close()
		return nil, ctx.Err()
	}
}

// memAddr has no host:port on purpose: framework's local-run registry and
// dev-MCP loopback guard both read the bound address, and an address that
// does not split as host:port opts out of the first and is treated as
// not-loopback (the conservative answer) by the second.
type memAddr struct{}

func (memAddr) Network() string { return "memory" }
func (memAddr) String() string  { return "memory" }
