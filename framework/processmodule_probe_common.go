package framework

import (
	"fmt"
	"net"
	"os"
	"strings"
	"sync/atomic"
	"time"
)

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

type probeNetControl struct {
	ln       net.Listener
	accepted atomic.Int64
	done     chan struct{}
}

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

func closeProbeNetControls(controls []*probeNetControl) {
	for _, c := range controls {
		_ = c.ln.Close()
	}
}

func probeNetControlsVerdict(controls []*probeNetControl, res ProbeResult) ProbeResult {
	for _, c := range controls {
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

func pidStr() string { return fmt.Sprintf("%d", os.Getpid()) }

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
