package rtc

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"maps"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

var iceSchemes = map[string]bool{
	"stun": true, "stuns": true, "turn": true, "turns": true,
}

func validICEURLs(urls []string) bool {
	for _, raw := range urls {
		u, err := url.Parse(raw)
		if err != nil || u.Scheme == "" {
			return false
		}
		scheme := strings.ToLower(u.Scheme)
		if !iceSchemes[scheme] {
			return false
		}
		if u.User != nil || u.Path != "" || strings.Contains(raw, "#") {
			return false
		}
		endpoint := u.Opaque
		if endpoint == "" {
			endpoint = u.Host
		}
		if endpoint == "" || strings.HasPrefix(endpoint, ":") {
			return false
		}
		if !validEndpointPort(endpoint) {
			return false
		}
		switch {
		case u.RawQuery == "":
		case scheme == "turn" || scheme == "turns":
			if u.RawQuery != "transport=udp" && u.RawQuery != "transport=tcp" {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func validEndpointPort(endpoint string) bool {
	if strings.Contains(endpoint, "@") {
		return false
	}
	host := endpoint
	if strings.HasPrefix(endpoint, "[") {
		closeIdx := strings.IndexByte(endpoint, ']')
		if closeIdx < 0 {
			return false
		}
		host = endpoint[:closeIdx+1]
		endpoint = endpoint[closeIdx+1:]
	} else if i := strings.LastIndexByte(endpoint, ':'); i >= 0 {
		host, endpoint = endpoint[:i], endpoint[i:]
	} else {
		endpoint = ""
	}
	if host == "" {
		return false
	}
	if strings.HasPrefix(host, "[") {
		a, err := netip.ParseAddr(host[1 : len(host)-1])
		if err != nil || !a.Is6() || a.Zone() != "" {
			return false
		}
	} else if !validHostname(host) {
		return false
	}
	if endpoint == "" {
		return true
	}
	if !strings.HasPrefix(endpoint, ":") {
		return false
	}
	port := endpoint[1:]
	if port == "" {
		return false
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return false
	}
	return true
}

func validHostname(host string) bool {
	for i := 0; i < len(host); i++ {
		c := host[i]
		switch {
		case 'a' <= c && c <= 'z', 'A' <= c && c <= 'Z', '0' <= c && c <= '9':
		case c == '.', c == '-', c == '_':
		default:
			return false
		}
	}
	return true
}

func turnTTL(ttl time.Duration) time.Duration {
	switch {
	case ttl == 0:
		return time.Hour
	case ttl < time.Minute:
		return time.Minute
	default:
		return ttl
	}
}

// Credentials returns the ICEServer entry for one peer, valid until
// now+TTL, in the coturn static-auth-secret convention (the "TURN REST
// API"): username is "<unix-expiry>:<peer id>", credential is
// base64(HMAC-SHA1(secret, username)).
func (t TURN) Credentials(peerID string, now time.Time) ICEServer {
	username := strconv.FormatInt(now.Add(turnTTL(t.TTL)).Unix(), 10) + ":" + peerID
	mac := hmac.New(sha1.New, []byte(t.Secret))
	mac.Write([]byte(username))
	return ICEServer{
		URLs:       append([]string(nil), t.URLs...),
		Username:   username,
		Credential: base64.StdEncoding.EncodeToString(mac.Sum(nil)),
	}
}

func (s *Signaler) closeRevokedPeers() {
	s.mu.Lock()
	var peers []*roomPeer
	for _, rm := range s.rooms {
		for _, p := range rm.peers {
			if p.req != nil {
				peers = append(peers, p)
			}
		}
	}
	s.mu.Unlock()
	for _, p := range peers {
		if !s.peerHeld(p.req, p.recheck) {
			s.logger.Warn("rtc: peer closed", "reason", "unauthorized")
			_ = p.conn.CloseWithStatus(4000+http.StatusUnauthorized, "session ended")
		}
	}
}

func (s *Signaler) turnRefreshLoop(done chan struct{}) {
	defer s.refreshWG.Done()
	for {
		s.mu.Lock()
		every := s.turnRefreshEvery
		if every <= 0 {
			every = turnTTL(s.cfg.TURN.TTL) / 2
		}
		s.mu.Unlock()
		select {
		case <-done:
			return
		case <-time.After(every):
		}
		s.closeRevokedPeers()
		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			return
		}
		for _, name := range slices.Sorted(maps.Keys(s.rooms)) {
			if rm := s.rooms[name]; len(rm.peers) > 0 {
				s.publishLocked(rm, evICEServers, roomEvent{kind: evICEServers})
			}
		}
		s.mu.Unlock()
	}
}
