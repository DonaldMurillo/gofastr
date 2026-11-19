package netguard

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"reflect"
	"slices"
	"syscall"
	"time"
	"unsafe"
)

// GuardedTransport returns a proxyless transport whose dialer refuses an
// internal address at connect time, or with allowPrivate a plain clone.
func GuardedTransport(allowPrivate bool, prefix string) *http.Transport {
	dialer := &net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: 30 * time.Second,
	}
	if !allowPrivate {
		dialer.Control = dialCheck(prefix)
	}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.DialContext = dialer.DialContext
	if !allowPrivate {
		tr.Proxy = nil
	}
	return tr
}

func dialCheck(prefix string) func(network, address string, _ syscall.RawConn) error {
	return func(network, address string, _ syscall.RawConn) error {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			host = address
		}
		ip := net.ParseIP(host)
		if ip == nil {
			return fmt.Errorf("%s: dial address %q is not a resolved IP", prefix, address)
		}
		return refuse(prefix, ip)
	}
}

// GuardPlainTransport returns a guarded, proxyless clone of tr when tr
// uses only the stock proxy and dialer, and false otherwise.
func GuardPlainTransport(tr *http.Transport, prefix string) (*http.Transport, bool) {
	return withDialer(tr, GuardedTransport(false, prefix).DialContext)
}

func stockProxy(p func(*http.Request) (*url.URL, error)) bool {
	return p == nil || reflect.ValueOf(p).Pointer() == reflect.ValueOf(http.ProxyFromEnvironment).Pointer()
}

func stockDial(d func(ctx context.Context, network, addr string) (net.Conn, error)) bool {
	return d == nil || defaultDial != nil && funcIdentity(d) == funcIdentity(defaultDial)
}

var defaultDial = func() func(context.Context, string, string) (net.Conn, error) {
	if tr, ok := http.DefaultTransport.(*http.Transport); ok {
		return tr.DialContext
	}
	return nil
}()

func funcIdentity(f func(context.Context, string, string) (net.Conn, error)) unsafe.Pointer {
	return *(*unsafe.Pointer)(unsafe.Pointer(&f))
}

func withDialer(tr *http.Transport, dial func(ctx context.Context, network, addr string) (net.Conn, error)) (*http.Transport, bool) {
	if !stockProxy(tr.Proxy) || !stockDial(tr.DialContext) || tr.DialTLSContext != nil ||
		tr.Dial != nil || tr.DialTLS != nil {
		return nil, false
	}
	guarded := tr.Clone()
	guarded.DialContext = dial
	guarded.Proxy = nil
	if guarded.TLSClientConfig != nil && slices.Contains(guarded.TLSClientConfig.NextProtos, "h2") {
		guarded.ForceAttemptHTTP2 = true
	}
	return guarded, true
}
