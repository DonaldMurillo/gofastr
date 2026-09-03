// Package ckalt holds compositekey positives in names and a package
// layout that never existed in this repo: the shape, not the site.
package ckalt

import "strings"

type session struct {
	user  string
	token string
}

type sessionRegistry struct {
	sessions map[string]*session
}

// Get joins inline at the map index with a NUL separator.
func (r *sessionRegistry) Get(user, token string) *session {
	return r.sessions[user+"\x00"+token] // want `this concatenation joins parts with the "\\x00" separator into a key`
}

// Put joins through a local.
func (r *sessionRegistry) Put(s *session) {
	sid := s.user + "\n" + s.token // want `sid joins parts with the "\\n" separator into a key`
	r.sessions[sid] = s
}

// shardKey is the one-return helper shape over a tab separator, keyed by
// a caller in another function.
func shardKey(tenant, shard string) string { return tenant + "\t" + shard } // want `shardKey joins parts with the "\\t" separator into a key`

func Claimed(tenant, shard string, claims map[string]bool) bool {
	return claims[shardKey(tenant, shard)]
}

// grantFor scans a cache with a joined prefix: the HasPrefix leg.
func grantFor(cache map[string]string, acct, name string) bool {
	for k := range cache {
		if strings.HasPrefix(k, acct+"\r"+name) { // want `this concatenation joins parts with the "\\r" separator into a key`
			return true
		}
	}
	return false
}

// Enroll uses the composite-literal key position with a third separator.
func Enroll(acct, region string) map[string]int {
	return map[string]int{acct + "\x1f" + region: 0} // want `this concatenation joins parts with the "\\x1f" separator into a key`
}

// ---- negatives --------------------------------------------------------

// PathKey is the "/" path join: a printable separator keys a readable
// domain, silent by design.
func PathKey(a, b string, m map[string]string) bool {
	_, ok := m[a+"/"+b]
	return ok
}

// DomainKey is the ":"/"/"/"|"/"." family generally: dotted ids, route
// keys, pipe shards — readable domain spaces, not invisible delimiters.
func DomainKey(user, host string, m map[string]string) bool {
	_, ok := m[user+":"+host]
	return ok
}

// Display never indexes on its join (and "~" is printable anyway).
func Display(a, b string) string { return a + "~" + b }

// StaticKey has no non-constant part besides the separator: nothing
// attacker-controlled to smuggle.
func StaticKey(m map[string]int) bool {
	_, ok := m["fixed"+"\x00"+"key"]
	return ok
}

// ---- indirections a real bug wears -----------------------------------

// loweredGet wraps the join in the standard key normalizer: the join
// still reaches the sink, one call deeper.
func loweredGet(r *sessionRegistry, user, token string) *session {
	return r.sessions[strings.ToLower(user+"\x00"+token)] // want `this concatenation joins parts with the "\\x00" separator into a key`
}

// holder parks the join on a struct field before keying.
type holder struct {
	k string // want `k joins parts with the "\\x00" separator into a key`
}

func fieldKey(r *sessionRegistry, h *holder, user, token string) {
	h.k = user + "\x00" + token
	r.sessions[h.k] = &session{user: user, token: token}
}

// guardedShard is the helper shape with a guard in front of the join:
// statements that only rebind parameters do not hide the join.
func guardedShard(tenant, shard string) string { // want `guardedShard joins parts with the "\\t" separator into a key`
	if tenant == "" {
		tenant = "anon"
	}
	return tenant + "\t" + shard
}

func claimedGuarded(claims map[string]bool, tenant, shard string) bool {
	return claims[guardedShard(tenant, shard)]
}
