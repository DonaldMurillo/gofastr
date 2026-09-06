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

// validICEURLs reports whether every URL uses an ICE scheme and parses
// as an absolute URL.
func validICEURLs(urls []string) bool {
	for _, raw := range urls {
		u, err := url.Parse(raw)
		if err != nil || u.Scheme == "" || !iceSchemes[strings.ToLower(u.Scheme)] {
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
