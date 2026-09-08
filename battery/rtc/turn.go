package rtc

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"maps"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

// iceSchemes is the URL scheme set RTCConfiguration.iceServers accepts.
// Anything else (http, ws, …) is refused at New: a misconfigured entry
// would reach the browser as a broken RTCIceServer.
var iceSchemes = map[string]bool{
	"stun": true, "stuns": true, "turn": true, "turns": true,
}

// validICEURLs reports whether every URL is an ICE URI a browser will
// accept: an ICE scheme, a host (RFC 7064 and 7065 make it mandatory),
// a port that is present-and-legal when present (numeric, 1-65535,
// never empty — bracketed-IPv6 aware), no userinfo (RFC 7065 has no
// userinfo production; a long-lived user:secret in the URL is copied
// verbatim by iceServersFor into every peer snapshot), and for
// turn/turns at most a ?transport=udp|tcp query; stun URIs take no
// query. A scheme with no endpoint parsed fine and reached the browser
// as a SyntaxError.
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

// validEndpointPort checks the port grammar of one ICE endpoint
// ("host", "host:port", "[v6]", "[v6]:port"): when a port component is
// present it must be numeric and within 1-65535, and an empty port
// ("host:") or any userinfo ("@", RFC 7065 has no such production)
// refuses the URL. A portless endpoint is legal (the schemes' default
// port applies). IPv6 literals must be bracketed; the port splits at
// the colon after the closing bracket, never inside the address.
func validEndpointPort(endpoint string) bool {
	if strings.Contains(endpoint, "@") {
		return false
	}
	host := endpoint
	if strings.HasPrefix(endpoint, "[") {
		closeIdx := strings.IndexByte(endpoint, ']')
		if closeIdx < 0 {
			return false // unterminated IPv6 literal
		}
		host = endpoint[:closeIdx+1]
		endpoint = endpoint[closeIdx+1:]
	} else if i := strings.LastIndexByte(endpoint, ':'); i >= 0 {
		host, endpoint = endpoint[:i], endpoint[i:]
	}
	if host == "" {
		return false
	}
	if endpoint == "" {
		return true // no port component
	}
	if !strings.HasPrefix(endpoint, ":") {
		return false // a suffix that is not :port (e.g. a path)
	}
	port := endpoint[1:]
	if port == "" {
		return false // "host:" — empty port
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return false
	}
	return true
}

// turnTTL clamps the credential lifetime: zero means one hour, and one
// minute is the floor (a credential that expires before the peer can
// finish negotiating is worse than none).
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
// base64(HMAC-SHA1(secret, username)). The secret is shared with the
// TURN server and never leaves the Go process; each peer receives only
// its own credential, inside its own snapshot.
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

// turnRefreshLoop pushes the iceServers event to every populated room
// each half TTL (or the test knob), so a peer connection that outlives
// its credential holds a fresh one before an ICE restart needs it:
// existing TURN allocations survive expiry, new ones are refused
// (draft-uberti-behave-turn-rest-00, section 2). Exits when done
// closes; started by the first room with TURN configured.
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
