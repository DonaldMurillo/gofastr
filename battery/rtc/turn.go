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
// and for turn/turns at most a ?transport=udp|tcp query; stun URIs
// take no query. A scheme with no endpoint parsed fine and reached the
// browser as a SyntaxError.
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
