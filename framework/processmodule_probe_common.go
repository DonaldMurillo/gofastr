package framework

import (
	"fmt"
	"net"
	"os"
	"strings"
	"sync/atomic"
	"time"
)

// Portable probe bodies and helpers shared by the build-tagged per-OS
// probe files (processmodule_probe_unix.go /
// processmodule_probe_windows.go). P2 (no inherited secrets) and P4 (no
// network egress) have the same observable-outcome contract on every OS;
// only the P4 breach detail names the OS-specific enforcement mechanism,
// so that wording stays a parameter. This file replaces the former
// byte-duplicated P2/P4 bodies, pidStr, and splitCSV that each tagged
// file carried its own copy of.

// probeNoInheritedSecretBody runs the P2 probe: canary env + secret file
// both invisible under baseline hygiene. Prints exactly one result line
// through pass/breach; the returned exit code is always 0.
func probeNoInheritedSecretBody(pass, breach func(string)) int {
	if name := os.Getenv("GOFASTR_PROBE_CANARY_NAME"); name != "" {
		if got := os.Getenv(name); got != "" {
			breach(fmt.Sprintf("inherited host env %s=%q (baseline hygiene failed)", name, got))
			return 0
		}
	}
	if secret := os.Getenv("GOFASTR_PROBE_SECRET_FILE"); secret != "" {
		if data, err := os.ReadFile(secret); err == nil {
			breach(fmt.Sprintf("read host secret file %s: %q (fs confinement failed)", secret, string(data)))
			return 0
		}
	}
	pass("no canary env, no host secret file visible")
	return 0
}

// probeNoNetworkEgressBody runs the P4 probe: every dial target must be
// refused. breachDetail completes the breach message with the platform's
// own wording ("network egress permitted" on Unix, the WFP stub wording
// on Windows). Short per-dial timeout so a black-holed route does not
// hang the probe past its wall budget.
//
// A refused dial proves nothing on its own: the fixed targets may simply
// be closed or filtered on this host. The proof is the positive controls
// (GOFASTR_PROBE_NET_CONTROLS), listeners the probe runner opened and
// dialled itself before the spawn, so each is known to accept. Only a
// child that cannot reach a known-open listener has been denied egress;
// with no control at all the probe is unreachable, never a pass.
func probeNoNetworkEgressBody(pass, breach, unreachable func(string), breachDetail string) int {
	controls := splitCSV(os.Getenv("GOFASTR_PROBE_NET_CONTROLS"))
	if len(controls) == 0 {
		unreachable("no GOFASTR_PROBE_NET_CONTROLS: no reachable listener to prove a refusal against")
		return 0
	}
	targets := splitCSV(os.Getenv("GOFASTR_PROBE_NET_TARGETS"))
	for _, t := range append(append([]string{}, controls...), targets...) {
		d := net.Dialer{Timeout: 2 * time.Second}
		if c, err := d.Dial("tcp", t); err == nil {
			_ = c.Close()
			breach(fmt.Sprintf("dialed %s %s", t, breachDetail))
			return 0
		}
	}
	pass(fmt.Sprintf("all %d dial targets refused, %d of them listeners the host reached", len(controls)+len(targets), len(controls)))
	return 0
}

// probeNetControl is one positive-control listener for P4: open, proven
// reachable from the probe runner, and counting every connection it
// accepts after that proof.
type probeNetControl struct {
	ln       net.Listener
	accepted atomic.Int64
	done     chan struct{} // closed when the accept loop exits
}

// openProbeNetControls opens the P4 positive controls: a loopback
// listener, and one on the host's first non-loopback IPv4 address when
// the host has one (a backend that refuses only loopback still reaches
// it). Each is dialled once from here; a listener the runner itself
// cannot reach is closed and dropped, since a refusal against it proves
// nothing. Returns an error when no control survives.
func openProbeNetControls() ([]*probeNetControl, error) {
	hosts := []string{"127.0.0.1"}
	if ip := firstNonLoopbackIPv4(); ip != "" {
		hosts = append(hosts, ip)
	}
	var out []*probeNetControl
	var lastErr error
	for _, h := range hosts {
		c, err := openProbeNetControl(h)
		if err != nil {
			lastErr = err
			continue
		}
		out = append(out, c)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no reachable control listener: %w", lastErr)
	}
	return out, nil
}

func openProbeNetControl(host string) (*probeNetControl, error) {
	ln, err := net.Listen("tcp", net.JoinHostPort(host, "0"))
	if err != nil {
		return nil, err
	}
	// The proof dial: the runner reaches the listener before the child
	// tries, so a refusal in the child is the sandbox, not the host.
	conn, err := net.DialTimeout("tcp", ln.Addr().String(), 2*time.Second)
	if err != nil {
		_ = ln.Close()
		return nil, fmt.Errorf("control %s unreachable from the host: %w", ln.Addr(), err)
	}
	_ = conn.Close()
	proof, err := ln.Accept()
	if err != nil {
		_ = ln.Close()
		return nil, fmt.Errorf("control %s accept: %w", ln.Addr(), err)
	}
	_ = proof.Close()
	c := &probeNetControl{ln: ln, done: make(chan struct{})}
	go func() {
		defer close(c.done)
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			c.accepted.Add(1)
			_ = conn.Close()
		}
	}()
	return c, nil
}

// firstNonLoopbackIPv4 is the host's first up, non-loopback, non-link-local
// IPv4 interface address, or "" when it has none.
func firstNonLoopbackIPv4() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	for _, a := range addrs {
		ipn, ok := a.(*net.IPNet)
		if !ok {
			continue
		}
		ip := ipn.IP.To4()
		if ip == nil || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
			continue
		}
		return ip.String()
	}
	return ""
}

// closeProbeNetControls closes every control listener.
func closeProbeNetControls(controls []*probeNetControl) {
	for _, c := range controls {
		_ = c.ln.Close()
	}
}

// probeNetControlsVerdict is P4's host-side check: a control listener
// that accepted a connection after its proof dial was reached by the
// child, whatever the child reported.
func probeNetControlsVerdict(controls []*probeNetControl, res ProbeResult) ProbeResult {
	for _, c := range controls {
		// Drain the backlog first: a connection the kernel completed
		// for the child may not have reached Accept yet. The deadline
		// ends the accept loop once the queue is empty.
		if tl, ok := c.ln.(*net.TCPListener); ok {
			_ = tl.SetDeadline(time.Now().Add(100 * time.Millisecond))
		} else {
			_ = c.ln.Close()
		}
		<-c.done
		if n := c.accepted.Load(); n > 0 {
			return ProbeResult{ID: res.ID, Status: ProbeStatusFail, Reported: res.Reported,
				Detail: fmt.Sprintf("control listener %s accepted %d connection(s) from the child", c.ln.Addr(), n)}
		}
	}
	return res
}

// pidStr is the current pid, formatted, for unique scratch filenames.
func pidStr() string { return fmt.Sprintf("%d", os.Getpid()) }

// splitCSV splits a comma-separated env value, trimming whitespace and
// dropping empties.
func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
